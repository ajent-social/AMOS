package authoritywriter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
)

// The operator supplies a separately reviewed W1 fixture with the singleton gate.
// No owner credentials, DDL, or fixture launch capability enter this test.
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

func TestWriterRuntimeRequiredService(t *testing.T) {
	db := runtimeDB(t)
	root, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = ActivateW1(root); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	realm := testRealm(t)
	person := testID(t)
	cleanup := func() {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM public.identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3`, person, realm.Installation, realm.Application)
			return err
		}); err != nil {
			t.Error("exact-owned person cleanup failed")
		}
	}
	t.Cleanup(cleanup)
	var retained *Attempt
	var binding Binding
	t.Run("committed reserved insert and terminal release", func(t *testing.T) {
		row := Row{Persons, person, ReservedInsert}
		plan, err := NewPlan(realm, []Row{row}, nil)
		if err != nil {
			t.Fatal(err)
		}
		completion, err := root.Run(ctx, func(ctx context.Context, a *Attempt) Outcome {
			retained = a
			started, startErr := a.StartedAt()
			if startErr != nil || started.IsZero() {
				t.Error("root database start unavailable")
				return UnavailableRollback
			}
			if err := a.SealPlan(plan); err != nil {
				t.Error(err)
				return UnavailableRollback
			}
			copied, copyErr := a.PlannedRows(Persons)
			if copyErr != nil || len(copied) != 1 || copied[0] != row {
				t.Error("sealed inventory unavailable")
				return UnavailableRollback
			}
			copied[0].ID = testID(t)
			original, copyErr := a.PlannedRows(Persons)
			if copyErr != nil || len(original) != 1 || original[0] != row {
				t.Error("inventory copy mutated sealed plan")
				return UnavailableRollback
			}
			if empty, e := a.PlannedRows(Sessions); e != nil || len(empty) != 0 {
				t.Error("empty planned table malformed")
				return UnavailableRollback
			}
			binding, err = a.Binding()
			if err != nil {
				t.Error(err)
				return UnavailableRollback
			}
			if err := a.Acquire(ctx, P); err != nil {
				t.Error(err)
				return UnavailableRollback
			}
			if err := a.CheckRows(P, []Row{row}); err != nil {
				t.Error("held row inspection failed", err)
				return UnavailableRollback
			}
			tx, err := a.ParticipantTx(ctx, P, []Row{row})
			if err != nil {
				t.Error(err)
				return UnavailableRollback
			}
			if err := a.RecordMutation(RegistrationWrite, []Row{row}); err != nil {
				t.Error(err)
				return UnavailableRollback
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO public.identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, realm.Installation, realm.Application); err != nil {
				t.Error("reserved person insert failed")
				return UnavailableRollback
			}
			for phase := C; phase <= W; phase++ {
				if err := a.Acquire(ctx, phase); err != nil {
					t.Error(err)
					return UnavailableRollback
				}
			}
			now, err := a.DrainAndSample(ctx)
			if err != nil || now.Before(started) || !a.IsFinalSample(now) || a.IsFinalSample(now.Add(time.Nanosecond)) {
				t.Error("exact final sample unavailable")
				return UnavailableRollback
			}
			if err := a.Finish(Success); err != nil {
				t.Error(err)
				return UnavailableRollback
			}
			return Success
		})
		if err != nil {
			t.Fatal("reserved insertion root failed", err)
		}
		release, err := completion.TakeRelease(binding)
		if err != nil || !release.Matches(binding) {
			t.Fatal("committed release unavailable")
		}
		if _, err := completion.TakeRelease(binding); err == nil {
			t.Fatal("completion replay admitted")
		}
		if _, err := retained.Realm(); !errors.Is(err, ErrClosed) {
			t.Fatal("retained attempt reopened")
		}
	})
	row := Row{Persons, person, ExistingUpdate}
	for _, missing := range []bool{true, false} {
		name := "occupied reserved row denies"
		expected := Row{Persons, person, ReservedInsert}
		if missing {
			name = "missing existing row denies"
			expected = Row{Persons, testID(t), ExistingUpdate}
		}
		t.Run(name, func(t *testing.T) {
			denialPlan, e := NewPlan(realm, []Row{expected}, nil)
			if e != nil {
				t.Fatal(e)
			}
			completion, e := root.Run(ctx, func(ctx context.Context, a *Attempt) Outcome {
				if e := a.SealPlan(denialPlan); e != nil {
					t.Error(e)
					return UnavailableRollback
				}
				if e := a.Acquire(ctx, P); !errors.Is(e, ErrDenied) {
					t.Error("expected row semantic denial", e)
					return UnavailableRollback
				}
				if _, e := a.ParticipantTx(ctx, P, []Row{expected}); !errors.Is(e, ErrDenied) {
					t.Error("denied attempt regained SQL", e)
				}
				if e := a.Finish(DeniedRollback); e != nil {
					t.Error("semantic rollback could not finish", e)
					return UnavailableRollback
				}
				return DeniedRollback
			})
			if !errors.Is(e, ErrDenied) {
				t.Fatal("missing or occupied planned row was not denied", e)
			}
			if _, e := completion.Outcome(); e == nil {
				t.Fatal("semantic rollback returned completion")
			}
		})
	}
	plan, err := NewPlan(realm, []Row{row}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"denied", "missing finish", "panic", "cancel", "phase poison", "wrong context", "unplanned row", "premature success", "duplicate finish", "mutation after F", "inspection wrong row", "inspection canceled", "inspection at F", "inventory canceled", "inventory wrong table"} {
		t.Run(mode, func(t *testing.T) {
			request, stop := context.WithCancel(ctx)
			defer stop()
			completion, err := root.Run(request, func(ctx context.Context, a *Attempt) Outcome {
				if err := a.SealPlan(plan); err != nil {
					t.Error(err)
					return UnavailableRollback
				}
				for phase := P; phase <= W; phase++ {
					if err := a.Acquire(ctx, phase); err != nil {
						t.Error(err)
						return UnavailableRollback
					}
				}
				tx, e := a.ParticipantTx(ctx, P, []Row{row})
				if e != nil {
					t.Error(e)
					return UnavailableRollback
				}
				if e = a.RecordMutation(CredentialWrite, []Row{row}); e != nil {
					t.Error(e)
					return UnavailableRollback
				}
				if _, e = tx.ExecContext(ctx, `UPDATE public.identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, person); e != nil {
					t.Error("test epoch update failed")
					return UnavailableRollback
				}
				switch mode {
				case "denied":
					if e = a.Finish(DeniedRollback); e != nil {
						t.Error(e)
					}
					return DeniedRollback
				case "missing finish":
					return Success
				case "panic":
					panic("synthetic callback panic")
				case "cancel":
					stop()
					return Success
				case "phase poison":
					if a.Acquire(ctx, P) == nil {
						t.Error("reverse acquisition accepted")
					}
				case "wrong context":
					if _, e = a.ParticipantTx(context.Background(), P, []Row{row}); e == nil {
						t.Error("unrooted context admitted")
					}
				case "unplanned row":
					if _, e = a.ParticipantTx(ctx, P, []Row{{Persons, testID(t), ExistingUpdate}}); e == nil {
						t.Error("unplanned row admitted")
					}
				case "inspection wrong row":
					if a.CheckRows(P, []Row{{Persons, testID(t), ExistingUpdate}}) == nil {
						t.Error("unplanned inspection admitted")
					}
				case "inspection canceled":
					stop()
					if a.CheckRows(P, []Row{row}) == nil {
						t.Error("canceled original request inspected rows")
					}
				case "inventory canceled":
					stop()
					if _, e := a.PlannedRows(Persons); e == nil {
						t.Error("canceled inventory admitted")
					}
				case "inventory wrong table":
					if _, e := a.PlannedRows(Table(255)); e == nil {
						t.Error("unknown table inventory admitted")
					}
				case "inspection at F":
					if _, e := a.DrainAndSample(ctx); e != nil {
						t.Error(e)
						return UnavailableRollback
					}
					if a.CheckRows(P, []Row{row}) == nil {
						t.Error("F reopened row inspection")
					}
				case "premature success":
					if a.Finish(Success) == nil {
						t.Error("success before F accepted")
					}
				case "duplicate finish", "mutation after F":
					if _, e := a.DrainAndSample(ctx); e != nil {
						t.Error(e)
						return UnavailableRollback
					}
					if mode == "mutation after F" {
						if a.RecordMutation(ContactWrite, []Row{row}) == nil {
							t.Error("mutation after final sample accepted")
						}
					} else {
						if e := a.Finish(Success); e != nil {
							t.Error(e)
						}
						if a.Finish(Success) == nil {
							t.Error("duplicate finish accepted")
						}
					}
				}
				return Success
			})
			if err == nil {
				t.Fatal("invalid completion committed")
			}
			if _, e := completion.Outcome(); e == nil {
				t.Fatal("failure returned completion")
			}
			if mode == "denied" && !errors.Is(err, ErrDenied) {
				t.Fatal("semantic denial lost")
			}
			var epoch int64
			if err := root.Read(ctx, func(ctx context.Context, tx *sql.Tx) error {
				return tx.QueryRowContext(ctx, `SELECT security_epoch FROM public.identity_persons WHERE id=$1`, person).Scan(&epoch)
			}); err != nil || epoch != 0 {
				t.Fatal("failed root exposed staged epoch", epoch, err)
			}
		})
	}
	t.Run("gate privileges and actual mode", func(t *testing.T) {
		for _, statement := range []string{
			`INSERT INTO public.identity_writer_gate(id) VALUES(1)`,
			`DELETE FROM public.identity_writer_gate WHERE id=1`,
			`TRUNCATE public.identity_writer_gate`,
			`UPDATE public.identity_writer_gate SET id=1 WHERE id=1`,
			`ALTER TABLE public.identity_writer_gate ADD COLUMN forbidden integer`,
		} {
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, statement); return err })
			var state interface{ SQLState() string }
			if !errors.As(err, &state) || state.SQLState() != "42501" {
				t.Fatal("gate operation did not fail with insufficient privilege")
			}
		}
		err := db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: false}, func(tx *sql.Tx) error {
			if checkMode(ctx, tx) == nil {
				t.Error("unsupported isolation accepted")
			}
			return ErrDenied
		})
		if !errors.Is(err, ErrDenied) {
			t.Fatal("mode fixture transaction failed")
		}
	})
	t.Run("gate wait cancellation excludes callback", func(t *testing.T) {
		held := make(chan struct{})
		release := make(chan struct{})
		holderDone := make(chan error, 1)
		go func() {
			holderDone <- db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				var id int
				if err := tx.QueryRowContext(ctx, `SELECT id FROM public.identity_writer_gate WHERE id=1 FOR UPDATE`).Scan(&id); err != nil {
					return err
				}
				close(held)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		select {
		case <-held:
		case err := <-holderDone:
			t.Fatal("gate holder failed", err)
		case <-ctx.Done():
			t.Fatal("gate holder deadline")
		}
		waitCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stop()
		called := false
		_, err := root.Run(waitCtx, func(context.Context, *Attempt) Outcome { called = true; return Success })
		close(release)
		holderErr := <-holderDone
		if err == nil || called || holderErr != nil {
			t.Fatal("blocked gate admitted callback or holder failed")
		}
	})
}
