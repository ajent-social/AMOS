package storage

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testRootPEM(t *testing.T) []byte {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func validRuntimeConfig(t *testing.T) RuntimeConfig {
	return RuntimeConfig{Host: "db.example.test", Port: 5432, Database: "amos", User: "runtime", Password: "secret", RootCAPEM: testRootPEM(t), StartupTimeout: time.Second, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Hour, ConnMaxIdleTime: time.Minute}
}

func TestRuntimeConfigValidation(t *testing.T) {
	good := validRuntimeConfig(t)
	if err := validateRuntimeConfig(good); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	cases := []struct {
		name string
		edit func(*RuntimeConfig)
	}{
		{"uppercase DNS", func(c *RuntimeConfig) { c.Host = "DB.example.test" }},
		{"IP literal", func(c *RuntimeConfig) { c.Host = "127.0.0.1" }},
		{"trailing dot", func(c *RuntimeConfig) { c.Host = "db.example.test." }},
		{"invalid label", func(c *RuntimeConfig) { c.Host = "-db.example.test" }},
		{"underscore", func(c *RuntimeConfig) { c.Host = "db_name.example.test" }},
		{"zero port", func(c *RuntimeConfig) { c.Port = 0 }},
		{"empty database", func(c *RuntimeConfig) { c.Database = "" }},
		{"control user", func(c *RuntimeConfig) { c.User = "bad\nuser" }},
		{"empty password", func(c *RuntimeConfig) { c.Password = "" }},
		{"long password", func(c *RuntimeConfig) { c.Password = string(make([]byte, 4097)) }},
		{"empty CA", func(c *RuntimeConfig) { c.RootCAPEM = nil }},
		{"startup limit", func(c *RuntimeConfig) { c.StartupTimeout = 31 * time.Second }},
		{"pool limit", func(c *RuntimeConfig) { c.MaxOpenConns = 65 }},
		{"idle exceeds open", func(c *RuntimeConfig) { c.MaxIdleConns = 5 }},
		{"lifetime limit", func(c *RuntimeConfig) { c.ConnMaxLifetime = 25 * time.Hour }},
		{"idle limit", func(c *RuntimeConfig) { c.ConnMaxIdleTime = time.Hour + time.Nanosecond }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.edit(&c)
			if err := validateRuntimeConfig(c); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestRuntimeFinalizesAllowListedPGXConfig(t *testing.T) {
	input := validRuntimeConfig(t)
	input.Password = "quote' and\\slash"
	roots, err := runtimeRoots(input.RootCAPEM)
	if err != nil {
		t.Fatal(err)
	}
	config, err := runtimeConnConfig(input, roots)
	if err != nil {
		t.Fatalf("parse runtime config: %v", err)
	}
	if config.Host != input.Host || config.Port != uint16(input.Port) || config.Database != input.Database || config.User != input.User || config.Password != input.Password {
		t.Fatal("typed connection fields changed")
	}
	if config.TLSConfig == nil || config.TLSConfig.InsecureSkipVerify || config.TLSConfig.ServerName != input.Host || config.TLSConfig.MinVersion != tls.VersionTLS12 || config.TLSConfig.RootCAs == nil || len(config.TLSConfig.RootCAs.Subjects()) != 1 {
		t.Fatalf("TLS verification not finalized: %#v", config.TLSConfig)
	}
	if config.Fallbacks != nil || len(config.RuntimeParams) != 0 {
		t.Fatalf("ambient fallback/runtime parameters retained: %#v %#v", config.Fallbacks, config.RuntimeParams)
	}
}

func TestRuntimeRejectsEveryAmbientPGVariable(t *testing.T) {
	original := lookupEnv
	t.Cleanup(func() { lookupEnv = original })
	for _, variable := range pgEnvironment {
		t.Run(variable, func(t *testing.T) {
			lookupEnv = func(key string) (string, bool) {
				if key == variable {
					return "poison", true
				}
				return "", false
			}
			_, err := OpenRuntime(context.Background(), validRuntimeConfig(t))
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("%s accepted: %v", variable, err)
			}
		})
	}
}

func TestRuntimeEmptyAmbientValuesAreIgnored(t *testing.T) {
	original := lookupEnv
	t.Cleanup(func() { lookupEnv = original })
	lookupEnv = func(string) (string, bool) { return "", true }
	// Invalid trust input guarantees configuration rejection before any network attempt.
	c := validRuntimeConfig(t)
	c.RootCAPEM = []byte("not PEM")
	if _, err := OpenRuntime(context.Background(), c); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestRuntimeRejectsMalformedOrMixedPEM(t *testing.T) {
	cases := [][]byte{nil, []byte("-----BEGIN CERTIFICATE-----\ninvalid\n-----END CERTIFICATE-----\n"), append(testRootPEM(t), []byte("trailing")...), append(testRootPEM(t), []byte("-----BEGIN PRIVATE KEY-----\nAA==\n-----END PRIVATE KEY-----\n")...)}
	for i, data := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			roots := x509.NewCertPool()
			if err := appendStrictCerts(roots, data); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("got %v", err)
			}
		})
	}
	roots := x509.NewCertPool()
	if err := appendStrictCerts(roots, testRootPEM(t)); err != nil {
		t.Fatalf("valid cert rejected: %v", err)
	}
}

