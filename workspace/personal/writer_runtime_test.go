package personal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	identitystore "github.com/ajent-social/amos/identity/store"
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

// These checks qualify atomic persistence with a synthetic encoded verifier,
// not password hashing, delivery, account activation or authenticated authority.
func TestWriterPendingBootstrapRequiredService(t *testing.T) {
	if os.Getenv("AMOS_PERSONAL_WRITER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestWriterPendingBootstrapRequiredService$", "-test.v")
		cmd.Env = append(os.Environ(), "AMOS_PERSONAL_WRITER_CHILD=1")
		out, e := cmd.CombinedOutput()
		t.Log(string(out))
		if e != nil {
			t.Fatal("required writer subprocess failed")
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, mode := range []string{"commit", "rollback", "unplanned workspace", "zero capability"} {
		t.Run(mode, func(t *testing.T) {
			realm := aw.Realm{Installation: newID(t), Application: newID(t), Environment: newID(t)}
			person, email, credential, challenge, workspace := newID(t), newID(t), newID(t), newID(t), newID(t)
			rows := []aw.Row{{Table: aw.Persons, ID: person, Access: aw.ReservedInsert}, {Table: aw.Emails, ID: email, Access: aw.ReservedInsert}, {Table: aw.Credentials, ID: credential, Access: aw.ReservedInsert}, {Table: aw.Challenges, ID: challenge, Access: aw.ReservedInsert}}
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
				if e := a.Acquire(request, aw.P); e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				store, e := identitystore.NewWriter(a)
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				b, e := a.StartedAt()
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				digest := sha256.Sum256([]byte("synthetic bootstrap challenge:" + challenge.String()))
				verifier := "$argon2id$v=19$m=65536,t=3,p=1$" + base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("s", 16))) + "$" + base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("h", 32)))
				registration, e := store.CreatePendingRegistration(request, identitystore.PendingAccount{PersonID: person, EmailID: email, CredentialID: credential, ChallengeID: challenge, InstallationID: realm.Installation, ApplicationID: realm.Application, EmailAddress: "pending@example.test", PasswordHash: verifier, ChallengeDigest: digest[:], ChallengeExpiry: b.Add(30 * time.Minute)})
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				for phase := aw.S; phase <= aw.W; phase++ {
					if e := a.Acquire(request, phase); e != nil {
						t.Error(e)
						return aw.UnavailableRollback
					}
				}
				participant, e := NewWriter(a)
				if e != nil {
					t.Error(e)
					return aw.UnavailableRollback
				}
				if mode == "zero capability" {
					registration = identitystore.PendingRegistration{}
				}
				result, e := participant.BootstrapPending(request, registration, workspace)
				if mode == "unplanned workspace" || mode == "zero capability" {
					if e == nil {
						t.Error("unbound bootstrap admitted")
					}
					if _, e = a.DrainAndSample(request); e == nil {
						t.Error("failed bootstrap allowed finalization")
					}
					return aw.UnavailableRollback
				}
				if e != nil || result.Workspace.ID != workspace || result.Workspace.State != "active" || result.Existing {
					t.Error("pending bootstrap mismatch", e)
					return aw.UnavailableRollback
				}
				if mode == "rollback" {
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
			if mode == "commit" && e != nil {
				t.Fatal(e)
			}
			if mode == "rollback" && !errors.Is(e, aw.ErrDenied) {
				t.Fatal("denial lost")
			}
			if (mode == "unplanned workspace" || mode == "zero capability") && !errors.Is(e, aw.ErrUnavailable) {
				t.Fatal("invalid bootstrap outcome")
			}
			e = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				var persons, workspaces int
				if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_persons WHERE id=$1 AND state='pending_verification'`, person).Scan(&persons); e != nil {
					return e
				}
				if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE id=$1 AND personal_owner_id=$2 AND state='active'`, workspace, person).Scan(&workspaces); e != nil {
					return e
				}
				want := 0
				if mode == "commit" {
					want = 1
				}
				if persons != want || workspaces != want {
					t.Error("atomic pending/workspace durability mismatch")
				}
				if _, e := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id=$1`, workspace); e != nil {
					return e
				}
				for _, owned := range []struct {
					query string
					id    any
				}{
					{`DELETE FROM identity_challenges WHERE id=$1`, challenge},
					{`DELETE FROM identity_credentials WHERE id=$1`, credential},
					{`DELETE FROM identity_emails WHERE id=$1`, email},
					{`DELETE FROM identity_persons WHERE id=$1`, person},
				} {
					if _, e := tx.ExecContext(ctx, owned.query, owned.id); e != nil {
						return e
					}
				}
				return nil
			})
			if e != nil {
				t.Fatal("owned rows verification/cleanup failed")
			}
		})
	}
}
