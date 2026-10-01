// Package protection enforces bounded authentication admission at HTTP boundaries.
package protection

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var ErrConfiguration = errors.New("invalid authentication protection configuration")
var ErrUnavailable = errors.New("authentication protection unavailable")

type Operation string

const (
	Signup         Operation = "signup"
	Signin         Operation = "signin"
	Verification   Operation = "verification"
	Recovery       Operation = "recovery"
	PasswordChange Operation = "password_change"
)

func validOperation(op Operation) bool {
	switch op {
	case Signup, Signin, Verification, Recovery, PasswordChange:
		return true
	}
	return false
}

type Config struct {
	DB                                           *storage.DB
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	// Key is stable across replicas and obtained through owner-controlled secret resolution.
	Key                   []byte
	Window                time.Duration
	IPLimit, AccountLimit int
}
type Limiter struct{ cfg Config }
type Admission struct {
	Allowed    bool
	RetryAfter time.Duration
}

func New(cfg Config) (*Limiter, error) {
	if cfg.DB == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) || len(cfg.Key) < 32 || len(cfg.Key) > 128 || cfg.Window < time.Second || cfg.Window > time.Hour || cfg.Window%time.Second != 0 || cfg.IPLimit < 1 || cfg.IPLimit > 10000 || cfg.AccountLimit < 1 || cfg.AccountLimit > 10000 {
		return nil, ErrConfiguration
	}
	cfg.Key = append([]byte(nil), cfg.Key...)
	return &Limiter{cfg: cfg}, nil
}
func validID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }

// PeerIP uses the socket peer exclusively. Forwarding headers are untrusted;
// proxy qualification must introduce its own separately reviewed binding.
func PeerIP(r *http.Request) (string, error) {
	if r == nil {
		return "", ErrConfiguration
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", ErrConfiguration
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", ErrConfiguration
	}
	return ip.String(), nil
}
func (l *Limiter) digest(dimension, key string) []byte {
	h := hmac.New(sha256.New, l.cfg.Key)
	h.Write([]byte(dimension))
	h.Write([]byte{0})
	h.Write([]byte(key))
	return h.Sum(nil)
}

// Allow atomically admits an IP/account pair using database time. A denied IP
// never creates attacker-selected account counters. Denial is independent of
// whether an account exists. Expired rows are reused; operators may prune them.
func (l *Limiter) Allow(ctx context.Context, op Operation, ip, account string) (Admission, error) {
	if l == nil || !validOperation(op) || net.ParseIP(ip) == nil || len(account) > 320 || strings.ContainsAny(account, "\x00\r\n") {
		return Admission{}, ErrConfiguration
	}
	ip = net.ParseIP(ip).String()
	account = strings.ToLower(strings.TrimSpace(account))
	if account == "" {
		account = "invalid-input"
	}
	result := Admission{Allowed: true}
	err := l.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		for _, dim := range []struct {
			name, key string
			limit     int
		}{{"ip", ip, l.cfg.IPLimit}, {"account", account, l.cfg.AccountLimit}} {
			var count int
			var remaining float64
			err := tx.QueryRowContext(ctx, `INSERT INTO identity_auth_limits (installation_id,application_id,environment_id,operation,dimension,key_digest,window_start,window_end,attempts)
VALUES ($1,$2,$3,$4,$5,$6,transaction_timestamp(),transaction_timestamp()+$7*interval '1 second',1)
ON CONFLICT (installation_id,application_id,environment_id,operation,dimension,key_digest) DO UPDATE SET
window_start=CASE WHEN identity_auth_limits.window_end<=transaction_timestamp() THEN transaction_timestamp() ELSE identity_auth_limits.window_start END,
window_end=CASE WHEN identity_auth_limits.window_end<=transaction_timestamp() THEN transaction_timestamp()+$7*interval '1 second' ELSE identity_auth_limits.window_end END,
attempts=CASE WHEN identity_auth_limits.window_end<=transaction_timestamp() THEN 1 ELSE LEAST(identity_auth_limits.attempts+1,$8+1) END
RETURNING attempts,EXTRACT(EPOCH FROM window_end-transaction_timestamp())`, l.cfg.InstallationID, l.cfg.ApplicationID, l.cfg.EnvironmentID, string(op), dim.name, l.digest(dim.name, dim.key), int(l.cfg.Window/time.Second), dim.limit).Scan(&count, &remaining)
			if err != nil {
				return err
			}
			if count > dim.limit {
				result.Allowed = false
				result.RetryAfter = time.Duration(remaining * float64(time.Second))
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return Admission{}, ErrUnavailable
	}
	return result, nil
}

// PruneExpired removes at most limit expired rows in this exact deployment
// scope. Locked admission rows are skipped. Schedule bounded batches in the
// application lifecycle; pruning never deletes a live budget window.
func (l *Limiter) PruneExpired(ctx context.Context, limit int) (int64, error) {
	if l == nil || limit < 1 || limit > 1000 {
		return 0, ErrConfiguration
	}
	var removed int64
	err := l.cfg.DB.WithTx(ctx, nil, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `WITH expired AS (
SELECT ctid FROM identity_auth_limits
WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3
AND window_end<=transaction_timestamp()
ORDER BY window_end LIMIT $4 FOR UPDATE SKIP LOCKED
) DELETE FROM identity_auth_limits counters USING expired WHERE counters.ctid=expired.ctid`, l.cfg.InstallationID, l.cfg.ApplicationID, l.cfg.EnvironmentID, limit)
		if err != nil {
			return err
		}
		removed, err = result.RowsAffected()
		return err
	})
	if err != nil {
		return 0, ErrUnavailable
	}
	return removed, nil
}
