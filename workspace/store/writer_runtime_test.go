package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	ws "github.com/ajent-social/amos/workspace/store"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
)

// The separately reviewed writer profile is owned by a different serial operator.
// This test exercises real workspace persistence under the shared writer root.
// It does not qualify current-session or operation-policy composition.
func runtimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_WRITER_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required TLS runtime fixture absent: AMOS_WRITER_RUNTIME_TEST_CONFIG")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("runtime fixture configuration unavailable")
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error("runtime fixture configuration close failed")
		}
	}()
	var config struct {
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
	if err := dec.Decode(&config); err != nil {
		t.Fatal("runtime fixture configuration invalid")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatal("runtime fixture configuration trailing data")
	}
	ca, err := os.ReadFile(config.CAPath)
	if err != nil {
		t.Fatal("runtime fixture trust unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: config.Host, Port: config.Port, Database: config.Database, User: config.User, Password: config.Password, RootCAPEM: ca, StartupTimeout: 3 * time.Second, MaxOpenConns: 4, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required TLS runtime unavailable")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("runtime close failed")
		}
	})
	return db
}

// A subprocess preserves the immutable legacy/W1 profile in combined packages.
func TestWriterWorkspaceRequiredService(t *testing.T) {
	if os.Getenv("AMOS_WORKSPACE_WRITER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestWriterWorkspaceRequiredService$", "-test.v")
		cmd.Env = append(os.Environ(), "AMOS_WORKSPACE_WRITER_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Log(string(out))
		if err != nil {
			t.Fatal("required W1 subprocess failed")
		}
		return
	}
	db := runtimeDB(t)
	root, err := aw.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = aw.ActivateW1(root); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	realm := aw.Realm{Installation: newID(t), Application: newID(t), Environment: newID(t)}
	scope := ws.Scope{InstallationID: realm.Installation, ApplicationID: realm.Application}
	owner := newID(t)
	err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, owner, realm.Installation, realm.Application)
		return e
	})
	if err != nil {
		t.Fatal("owner setup failed")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		e := db.WithTx(cleanup, nil, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(cleanup, `DELETE FROM workspaces WHERE installation_id=$1 AND application_id=$2`, realm.Installation, realm.Application)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(cleanup, `DELETE FROM identity_persons WHERE id=$1`, owner)
			return e
		})
		if e != nil {
			t.Error("owned rows cleanup failed")
		}
	})
	for _, mode := range []string{"create commit", "create rollback", "unplanned workspace", "share cannot upgrade", "wrong realm", "ignored failure", "closed attempt", "wrong context"} {
		t.Run(mode, func(t *testing.T) {
			workspace := newID(t)
			personAccess := aw.ExistingUpdate
			if mode == "share cannot upgrade" {
				personAccess = aw.ExistingShare
			}
			rows := []aw.Row{{Table: aw.Persons, ID: owner, Access: personAccess}}
			if mode != "unplanned workspace" {
				rows = append(rows, aw.Row{Table: aw.Workspaces, ID: workspace, Access: aw.ReservedInsert})
			}
			plan, e := aw.NewPlan(realm, rows, nil)
			if e != nil {
				t.Fatal(e)
			}
			_, e = root.Run(ctx, func(request context.Context, a *aw.Attempt) aw.Outcome {
				if e := a.SealPlan(plan); e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				for phase := aw.P; phase <= aw.W; phase++ {
					if e := a.Acquire(request, phase); e != nil {
						t.Error(e)
						return aw.UnavailableRollback
					}
				}
				store, e := ws.NewWriter(a)
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				in := ws.CreatePersonalInput{ID: workspace, Scope: scope, OwnerPersonID: owner}
				if mode == "wrong realm" {
					in.Scope.ApplicationID = newID(t)
				}
				if mode == "ignored failure" {
					in.OwnerPersonID = newID(t)
				}
				if mode == "closed attempt" {
					if e := a.Finish(aw.UnavailableRollback); e != nil {
						t.Error(e)
					}
				}
				callctx := request
				if mode == "wrong context" {
					callctx = context.Background()
				}
				_, e = store.CreatePersonalWorkspace(callctx, in)
				if mode != "create commit" && mode != "create rollback" {
					if e == nil {
						t.Error("invalid writer admitted")
					}
					if _, e = a.DrainAndSample(request); e == nil {
						t.Error("failed participant allowed finalization")
					}
					return aw.UnavailableRollback
				}
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				if mode == "create rollback" {
					if e = a.Finish(aw.DeniedRollback); e != nil {
						t.Error(e)
					}
					return aw.DeniedRollback
				}
				if _, e = a.DrainAndSample(request); e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				if e = a.Finish(aw.Success); e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				return aw.Success
			})
			if mode == "create commit" && e != nil {
				t.Fatal(e)
			}
			if mode == "create rollback" && !errors.Is(e, aw.ErrDenied) {
				t.Fatal("rollback outcome lost")
			}
			if mode != "create commit" && mode != "create rollback" && !errors.Is(e, aw.ErrUnavailable) {
				t.Fatal("invalid writer not unavailable")
			}
			err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				var count int
				if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE id=$1`, workspace).Scan(&count); e != nil {
					return e
				}
				want := 0
				if mode == "create commit" {
					want = 1
				}
				if count != want {
					t.Error("workspace durability disagrees with root outcome")
				}
				_, e := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id=$1`, workspace)
				return e
			})
			if err != nil {
				t.Fatal("durability check failed")
			}
		})
	}
}
