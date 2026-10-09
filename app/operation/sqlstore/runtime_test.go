package sqlstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// This suite needs a fresh disposable, owner-qualified TLS fixture. Its owner
// provisions migration 17, restricted runtime privileges and ONLY the synthetic
// capacity selectors below. The test never provisions quotas, modifies schema,
// connects as an owner or deletes immutable completed invocations. Teardown of
// the disposable fixture is an operator duty, including after failure.
func operationRuntime(t *testing.T) (*storage.RuntimeDB, *Store, context.Context) {
	t.Helper()
	path := os.Getenv("AMOS_OPERATION_STORE_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required owner-qualified operation storage TLS fixture absent")
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
	st, err := NewWithRunner(db)
	if err != nil {
		t.Fatal("operation store construction failed")
	}
	return db, st, ctx
}
func runtimeScope() operation.Scope {
	return operation.Scope{InstallationID: uuid.MustParse("01900000-0000-7000-8000-000000000101"), ApplicationID: uuid.MustParse("01900000-0000-7000-8000-000000000102"), EnvironmentID: uuid.MustParse("01900000-0000-7000-8000-000000000103"), WorkspaceID: uuid.MustParse("01900000-0000-7000-8000-000000000104"), ActorKind: "person", ActorID: uuid.MustParse("01900000-0000-7000-8000-000000000105"), OperationID: "storage.synthetic"}
}
func runtimeID(t *testing.T) identity.ID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal("synthetic ID unavailable")
	}
	return id
}
func runtimeKey(t *testing.T) operation.KeyDigest {
	t.Helper()
	id := runtimeID(t)
	return operation.KeyDigest(sha256.Sum256(id[:]))
}

var errRuntimeRollback = errors.New("synthetic operation rollback")
var errRuntimeAssertion = errors.New("synthetic operation assertion failed")

func runtimeUsage(ctx context.Context, db *storage.RuntimeDB, s operation.Scope) (int64, int64, error) {
	var actor, workspace int64
	err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT used_bytes FROM amos_operation_actor_capacity WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND actor_kind=$4 AND actor_id=$5`, actorArgs(s)...).Scan(&actor); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT used_bytes FROM amos_operation_workspace_capacity WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND actor_kind=$4 AND actor_id=$5 AND workspace_id=$6`, append(actorArgs(s), s.WorkspaceID)...).Scan(&workspace)
	})
	return actor, workspace, err
}
func requireRuntimeUsage(t *testing.T, ctx context.Context, db *storage.RuntimeDB, s operation.Scope) (int64, int64) {
	t.Helper()
	a, w, err := runtimeUsage(ctx, db, s)
	if err != nil {
		t.Fatal("required scoped usage unavailable")
	}
	return a, w
}

