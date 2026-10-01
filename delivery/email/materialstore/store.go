// Package materialstore encrypts short-lived email delivery material in the
// same PostgreSQL transaction as its identity challenge and outbox intent.
package materialstore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var ErrConfiguration = errors.New("invalid protected material configuration")
var ErrUnavailable = errors.New("protected delivery material unavailable")
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Config struct {
	DevelopmentLoopback                          bool
	DB                                           *storage.DB
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ActiveKeyID                                  string
	// Keys come from explicit owner secret resolution. Keep prior keys during
	// rotation until all retained material has expired; never persist key bytes.
	Keys              map[string][]byte
	ApplicationOrigin string
}
type Store struct {
	db                                     *storage.DB
	installation, application, environment uuid.UUID
	active                                 string
	keys                                   map[string]cipher.AEAD
	origin                                 string
}

func New(cfg Config) (*Store, error) {
	if cfg.DB == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) || !keyPattern.MatchString(cfg.ActiveKeyID) || len(cfg.Keys) == 0 || len(cfg.Keys) > 8 {
		return nil, ErrConfiguration
	}
	origin, e := email.ParseApplicationOrigin(cfg.ApplicationOrigin, cfg.DevelopmentLoopback)
	if e != nil {
		return nil, ErrConfiguration
	}
	s := &Store{db: cfg.DB, installation: cfg.InstallationID, application: cfg.ApplicationID, environment: cfg.EnvironmentID, active: cfg.ActiveKeyID, keys: make(map[string]cipher.AEAD), origin: origin.String()}
	for id, key := range cfg.Keys {
		if !keyPattern.MatchString(id) || len(key) != 32 {
			return nil, ErrConfiguration
		}
		block, e := aes.NewCipher(key)
		if e != nil {
			return nil, ErrConfiguration
		}
		aead, e := cipher.NewGCM(block)
		if e != nil {
			return nil, ErrConfiguration
		}
		s.keys[id] = aead
	}
	if s.keys[s.active] == nil {
		return nil, ErrConfiguration
	}
	return s, nil
}
func validID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
func parseRef(ref email.SecretReference) (uuid.UUID, error) {
	raw := strings.TrimPrefix(string(ref), "material:")
	if raw == string(ref) {
		return uuid.Nil, ErrUnavailable
	}
	id, e := uuid.Parse(raw)
	if e != nil || !validID(id) || id.String() != raw {
		return uuid.Nil, ErrUnavailable
	}
	return id, nil
}
func (s *Store) aad(id uuid.UUID, key string, expiry time.Time) []byte {
	return []byte(fmt.Sprintf("amos-email-material-v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d", s.installation, s.application, s.environment, id, key, expiry.UnixMicro()))
}
func (s *Store) validMaterial(m email.PrivateMaterial) bool {
	if len(m.Recipient) > email.MaxRecipientBytes || len(m.ActionURL) > email.MaxActionURLBytes || strings.ContainsAny(m.Recipient+m.ActionURL, "\x00\r\n") {
		return false
	}
	address, e := mail.ParseAddress(m.Recipient)
	if e != nil || address.Address != m.Recipient || address.Name != "" {
		return false
	}
	u, e := url.Parse(m.ActionURL)
	return e == nil && u.Scheme+"://"+u.Host == s.origin && u.User == nil && u.Fragment == "" && (u.Path == "/verify-email" || u.Path == "/reset-password" || u.Path == "/magic-link") && u.RawPath == ""
}

// PutVerificationMaterial never writes plaintext. Failure aborts the caller's
// transaction; generic errors omit recipients, URLs, tokens and database text.
func (s *Store) PutVerificationMaterial(ctx context.Context, tx *sql.Tx, ref email.SecretReference, m email.PrivateMaterial, expiry time.Time) error {
	return s.putMaterial(ctx, tx, ref, m, expiry, "/verify-email")
}

// PutSignInMaterial binds encrypted material to the magic-link action only.
func (s *Store) PutSignInMaterial(ctx context.Context, tx *sql.Tx, ref email.SecretReference, m email.PrivateMaterial, expiry time.Time) error {
	return s.putMaterial(ctx, tx, ref, m, expiry, "/magic-link")
}

