package sqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/audit"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// These checks qualify the shared insert/read persistence and SQL constraints
// only. Synthetic actor values are not trusted middleware attribution and do
// not qualify InvocationWriter's successful native context path or an executor.
// The independent operator owns the disposable schema and final teardown.
func operationAuditRuntime(t *testing.T) (*storage.RuntimeDB, context.Context) {
	t.Helper()
	path := os.Getenv("AMOS_OPERATION_AUDIT_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required owner-qualified operation audit TLS fixture absent")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("required operation fixture config unavailable")
	}
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
	data, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	closed := f.Close()
	if readErr != nil || closed != nil || len(data) > 1<<20 {
		t.Fatal("required operation fixture config invalid")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	first := dec.Decode(&input)
	tail := dec.Decode(new(any))
	if first != nil || tail != io.EOF {
		t.Fatal("required operation fixture config invalid")
	}
	roots, err := os.ReadFile(input.CAPath)
	if err != nil {
		t.Fatal("required operation fixture CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: input.Host, Port: input.Port, Database: input.Database, User: input.User, Password: input.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 2, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("required operation runtime connection unavailable")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("operation runtime close failed")
		}
	})
	return db, ctx
}

func TestOperationAuditRequiredService(t *testing.T) {
	db, ctx := operationAuditRuntime(t)
	actor := audit.Actor{Kind: audit.ActorPerson, ID: newID(t)}
	var committed uuid.UUID
	for _, outcome := range []audit.Outcome{audit.OutcomeSucceeded, audit.OutcomeDenied, audit.OutcomeUnavailable} {
		t.Run("invocation_"+string(outcome), func(t *testing.T) {
			event := validEvent(t)
			event.Action = audit.ActionOperationInvoked
			event.ResourceType = audit.ResourceInvocation
			event.OperationID = "synthetic.audit"
			event.Outcome = outcome
			event.Attributes = []audit.Attribute{}
			var created audit.Record
			err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
				var err error
				created, err = insertEvent(ctx, tx, event, actor)
				return err
			})
			if err != nil {
				t.Fatal("invocation insert did not commit")
			}
			committed = created.ID
			err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				stored, err := scanRecord(tx.QueryRowContext(ctx, selectRecords+" WHERE id=$1", created.ID))
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(stored, created) || !reflect.DeepEqual(stored.Event, event) || stored.Actor != actor || stored.CreatedAt.Location() != time.UTC {
					return errors.New("synthetic audit read mismatch")
				}
				return nil
			})
			if err != nil {
				t.Fatal("committed invocation read mismatch")
			}
		})
	}
	t.Run("legacy_nullable_operation_read", func(t *testing.T) {
		event := validEvent(t)
		var created audit.Record
		err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { var err error; created, err = insertEvent(ctx, tx, event, actor); return err })
		if err != nil {
			t.Fatal("legacy insert failed after additive migration")
		}
		err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			stored, err := scanRecord(tx.QueryRowContext(ctx, selectRecords+" WHERE id=$1", created.ID))
			if err != nil {
				return err
			}
			var isNull bool
			if err = tx.QueryRowContext(ctx, "SELECT operation_id IS NULL FROM amos_security_audit_events WHERE id=$1", created.ID).Scan(&isNull); err != nil {
				return err
			}
			if !isNull || !reflect.DeepEqual(stored, created) || stored.OperationID != "" {
				return errors.New("synthetic legacy mismatch")
			}
			return nil
		})
		if err != nil {
			t.Fatal("legacy read failed after additive migration")
		}
	})
	t.Run("shared_insert_rolls_back", func(t *testing.T) {
		marker := errors.New("synthetic rollback")
		var id uuid.UUID
		err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			r, e := insertEvent(ctx, tx, validEvent(t), actor)
			id = r.ID
			if e != nil {
				return e
			}
			return marker
		})
		if !errors.Is(err, marker) || id == uuid.Nil {
			t.Fatal("shared insert rollback failed")
		}
		var count int
		err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, "SELECT count(*) FROM amos_security_audit_events WHERE id=$1", id).Scan(&count)
		})
		if err != nil || count != 0 {
			t.Fatal("rolled back audit row visible")
		}
	})
	type bindingCase struct {
		name, action, resource string
		operation              any
		attrs, outcome, state  string
	}
	for _, tc := range []bindingCase{
		{"missing_operation", "operation.invoked", "invocation", nil, "[]", "succeeded", "23514"},
		{"wrong_resource", "operation.invoked", "material", "synthetic.audit", "[]", "succeeded", "23514"},
		{"legacy_operation", "material.created", "material", "synthetic.audit", "[]", "succeeded", "23514"},
		{"legacy_invocation", "material.created", "invocation", nil, "[]", "succeeded", "23514"},
		{"invocation_attributes", "operation.invoked", "invocation", "synthetic.audit", `[{"key":"factor","value":"password"}]`, "succeeded", "23514"},
		{"invalid_operation", "operation.invoked", "invocation", "INVALID", "[]", "succeeded", "23514"},
		{"unknown_action", "unknown.action", "invocation", "synthetic.audit", "[]", "succeeded", "23514"},
		{"unknown_outcome", "operation.invoked", "invocation", "synthetic.audit", "[]", "unknown", "23514"},
		{"arbitrary_attributes", "material.created", "material", nil, `[{"key":"details","value":"synthetic"}]`, "succeeded", "P0001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := validEvent(t)
			id := newID(t)
			var code string
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				_, e := tx.ExecContext(ctx, `INSERT INTO amos_security_audit_events(id,installation_id,application_id,environment_id,workspace_id,actor_kind,actor_id,action,resource_type,resource_id,outcome,correlation_id,attributes,operation_id) VALUES($1,$2,$3,$4,$5,'person',$6,$7,$8,$9,$10,$11,$12::jsonb,$13)`, id, event.InstallationID, event.ApplicationID, event.EnvironmentID, event.WorkspaceID, actor.ID, tc.action, tc.resource, event.ResourceID, tc.outcome, event.CorrelationID, tc.attrs, tc.operation)
				var state interface{ SQLState() string }
				if errors.As(e, &state) {
					code = state.SQLState()
				}
				return e
			})
			if err == nil || code != tc.state {
				t.Fatal("expected exact SQL binding rejection absent")
			}
		})
	}
	for name, query := range map[string]string{"runtime_update": "UPDATE amos_security_audit_events SET operation_id=operation_id WHERE id=$1", "runtime_delete": "DELETE FROM amos_security_audit_events WHERE id=$1", "runtime_truncate": "TRUNCATE amos_security_audit_events"} {
		t.Run(name, func(t *testing.T) {
			var code string
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				var e error
				if name == "runtime_truncate" {
					_, e = tx.ExecContext(ctx, query)
				} else {
					_, e = tx.ExecContext(ctx, query, committed)
				}
				var state interface{ SQLState() string }
				if errors.As(e, &state) {
					code = state.SQLState()
				}
				return e
			})
			if err == nil || code != "42501" {
				t.Fatal("required runtime append-only privilege rejection absent")
			}
		})
	}
	t.Run("missing_trusted_context_denied", func(t *testing.T) {
		err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			return NewInvocationWriter().AppendInvocationTx(ctx, tx, newID(t), "synthetic.audit", audit.OutcomeDenied, newID(t))
		})
		if !errors.Is(err, audit.ErrForbidden) {
			t.Fatal("untrusted context accepted")
		}
	})
}
