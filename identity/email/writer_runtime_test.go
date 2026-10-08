package email

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
	"io"
	"os"
	"os/exec"
	"strings"
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

func TestEmailWriterRequiredService(t *testing.T) {
	if os.Getenv("AMOS_EMAIL_WRITER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestEmailWriterRequiredService$", "-test.v")
		cmd.Env = append(os.Environ(), "AMOS_EMAIL_WRITER_CHILD=1")
		out, e := cmd.CombinedOutput()
		t.Log(string(out))
		if e != nil {
			t.Fatal("required email writer subprocess failed")
		}
		return
	}
	db := runtimeDB(t)
	root, e := aw.New(db)
	if e != nil {
		t.Fatal(e)
	}
	if e = aw.ActivateW1(root); e != nil {
		t.Fatal(e)
	}
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	cfg := Config{InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), ApplicationOrigin: "https://example.test", ChallengeLifetime: 30 * time.Minute}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	materials, e := materialstore.NewWriter(materialstore.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ActiveKeyID: "synthetic", Keys: map[string][]byte{"synthetic": make([]byte, 32)}, ApplicationOrigin: cfg.ApplicationOrigin})
	if e != nil {
		t.Fatal(e)
	}
	queue, e := sqlstore.NewTxWriter(sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 3, MaxReconciliationAttempts: 3, MaxLease: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	renderer, e := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "sender@example.test", ApplicationOrigin: cfg.ApplicationOrigin, MaxBodyBytes: 4096})
	if e != nil {
		t.Fatal(e)
	}
	service, e := NewWithWriter(root, queue, renderer, materials, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		e := db.WithTx(cleanup, nil, func(tx *sql.Tx) error {
			for _, q := range []string{`DELETE FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM amos_jobs WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM identity_challenges WHERE person_id IN(SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2)`, `DELETE FROM identity_emails WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM workspaces WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM identity_persons WHERE installation_id=$1 AND application_id=$2`} {
				if _, e := tx.ExecContext(cleanup, q, cfg.InstallationID, cfg.ApplicationID); e != nil {
					return e
				}
			}
			return nil
		})
		if e != nil {
			t.Error("exact owned email rows cleanup failed")
		}
	})
	newContact := func() (uuid.UUID, uuid.UUID) {
		t.Helper()
		person, email := id(), id()
		e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'pending_verification')`, person, cfg.InstallationID, cfg.ApplicationID); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key) VALUES($1,$2,$3,$4,$5,$5)`, email, person, cfg.InstallationID, cfg.ApplicationID, email.String()+"@example.test"); e != nil {
				return e
			}
			_, e := tx.ExecContext(ctx, `INSERT INTO workspaces(id,installation_id,application_id,kind,state,personal_owner_id) VALUES($1,$2,$3,'personal','active',$4)`, id(), cfg.InstallationID, cfg.ApplicationID, person)
			return e
		})
		if e != nil {
			t.Fatal("contact setup failed")
		}
		return person, email
	}
	t.Run("issue durable and capped", func(t *testing.T) {
		person, email := newContact()
		for n := 0; n < 4; n++ {
			ack, e := service.IssueVerification(ctx, person, email, id())
			if e != nil || !ack.Received {
				t.Fatal("issue failed", e)
			}
		}
		e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			var challenges, materials, jobs int
			if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_challenges WHERE person_id=$1`, person).Scan(&challenges); e != nil {
				return e
			}
			if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID).Scan(&materials); e != nil {
				return e
			}
			if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM amos_jobs WHERE installation_id=$1 AND application_id=$2`, cfg.InstallationID, cfg.ApplicationID).Scan(&jobs); e != nil {
				return e
			}
			if challenges != 3 || materials != 3 || jobs != 3 {
				t.Error("rate cap or atomic D durability mismatch")
			}
			return nil
		})
		if e != nil {
			t.Fatal("durability check failed")
		}
	})
	t.Run("unknown is generic no write", func(t *testing.T) {
		ack, e := service.IssueVerification(ctx, id(), id(), id())
		if e != nil || !ack.Received {
			t.Fatal(e)
		}
	})
	for _, mode := range []string{"confirm once", "wrong digest", "expired", "future created", "wrong realm"} {
		t.Run(mode, func(t *testing.T) {
			person, email := newContact()
			challenge := id()
			seed := sha256.Sum256([]byte(challenge.String()))
			raw := base64.RawURLEncoding.EncodeToString(seed[:])
			digest := sha256.Sum256(seed[:])
			e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				created := "clock_timestamp()-interval '1 minute'"
				expires := "clock_timestamp()+interval '10 minutes'"
				if mode == "expired" {
					expires = "clock_timestamp()-interval '1 second'"
				}
				if mode == "future created" {
					created = "clock_timestamp()+interval '1 minute'"
				}
				_, e := tx.ExecContext(ctx, `INSERT INTO identity_challenges(id,person_id,email_id,purpose,token_digest,created_at,expires_at) VALUES($1,$2,$3,'email_verification',$4,`+created+`,`+expires+`)`, challenge, person, email, digest[:])
				return e
			})
			if e != nil {
				t.Fatal("challenge setup failed")
			}
			selected := service
			if mode == "wrong realm" {
				foreign := cfg
				foreign.ApplicationID = id()
				selected, e = NewWithWriter(root, nil, nil, nil, foreign)
				if e != nil {
					t.Fatal(e)
				}
			}
			supplied := raw
			if mode == "wrong digest" {
				supplied = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
			}
			page, e := selected.Preview(ctx, challenge, supplied)
			if e != nil {
				t.Fatal(e)
			}
			if page.Available != (mode == "confirm once") {
				t.Error("preview availability mismatch")
			}
			e = selected.Confirm(ctx, challenge, supplied)
			if mode == "confirm once" {
				if e != nil {
					t.Fatal(e)
				}
				if e = selected.Confirm(ctx, challenge, supplied); !errors.Is(e, ErrChallengeUnavailable) {
					t.Fatal("replay admitted", e)
				}
			} else if mode == "future created" {
				if !errors.Is(e, ErrUnavailable) {
					t.Fatal("malformed chronology not unavailable", e)
				}
			} else if !errors.Is(e, ErrChallengeUnavailable) {
				t.Fatal("invalid challenge not denied", e)
			}
		})
	}
}
