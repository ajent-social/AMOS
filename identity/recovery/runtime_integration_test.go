package recovery

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// The standard eleven-field runtime profile admits probe metadata, never admin
// credentials. Only the dedicated same-database recovery profile is accepted.
type recoveryRuntimeConfig struct {
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

func decodeRecoveryRuntimeConfig(r io.Reader) (recoveryRuntimeConfig, error) {
	const invalid = "invalid runtime config"
	raw, err := io.ReadAll(io.LimitReader(r, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	fields := make(map[string]json.RawMessage)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return recoveryRuntimeConfig{}, errors.New(invalid)
		}
		key, ok := token.(string)
		if !ok {
			return recoveryRuntimeConfig{}, errors.New(invalid)
		}
		if _, exists := fields[key]; exists {
			return recoveryRuntimeConfig{}, errors.New(invalid)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return recoveryRuntimeConfig{}, errors.New(invalid)
		}
		fields[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	for _, key := range []string{"host", "port", "database", "user", "password", "ca_path", "wrong_host", "dml_table", "ledger_table", "privileged_role", "owner_role"} {
		if _, ok := fields[key]; !ok {
			return recoveryRuntimeConfig{}, errors.New(invalid)
		}
	}
	if len(fields) != 11 {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	strict := json.NewDecoder(strings.NewReader(string(data)))
	strict.DisallowUnknownFields()
	var cfg recoveryRuntimeConfig
	if err := strict.Decode(&cfg); err != nil {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	if cfg.Host == "" || cfg.Port == 0 || cfg.Database == "" || cfg.User == "" || cfg.Password == "" || cfg.CAPath == "" || cfg.WrongHost == "" || (cfg.DMLTable != "identity_persons" && cfg.DMLTable != "public.identity_persons") || cfg.LedgerTable == "" || cfg.PrivilegedRole == "" || cfg.OwnerRole == "" {
		return recoveryRuntimeConfig{}, errors.New(invalid)
	}
	return cfg, nil
}

func TestRecoveryRuntimeConfigFormat(t *testing.T) {
	const standard = `{"host":"fixture.invalid","port":5432,"database":"fixture","user":"runtime","password":"synthetic","ca_path":"fixture-ca.pem","wrong_host":"wrong.invalid","dml_table":"identity_persons","ledger_table":"fixture_migration_ledger","privileged_role":"fixture_admin","owner_role":"fixture_owner"}`
	cfg, e := decodeRecoveryRuntimeConfig(strings.NewReader(standard))
	if e != nil || cfg.Host != "fixture.invalid" || cfg.Port != 5432 || cfg.OwnerRole != "fixture_owner" {
		t.Fatal("standard eleven-field runtime config rejected")
	}
	if _, e := decodeRecoveryRuntimeConfig(strings.NewReader(strings.Replace(standard, `"identity_persons"`, `"public.identity_persons"`, 1))); e != nil {
		t.Fatal("qualified standard DML metadata rejected")
	}
	for _, input := range []string{standard + `{}`, standard + strings.Repeat(" ", 2<<20), standard[:len(standard)-1] + `,"unexpected":true}`, `{"port":"invalid"}`, `{}`, `null`, strings.Replace(standard, `"port":5432`, `"port":5432,"port":5432`, 1), strings.Replace(standard, `"user":"runtime"`, `"user":null`, 1), strings.Replace(standard, `"port":5432`, `"port":0`, 1), strings.Replace(standard, `"host"`, `"HOST"`, 1)} {
		if got, e := decodeRecoveryRuntimeConfig(strings.NewReader(input)); e == nil || got != (recoveryRuntimeConfig{}) {
			t.Fatal("invalid config accepted or partially disclosed")
		}
	}
}

func recoveryRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_RECOVERY_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required same-database identity/jobs/material/session TLS PostgreSQL profile absent: set AMOS_RECOVERY_RUNTIME_TEST_CONFIG")
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal("required recovery runtime config unavailable")
	}
	defer func() {
		if e := f.Close(); e != nil {
			t.Error("runtime config close failed")
		}
	}()
	cfg, e := decodeRecoveryRuntimeConfig(f)
	if e != nil {
		t.Fatal("required recovery runtime JSON invalid")
	}
	roots, e := os.ReadFile(cfg.CAPath)
	if e != nil {
		t.Fatal("required recovery runtime CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, e := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if e != nil {
		t.Fatal("required recovery runtime TLS connection failed")
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error("runtime close failed")
		}
	})
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		var roleOK bool
		if e := tx.QueryRowContext(ctx, `SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&roleOK); e != nil {
			return e
		}
		if !roleOK {
			return errors.New("runtime role required")
		}
		for _, table := range []string{"identity_persons", "identity_emails", "identity_credentials", "identity_challenges", "identity_sessions", "amos_jobs", "email_delivery_material"} {
			var tableOK bool
			if e := tx.QueryRowContext(ctx, `SELECT COALESCE(to_regclass($1)=to_regclass('public.' || $1),false) AND has_table_privilege(current_user,'public.' || $1,'SELECT,INSERT,UPDATE,DELETE') AND NOT pg_has_role(current_user,(SELECT relowner FROM pg_class WHERE oid=to_regclass('public.' || $1)),'USAGE')`, table).Scan(&tableOK); e != nil {
				return e
			}
			if !tableOK {
				return errors.New("precreated runtime table required")
			}
		}
		return nil
	}); e != nil {
		t.Fatal("required precreated same-database identity/jobs/material/session schema or runtime-only DML role absent")
	}
	return db
}

// The operator precreates identity.sql, jobs.sql and email-material.sql in ONE
// database. This suite performs runtime DML only. The named local synthetic
// password and recovery policies do not qualify current production authority.
func TestRecoveryRuntimeRequiredService(t *testing.T) {
	db := recoveryRuntimeDB(t)
	for _, operation := range []string{"request", "complete", "change"} {
		for _, fault := range []string{"none", "rollback", "cancel", "commit"} {
			t.Run(operation+"/"+fault, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cfg := recoveryTxConfig(t)
				hasher, e := password.New(recoveryBlocklist{}, recoveryBudget{}, 2)
				if e != nil {
					t.Fatal(e)
				}
				cfg.Passwords = hasher
				outbox, e := sqlstore.NewWithTx(db, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
				if e != nil {
					t.Fatal(e)
				}
				cfg.Outbox = outbox
				materials, e := materialstore.NewWithTxRunner(db, materialstore.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ActiveKeyID: "test", Keys: map[string][]byte{"test": bytes32(11)}, ApplicationOrigin: cfg.ApplicationOrigin})
				if e != nil {
					t.Fatal(e)
				}
				cfg.Materials = materials
				svc, e := NewWithTxRunner(db, cfg)
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { recoveryRuntimeCleanup(t, db, cfg) })
				account := recoveryRuntimeAccount(t, db, svc, cfg)
				var challenge uuid.UUID
				var token string
				if operation == "complete" {
					if e := svc.request(ctx, "person@example.test"); e != nil {
						t.Fatal(e)
					}
					if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
						return tx.QueryRowContext(ctx, `SELECT id FROM identity_challenges WHERE person_id=$1 AND consumed_at IS NULL`, account.personID).Scan(&challenge)
					}); e != nil {
						t.Fatal(e)
					}
					material, e := materials.ResolveForTemplate(ctx, deliveryemail.SecretReference("material:"+challenge.String()), deliveryemail.TemplatePasswordReset)
					if e != nil {
						t.Fatal(e)
					}
					action, e := url.Parse(material.ActionURL)
					if e != nil {
						t.Fatal(e)
					}
					token = action.Query().Get("token")
					if action.Path != "/reset-password" || token == "" {
						t.Fatal("purpose-bound reset material absent")
					}
				}
				before := recoveryRuntimeSnapshot(t, db, cfg, account)
				proof, e := authproof.NewVerifiedCredential(account.personID, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, 0, "email_password", time.Now().UTC().Add(-time.Minute), "aal1", time.Now().UTC().Add(time.Hour))
				if e != nil {
					t.Fatal(e)
				}
				ctx = identity.ContextWithVerifiedCredential(ctx, proof)
				injected := false
				calls := 0
				var runnerErr error
				rollbackErr := errors.New("local synthetic callback rollback")
				runner := recoveryRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
					err := db.WithTx(c, o, func(tx *sql.Tx) error {
						if e := fn(tx); e != nil {
							return e
						}
						if o != nil && o.ReadOnly {
							return nil
						}
						calls++
						target := 1
						if operation == "request" {
							target = 2
						} // readiness precedes request writes.
						if calls != target {
							return nil
						}
						inside, e := recoveryRuntimeSnapshotTx(c, tx, cfg, account)
						if e != nil {
							return e
						}
						recoveryAssertWrites(t, operation, before, inside)
						if fault == "none" {
							return nil
						}
						injected = true
						switch fault {
						case "rollback":
							return rollbackErr
						case "cancel":
							cancel()
							return nil
						case "commit":
							if e := tx.Rollback(); e != nil {
								return e
							}
							return nil
						}
						return errors.New("unknown fault")
					})
					runnerErr = err
					return err
				})
				svc, e = NewWithTxRunner(runner, cfg)
				if e != nil {
					t.Fatal(e)
				}
				path, body := "/forgot-password", `{"email":"person@example.test"}`
				handler := svc.RequestHandler()
				if operation == "complete" {
					path = "/reset-password"
					body = `{"challenge_id":"` + challenge.String() + `","token":"` + token + `","password":"` + newPassword + `"}`
					handler = svc.CompleteHandler()
				}
				if operation == "change" {
					path = "/account/password"
					body = `{"current_password":"` + oldPassword + `","new_password":"` + newPassword + `"}`
					handler = svc.PasswordChangeHandler()
				}
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				after := recoveryRuntimeSnapshot(t, db, cfg, account)
				if fault != "none" {
					if !injected || runnerErr == nil || response.Code != http.StatusServiceUnavailable || response.Header().Get("Set-Cookie") != "" || !strings.Contains(response.Body.String(), `"code":"dependency.unavailable"`) || strings.Contains(response.Body.String(), "accepted") {
						t.Fatal("failed transaction exposed success")
					}
					if fault == "commit" && (runnerErr != storage.ErrTransaction || ctx.Err() != nil) {
						t.Fatal("real commit error with live context required")
					}
					if fault == "rollback" && runnerErr != rollbackErr {
						t.Fatal("callback error not preserved")
					}
					if fault == "cancel" && !errors.Is(runnerErr, context.Canceled) {
						t.Fatal("cancellation not preserved")
					}
					if after != before {
						t.Fatal("failed transaction left durable writes")
					}
					return
				}
				want := http.StatusNoContent
				if operation == "request" {
					want = http.StatusAccepted
				}
				if response.Code != want || runnerErr != nil {
					t.Fatalf("success status=%d want=%d", response.Code, want)
				}
				recoveryAssertWrites(t, operation, before, after)
				if operation != "request" {
					result, e := hasher.Verify(context.Background(), "runtime-verify", newPassword, after.hash, nil)
					if e != nil || !result.Verified {
						t.Fatal("new password not durable")
					}
					result, e = hasher.Verify(context.Background(), "runtime-old", oldPassword, after.hash, nil)
					if e != nil || result.Verified {
						t.Fatal("old password remained valid")
					}
				}
				if operation == "complete" && completeResetHTTP(t, svc, challenge, token, newPassword).Code != http.StatusUnauthorized {
					t.Fatal("reset replay admitted")
				}
			})
		}
	}
}

type recoveryRuntimeState struct {
	challenges, materials, jobs, consumed, revoked, epoch int
	hash                                                  string
}

func recoveryRuntimeSnapshotTx(ctx context.Context, tx *sql.Tx, cfg TxConfig, a recoveryAccount) (recoveryRuntimeState, error) {
	var s recoveryRuntimeState
	e := tx.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM identity_challenges WHERE person_id=$1),
 (SELECT count(*) FROM email_delivery_material WHERE installation_id=$2 AND application_id=$3),
 (SELECT count(*) FROM amos_jobs WHERE installation_id=$2 AND application_id=$3),
 (SELECT count(*) FROM identity_challenges WHERE person_id=$1 AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM identity_sessions WHERE person_id=$1 AND revoked_at IS NOT NULL),
 (SELECT security_epoch FROM identity_persons WHERE id=$1),
 (SELECT verifier_hash FROM identity_credentials WHERE person_id=$1 AND method='email_password')`, a.personID, cfg.InstallationID, cfg.ApplicationID).Scan(&s.challenges, &s.materials, &s.jobs, &s.consumed, &s.revoked, &s.epoch, &s.hash)
	return s, e
}
func recoveryRuntimeSnapshot(t *testing.T, db TxRunner, cfg TxConfig, a recoveryAccount) recoveryRuntimeState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var s recoveryRuntimeState
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error { var e error; s, e = recoveryRuntimeSnapshotTx(ctx, tx, cfg, a); return e }); e != nil {
		t.Fatal(e)
	}
	return s
}
func recoveryAssertWrites(t *testing.T, op string, before, after recoveryRuntimeState) {
	t.Helper()
	want := before
	if op == "request" {
		want.challenges++
		want.materials++
		want.jobs++
	} else {
		want.epoch++
		want.revoked++
		if op == "complete" {
			want.consumed++
		}
		if before.hash == after.hash {
			t.Fatal("password callback did not write")
		}
		want.hash = after.hash
	}
	if after != want {
		t.Fatalf("%s callback writes differ from expected atomic transition", op)
	}
}
func recoveryRuntimeCleanup(t *testing.T, db TxRunner, cfg TxConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2`,
			`DELETE FROM amos_jobs WHERE installation_id=$1 AND application_id=$2`,
			`DELETE FROM identity_sessions WHERE installation_id=$1 AND application_id=$2`,
			`DELETE FROM identity_challenges WHERE person_id IN (SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2)`,
			`DELETE FROM identity_credentials WHERE person_id IN (SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2)`,
			`DELETE FROM identity_emails WHERE installation_id=$1 AND application_id=$2`,
			`DELETE FROM identity_persons WHERE installation_id=$1 AND application_id=$2`,
		} {
			if _, e := tx.ExecContext(ctx, q, cfg.InstallationID, cfg.ApplicationID); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Error("owned recovery runtime rows cleanup failed")
	}
}
func recoveryRuntimeAccount(t *testing.T, db TxRunner, svc *Service, cfg TxConfig) recoveryAccount {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	personID, emailID, credentialID := newRecoveryID(t), newRecoveryID(t), newRecoveryID(t)
	hash, err := svc.cfg.Passwords.Hash(ctx, "fixture", oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, personID, cfg.InstallationID, cfg.ApplicationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,$5,$6,transaction_timestamp())`, emailID, personID, cfg.InstallationID, cfg.ApplicationID, "person@example.test", "person@example.test"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password',$3)`, credentialID, personID, hash); err != nil {
			return err
		}
		{
			sessionID := newRecoveryID(t)
			tokenDigest := make([]byte, 32)
			if _, err := rand.Read(tokenDigest); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,authenticated_at,expires_at,idle_expires_at) VALUES($1,$2,$3,$4,$5,$6,0,'email_password',transaction_timestamp(),transaction_timestamp()+interval '1 hour',transaction_timestamp()+interval '30 minutes')`, sessionID, personID, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, tokenDigest)
			return err
		}
	}); err != nil {
		t.Fatal(err)
	}
	return recoveryAccount{personID: personID, emailID: emailID}
}
