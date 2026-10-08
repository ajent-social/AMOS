package writerproof

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
)

// The separately reviewed writer profile is owned by a different serial operator.
// This test exercises real root/evidence lifecycle using synthetic stored rows;
// it does not qualify a native password verification or session producer.
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

func TestEvidenceRuntimeRequiredService(t *testing.T) {
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
	sub := subject(t)
	emailID, credentialID := id(t), id(t)
	var verified time.Time
	err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if e := tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&verified); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO public.identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, sub.Person, sub.Realm.Installation, sub.Realm.Application); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO public.identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,'proof@example.test','proof@example.test',$5)`, emailID, sub.Person, sub.Realm.Installation, sub.Realm.Application, verified); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO public.identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password','synthetic-stored-verifier')`, credentialID, sub.Person)
		return e
	})
	if err != nil {
		t.Fatal("synthetic evidence rows setup failed")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err := db.WithTx(cleanup, nil, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(cleanup, `DELETE FROM public.identity_credentials WHERE id=$1 AND person_id=$2`, credentialID, sub.Person); e != nil {
				return e
			}
			if _, e := tx.ExecContext(cleanup, `DELETE FROM public.identity_emails WHERE id=$1 AND person_id=$2`, emailID, sub.Person); e != nil {
				return e
			}
			_, e := tx.ExecContext(cleanup, `DELETE FROM public.identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3`, sub.Person, sub.Realm.Installation, sub.Realm.Application)
			return e
		})
		if err != nil {
			t.Error("exact synthetic evidence rows cleanup failed")
		}
	})
	plan, err := aw.NewPlan(sub.Realm, []aw.Row{personRow(sub), {Table: aw.Emails, ID: emailID, Access: aw.ExistingUpdate}, {Table: aw.Credentials, ID: credentialID, Access: aw.ExistingUpdate}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "duplicate issue", "duplicate credential", "duplicate finalizer", "no credential", "wrong final time", "nonissuing primary", "backward verification"} {
		t.Run(mode, func(t *testing.T) {
			var permit Permit
			var issuance Issuance
			var attempt *aw.Attempt
			var binding aw.Binding
			completion, err := root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
				attempt = a
				if err := a.SealPlan(plan); err != nil {
					t.Error(err)
					return aw.UnavailableRollback
				}
				binding, err = a.Binding()
				if err != nil {
					t.Error(err)
					return aw.UnavailableRollback
				}
				for phase := aw.P; phase <= aw.W; phase++ {
					if err := a.Acquire(ctx, phase); err != nil {
						t.Error(err)
						return aw.UnavailableRollback
					}
				}
				tx, e := a.ParticipantTx(ctx, aw.C, []aw.Row{{Table: aw.Credentials, ID: credentialID, Access: aw.ExistingUpdate}})
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				var v time.Time
				if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&v); e != nil {
					t.Error("verification sample failed")
					return aw.UnavailableRollback
				}
				if mode == "backward verification" {
					v = v.Add(time.Minute)
				} // synthetic malformed snapshot, not a changed DB clock
				action := PasswordSignIn
				if mode == "nonissuing primary" {
					action = PasswordChange
				}
				evidence, e := Password(a, action, PasswordCheck{Subject: sub, Contact: Contact{ID: emailID, ComparisonKey: "proof@example.test", VerifiedAt: verified}, CredentialID: credentialID, VerifiedHash: "synthetic-stored-verifier", VerifiedAt: v, ValidUntil: v.Add(15 * time.Minute)})
				if e != nil {
					t.Error("evidence construction failed", e)
					return aw.UnavailableRollback
				}
				issuance, e = ForIssue(a, evidence)
				if mode == "nonissuing primary" {
					if e == nil {
						t.Error("password-change primary issued a credential")
					}
					if e = a.Finish(aw.DeniedRollback); e != nil {
						t.Error(e)
					}
					return aw.DeniedRollback
				}
				if e != nil {
					t.Error("issuance construction failed", e)
					return aw.UnavailableRollback
				}
				if mode == "duplicate issue" {
					if _, e = ForIssue(a, evidence); e == nil {
						t.Error("duplicate issuance admitted")
					}
					return aw.UnavailableRollback
				}
				if e = issuance.Check(a); e != nil {
					t.Error("nonconsuming issuance check failed", e)
					return aw.UnavailableRollback
				}
				if e = issuance.CheckAction(a, PasswordSignIn); e != nil {
					t.Error("matching action rejected", e)
					return aw.UnavailableRollback
				}
				if e = issuance.CheckAction(a, MagicConfirm); !errors.Is(e, ErrUnavailable) {
					t.Error("wrong request action admitted", e)
					return aw.UnavailableRollback
				}
				if mode != "no credential" {
					credential, e := issuance.Credential(a)
					if e != nil || credential.PersonID() != sub.Person || credential.Method() != "email_password" || !credential.AuthenticatedAt().Equal(v) {
						t.Error("credential translation mismatch")
						return aw.UnavailableRollback
					}
					if e = issuance.Check(a); e != nil {
						t.Error("Check consumed or rejected one-use getter", e)
						return aw.UnavailableRollback
					}
				}
				if mode == "duplicate credential" {
					if _, e = issuance.Credential(a); e == nil {
						t.Error("credential getter replay admitted")
					}
					return aw.UnavailableRollback
				}
				f, e := a.DrainAndSample(ctx)
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				if mode == "wrong final time" {
					f = f.Add(time.Microsecond)
				}
				permit, e = Finalize(a, evidence, f)
				if mode == "no credential" || mode == "wrong final time" || mode == "backward verification" {
					if e == nil {
						t.Error("invalid finalization admitted")
					}
					if mode == "backward verification" && !errors.Is(e, ErrUnavailable) {
						t.Error("backward verification chronology was not unavailable")
					}
					if e = a.Finish(aw.UnavailableRollback); e != nil {
						t.Error(e)
					}
					return aw.UnavailableRollback
				}
				if e != nil {
					t.Error("finalization failed", e)
					return aw.UnavailableRollback
				}
				if mode == "duplicate finalizer" {
					if _, e = Finalize(a, evidence, f); e == nil {
						t.Error("duplicate finalization admitted")
					}
					return aw.UnavailableRollback
				}
				if e = a.Finish(aw.Success); e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				return aw.Success
			})
			if mode != "success" {
				if err == nil {
					t.Fatal("failed evidence attempt committed")
				}
				if mode == "nonissuing primary" && !errors.Is(err, aw.ErrDenied) {
					t.Fatal("nonissuing denial mapping lost", err)
				}
				if _, e := completion.Outcome(); e == nil {
					t.Fatal("failed evidence returned completion")
				}
				return
			}
			if err != nil {
				t.Fatal("valid evidence attempt failed", err)
			}
			if !permit.Matches(attempt, issuance) {
				t.Fatal("terminal permit lost staged issuance identity")
			}
			release, e := completion.TakeRelease(binding)
			if e != nil || !permit.MatchesRelease(release) {
				t.Fatal("permit did not match committed release")
			}
			if issuance.Check(attempt) == nil {
				t.Fatal("closed issuance remained live")
			}
			if _, e := issuance.Credential(attempt); e == nil {
				t.Fatal("closed credential getter reopened")
			}
			if !permit.MatchesRelease(release) {
				t.Fatal("live-use rejection invalidated terminal snapshot")
			}
		})
	}
}
