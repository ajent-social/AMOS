package storage

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	if config.TLSConfig == nil || config.TLSConfig.InsecureSkipVerify || config.TLSConfig.ServerName != input.Host || config.TLSConfig.MinVersion != tls.VersionTLS12 || config.TLSConfig.RootCAs == nil || !config.TLSConfig.RootCAs.Equal(roots) {
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
	expected := x509.NewCertPool()
	if !expected.AppendCertsFromPEM(input) {
		t.Fatal("test trust root invalid")
	}
	input[0] ^= 0xff
	if !roots.Equal(expected) {
		t.Fatal("copied trust root changed")
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
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error("compile-negative directory cleanup failed")
		}
	})
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
	var nilContext context.Context
	if err := db.PingContext(nilContext); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil ping context: %v", err)
	}
}

func TestRuntimeParserDoesNotReadAmbientFiles(t *testing.T) {
	const helper = "AMOS_RUNTIME_FILE_DISCOVERY_HELPER"
	if os.Getenv(helper) == "1" {
		input := validRuntimeConfig(t)
		roots, err := runtimeRoots(input.RootCAPEM)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeConnConfig(input, roots); err != nil {
			t.Fatalf("config parser consulted ambient file contents: %v", err)
		}
		return
	}
	root := t.TempDir()
	paths := map[string]string{
		"PGPASSFILE":    filepath.Join(root, "passfile"),
		"PGSERVICEFILE": filepath.Join(root, "servicefile"),
		"PGSSLROOTCERT": filepath.Join(root, "root-ca"),
		"PGSSLCERT":     filepath.Join(root, "client-cert"),
		"PGSSLKEY":      filepath.Join(root, "client-key"),
	}
	for _, path := range paths {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeParserDoesNotReadAmbientFiles$")
	cmd.Env = []string{"HOME=" + root, helper + "=1"}
	for key, value := range paths {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated parser check failed: %v: %s", err, output)
	}
}

// runtimeServiceFixture is a freshly generated, owner-qualified fixture input,
// not a production credential source. DMLTable has a bigint identity id and
// text value. Only the fixture owner creates schemas, roles, and credentials.
type runtimeServiceFixture struct {
	RuntimeConfig
	WrongHost      string
	DMLTable       string
	LedgerTable    string
	PrivilegedRole string
}

func runtimeRequiredFixture(t *testing.T) runtimeServiceFixture {
	t.Helper()
	path := os.Getenv("AMOS_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS PostgreSQL fixture absent: set AMOS_RUNTIME_TEST_CONFIG to fresh owner-qualified fixture JSON")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("required TLS PostgreSQL fixture config cannot be opened")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("fixture config close failed")
		}
	}()
	var input struct {
		Host           string `json:"host"`
		Port           uint16 `json:"port"`
		Database       string `json:"database"`
		User           string `json:"user"`
		Password       string `json:"password"`
		CAPath         string `json:"ca_path"`
		WrongHost      string `json:"wrong_host"`
		DMLTable       string `json:"dml_table"`
		LedgerTable    string `json:"ledger_table"`
		PrivilegedRole string `json:"privileged_role"`
		OwnerRole      string `json:"owner_role"`
	}
	dec := json.NewDecoder(io.LimitReader(f, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		t.Fatal("required TLS PostgreSQL fixture config is invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("required TLS PostgreSQL fixture config has trailing data")
	}
	roots, err := os.ReadFile(input.CAPath)
	if err != nil {
		t.Fatal("required TLS PostgreSQL fixture CA cannot be read")
	}
	fixture := runtimeServiceFixture{
		RuntimeConfig: RuntimeConfig{Host: input.Host, Port: input.Port, Database: input.Database, User: input.User, Password: input.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute},
		WrongHost:     input.WrongHost, DMLTable: input.DMLTable, LedgerTable: input.LedgerTable, PrivilegedRole: input.OwnerRole,
	}
	if !validName(input.PrivilegedRole) {
		t.Fatal("required privileged fixture role is invalid")
	}
	if validateRuntimeConfig(fixture.RuntimeConfig) != nil || !validDNSName(fixture.WrongHost) || fixture.WrongHost == fixture.Host || !validName(fixture.PrivilegedRole) {
		t.Fatal("required TLS PostgreSQL fixture fields are invalid")
	}
	for _, table := range []string{fixture.DMLTable, fixture.LedgerTable} {
		parts := strings.Split(table, ".")
		if len(parts) != 2 || !validName(parts[0]) || !validName(parts[1]) {
			t.Fatal("fixture tables must be schema-qualified names")
		}
	}
	return fixture
}

func runtimeServiceOpen(t *testing.T, config RuntimeConfig) *RuntimeDB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := OpenRuntime(ctx, config)
	if err != nil {
		t.Fatalf("required TLS PostgreSQL runtime connection failed: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("runtime close failed: %v", err)
		}
	})
	return db
}