func TestRuntimeConfigCopiesTrustInput(t *testing.T) {
	input := testRootPEM(t)
	roots, err := runtimeRoots(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] ^= 0xff
	if len(roots.Subjects()) != 1 {
		t.Fatalf("copied trust root missing: %d", len(roots.Subjects()))
	}
}

func TestRuntimeNilAndClosedHandles(t *testing.T) {
	var nilDB *RuntimeDB
	if err := nilDB.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
	if err := nilDB.PingContext(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil ping: %v", err)
	}
	if err := nilDB.WithTx(context.Background(), nil, func(*sql.Tx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil tx: %v", err)
	}
	db := &RuntimeDB{}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed ping: %v", err)
	}
}

func TestRuntimeConcurrentNilClose(t *testing.T) {
	db := &RuntimeDB{}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = db.Close() }()
	}
	wg.Wait()
}

type runtimeTestDriver struct{}
type runtimeTestConn struct{}
type runtimeTestTx struct{}

var runtimeTestDriverOnce sync.Once
var runtimeTestRollbackErr error
var runtimeTestCommitErr error
var runtimeTestPingEntered chan struct{}
var runtimeTestPingRelease chan struct{}

func runtimeTestPool(t *testing.T) *sql.DB {
	t.Helper()
	runtimeTestDriverOnce.Do(func() { sql.Register("amos-runtime-test", runtimeTestDriver{}) })
	runtimeTestRollbackErr, runtimeTestCommitErr = nil, nil
	runtimeTestPingEntered, runtimeTestPingRelease = nil, nil
	pool, err := sql.Open("amos-runtime-test", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}
func (runtimeTestDriver) Open(string) (driver.Conn, error)  { return runtimeTestConn{}, nil }
func (runtimeTestConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (runtimeTestConn) Close() error                        { return nil }
func (runtimeTestConn) Ping(ctx context.Context) error {
	if runtimeTestPingEntered != nil {
		close(runtimeTestPingEntered)
		select {
		case <-runtimeTestPingRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (runtimeTestConn) Begin() (driver.Tx, error) { return runtimeTestTx{}, nil }
func (runtimeTestConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return runtimeTestTx{}, nil
}
func (runtimeTestTx) Commit() error   { return runtimeTestCommitErr }
func (runtimeTestTx) Rollback() error { return runtimeTestRollbackErr }

func TestRuntimeWithTxErrorAndPanicPrecedence(t *testing.T) {
	callbackErr := errors.New("callback")
	db := &RuntimeDB{pool: runtimeTestPool(t)}
	if err := db.WithTx(context.Background(), nil, func(*sql.Tx) error { return callbackErr }); err != callbackErr {
		t.Fatalf("callback error changed: %v", err)
	}
	runtimeTestRollbackErr = errors.New("private rollback diagnostic")
	joined := db.WithTx(context.Background(), nil, func(*sql.Tx) error { return callbackErr })
	if !errors.Is(joined, callbackErr) || !errors.Is(joined, ErrTransaction) || strings.Contains(joined.Error(), "private rollback") {
		t.Fatalf("rollback precedence: %v", joined)
	}
	panicValue := &struct{ value string }{"original panic"}
	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Fatalf("panic changed: %#v", got)
			}
		}()
		_ = db.WithTx(context.Background(), nil, func(*sql.Tx) error { panic(panicValue) })
	}()
}

func TestRuntimeWithTxContextAndCommitErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := &RuntimeDB{pool: runtimeTestPool(t)}
	if err := db.WithTx(ctx, nil, func(*sql.Tx) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("begin cancellation: %v", err)
	}
	commitCtx, cancelCommit := context.WithCancel(context.Background())
	if err := db.WithTx(commitCtx, nil, func(*sql.Tx) error { cancelCommit(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("commit cancellation: %v", err)
	}
	runtimeTestCommitErr = errors.New("private commit diagnostic")
	if err := db.WithTx(context.Background(), nil, func(*sql.Tx) error { return nil }); !errors.Is(err, ErrTransaction) || strings.Contains(err.Error(), "private commit") {
		t.Fatalf("commit error: %v", err)
	}
}

func TestRuntimeCloseLinearizesAgainstAdmittedTransaction(t *testing.T) {
	pool := runtimeTestPool(t)
	db := &RuntimeDB{pool: pool}
	entered, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- db.WithTx(context.Background(), nil, func(*sql.Tx) error { close(entered); <-release; return nil })
	}()
	<-entered
	go func() { _ = db.Close(); close(closed) }()
	// Observe the linearization point without sleeps or dependence on the close duration.
	for {
		db.mu.Lock()
		isClosed := db.closed
		db.mu.Unlock()
		if isClosed {
			break
		}
		runtime.Gosched()
	}
	if err := db.PingContext(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("post-close admission: %v", err)
	}
	close(release)
	if err := <-txDone; err != nil {
		t.Fatalf("admitted tx: %v", err)
	}
	<-closed
	if err := db.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
}

func TestRuntimeDBCannotBePassedToMigrate(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".runtime-migrate-negative-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	source := `package compilecheck
import (
 "context"
 "github.com/ajent-social/amos/migrations"
 "github.com/ajent-social/amos/storage"
)
func invalid(ctx context.Context, db *storage.RuntimeDB, registry migrations.Registry) error {
 return storage.Migrate(ctx, db, registry)
}
`
	if err := os.WriteFile(filepath.Join(dir, "compile_negative.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-p=1", "./"+filepath.ToSlash(dir))
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("RuntimeDB unexpectedly compiled as a Migrate argument")
	}
	if ctx.Err() != nil {
		t.Fatalf("compile-negative timed out: %v", ctx.Err())
	}
	if !strings.Contains(string(output), "cannot use db") || !strings.Contains(string(output), "*storage.DB") {
		t.Fatalf("compile failed for an unexpected reason: %s", output)
	}
}

func TestRuntimePingCloseLinearizes(t *testing.T) {
	db := &RuntimeDB{pool: runtimeTestPool(t)}
	runtimeTestPingEntered, runtimeTestPingRelease = make(chan struct{}), make(chan struct{})
	defer func() { runtimeTestPingEntered, runtimeTestPingRelease = nil, nil }()
	pingDone, closeDone := make(chan error, 1), make(chan struct{})
	go func() { pingDone <- db.PingContext(context.Background()) }()
	<-runtimeTestPingEntered
	go func() { _ = db.Close(); close(closeDone) }()
	for {
		db.mu.Lock()
		closed := db.closed
		db.mu.Unlock()
		if closed {
			break
		}
		runtime.Gosched()
	}
	if err := db.PingContext(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("post-close ping admission: %v", err)
	}
	close(runtimeTestPingRelease)
	if err := <-pingDone; err != nil {
		t.Fatalf("admitted ping: %v", err)
	}
	<-closeDone
}

func TestRuntimePingPreservesCallerContext(t *testing.T) {
	db := &RuntimeDB{pool: runtimeTestPool(t)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.PingContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ping cancellation: %v", err)
	}
	if err := db.PingContext(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil ping context: %v", err)
	}
}
