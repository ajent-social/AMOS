package operation

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// The driver double qualifies delegation only, never PostgreSQL semantics.
type invocationDriver struct{ conn *invocationConn }

func (d invocationDriver) Open(string) (driver.Conn, error)             { return d.conn, nil }
func (d invocationDriver) Connect(context.Context) (driver.Conn, error) { return d.conn, nil }
func (d invocationDriver) Driver() driver.Driver                        { return d }

type invocationConn struct {
	ctx    context.Context
	query  string
	args   []driver.NamedValue
	err    error
	result driver.Result
}

func (*invocationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*invocationConn) Close() error                { return nil }
func (c *invocationConn) Begin() (driver.Tx, error) { return c, nil }
func (*invocationConn) Commit() error               { return nil }
func (*invocationConn) Rollback() error             { return nil }
func (c *invocationConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.ctx, c.query, c.args = ctx, q, args
	return c.result, c.err
}
func (c *invocationConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.ctx, c.query, c.args = ctx, q, args
	if c.err != nil {
		return nil, c.err
	}
	return &invocationRows{}, nil
}

type invocationRows struct{ read bool }

func (*invocationRows) Columns() []string { return []string{"value"} }
func (*invocationRows) Close() error      { return nil }
func (r *invocationRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	dest[0] = int64(37)
	return nil
}

func TestSQLTxAdapterUnit(t *testing.T) {
	t.Run("nil and restricted method set", func(t *testing.T) {
		db, err := newSQLTxAdapter(nil)
		if db != nil || err != errNilSQLTx {
			t.Fatal("nil transaction not rejected")
		}
		typ := reflect.TypeOf((*sqlTxAdapter)(nil))
		if typ.NumMethod() != 3 {
			t.Fatal("adapter exposes extra methods")
		}
		for _, name := range []string{"ExecContext", "QueryContext", "QueryRowContext"} {
			if _, ok := typ.MethodByName(name); !ok {
				t.Fatal("missing DBTX method")
			}
		}
		field := typ.Elem().Field(0)
		if typ.Elem().NumField() != 1 || field.Anonymous || field.IsExported() || field.Type != reflect.TypeOf((*sql.Tx)(nil)) {
			t.Fatal("transaction must be a private nonembedded field")
		}
	})
	for _, method := range []string{"exec", "query", "row"} {
		t.Run(method, func(t *testing.T) {
			c := &invocationConn{result: driver.RowsAffected(7)}
			pool := sql.OpenDB(invocationDriver{conn: c})
			t.Cleanup(func() {
				if err := pool.Close(); err != nil {
					t.Error(err)
				}
			})
			tx, err := pool.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
					t.Error(err)
				}
			})
			db, err := newSQLTxAdapter(tx)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			query := "unchanged query $1 $2"
			args := []any{int64(19), sql.Named("payload", "unchanged")}
			call := func() error {
				switch method {
				case "exec":
					result, err := db.ExecContext(ctx, query, args...)
					if err == nil {
						n, e := result.RowsAffected()
						if e != nil || n != 7 {
							t.Fatal("result changed")
						}
					}
					return err
				case "query":
					rows, err := db.QueryContext(ctx, query, args...)
					if err != nil {
						if rows != nil {
							t.Fatal("query error returned typed-nil Rows")
						}
						return err
					}
					if !rows.Next() {
						t.Fatal("missing row")
					}
					var value int64
					if err := rows.Scan(&value); err != nil || value != 37 {
						t.Fatal("scan changed")
					}
					if rows.Next() || rows.Err() != nil {
						t.Fatal("iteration changed")
					}
					return rows.Close()
				default:
					var value int64
					err := db.QueryRowContext(ctx, query, args...).Scan(&value)
					if err == nil && value != 37 {
						t.Fatal("row changed")
					}
					return err
				}
			}
			if err := call(); err != nil {
				t.Fatal(err)
			}
			want := []driver.NamedValue{{Ordinal: 1, Value: int64(19)}, {Ordinal: 2, Name: "payload", Value: "unchanged"}}
			if c.ctx != ctx || c.query != query || !reflect.DeepEqual(c.args, want) {
				t.Fatal("context/query/arguments changed")
			}
			sentinel := errors.New("driver sentinel")
			c.err = sentinel
			if call() != sentinel {
				t.Fatal("driver error identity changed")
			}
			cancel()
			if !errors.Is(call(), context.Canceled) {
				t.Fatal("canceled context lost")
			}
			ctx = context.Background()
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if call() != sql.ErrTxDone {
				t.Fatal("completed transaction error changed")
			}
		})
	}
}