func runtimeServiceTx(t *testing.T, db *RuntimeDB, fn func(context.Context, *sql.Tx) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { return fn(ctx, tx) }); err != nil {
		// SQL diagnostics can contain private connection and fixture identifiers.
		t.Fatal("required runtime transaction failed")
	}
}

func TestRuntimeRequiredService(t *testing.T) {
	fixture := runtimeRequiredFixture(t)
	db := runtimeServiceOpen(t, fixture.RuntimeConfig)
	table := pgx.Identifier(strings.Split(fixture.DMLTable, ".")).Sanitize()
	ledger := pgx.Identifier(strings.Split(fixture.LedgerTable, ".")).Sanitize()
	role := pgx.Identifier{fixture.PrivilegedRole}.Sanitize()
	var id int64
	t.Cleanup(func() {
		runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id = $1", id)
			return err
		})
	})

	t.Run("verified TLS and least privilege prerequisites", func(t *testing.T) {
		runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
			var ssl, super, createDB, createRole, replication, bypassRLS bool
			var version string
			if err := tx.QueryRowContext(ctx, `SELECT s.ssl, s.version, r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolreplication, r.rolbypassrls FROM pg_stat_ssl s JOIN pg_roles r ON r.rolname = current_user WHERE s.pid = pg_backend_pid()`).Scan(&ssl, &version, &super, &createDB, &createRole, &replication, &bypassRLS); err != nil {
				return err
			}
			if !ssl || (version != "TLSv1.2" && version != "TLSv1.3") || super || createDB || createRole || replication || bypassRLS {
				t.Error("fixture lacks verified TLS or a least-privilege runtime role")
			}
			var present bool
			for _, name := range []string{fixture.DMLTable, fixture.LedgerTable} {
				if err := tx.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", name).Scan(&present); err != nil {
					return err
				}
				if !present {
					t.Error("required precreated fixture table is absent")
				}
			}
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", fixture.PrivilegedRole).Scan(&present); err != nil {
				return err
			}
			if !present {
				t.Error("required privileged fixture role is absent")
			}
			return nil
		})
	})
	if t.Failed() {
		t.Fatal("fixture prerequisites failed")
	}

	readValue := func(t *testing.T, want string) {
		t.Helper()
		runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
			var got string
			if err := tx.QueryRowContext(ctx, "SELECT value FROM "+table+" WHERE id = $1", id).Scan(&got); err != nil {
				return err
			}
			if got != want {
				t.Error("transaction persisted an unexpected value")
			}
			return nil
		})
	}
	t.Run("DML commits", func(t *testing.T) {
		runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, "INSERT INTO "+table+" (value) VALUES ($1) RETURNING id", "inserted").Scan(&id)
		})
		readValue(t, "inserted")
		runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE "+table+" SET value = $2 WHERE id = $1", id, "committed")
			return err
		})
		readValue(t, "committed")
	})
	if t.Failed() {
		t.Fatal("runtime DML prerequisite failed")
	}
	for _, panicPath := range []bool{false, true} {
		name := "callback error rollback"
		if panicPath {
			name = "panic rollback"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			marker := errors.New("callback marker")
			func() {
				if panicPath {
					defer func() {
						if recover() != marker {
							t.Error("original panic was not preserved")
						}
					}()
				}
				err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
					if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET value = $2 WHERE id = $1", id, "rolled back"); err != nil {
						return err
					}
					if panicPath {
						panic(marker)
					}
					return marker
				})
				if panicPath || err != marker {
					t.Error("callback outcome was not preserved")
				}
			}()
			readValue(t, "committed")
		})
	}

	t.Run("canceled transaction rolls back", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET value = $2 WHERE id = $1", id, "canceled"); err != nil {
				return err
			}
			cancel()
			return nil
		})
		if err != context.Canceled {
			t.Fatal("canceled transaction did not preserve context error")
		}
		readValue(t, "committed")
	})

	for _, tc := range []struct{ name, query string }{
		{"schema creation", `CREATE SCHEMA amos_runtime_denied_probe`},
		{"table creation", "CREATE TABLE " + pgx.Identifier{strings.Split(fixture.DMLTable, ".")[0], "runtime_denied_probe"}.Sanitize() + " (id text)"},
		{"alter table", "ALTER TABLE " + table + " ADD COLUMN runtime_denied_probe text"},
		{"drop table", "DROP TABLE " + table},
		{"temporary table", "CREATE TEMP TABLE runtime_denied_probe (id text)"},
		{"ledger read", "SELECT * FROM " + ledger},
		{"ledger mutation", "DELETE FROM " + ledger},
		{"ledger DDL", "ALTER TABLE " + ledger + " ADD COLUMN runtime_denied_probe text"},
		{"role creation", "CREATE ROLE amos_runtime_denied_probe"},
		{"role alteration", "ALTER ROLE " + role + " NOLOGIN"},
		{"role assumption", "SET ROLE " + role},
		{"role grant", "GRANT " + role + " TO " + pgx.Identifier{fixture.User}.Sanitize()},
		{"ownership", "ALTER TABLE " + table + " OWNER TO " + pgx.Identifier{fixture.User}.Sanitize()},
	} {
		t.Run("denied "+tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rollback := errors.New("rollback authority probe")
			var denied bool
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				_, execErr := tx.ExecContext(ctx, tc.query)
				var pgErr *pgconn.PgError
				denied = errors.As(execErr, &pgErr) && pgErr.Code == "42501"
				return rollback // Never commit even an unexpectedly permitted probe.
			})
			if err != rollback || !denied {
				t.Fatal("expected PostgreSQL insufficient_privilege (42501) and successful rollback")
			}
		})
	}

	t.Run("wrong CA", func(t *testing.T) {
		bad := fixture.RuntimeConfig
		bad.RootCAPEM = testRootPEM(t)
		runtimeServiceReject(t, bad)
	})
	t.Run("wrong hostname", func(t *testing.T) {
		// First establish that the wrong name reaches the same TLS service and
		// chains to the correct CA when verified under the correct DNS name.
		// This prevents DNS or routing failure from masquerading as TLS denial.
		roots, err := runtimeRoots(fixture.RootCAPEM)
		if err != nil {
			t.Fatal("fixture trust roots invalid")
		}
		config, err := runtimeConnConfig(fixture.RuntimeConfig, roots)
		if err != nil {
			t.Fatal("fixture connection configuration invalid")
		}
		config.Host = fixture.WrongHost
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			t.Fatal("wrong-host prerequisite must reach the trusted fixture endpoint")
		}
		if err := conn.Close(ctx); err != nil {
			t.Fatal("wrong-host prerequisite close failed")
		}
		bad := fixture.RuntimeConfig
		bad.Host = fixture.WrongHost
		runtimeServiceReject(t, bad)
	})
	t.Run("cancellation and lifecycle", func(t *testing.T) {
		runtimeServiceLifecycle(t, fixture.RuntimeConfig)
	})
	t.Run("bounded startup", func(t *testing.T) {
		runtimeServiceStartupTimeout(t, fixture.RuntimeConfig)
	})
	t.Run("delete commits", func(t *testing.T) {
		runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id = $1", id); err != nil {
				return err
			}
			return nil
		})
		runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id = $1", id).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Error("delete was not committed")
			}
			return nil
		})
	})
}