func TestOperationStoreRequiredService(t *testing.T) {
	db, st, ctx := operationRuntime(t)
	s := runtimeScope()
	c := contract()
	req := operation.RequestHash{17}
	// Fail visibly when allocations are missing, inconsistent or not the selected
	// fixture profile. The test does not create or repair any prerequisite.
	if err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		cap, err := lockCapacity(ctx, tx, s)
		if err != nil {
			return err
		}
		if cap.actorLimit != 262144 || cap.workspaceLimit != 131072 {
			return errRuntimeAssertion
		}
		second := s
		second.WorkspaceID = uuid.MustParse("01900000-0000-7000-8000-000000000106")
		cap, err = lockCapacity(ctx, tx, second)
		if err != nil {
			return err
		}
		if cap.workspaceLimit != 65536 {
			return errRuntimeAssertion
		}
		return nil
	}); err != nil {
		t.Fatal("required exact synthetic allocations unavailable")
	}
	var replayID identity.ID
	var replayKey operation.KeyDigest
	var replayBody []byte
	for i, body := range []string{` {"answer":42} `, `"\u0000"`, `1e1000000`, `null`} {
		t.Run("exact_bytes", func(t *testing.T) {
			key := runtimeKey(t)
			id := runtimeID(t)
			a, w := requireRuntimeUsage(t, ctx, db, s)
			err := st.WithTx(ctx, func(tx *sql.Tx) error {
				got, kind, err := st.ClaimTx(ctx, tx, id, s, key, req, c, 1024)
				if err != nil {
					return err
				}
				if kind != operation.ClaimNew || got.ID != id {
					return errRuntimeAssertion
				}
				return st.CompleteTx(ctx, tx, id, operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(body)})
			})
			if err != nil {
				t.Fatal("claim/completion transaction failed")
			}
			afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
			if afterA-a != int64(len(body)) || afterW-w != int64(len(body)) {
				t.Fatal("completion did not shrink both reservations")
			}
			err = st.WithTx(ctx, func(tx *sql.Tx) error {
				got, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, key, req, c, 1)
				if err != nil {
					return err
				}
				if kind != operation.ClaimReplay || got.ID != id || !bytes.Equal(got.Result.CanonicalJSON, []byte(body)) || got.ResultSHA256 != sha256.Sum256([]byte(body)) {
					return errRuntimeAssertion
				}
				got.Result.CanonicalJSON[0] = '!'
				return nil
			})
			if err != nil {
				t.Fatal("exact replay failed")
			}
			finalA, finalW := requireRuntimeUsage(t, ctx, db, s)
			if finalA != afterA || finalW != afterW {
				t.Fatal("replay charged capacity")
			}
			if i == 0 {
				replayID = id
				replayKey = key
				replayBody = []byte(body)
			}
		})
	}
	if replayID == uuid.Nil {
		t.Fatal("required replay control failed")
	}
	t.Run("binding_conflicts", func(t *testing.T) {
		a, w := requireRuntimeUsage(t, ctx, db, s)
		for _, field := range []string{"request", "revision", "descriptor", "input", "output"} {
			changedReq, changed := req, c
			switch field {
			case "request":
				changedReq[1] = 1
			case "revision":
				changed.OperationRevision = "v2"
			case "descriptor":
				changed.DescriptorDigest[1] = 1
			case "input":
				changed.InputSchemaDigest[1] = 1
			case "output":
				changed.OutputSchemaDigest[1] = 1
			}
			err := st.WithTx(ctx, func(tx *sql.Tx) error {
				got, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, replayKey, changedReq, changed, 65536)
				if err != nil {
					return err
				}
				if kind != operation.ClaimConflict || !reflect.DeepEqual(got, operation.Invocation{}) {
					return errRuntimeAssertion
				}
				return nil
			})
			if err != nil {
				t.Fatal("binding conflict failed")
			}
		}
		afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
		if a != afterA || w != afterW {
			t.Fatal("conflict charged capacity")
		}
	})
	t.Run("workspace_and_operation_isolation", func(t *testing.T) {
		for _, change := range []string{"workspace", "operation"} {
			other := s
			if change == "workspace" {
				other.WorkspaceID = uuid.MustParse("01900000-0000-7000-8000-000000000106")
			} else {
				other.OperationID = "storage.synthetic.other"
			}
			id := runtimeID(t)
			err := st.WithTx(ctx, func(tx *sql.Tx) error {
				got, kind, err := st.ClaimTx(ctx, tx, id, other, replayKey, req, c, 128)
				if err != nil {
					return err
				}
				if kind != operation.ClaimNew || got.ID != id {
					return errRuntimeAssertion
				}
				return st.CompleteTx(ctx, tx, id, operation.CachedResult{Kind: operation.ResultCreated, CanonicalJSON: []byte(`{}`)})
			})
			if err != nil {
				t.Fatal("scoped key isolation failed")
			}
		}
	})
	t.Run("rollback_row_and_both_counters", func(t *testing.T) {
		key, id := runtimeKey(t), runtimeID(t)
		a, w := requireRuntimeUsage(t, ctx, db, s)
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			_, kind, err := st.ClaimTx(ctx, tx, id, s, key, req, c, 4096)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			if err := st.CompleteTx(ctx, tx, id, operation.CachedResult{Kind: operation.ResultAccepted, CanonicalJSON: []byte(`{}`)}); err != nil {
				return err
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("callback rollback identity failed")
		}
		afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
		if afterA != a || afterW != w {
			t.Fatal("rollback retained usage")
		}
		err = st.WithTx(ctx, func(tx *sql.Tx) error {
			_, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, key, req, c, 4096)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("rollback retained invocation")
		}
	})
	t.Run("unfinished_commit_rejected", func(t *testing.T) {
		key, id := runtimeKey(t), runtimeID(t)
		a, w := requireRuntimeUsage(t, ctx, db, s)
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			_, kind, err := st.ClaimTx(ctx, tx, id, s, key, req, c, 4096)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			return nil
		})
		if err != ErrUnavailable {
			t.Fatal("unfinished claim committed or wrong failure")
		}
		afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
		if afterA != a || afterW != w {
			t.Fatal("unfinished commit retained usage")
		}
		err = st.WithTx(ctx, func(tx *sql.Tx) error {
			_, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, key, req, c, 4096)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("unfinished invocation survived")
		}
	})
	t.Run("capacity_replay_and_conflict_before_reservation", func(t *testing.T) {
		a, w := requireRuntimeUsage(t, ctx, db, s)
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			exhausted := false
			claims := 0
			for range 4 {
				_, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, runtimeKey(t), req, c, 65536)
				if err != nil {
					return err
				}
				if kind == operation.ClaimCapacityUnavailable {
					exhausted = true
					break
				}
				if kind != operation.ClaimNew {
					return errRuntimeAssertion
				}
				claims++
			}
			if !exhausted || claims == 0 {
				return errRuntimeAssertion
			}
			got, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, replayKey, req, c, 65536)
			if err != nil {
				return err
			}
			if kind != operation.ClaimReplay || !bytes.Equal(got.Result.CanonicalJSON, replayBody) {
				return errRuntimeAssertion
			}
			changed := req
			changed[2] = 1
			got, kind, err = st.ClaimTx(ctx, tx, runtimeID(t), s, replayKey, changed, c, 65536)
			if err != nil {
				return err
			}
			if kind != operation.ClaimConflict || !reflect.DeepEqual(got, operation.Invocation{}) {
				return errRuntimeAssertion
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("capacity ordering failed")
		}
		afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
		if a != afterA || w != afterW {
			t.Fatal("capacity probe retained usage")
		}
	})
	t.Run("concurrent_same_key", func(t *testing.T) {
		key, id := runtimeKey(t), runtimeID(t)
		otherID := runtimeID(t)
		a, w := requireRuntimeUsage(t, ctx, db, s)
		started := make(chan int, 1)
		done := make(chan error, 1)
		startedWorker := false
		earlyResult := false
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			_, kind, err := st.ClaimTx(ctx, tx, id, s, key, req, c, 1024)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			var blockerPID int
			if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				return errRuntimeAssertion
			}
			startedWorker = true
			go func() {
				done <- st.WithTx(ctx, func(other *sql.Tx) error {
					var waitingPID int
					if err := other.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&waitingPID); err != nil {
						return errRuntimeAssertion
					}
					started <- waitingPID
					got, kind, err := st.ClaimTx(ctx, other, otherID, s, key, req, c, 1024)
					if err != nil {
						return err
					}
					if kind != operation.ClaimReplay || got.ID != id || !bytes.Equal(got.Result.CanonicalJSON, []byte(`{}`)) {
						return errRuntimeAssertion
					}
					return nil
				})
			}()
			waitCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			defer stop()
			var waitingPID int
			select {
			case waitingPID = <-started:
			case <-done:
				earlyResult = true
				return errRuntimeAssertion
			case <-waitCtx.Done():
				return ErrUnavailable
			}
			// Observe the second backend blocked by the exact first backend,
			// using the already-held first transaction, never a third pool.
			// PIDs remain local and are not written to evidence or diagnostics.
			tick := time.NewTicker(25 * time.Millisecond)
			defer tick.Stop()
			for {
				var blocked bool
				if err := tx.QueryRowContext(waitCtx, `SELECT $1::integer = ANY(pg_blocking_pids($2::integer))`, blockerPID, waitingPID).Scan(&blocked); err != nil {
					return errRuntimeAssertion
				}
				if blocked {
					break
				}
				select {
				case <-done:
					earlyResult = true
					return errRuntimeAssertion
				case <-tick.C:
				case <-waitCtx.Done():
					return ErrUnavailable
				}
			}
			return st.CompleteTx(ctx, tx, id, operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`{}`)})
		})
		if !startedWorker {
			t.Fatal("concurrent setup failed")
		}
		if earlyResult {
			t.Fatal("same-key contender completed before first transaction")
		}
		select {
		case otherErr := <-done:
			if err != nil || otherErr != nil {
				t.Fatal("same-key serialization failed")
			}
		case <-ctx.Done():
			t.Fatal("same-key serialization timed out")
		}
		afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
		if afterA-a != 2 || afterW-w != 2 {
			t.Fatal("concurrent claim charged more than once")
		}
	})
	t.Run("invalid_completion_and_missing_claim", func(t *testing.T) {
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			if err := st.CompleteTx(ctx, tx, runtimeID(t), operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`{}`)}); err != ErrInvalid {
				return errRuntimeAssertion
			}
			if err := st.CompleteTx(ctx, tx, replayID, operation.CachedResult{Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`{}`)}); err != ErrInvalid {
				return errRuntimeAssertion
			}
			id := runtimeID(t)
			_, kind, err := st.ClaimTx(ctx, tx, id, s, runtimeKey(t), req, c, 2)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			for _, r := range []operation.CachedResult{{Kind: "unknown", CanonicalJSON: []byte(`{}`)}, {Kind: operation.ResultSucceeded}, {Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`{`)}, {Kind: operation.ResultSucceeded, CanonicalJSON: []byte{'"', 255, '"'}}, {Kind: operation.ResultSucceeded, CanonicalJSON: []byte(`"large"`)}} {
				if err := st.CompleteTx(ctx, tx, id, r); err != ErrInvalid {
					return errRuntimeAssertion
				}
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("invalid completion failed")
		}
	})
	t.Run("missing_capacity_and_isolation", func(t *testing.T) {
		missing := s
		missing.WorkspaceID = runtimeID(t)
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			got, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), missing, runtimeKey(t), req, c, 100)
			if err != ErrUnavailable || kind != "" || !reflect.DeepEqual(got, operation.Invocation{}) {
				return errRuntimeAssertion
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("missing capacity did not fail closed")
		}
		err = db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(tx *sql.Tx) error {
			_, _, err := st.ClaimTx(ctx, tx, runtimeID(t), s, runtimeKey(t), req, c, 100)
			if err != ErrInvalid {
				return errRuntimeAssertion
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("wrong isolation accepted")
		}
	})
	for _, invalid := range []struct {
		name string
		body []byte
		code string
	}{
		{"database_invalid_utf8", []byte{'"', 255, '"'}, "22021"},
		{"database_malformed_json", []byte(`{`), "22P02"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			id, key := runtimeID(t), runtimeKey(t)
			a, w := requireRuntimeUsage(t, ctx, db, s)
			err := st.WithTx(ctx, func(tx *sql.Tx) error {
				_, kind, err := st.ClaimTx(ctx, tx, id, s, key, req, c, len(invalid.body))
				if err != nil {
					return err
				}
				if kind != operation.ClaimNew {
					return errRuntimeAssertion
				}
				// Bypass Go validation only for this exact-owned pending row so
				// PostgreSQL's syntax/encoding constraint is actually exercised.
				digest := sha256.Sum256(invalid.body)
				_, err = tx.ExecContext(ctx, completeInvocation, id, "succeeded", invalid.body, digest[:], int64(len(invalid.body)))
				var postgres interface{ SQLState() string }
				if !errors.As(err, &postgres) || postgres.SQLState() != invalid.code {
					return errRuntimeAssertion
				}
				return errRuntimeRollback
			})
			if err != errRuntimeRollback {
				t.Fatal("database result validation did not return expected data exception")
			}
			afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
			if a != afterA || w != afterW {
				t.Fatal("rejected database result retained capacity")
			}
		})
	}
	t.Run("stored_checksum_corruption", func(t *testing.T) {
		key, id := runtimeKey(t), runtimeID(t)
		// Create an exact-owned malformed completed row through the ordinary
		// pending transition, not by changing an immutable completed row or
		// disabling a constraint. SHA-256 is an integrity check, not authority.
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			_, kind, err := st.ClaimTx(ctx, tx, id, s, key, req, c, 2)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			return execOne(ctx, tx, completeInvocation, id, "succeeded", []byte(`{}`), make([]byte, 32), int64(2))
		})
		if err != nil {
			t.Fatal("owned checksum-corruption setup failed")
		}
		a, w := requireRuntimeUsage(t, ctx, db, s)
		for _, changed := range []bool{false, true} {
			request := req
			if changed {
				request[1] = 9
			}
			err = st.WithTx(ctx, func(tx *sql.Tx) error {
				got, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, key, request, c, 100)
				if err != ErrUnavailable || kind != "" || !reflect.DeepEqual(got, operation.Invocation{}) {
					return errRuntimeAssertion
				}
				return errRuntimeRollback
			})
			if err != errRuntimeRollback {
				t.Fatal("stored checksum corruption disclosed a replay or conflict")
			}
		}
		afterA, afterW := requireRuntimeUsage(t, ctx, db, s)
		if a != afterA || w != afterW {
			t.Fatal("corruption rejection charged capacity")
		}
	})
	// A maximal valid string exercises bytea storage without canonical rewriting.
	t.Run("maximum_exact_bytes", func(t *testing.T) {
		body := []byte(`"` + strings.Repeat("x", 65534) + `"`)
		id, key := runtimeID(t), runtimeKey(t)
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			_, kind, err := st.ClaimTx(ctx, tx, id, s, key, req, c, 65536)
			if err != nil {
				return err
			}
			if kind != operation.ClaimNew {
				return errRuntimeAssertion
			}
			if err := st.CompleteTx(ctx, tx, id, operation.CachedResult{Kind: operation.ResultNoContent, CanonicalJSON: body}); err != nil {
				return err
			}
			got, kind, err := st.ClaimTx(ctx, tx, runtimeID(t), s, key, req, c, 1)
			if err != nil {
				return err
			}
			if kind != operation.ClaimReplay || !bytes.Equal(got.Result.CanonicalJSON, body) {
				return errRuntimeAssertion
			}
			return errRuntimeRollback
		})
		if err != errRuntimeRollback {
			t.Fatal("maximum exact byte result failed")
		}
	})
}