// This suite deliberately fails without an admitted, precreated TLS fixture.
// It performs no DDL and must run only in a coordinator-assigned service window.
func TestSQLTxAdapterRequiredService(t *testing.T) {
	pool, table := invocationServicePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var seed [8]byte
	if _, err := rand.Read(seed[:]); err != nil {
		t.Fatal("test identifier generation failed")
	}
	id := int64(binary.BigEndian.Uint64(seed[:]) & ((1 << 63) - 2))
	// Delete only rows inserted by this test; collisions fail without cleanup ownership.
	owned := []int64{}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, key := range owned {
			if _, err := pool.ExecContext(cleanupCtx, "DELETE FROM "+table+" WHERE id=$1", key); err != nil {
				t.Error("fixture row cleanup failed")
			}
		}
	})
	for i, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			key := id + int64(i)
			tx, err := pool.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal("begin failed")
			}
			defer func() {
				if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
					t.Error("rollback cleanup failed")
				}
			}()
			db, err := newSQLTxAdapter(tx)
			if err != nil {
				t.Fatal("adapter construction failed")
			}
			result, err := db.ExecContext(ctx, "INSERT INTO "+table+" (id,value) VALUES ($1,$2)", key, "adapter")
			if err != nil {
				t.Fatal("adapter insert failed")
			}
			n, err := result.RowsAffected()
			if err != nil || n != 1 {
				t.Fatal("insert result incorrect")
			}
			var value string
			if err := tx.QueryRowContext(ctx, "SELECT value FROM "+table+" WHERE id=$1", key).Scan(&value); err != nil || value != "adapter" {
				t.Fatal("raw transaction cannot see adapter write")
			}
			if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET value=$2 WHERE id=$1", key, "owner"); err != nil {
				t.Fatal("owner update failed")
			}
			if err := db.QueryRowContext(ctx, "SELECT value FROM "+table+" WHERE id=$1", key).Scan(&value); err != nil || value != "owner" {
				t.Fatal("adapter cannot see owner write")
			}
			var rawID, adapterID string
			if err := tx.QueryRowContext(ctx, "SELECT pg_current_xact_id()::text").Scan(&rawID); err != nil {
				t.Fatal("transaction identity query failed")
			}
			rows, err := db.QueryContext(ctx, "SELECT pg_current_xact_id()::text, value FROM "+table+" WHERE id=$1", key)
			if err != nil {
				t.Fatal("adapter query failed")
			}
			if !rows.Next() {
				t.Fatal("adapter row missing")
			}
			if err := rows.Scan(&adapterID, &value); err != nil || rawID != adapterID || value != "owner" {
				t.Fatal("transaction identity or row changed")
			}
			if rows.Next() || rows.Err() != nil {
				t.Fatal("unexpected iteration result")
			}
			if err := rows.Close(); err != nil {
				t.Fatal("rows close failed")
			}
			var count int
			if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", key).Scan(&count); err != nil || count != 0 {
				t.Fatal("uncommitted write escaped transaction")
			}
			if err := db.QueryRowContext(ctx, "SELECT value FROM "+table+" WHERE false").Scan(&value); err != sql.ErrNoRows {
				t.Fatal("no-rows behavior changed")
			}
			if finish == "commit" {
				if err := tx.Commit(); err != nil {
					t.Fatal("commit failed")
				}
				owned = append(owned, key)
			} else if err := tx.Rollback(); err != nil {
				t.Fatal("rollback failed")
			}
			want := 0
			if finish == "commit" {
				want = 1
			}
			if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", key).Scan(&count); err != nil || count != want {
				t.Fatal("completion visibility incorrect")
			}
			if _, err := db.ExecContext(ctx, "UPDATE "+table+" SET value=$2 WHERE id=$1", key, "late"); err != sql.ErrTxDone {
				t.Fatal("completed exec did not fail")
			}
			if rows, err := db.QueryContext(ctx, "SELECT 1"); err != sql.ErrTxDone || rows != nil {
				t.Fatal("completed query did not return nil and ErrTxDone")
			}
			if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&count); err != sql.ErrTxDone {
				t.Fatal("completed row did not fail")
			}
		})
	}
	t.Run("cancellation and SQL errors", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal("begin failed")
		}
		defer func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				t.Error("rollback failed")
			}
		}()
		db, err := newSQLTxAdapter(tx)
		if err != nil {
			t.Fatal("adapter construction failed")
		}
		canceled, stop := context.WithCancel(ctx)
		stop()
		if _, err := db.ExecContext(canceled, "SELECT 1"); !errors.Is(err, context.Canceled) {
			t.Fatal("exec cancellation lost")
		}
		if rows, err := db.QueryContext(canceled, "SELECT 1"); !errors.Is(err, context.Canceled) || rows != nil {
			t.Fatal("query cancellation lost")
		}
		var value int
		if err := db.QueryRowContext(canceled, "SELECT 1").Scan(&value); !errors.Is(err, context.Canceled) {
			t.Fatal("row cancellation lost")
		}
		if rows, err := db.QueryContext(ctx, "SELECT 1/0"); err == nil || rows != nil {
			t.Fatal("SQL error did not return nil Rows")
		}
	})
	t.Run("in-flight query deadline", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal("begin failed")
		}
		defer func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				// pgx may discard the connection after a network deadline.
				t.Log("owner rollback attempted after deadline; connection unavailable")
			}
		}()
		db, err := newSQLTxAdapter(tx)
		if err != nil {
			t.Fatal("adapter construction failed")
		}
		deadline, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stop()
		// Server-side delay exercises cancellation of an actual PostgreSQL request.
		rows, err := db.QueryContext(deadline, "SELECT pg_sleep(10)")
		if !errors.Is(err, context.DeadlineExceeded) || rows != nil {
			t.Fatal("in-flight query deadline not preserved")
		}
	})

}

