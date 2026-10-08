package materialstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// The operator supplies a separately reviewed W1 fixture with the singleton gate.
// No owner credentials, DDL, or fixture launch capability enter this test.
func deliveryRuntimeDB(t *testing.T) *storage.RuntimeDB {
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

// This is a finite ordering/durable-intent source test, not a native credential
// producer, whole-graph finalizer, mail transport or provider qualification.
// Run once per process: W1 activation is intentionally permanent.
func TestDeliveryWriterRuntimeRequiredService(t *testing.T) {
	db := deliveryRuntimeDB(t)
	root, err := aw.New(db)
	if err != nil {
		t.Fatal(err)
	}
	cfg := materialTxConfig(t)
	writer, err := NewWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := sqlstore.NewTxWriter(sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 3, MaxReconciliationAttempts: 3, MaxLease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := email.NewRenderer(email.RenderConfig{FromAddress: "sender@example.test", ApplicationOrigin: cfg.ApplicationOrigin, MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	// Both constructors must be legal before activating the sole W1 root.
	if err := aw.ActivateW1(root); err != nil {
		t.Fatal(err)
	}
	realm := aw.Realm{Installation: cfg.InstallationID, Application: cfg.ApplicationID, Environment: cfg.EnvironmentID}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM amos_jobs WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `DELETE FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3`, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID)
			return err
		}); err != nil {
			t.Error("exact-owned delivery cleanup failed")
		}
	})
	count := func(t *testing.T, d aw.Delivery, want int) {
		t.Helper()
		if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
			var materials, queued int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, d.MaterialID).Scan(&materials); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM amos_jobs WHERE installation_id=$1 AND application_id=$2 AND idempotency_key=$3`, cfg.InstallationID, cfg.ApplicationID, d.JobKey).Scan(&queued); err != nil {
				return err
			}
			if materials != want || queued != want {
				t.Errorf("durable material/job counts %d/%d; want %d/%d", materials, queued, want, want)
			}
			return nil
		}); err != nil {
			t.Fatal("durable delivery read failed")
		}
	}
	for _, scenario := range []string{"verification", "sign-in", "reset", "rollback", "material replay", "job replay", "duplicate material", "duplicate job", "premature job", "wrong scope", "wrong environment", "wrong reference", "wrong purpose", "wrong key", "wrong request reference", "invalid request", "foreign delivery", "detached context", "expired material", "expired job", "ignored invalid material", "ignored invalid job", "wrong job realm", "missing material writer", "missing job writer", "missing renderer"} {
		t.Run(scenario, func(t *testing.T) {
			id, err := uuid.NewV7()
			if err != nil {
				t.Fatal(err)
			}
			d := aw.Delivery{MaterialID: id, JobKey: "delivery-" + id.String()}
			ref := email.SecretReference("material:" + id.String())
			template, path := email.TemplateVerifyEmail, "/verify-email"
			put := writer.PutVerificationWriter
			if scenario == "sign-in" {
				template, path, put = email.TemplateSignIn, "/magic-link", writer.PutSignInWriter
			}
			if scenario == "reset" {
				template, path, put = email.TemplatePasswordReset, "/reset-password", writer.PutPasswordResetWriter
			}
			material := email.PrivateMaterial{Recipient: "person@example.test", ActionURL: cfg.ApplicationOrigin + path + "?token=synthetic"}
			request := email.Request{Template: template, MaterialRef: ref, ExpiresInSeconds: 60}
			var retained *aw.Attempt
			var savedContext context.Context
			var savedJob jobs.Job
			var originalExpiry time.Time
			succeeds := scenario == "verification" || scenario == "sign-in" || scenario == "reset" || scenario == "material replay" || scenario == "job replay"
			run := func(replay bool) error {
				_, err := root.Run(ctx, func(c context.Context, a *aw.Attempt) aw.Outcome {
					retained, savedContext = a, c
					plan, err := aw.NewPlan(realm, nil, []aw.Delivery{d})
					if err != nil {
						t.Error(err)
						return aw.UnavailableRollback
					}
					if err := a.SealPlan(plan); err != nil {
						t.Error(err)
						return aw.UnavailableRollback
					}
					for phase := aw.P; phase <= aw.W; phase++ {
						if err := a.Acquire(c, phase); err != nil {
							t.Error(err)
							return aw.UnavailableRollback
						}
					}
					started, err := a.StartedAt()
					if err != nil {
						t.Error(err)
						return aw.UnavailableRollback
					}
					if originalExpiry.IsZero() {
						originalExpiry = started.Add(time.Minute)
					}
					expiry := originalExpiry
					enqueue := func(r email.Request, key string, deadline time.Time) (jobs.Job, error) {
						return email.EnqueueWriter(c, a, d, queue, renderer, cfg.InstallationID, cfg.ApplicationID, key, r, deadline)
					}
					fail := func(err error) aw.Outcome {
						if err == nil {
							t.Error("invalid participant call succeeded")
						}
						// Deliberately ignore the error and try the valid material/job steps.
						if e := put(c, a, d, ref, material, expiry); e == nil {
							t.Error("failure left usable material permission")
						}
						if _, e := enqueue(request, d.JobKey, expiry); e == nil {
							t.Error("failure left usable job permission")
						}
						if _, e := a.DrainAndSample(c); e == nil {
							t.Error("failed attempt reached final sample")
						}
						return aw.Success // Deliberately dishonest caller must never commit.
					}
					if scenario == "premature job" {
						_, e := enqueue(request, d.JobKey, expiry)
						return fail(e)
					}
					if scenario == "ignored invalid job" {
						_, e := queue.EnqueueWriter(c, a, d, jobs.Intent{})
						return fail(e)
					}
					activePut := put
					activeRef, activeMaterial, activeDelivery, activeContext, activeExpiry := ref, material, d, c, expiry
					switch scenario {
					case "wrong scope", "wrong environment":
						other := cfg
						otherID, e := uuid.NewV7()
						if e != nil {
							t.Error(e)
							return aw.UnavailableRollback
						}
						if scenario == "wrong scope" {
							other.ApplicationID = otherID
						} else {
							other.EnvironmentID = otherID
						}
						foreign, e := NewWriter(other)
						if e != nil {
							t.Error(e)
							return aw.UnavailableRollback
						}
						activePut = foreign.PutVerificationWriter
					case "wrong reference", "ignored invalid material":
						activeRef = "material:invalid"
					case "wrong purpose":
						activeMaterial.ActionURL = cfg.ApplicationOrigin + "/reset-password?token=synthetic"
					case "foreign delivery":
						activeDelivery.JobKey = "other"
					case "detached context":
						activeContext = context.Background()
					case "missing material writer":
						activePut = (*Writer)(nil).PutVerificationWriter
					case "expired material":
						activeExpiry = started.Add(-time.Minute)
					}
					err = activePut(activeContext, a, activeDelivery, activeRef, activeMaterial, activeExpiry)
					if replay {
						return fail(err)
					}
					switch scenario {
					case "wrong scope", "wrong environment", "wrong reference", "wrong purpose", "foreign delivery", "detached context", "expired material", "ignored invalid material", "missing material writer":
						return fail(err)
					}
					if err != nil {
						t.Error("material insertion failed", err)
						return aw.UnavailableRollback
					}
					if scenario == "missing job writer" {
						_, e := email.EnqueueWriter(c, a, d, nil, renderer, cfg.InstallationID, cfg.ApplicationID, d.JobKey, request, expiry)
						return fail(e)
					}
					if scenario == "missing renderer" {
						_, e := email.EnqueueWriter(c, a, d, queue, nil, cfg.InstallationID, cfg.ApplicationID, d.JobKey, request, expiry)
						return fail(e)
					}
					if scenario == "wrong job realm" {
						_, e := email.EnqueueWriter(c, a, d, queue, renderer, cfg.EnvironmentID, cfg.ApplicationID, d.JobKey, request, expiry)
						return fail(e)
					}
					if scenario == "duplicate material" {
						return fail(put(c, a, d, ref, material, expiry))
					}
					activeRequest, key, deadline := request, d.JobKey, expiry
					switch scenario {
					case "wrong key":
						key = "other"
					case "wrong request reference":
						activeRequest.MaterialRef = "material:invalid"
					case "invalid request":
						activeRequest.Template = "unknown"
					case "expired job":
						deadline = started.Add(-time.Minute)
					}
					job, err := enqueue(activeRequest, key, deadline)
					switch scenario {
					case "wrong key", "wrong request reference", "invalid request", "expired job":
						return fail(err)
					}
					if err != nil {
						t.Error("job insertion failed", err)
						return aw.UnavailableRollback
					}
					if job.State != jobs.StateQueued || job.ID == uuid.Nil || job.Kind != email.Kind || !job.ExternalEffect {
						t.Error("invalid durable intent")
					}
					savedJob = job
					if scenario == "duplicate job" {
						_, e := enqueue(request, key, deadline)
						return fail(e)
					}
					if scenario == "rollback" {
						if err := a.Finish(aw.UnavailableRollback); err != nil {
							t.Error(err)
						}
						return aw.UnavailableRollback
					}
					final, err := a.DrainAndSample(c)
					if err != nil || !final.Before(expiry) || !final.Before(job.Deadline) {
						t.Error("original delivery deadline failed")
						return aw.UnavailableRollback
					}
					if err := a.Finish(aw.Success); err != nil {
						t.Error(err)
						return aw.UnavailableRollback
					}
					return aw.Success
				})
				return err
			}
			err = run(false)
			if succeeds && err != nil {
				t.Fatal("durable delivery failed", err)
			}
			if !succeeds && !errors.Is(err, aw.ErrUnavailable) {
				t.Fatal("invalid delivery did not roll back", err)
			}
			want := 0
			if succeeds {
				want = 1
			}
			count(t, d, want)
			if err := put(savedContext, retained, d, ref, material, time.Now().Add(time.Minute)); !errors.Is(err, ErrUnavailable) {
				t.Fatal("retained material writer admitted")
			}
			if _, err := email.EnqueueWriter(savedContext, retained, d, queue, renderer, cfg.InstallationID, cfg.ApplicationID, d.JobKey, request, time.Now().Add(time.Minute)); !errors.Is(err, email.ErrUnavailable) {
				t.Fatal("retained job writer admitted")
			}
			if scenario == "job replay" {
				original := savedJob.ID
				// Simulate exact-owned material-only maintenance, without an
				// identity/workspace lock or a schema change. The queued intent
				// remains, so the new rooted D path must reuse its exact hash.
				if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, `DELETE FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, d.MaterialID)
					return err
				}); err != nil {
					t.Fatal("exact-owned material maintenance failed")
				}
				if err := run(false); err != nil {
					t.Fatal("stable job replay failed", err)
				}
				if savedJob.ID != original {
					t.Fatal("idempotent replay created another job")
				}
				count(t, d, 1)
			}
			if scenario == "material replay" {
				original := savedJob.ID
				if err := run(true); !errors.Is(err, aw.ErrUnavailable) {
					t.Fatal("duplicate durable material replay admitted", err)
				}
				if savedJob.ID != original {
					t.Fatal("replay replaced durable job")
				}
				count(t, d, 1)
			}
		})
	}
}