// PutPasswordResetMaterial binds encrypted material to the reset action only.
func (s *Store) PutPasswordResetMaterial(ctx context.Context, tx *sql.Tx, ref email.SecretReference, m email.PrivateMaterial, expiry time.Time) error {
	return s.putMaterial(ctx, tx, ref, m, expiry, "/reset-password")
}
func (s *Store) putMaterial(ctx context.Context, tx *sql.Tx, ref email.SecretReference, m email.PrivateMaterial, expiry time.Time, path string) error {
	if s == nil || ctx == nil || tx == nil || !s.validMaterial(m) {
		return ErrUnavailable
	}
	action, parseErr := url.Parse(m.ActionURL)
	if parseErr != nil || action.Path != path {
		return ErrUnavailable
	}
	id, e := parseRef(ref)
	if e != nil {
		return ErrUnavailable
	}
	var now time.Time
	if e = tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&now); e != nil {
		return ErrUnavailable
	}
	expiry = expiry.UTC().Truncate(time.Microsecond)
	if !expiry.After(now) || expiry.After(now.Add(24*time.Hour)) {
		return ErrUnavailable
	}
	plain, e := json.Marshal(m)
	if e != nil || len(plain) > 4080 {
		return ErrUnavailable
	}
	defer wipe(plain)
	aead := s.keys[s.active]
	nonce := make([]byte, aead.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return ErrUnavailable
	}
	encrypted := aead.Seal(nil, nonce, plain, s.aad(id, s.active, expiry))
	_, e = tx.ExecContext(ctx, `INSERT INTO email_delivery_material(installation_id,application_id,environment_id,material_id,key_id,nonce,ciphertext,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, s.installation, s.application, s.environment, id, s.active, nonce, encrypted, expiry)
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ResolveDelivery returns only current material in this deployment scope. An
// absent key, expiry, ciphertext/metadata tampering or outage fails closed.
func (s *Store) ResolveDelivery(ctx context.Context, ref email.SecretReference) (email.PrivateMaterial, error) {
	if s == nil || ctx == nil {
		return email.PrivateMaterial{}, ErrUnavailable
	}
	id, e := parseRef(ref)
	if e != nil {
		return email.PrivateMaterial{}, ErrUnavailable
	}
	var key string
	var nonce, encrypted []byte
	var expiry time.Time
	e = s.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT key_id,nonce,ciphertext,expires_at FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4 AND expires_at>transaction_timestamp()`, s.installation, s.application, s.environment, id).Scan(&key, &nonce, &encrypted, &expiry)
	})
	if e != nil {
		return email.PrivateMaterial{}, ErrUnavailable
	}
	aead := s.keys[key]
	if aead == nil || len(nonce) != aead.NonceSize() || len(encrypted) > 4096 {
		return email.PrivateMaterial{}, ErrUnavailable
	}
	plain, e := aead.Open(nil, nonce, encrypted, s.aad(id, key, expiry))
	if e != nil {
		return email.PrivateMaterial{}, ErrUnavailable
	}
	defer wipe(plain)
	var m email.PrivateMaterial
	if json.Unmarshal(plain, &m) != nil || !s.validMaterial(m) {
		return email.PrivateMaterial{}, ErrUnavailable
	}
	return m, nil
}
func (s *Store) PruneExpired(ctx context.Context, limit int) (int64, error) {
	if s == nil || limit < 1 || limit > 1000 {
		return 0, ErrConfiguration
	}
	var count int64
	e := s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		result, e := tx.ExecContext(ctx, `WITH expired AS (SELECT ctid FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND expires_at<=transaction_timestamp() ORDER BY expires_at LIMIT $4 FOR UPDATE SKIP LOCKED) DELETE FROM email_delivery_material material USING expired WHERE material.ctid=expired.ctid`, s.installation, s.application, s.environment, limit)
		if e != nil {
			return e
		}
		count, e = result.RowsAffected()
		return e
	})
	if e != nil {
		return 0, ErrUnavailable
	}
	return count, nil
}

// ResolveForTemplate prevents a protected reference from being repurposed under
// another authentication message template. Purpose is authenticated in the URL.
func (s *Store) ResolveForTemplate(ctx context.Context, ref email.SecretReference, template email.TemplateID) (email.PrivateMaterial, error) {
	var path string
	switch template {
	case email.TemplateVerifyEmail:
		path = "/verify-email"
	case email.TemplatePasswordReset:
		path = "/reset-password"
	case email.TemplateSignIn:
		path = "/magic-link"
	default:
		return email.PrivateMaterial{}, ErrUnavailable
	}
	m, e := s.ResolveDelivery(ctx, ref)
	if e != nil {
		return email.PrivateMaterial{}, e
	}
	u, e := url.Parse(m.ActionURL)
	if e != nil || u.Path != path {
		return email.PrivateMaterial{}, ErrUnavailable
	}
	return m, nil
}