func invocationServicePool(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := os.Getenv("AMOS_OPERATION_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS PostgreSQL fixture absent: set AMOS_OPERATION_TEST_CONFIG only after admission and service-window assignment")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("fixture config unavailable")
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
		DMLTable       string `json:"dml_table"`
		WrongHost      string `json:"wrong_host"`
		LedgerTable    string `json:"ledger_table"`
		PrivilegedRole string `json:"privileged_role"`
		OwnerRole      string `json:"owner_role"`
	}
	dec := json.NewDecoder(io.LimitReader(f, 2<<20))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
		t.Fatal("fixture config invalid")
	}
	if input.Host == "" || strings.ContainsAny(input.Host, "/\\") || input.Port == 0 || input.Database == "" || input.User == "" || input.Password == "" {
		t.Fatal("explicit fixture connection fields required")
	}
	parts := strings.Split(input.DMLTable, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatal("precreated schema-qualified table required")
	}
	// Refuse ambient parser configuration rather than discovering service/pass files.
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "PG") {
			t.Fatal("ambient PostgreSQL configuration forbidden")
		}
	}
	rootsPEM, err := os.ReadFile(input.CAPath)
	if err != nil {
		t.Fatal("fixture trust root unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootsPEM) {
		t.Fatal("fixture trust root invalid")
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(input.Host, strconv.Itoa(int(input.Port))), Path: "/" + input.Database, User: url.UserPassword(input.User, input.Password)}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("sslrootcert", "")
	u.RawQuery = q.Encode()
	cfg, err := pgx.ParseConfigWithOptions(u.String(), pgx.ParseConfigOptions{ConnStringAllowedKeys: []string{"host", "port", "dbname", "user", "password", "sslmode", "sslrootcert"}})
	if err != nil {
		t.Fatal("fixture connection configuration invalid")
	}
	if cfg.Host != input.Host || cfg.Port != input.Port || cfg.Database != input.Database || cfg.User != input.User || cfg.Password != input.Password {
		t.Fatal("fixture parser changed explicit connection fields")
	}
	cfg.TLSConfig = &tls.Config{RootCAs: roots, ServerName: input.Host, MinVersion: tls.VersionTLS12}
	cfg.Fallbacks = nil
	cfg.RuntimeParams = nil
	cfg.ConnectTimeout = 3 * time.Second
	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(2)
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error("fixture connection close failed")
		}
	})
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if pool.PingContext(ctx) != nil {
		t.Fatal("required TLS PostgreSQL connection failed")
	}
	var secure bool
	if err := pool.QueryRowContext(ctx, "SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&secure); err != nil || !secure {
		t.Fatal("fixture TLS not active")
	}
	return pool, pgx.Identifier(parts).Sanitize()
}