func runtimeServiceReject(t *testing.T, config RuntimeConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := OpenRuntime(ctx, config)
	if db != nil {
		if closeErr := db.Close(); closeErr != nil {
			t.Error("unexpected connection close failed")
		}
	}
	if db != nil || err != ErrUnavailable {
		t.Fatal("untrusted TLS endpoint was not rejected with the safe unavailable sentinel")
	}
}

func runtimeServiceLifecycle(t *testing.T, config RuntimeConfig) {
	t.Helper()
	config.MaxOpenConns, config.MaxIdleConns = 1, 1
	db := runtimeServiceOpen(t, config)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := db.WithTx(ctx, nil, func(*sql.Tx) error { called = true; return nil }); err != context.Canceled || called {
		t.Fatal("canceled begin admitted callback or lost context error")
	}
	if err := db.PingContext(ctx); err != context.Canceled {
		t.Fatal("canceled ping lost context error")
	}
	if opened, err := OpenRuntime(ctx, config); err != context.Canceled || opened != nil {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("canceled startup lost context error")
	}
	commitCtx, cancelCommit := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCommit()
	if err := db.WithTx(commitCtx, nil, func(*sql.Tx) error { cancelCommit(); return nil }); err != context.Canceled {
		t.Fatal("canceled commit lost context error")
	}
	// Hold the only connection. Both readiness and transaction admission must
	// honor their deadlines while waiting for pool capacity.
	runtimeServiceTx(t, db, func(_ context.Context, _ *sql.Tx) error {
		waitCtx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer stop()
		if err := db.PingContext(waitCtx); err != context.DeadlineExceeded {
			t.Error("pool wait ping ignored deadline")
		}
		if err := db.WithTx(waitCtx, nil, func(*sql.Tx) error { t.Error("expired pool wait admitted callback"); return nil }); err != context.DeadlineExceeded {
			t.Error("pool wait transaction ignored deadline")
		}
		return nil
	})

	queryCtx, stopQuery := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopQuery()
	queryCanceled := false
	err := db.WithTx(queryCtx, nil, func(tx *sql.Tx) error {
		_, queryErr := tx.ExecContext(queryCtx, "SELECT pg_sleep(5)")
		queryCanceled = queryErr != nil && queryCtx.Err() == context.DeadlineExceeded
		return queryCtx.Err()
	})
	if !queryCanceled || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("in-flight PostgreSQL query did not honor cancellation")
	}
	runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "SELECT 1")
		return err
	})

	// Closing while an admitted real transaction is active must not permit
	// later operations; the admitted transaction may finish under its context.
	runtimeServiceTx(t, db, func(ctx context.Context, tx *sql.Tx) error {
		if err := db.Close(); err != nil {
			return err
		}
		if err := db.PingContext(ctx); err != ErrClosed {
			t.Error("post-close ping admitted")
		}
		if err := db.WithTx(ctx, nil, func(*sql.Tx) error { t.Error("post-close callback admitted"); return nil }); err != ErrClosed {
			t.Error("post-close transaction lost closed sentinel")
		}
		_, err := tx.ExecContext(ctx, "SELECT 1")
		return err
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := db.Close(); err != nil {
				t.Error("concurrent repeated close failed")
			}
		}()
	}
	wg.Wait()
}

func runtimeServiceStartupTimeout(t *testing.T, config RuntimeConfig) {
	t.Helper()
	// A local TCP sink deliberately never answers the PostgreSQL handshake.
	// This qualifies timeout behavior only, not TLS or provider availability.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("cannot create bounded startup timeout probe")
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Error("startup probe close failed")
		}
	}()
	config.Host = "localhost"
	config.Port = uint16(listener.Addr().(*net.TCPAddr).Port)
	config.StartupTimeout = 50 * time.Millisecond
	for _, callerFirst := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		want := ErrUnavailable
		if callerFirst {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			config.StartupTimeout = 2 * time.Second
			want = context.DeadlineExceeded
		}
		started := time.Now()
		db, err := OpenRuntime(ctx, config)
		cancel()
		if db != nil {
			_ = db.Close()
		}
		if db != nil || err != want || time.Since(started) > time.Second {
			t.Error("startup did not honor the smaller timeout with its safe error")
		}
	}
}
