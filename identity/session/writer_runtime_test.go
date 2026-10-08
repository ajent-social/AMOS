package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// These required-service tests use the separately provisioned eleven-field
// writer fixture. They perform no DDL and never reset the process profile.
func writerRuntimeDB(t *testing.T) *storage.RuntimeDB {
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
	if os.Getenv("AMOS_WRITER_RUNTIME_TEST_CONFIG") == "" {
		t.Fatal("required TLS runtime fixture absent: AMOS_WRITER_RUNTIME_TEST_CONFIG")
	}
	if os.Getenv("AMOS_IDENTITY_WRITER_TEST_CHILD") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(executable, "-test.run=^TestWriterRuntimeRequiredService$", "-test.v", "-test.count=1")
		command.Env = append(os.Environ(), "AMOS_IDENTITY_WRITER_TEST_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated writer test failed: %v\n%s", err, output)
		}
		t.Log(string(output))
		return
	}
	db := writerRuntimeDB(t)
	root, err := aw.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = aw.ActivateW1(root); err != nil {
		t.Fatal(err)
	}
	cfg := writerConfig(t)
	cfg.PersistAssurance = true
	service, err := NewWithWriter(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.New(&sql.Tx{}); err == nil {
		t.Fatal("Legacy store entered W1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	realm := aw.Realm{Installation: cfg.InstallationID, Application: cfg.ApplicationID, Environment: cfg.EnvironmentID}
	t.Run("registration ordered and live pending binding", func(t *testing.T) { writerRegistration(t, ctx, root, realm) })
	person, email, credential := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	var verified time.Time
	err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if e := tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&verified); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO public.identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, cfg.InstallationID, cfg.ApplicationID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO public.identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,'writer@example.test','writer@example.test',$5)`, email, person, cfg.InstallationID, cfg.ApplicationID, verified); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO public.identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password','synthetic-stored-verifier')`, credential, person)
		return e
	})
	if err != nil {
		t.Fatal("synthetic native-session seed failed")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err := db.WithTx(cleanup, nil, func(tx *sql.Tx) error {
			for _, query := range []string{`DELETE FROM public.identity_sessions WHERE person_id=$1`, `DELETE FROM public.identity_credentials WHERE person_id=$1`, `DELETE FROM public.identity_emails WHERE person_id=$1`, `DELETE FROM public.identity_persons WHERE id=$1`} {
				if _, e := tx.ExecContext(cleanup, query, person); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			t.Error("exact synthetic rows cleanup failed")
		}
	})

	t.Run("writer store cannot issue even with acquired session", func(t *testing.T) {
		id := uuid.Must(uuid.NewV7())
		plan, e := aw.NewPlan(realm, []aw.Row{{Table: aw.Persons, ID: person, Access: aw.ExistingUpdate}, {Table: aw.Sessions, ID: id, Access: aw.ReservedInsert}}, nil)
		if e != nil {
			t.Fatal(e)
		}
		completion, e := root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
			if e := a.SealPlan(plan); e != nil {
				t.Error(e)
				return aw.UnavailableRollback
			}
			for phase := aw.P; phase <= aw.W; phase++ {
				if e := a.Acquire(ctx, phase); e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
			}
			st, e := store.NewWriter(a)
			if e != nil {
				t.Error(e)
				return aw.UnavailableRollback
			}
			if e = st.CreateSession(ctx, store.Session{ID: id, PersonID: person, InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, TokenDigest: make([]byte, 32), AuthenticationMethod: "email_password", AuthenticatedAt: verified, ExpiresAt: verified.Add(time.Hour), AssuranceLevel: "aal1", AssuranceExpires: verified.Add(time.Hour)}); e == nil {
				t.Error("ordering-only store issued")
			}
			if _, e = a.DrainAndSample(ctx); e == nil {
				t.Error("ignored failed issuance remained usable")
			}
			return aw.UnavailableRollback
		})
		if e == nil {
			t.Fatal("ordering-only issuer committed")
		}
		if _, e = completion.Outcome(); e == nil {
			t.Fatal("failed issuance returned completion")
		}
	})
	t.Run("password reads only acquired exact credential and contact", func(t *testing.T) {
		plan, e := aw.NewPlan(realm, []aw.Row{{Table: aw.Persons, ID: person, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: email, Access: aw.ExistingUpdate}, {Table: aw.Credentials, ID: credential, Access: aw.ExistingUpdate}}, nil)
		if e != nil {
			t.Fatal(e)
		}
		_, e = root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
			if e := a.SealPlan(plan); e != nil {
				t.Error(e)
				return aw.UnavailableRollback
			}
			for phase := aw.P; phase <= aw.C; phase++ {
				if e := a.Acquire(ctx, phase); e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
			}
			st, e := store.NewWriter(a)
			if e != nil {
				t.Error(e)
				return aw.UnavailableRollback
			}
			hash, e := st.FindCurrentPassword(ctx, store.SessionScope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID}, person, 0)
			if e != nil || hash != "synthetic-stored-verifier" {
				t.Error("planned password read failed", e)
				return aw.UnavailableRollback
			}
			if e = a.Finish(aw.DeniedRollback); e != nil {
				t.Error(e)
				return aw.UnavailableRollback
			}
			return aw.DeniedRollback
		})
		if !errors.Is(e, aw.ErrDenied) {
			t.Fatal(e)
		}
	})
	var issued Issued
	for _, mode := range []string{"success", "cancel after finish", "staging replay", "action mismatch", "disabled person"} {
		t.Run("staging "+mode, func(t *testing.T) {
			caseCtx, stop := context.WithCancel(ctx)
			defer stop()
			requestHTTP := httptest.NewRequest(http.MethodPost, "https://example.test/login", nil).WithContext(caseCtx)
			request, e := service.AdmitWriterRequest(requestHTTP)
			if e != nil {
				t.Fatal(e)
			}
			var staged Staged
			var permit wp.Permit
			completion, e := root.Run(requestHTTP.Context(), func(ctx context.Context, a *aw.Attempt) aw.Outcome {
				fail := func(e error) aw.Outcome { t.Error(e); return aw.UnavailableRollback }
				action := wp.PasswordSignIn
				if mode == "action mismatch" {
					action = wp.MagicConfirm
				}
				rows, e := service.DiscoverPrior(ctx, a, request, action)
				if e != nil {
					return fail(e)
				}
				rows = append(rows, aw.Row{Table: aw.Persons, ID: person, Access: aw.ExistingUpdate}, aw.Row{Table: aw.Emails, ID: email, Access: aw.ExistingUpdate}, aw.Row{Table: aw.Credentials, ID: credential, Access: aw.ExistingUpdate})
				plan, e := aw.NewPlan(realm, rows, nil)
				if e != nil {
					return fail(e)
				}
				if e = a.SealPlan(plan); e != nil {
					return fail(e)
				}
				for phase := aw.P; phase <= aw.W; phase++ {
					if e = a.Acquire(ctx, phase); e != nil {
						return fail(e)
					}
				}
				tx, e := a.ParticipantTx(ctx, aw.C, []aw.Row{{Table: aw.Credentials, ID: credential, Access: aw.ExistingUpdate}})
				if e != nil {
					return fail(e)
				}
				var v time.Time
				if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&v); e != nil {
					return fail(e)
				}
				// Synthetic evidence explicitly does not qualify password verification.
				evidence, e := wp.Password(a, wp.PasswordSignIn, wp.PasswordCheck{Subject: wp.Subject{Person: person, Realm: realm, Epoch: 0}, Contact: wp.Contact{ID: email, ComparisonKey: "writer@example.test", VerifiedAt: verified}, CredentialID: credential, VerifiedHash: "synthetic-stored-verifier", VerifiedAt: v, ValidUntil: v.Add(15 * time.Minute)})
				if e != nil {
					return fail(e)
				}
				issuance, e := wp.ForIssue(a, evidence)
				if e != nil {
					return fail(e)
				}
				if _, e = store.NewIssuer(a, issuance); e != nil {
					return fail(e)
				}
				if mode == "disabled person" {
					// Synthetic same-transaction state change tests the native terminal mapping.
					if e = a.RecordMutation(aw.CredentialWrite, []aw.Row{{Table: aw.Persons, ID: person, Access: aw.ExistingUpdate}}); e != nil {
						return fail(e)
					}
					if _, e = tx.ExecContext(ctx, `UPDATE identity_persons SET state='self_disabled' WHERE id=$1`, person); e != nil {
						return fail(e)
					}
				}
				staged, e = service.StageWriter(ctx, a, issuance, request)
				if mode == "disabled person" {
					if !errors.Is(e, ErrUnauthenticated) {
						t.Error("terminal semantic denial misclassified", e)
					}
					return aw.DeniedRollback
				}
				if mode == "action mismatch" {
					if e == nil {
						t.Error("wrong action issued")
					}
					return aw.UnavailableRollback
				}
				if e != nil {
					return fail(e)
				}
				var initialIdle time.Time
				if e = tx.QueryRowContext(ctx, `SELECT idle_expires_at FROM identity_sessions WHERE id=$1`, request.data.newID).Scan(&initialIdle); e != nil {
					return fail(e)
				}
				// SQL timestamps have microsecond precision; V came from this DB.
				if !initialIdle.Equal(v.Add(30 * time.Minute)) {
					return fail(errors.New("initial idle bound refreshed after verification"))
				}
				if e = issuance.Check(a); e != nil {
					return fail(e)
				}
				if mode == "staging replay" {
					if _, e = service.StageWriter(ctx, a, issuance, request); e == nil {
						t.Error("staging replay admitted")
					}
					return aw.UnavailableRollback
				}
				if got, e := service.PublishWriter(aw.Completion{}, wp.Permit{}, staged); e == nil || got.Cookie != nil {
					return fail(errors.New("precommit publication"))
				}
				f, e := a.DrainAndSample(ctx)
				if e != nil {
					return fail(e)
				}
				permit, e = wp.Finalize(a, evidence, f)
				if e != nil {
					return fail(e)
				}
				if e = a.Finish(aw.Success); e != nil {
					return fail(e)
				}
				if mode == "cancel after finish" {
					stop()
				}
				return aw.Success
			})

			if mode == "disabled person" && !errors.Is(e, aw.ErrDenied) {
				t.Error("terminal participant outcome lost", e)
			}
			if mode != "success" {
				if e == nil {
					t.Fatal("failed attempt committed")
				}
				if got, e := service.PublishWriter(completion, permit, staged); e == nil || got.Cookie != nil || got.CSRFToken != "" {
					t.Fatal("failed attempt disclosed")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if got, e := service.PublishWriter(completion, wp.Permit{}, staged); e == nil || got.Cookie != nil {
				t.Fatal("wrong permit disclosed")
			}
			issued, e = service.PublishWriter(completion, permit, staged)
			if e != nil || issued.Cookie == nil || issued.CSRFToken == "" {
				t.Fatalf("publish: %v", e)
			}
			if got, e := service.PublishWriter(completion, permit, staged); e == nil || got.Cookie != nil {
				t.Fatal("replay disclosed")
			}
		})
	}
	if issued.Cookie == nil {
		t.Fatal("staging prerequisite missing")
	}
	t.Run("native middleware private reader and no mutation", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "https://example.test/protected", nil).WithContext(ctx)
		request.AddCookie(issued.Cookie)
		response := httptest.NewRecorder()
		called := false
		service.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			admitted, ok := identity.PrincipalFromContext(r.Context())
			if !ok || admitted.PersonID() != person {
				t.Error("missing native principal")
				return
			}
			e := root.Read(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
				first, e := service.RecheckCurrentTx(ctx, tx)
				if e != nil {
					return e
				}
				second, e := service.RecheckCurrentTx(ctx, tx)
				if e != nil {
					return e
				}
				if first != second {
					return errors.New("reader changed immutable principal")
				}
				return nil
			})
			if e != nil {
				t.Error(e)
			}
			other, e := NewWithWriter(root, cfg)
			if e != nil {
				t.Error(e)
				return
			}
			if _, e = other.RecheckCurrentTx(r.Context(), &sql.Tx{}); !errors.Is(e, ErrUnauthenticated) {
				t.Error("cross instance reader admitted")
			}
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(response, request)
		if !called || response.Code != http.StatusNoContent {
			t.Fatalf("native middleware status %d", response.Code)
		}
	})
	t.Run("duplicate cookies never reach handler", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "https://example.test/protected", nil).WithContext(ctx)
		request.AddCookie(issued.Cookie)
		request.AddCookie(issued.Cookie)
		response := httptest.NewRecorder()
		service.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("duplicate reached handler") })).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", response.Code)
		}
	})
	t.Run("native signout revocation", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "https://example.test/signout", nil).WithContext(ctx)
		request.AddCookie(issued.Cookie)
		request.Header.Set("Origin", "https://example.test")
		request.Header.Set("X-CSRF-Token", issued.CSRFToken)
		response := httptest.NewRecorder()
		service.SignOut(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("signout %d", response.Code)
		}
		request = httptest.NewRequest(http.MethodGet, "https://example.test/protected", nil).WithContext(ctx)
		request.AddCookie(issued.Cookie)
		response = httptest.NewRecorder()
		service.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("revoked reached handler") })).ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("revoked %d", response.Code)
		}
	})
}
func writerRegistration(t *testing.T, ctx context.Context, root *aw.Root, realm aw.Realm) {
	t.Helper()
	v := store.PendingAccount{PersonID: uuid.Must(uuid.NewV7()), EmailID: uuid.Must(uuid.NewV7()), CredentialID: uuid.Must(uuid.NewV7()), ChallengeID: uuid.Must(uuid.NewV7()), InstallationID: realm.Installation, ApplicationID: realm.Application, EmailAddress: "registration@example.test", PasswordHash: "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ChallengeDigest: make([]byte, 32)}
	var pending store.PendingRegistration
	var attempt *aw.Attempt
	_, err := root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		attempt = a
		fail := func(e error) aw.Outcome { t.Error(e); return aw.UnavailableRollback }
		b, e := a.StartedAt()
		if e != nil {
			return fail(e)
		}
		v.ChallengeExpiry = b.Add(time.Hour)
		plan, e := aw.NewPlan(realm, []aw.Row{{Table: aw.Persons, ID: v.PersonID, Access: aw.ReservedInsert}, {Table: aw.Emails, ID: v.EmailID, Access: aw.ReservedInsert}, {Table: aw.Credentials, ID: v.CredentialID, Access: aw.ReservedInsert}, {Table: aw.Challenges, ID: v.ChallengeID, Access: aw.ReservedInsert}}, nil)
		if e != nil {
			return fail(e)
		}
		if e = a.SealPlan(plan); e != nil {
			return fail(e)
		}
		if e = a.Acquire(ctx, aw.P); e != nil {
			return fail(e)
		}
		st, e := store.NewWriter(a)
		if e != nil {
			return fail(e)
		}
		pending, e = st.CreatePendingRegistration(ctx, v)
		if e != nil {
			return fail(e)
		}
		if !pending.InAttempt(a) || pending.InTransaction(&sql.Tx{}) {
			return fail(errors.New("pending binding mismatch"))
		}
		if e = a.Finish(aw.DeniedRollback); e != nil {
			return fail(e)
		}
		return aw.DeniedRollback
	})
	if !errors.Is(err, aw.ErrDenied) {
		t.Fatalf("registration rollback: %v", err)
	}
	if pending.InAttempt(attempt) {
		t.Fatal("closed registration retained authority")
	}
}
