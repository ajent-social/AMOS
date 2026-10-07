package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// TxRunner is the transactional capability shared by storage handles.
type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

// RuntimeConfig contains the complete, finite configuration for a runtime pool.
type RuntimeConfig struct {
	Host            string
	Port            uint16
	Database        string
	User            string
	Password        string
	RootCAPEM       []byte
	StartupTimeout  time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// RuntimeDB is a runtime-only handle backed by one private pool.
type RuntimeDB struct {
	pool     *sql.DB
	mu       sync.Mutex
	closed   bool
	closeErr error
}

var _ TxRunner = (*RuntimeDB)(nil)

var pgEnvironment = [...]string{
	"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE", "PGAPPNAME", "PGCONNECT_TIMEOUT", "PGSSLMODE", "PGSSLKEY", "PGSSLCERT", "PGSSLSNI", "PGSSLROOTCERT", "PGSSLPASSWORD", "PGSSLNEGOTIATION", "PGTARGETSESSIONATTRS", "PGSERVICE", "PGSERVICEFILE", "PGTZ", "PGOPTIONS", "PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION", "PGCHANNELBINDING", "PGREQUIREAUTH",
}

var runtimeOpenMu sync.Mutex

// OpenRuntime validates all supplied configuration before constructing a pool.
func OpenRuntime(ctx context.Context, input RuntimeConfig) (*RuntimeDB, error) {
	if ctx == nil {
		return nil, ErrInvalidConfig
	}
	runtimeOpenMu.Lock()
	defer runtimeOpenMu.Unlock()
	for _, name := range pgEnvironment {
		if value, ok := lookupEnv(name); ok && value != "" {
			return nil, ErrInvalidConfig
		}
	}
	if err := validateRuntimeConfig(input); err != nil {
		return nil, err
	}
	roots, err := runtimeRoots(input.RootCAPEM)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	config, err := runtimeConnConfig(input, roots)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	pool := stdlib.OpenDB(*config)
	pool.SetMaxOpenConns(input.MaxOpenConns)
	pool.SetMaxIdleConns(input.MaxIdleConns)
	pool.SetConnMaxLifetime(input.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(input.ConnMaxIdleTime)
	startupCtx, cancel := context.WithTimeout(ctx, input.StartupTimeout)
	defer cancel()
	if err := pool.PingContext(startupCtx); err != nil {
		_ = pool.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	return &RuntimeDB{pool: pool}, nil
}

// lookupEnv is isolated for deterministic poison tests without mutating startup's process environment.
var lookupEnv = os.LookupEnv

func runtimeConnConfig(input RuntimeConfig, roots *x509.CertPool) (*pgx.ConnConfig, error) {
	tlsConfig := &tls.Config{ // #nosec G402 -- verification remains enabled.
		RootCAs: roots, ServerName: input.Host, MinVersion: tls.VersionTLS12,
	}
	config, err := pgx.ParseConfigWithOptions(runtimeConnString(input), pgx.ParseConfigOptions{
		ConnStringAllowedKeys: []string{"host", "port", "dbname", "user", "password", "sslmode", "sslrootcert"},
	})
	if err != nil {
		return nil, err
	}
	config.TLSConfig = tlsConfig
	config.Fallbacks = nil
	config.RuntimeParams = nil
	if config.Host != input.Host || config.Port != uint16(input.Port) || config.Database != input.Database || config.User != input.User || config.Password != input.Password || config.TLSConfig != tlsConfig || config.Fallbacks != nil || len(config.RuntimeParams) != 0 {
		return nil, ErrInvalidConfig
	}
	return config, nil
}

func runtimeRoots(input []byte) (*x509.CertPool, error) {
	copyOfInput := append([]byte(nil), input...)
	roots := x509.NewCertPool()
	if err := appendStrictCerts(roots, copyOfInput); err != nil {
		return nil, err
	}
	return roots, nil
}

func appendStrictCerts(roots *x509.CertPool, data []byte) error {
	count := 0
	for len(data) > 0 {
		data = []byte(strings.TrimLeft(string(data), " \t\r\n"))
		if len(data) == 0 {
			break
		}
		if !strings.HasPrefix(string(data), "-----BEGIN CERTIFICATE-----") {
			return ErrInvalidConfig
		}
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return ErrInvalidConfig
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return ErrInvalidConfig
		}
		if !roots.AppendCertsFromPEM(pem.EncodeToMemory(block)) {
			return ErrInvalidConfig
		}
		count++
		data = rest
	}
	if count == 0 {
		return ErrInvalidConfig
	}
	return nil
}

func validateRuntimeConfig(c RuntimeConfig) error {
	if !validDNSName(c.Host) || c.Port == 0 || !validName(c.Database) || !validName(c.User) || len(c.Password) == 0 || len(c.Password) > 4096 || len(c.RootCAPEM) == 0 || len(c.RootCAPEM) > 1<<20 || c.StartupTimeout <= 0 || c.StartupTimeout > 30*time.Second || c.MaxOpenConns < 1 || c.MaxOpenConns > 64 || c.MaxIdleConns < 0 || c.MaxIdleConns > c.MaxOpenConns || c.ConnMaxLifetime <= 0 || c.ConnMaxLifetime > 24*time.Hour || c.ConnMaxIdleTime <= 0 || c.ConnMaxIdleTime > time.Hour {
		return ErrInvalidConfig
	}
	return nil
}

func validName(s string) bool {
	if !utf8.ValidString(s) || len(s) == 0 || len(s) > 63 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validDNSName(host string) bool {
	if host == "" || strings.ToLower(host) != host || net.ParseIP(host) != nil || strings.HasSuffix(host, ".") || strings.ContainsAny(host, "/\\:% \t\r\n") {
		return false
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii != host || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func runtimeConnString(c RuntimeConfig) string {
	pairs := [][2]string{{"host", c.Host}, {"port", fmt.Sprint(c.Port)}, {"dbname", c.Database}, {"user", c.User}, {"password", c.Password}, {"sslmode", "disable"}, {"sslrootcert", ""}}
	var b strings.Builder
	for i, pair := range pairs {
		if i != 0 {
			b.WriteByte(' ')
		}
		b.WriteString(pair[0])
		b.WriteString("='")
		b.WriteString(strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(pair[1]))
		b.WriteByte('\'')
	}
	return b.String()
}

func (db *RuntimeDB) admit() (*sql.DB, error) {
	if db == nil {
		return nil, ErrClosed
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed || db.pool == nil {
		return nil, ErrClosed
	}
	return db.pool, nil
}

// WithTx runs fn in one context-bound transaction.
func (db *RuntimeDB) WithTx(ctx context.Context, options *sql.TxOptions, fn func(*sql.Tx) error) (result error) {
	pool, err := db.admit()
	if err != nil {
		return err
	}
	if ctx == nil || fn == nil {
		return ErrInvalidConfig
	}
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrTransaction
	}
	finished := false
	defer func() {
		if finished {
			return
		}
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			result = errors.Join(result, ErrTransaction)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrTransaction
	}
	finished = true
	return nil
}

// PingContext checks pool availability without exposing driver diagnostics.
func (db *RuntimeDB) PingContext(ctx context.Context) error {
	pool, err := db.admit()
	if err != nil {
		return err
	}
	if ctx == nil {
		return ErrInvalidConfig
	}
	if err := pool.PingContext(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	return nil
}

// Close marks the handle closed before closing its single pool; repeated calls are safe.
func (db *RuntimeDB) Close() error {
	if db == nil {
		return nil
	}
	db.mu.Lock()
	if db.closed {
		db.mu.Unlock()
		return db.closeErr
	}
	db.closed = true
	if db.pool != nil && db.pool.Close() != nil {
		db.closeErr = ErrClose
	}
	err := db.closeErr
	db.mu.Unlock()
	return err
}
