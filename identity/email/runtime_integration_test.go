package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// The standard eleven-field runtime profile admits probe metadata, never admin
// credentials. Only the dedicated same-database email profile is accepted.
type emailRuntimeConfig struct {
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

func decodeEmailRuntimeConfig(r io.Reader) (emailRuntimeConfig, error) {
	var cfg emailRuntimeConfig
	dec := json.NewDecoder(io.LimitReader(r, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return emailRuntimeConfig{}, errors.New("invalid runtime config")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return emailRuntimeConfig{}, errors.New("trailing runtime config data")
	}
	return cfg, nil
}

func TestEmailRuntimeConfigFormat(t *testing.T) {
	const standard = `{"host":"fixture.invalid","port":5432,"database":"fixture","user":"runtime","password":"synthetic","ca_path":"fixture-ca.pem","wrong_host":"wrong.invalid","dml_table":"identity_persons","ledger_table":"fixture_migration_ledger","privileged_role":"fixture_admin","owner_role":"fixture_owner"}`
	cfg, e := decodeEmailRuntimeConfig(strings.NewReader(standard))
	if e != nil || cfg.Host != "fixture.invalid" || cfg.Port != 5432 || cfg.OwnerRole != "fixture_owner" {
		t.Fatal("standard eleven-field runtime config rejected")
	}
	for _, input := range []string{standard + `{}`, standard[:len(standard)-1] + `,"unexpected":true}`, `{"port":"invalid"}`} {
		if got, e := decodeEmailRuntimeConfig(strings.NewReader(input)); e == nil || got != (emailRuntimeConfig{}) {
			t.Fatal("invalid config accepted or partially disclosed")
		}
	}
}

func emailRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_EMAIL_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required same-database identity/jobs/material TLS PostgreSQL profile absent: set AMOS_EMAIL_RUNTIME_TEST_CONFIG")
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal("required email runtime config unavailable")
	}
	defer func() {
		if e := f.Close(); e != nil {
			t.Error("runtime config close failed")
		}
	}()
	cfg, e := decodeEmailRuntimeConfig(f)
	if e != nil {
		t.Fatal("required email runtime JSON invalid")
	}
	roots, e := os.ReadFile(cfg.CAPath)
	if e != nil {
		t.Fatal("required email runtime CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, e := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if e != nil {
		t.Fatal("required email runtime TLS connection failed")
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
		for _, table := range []string{"identity_persons", "identity_emails", "identity_challenges", "amos_jobs", "email_delivery_material"} {
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
		t.Fatal("required precreated same-database identity/jobs/material schema or runtime-only DML role absent")
	}
	return db
}

// The operator precreates identity.sql, jobs.sql and email-material.sql in ONE
// database. No migrations, DDL, provider calls or privileged connections run here.
func TestEmailRuntimeRequiredService(t *testing.T) {
	db := emailRuntimeDB(t)
	cfg := emailTxConfig(t)
	environment := emailID(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	exec := func(query string, args ...any) {
		t.Helper()
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, query, args...); return e }); e != nil {
			t.Fatal("runtime email DML failed")
		}
	}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := db.WithTx(c, nil, func(tx *sql.Tx) error {
			for _, q := range []string{
				`DELETE FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM amos_jobs WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM identity_challenges WHERE person_id IN (SELECT id FROM identity_persons WHERE installation_id=$1 AND application_id=$2)`,
				`DELETE FROM identity_emails WHERE installation_id=$1 AND application_id=$2`,
				`DELETE FROM identity_persons WHERE installation_id=$1 AND application_id=$2`,
			} {
				if _, e := tx.ExecContext(c, q, cfg.InstallationID, cfg.ApplicationID); e != nil {
					return e
				}
			}
			return nil
		}); e != nil {
			t.Error("exact-owned email scope DML cleanup failed")
		}
	})
	materials, e := materialstore.NewWithTxRunner(db, materialstore.TxConfig{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: environment, ActiveKeyID: "test", Keys: map[string][]byte{"test": make([]byte, 32)}, ApplicationOrigin: cfg.ApplicationOrigin})
	if e != nil {
		t.Fatal(e)
	}
	renderer, e := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: cfg.ApplicationOrigin, MaxBodyBytes: 4096})
	if e != nil {
		t.Fatal(e)
	}
	outboxConfig := sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute}
	outbox, e := sqlstore.NewWithTx(db, outboxConfig)
	if e != nil {
		t.Fatal(e)
	}
	service, e := NewWithTxRunner(db, outbox, renderer, materials, cfg)
	if e != nil {
		t.Fatal(e)
	}
	newContact := func() (uuid.UUID, uuid.UUID) {
		t.Helper()
		person, contact := emailID(t), emailID(t)
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'pending_verification')`, person, cfg.InstallationID, cfg.ApplicationID); e != nil {
				return e
			}
			address := "person-" + person.String() + "@example.test"
			_, e := tx.ExecContext(ctx, `INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key) VALUES($1,$2,$3,$4,$5,$5)`, contact, person, cfg.InstallationID, cfg.ApplicationID, address)
			return e
		}); e != nil {
			t.Fatal("create runtime contact failed")
		}
		return person, contact
	}
	counts := func() [3]int {
		t.Helper()
		var n [3]int
		if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM identity_challenges c JOIN identity_persons p ON p.id=c.person_id WHERE p.installation_id=$1 AND p.application_id=$2),(SELECT count(*) FROM amos_jobs WHERE installation_id=$1 AND application_id=$2),(SELECT count(*) FROM email_delivery_material WHERE installation_id=$1 AND application_id=$2)`, cfg.InstallationID, cfg.ApplicationID).Scan(&n[0], &n[1], &n[2])
		}); e != nil {
			t.Fatal("count email transaction rows failed")
		}
		return n
	}
	issue := func(s *Service, p, c uuid.UUID, request uuid.UUID) {
		t.Helper()
		if a, e := s.IssueVerification(ctx, p, c, request); e != nil || !a.Received {
			t.Fatal("runtime issue failed")
		}
	}
	person, contact := newContact()
	request := emailID(t)
	before := counts()
	issue(service, person, contact, request)
	if got := counts(); got != ([3]int{before[0] + 1, before[1] + 1, before[2] + 1}) {
		t.Fatal("issue did not commit challenge/material/job together")
	}
	var payload string
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT payload::text FROM amos_jobs WHERE installation_id=$1 AND application_id=$2 AND idempotency_key=$3`, cfg.InstallationID, cfg.ApplicationID, "email-verification:"+request.String()).Scan(&payload)
	}); e != nil {
		t.Fatal("read opaque job failed")
	}
	var intent deliveryemail.Request
	if e := json.Unmarshal([]byte(payload), &intent); e != nil || intent.Template != deliveryemail.TemplateVerifyEmail {
		t.Fatal("verification intent invalid")
	}
	material, e := materials.ResolveForTemplate(ctx, intent.MaterialRef, deliveryemail.TemplateVerifyEmail)
	if e != nil {
		t.Fatal("resolve actual encrypted verification material failed")
	}
	action, e := url.Parse(material.ActionURL)
	if e != nil {
		t.Fatal("verification URL invalid")
	}
	challenge, e := uuid.Parse(action.Query().Get("challenge"))
	if e != nil {
		t.Fatal("challenge ID invalid")
	}
	token := action.Query().Get("token")
	for _, secret := range []string{material.Recipient, material.ActionURL, token} {
		if secret == "" || strings.Contains(payload, secret) {
			t.Fatal("opaque job contains protected material")
		}
	}
	if _, e := materials.ResolveForTemplate(ctx, intent.MaterialRef, deliveryemail.TemplatePasswordReset); e != materialstore.ErrUnavailable {
		t.Fatal("material purpose binding lost")
	}
	if p, e := service.Preview(ctx, challenge, token); e != nil || !p.Available {
		t.Fatal("runtime preview failed")
	}
	// Duplicate delivery request rolls back the newly generated challenge/material.
	before = counts()
	issue(service, person, contact, request)
	if counts() != before {
		t.Fatal("duplicate request left partial rows")
	}
	// Budget remains generic and never creates a fourth challenge in the hour.
	issue(service, person, contact, emailID(t))
	issue(service, person, contact, emailID(t))
	if got := counts(); got != ([3]int{before[0] + 2, before[1] + 2, before[2] + 2}) {
		t.Fatal("issue budget denied requests before the third challenge")
	}
	before = counts()
	issue(service, person, contact, emailID(t))
	if counts() != before {
		t.Fatal("issue budget changed")
	}
	foreignCfg := cfg
	foreignCfg.ApplicationID = emailID(t)
	foreign, e := NewWithTxRunner(db, outbox, renderer, materials, foreignCfg)
	if e != nil {
		t.Fatal(e)
	}
	if p, e := foreign.Preview(ctx, challenge, token); e != nil || p.Available {
		t.Fatal("cross-realm preview accepted")
	}
	if e := foreign.Confirm(ctx, challenge, token); e != ErrChallengeUnavailable {
		t.Fatal("cross-realm confirmation accepted")
	}
	if e := service.Confirm(ctx, challenge, token); e != nil {
		t.Fatal("runtime confirmation failed")
	}
	if e := service.Confirm(ctx, challenge, token); e != ErrChallengeUnavailable {
		t.Fatal("runtime replay accepted")
	}
	var state string
	var verified bool
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT p.state,e.verified_at IS NOT NULL FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id WHERE p.id=$1 AND e.id=$2`, person, contact).Scan(&state, &verified)
	}); e != nil || state != "active" || !verified {
		t.Fatal("confirmation did not activate bound contact")
	}
	// A real outbox rejection occurs AFTER the encrypted material write.
	limited := outboxConfig
	limited.MaxPayloadBytes = 1
	small, e := sqlstore.NewWithTx(db, limited)
	if e != nil {
		t.Fatal(e)
	}
	failing, e := NewWithTxRunner(db, small, renderer, materials, cfg)
	if e != nil {
		t.Fatal(e)
	}
	p, c := newContact()
	before = counts()
	if a, e := failing.IssueVerification(ctx, p, c, emailID(t)); e != ErrUnavailable || a.Received {
		t.Fatal("outbox failure mapping changed")
	}
	if counts() != before {
		t.Fatal("outbox rejection did not roll back all three rows")
	}
	// Execute the actual callback, then cancel before commit; all writes roll back.
	canceledCtx, stop := context.WithCancel(ctx)
	callbackCompleted := false
	cancelRunner := emailRunnerFunc(func(c context.Context, o *sql.TxOptions, fn func(*sql.Tx) error) error {
		return db.WithTx(c, o, func(tx *sql.Tx) error {
			if e := fn(tx); e != nil {
				return e
			}
			callbackCompleted = true
			stop()
			return c.Err()
		})
	})
	cancelService, e := NewWithTxRunner(cancelRunner, outbox, renderer, materials, cfg)
	if e != nil {
		t.Fatal(e)
	}
	a, e := cancelService.IssueVerification(canceledCtx, p, c, emailID(t))
	stop()
	if e != ErrUnavailable || a.Received || !callbackCompleted || canceledCtx.Err() != context.Canceled || counts() != before {
		t.Fatal("cancellation left partial issue rows")
	}
	// Caller-owned registration transaction must include challenge and delivery.
	challenge = emailID(t)
	token, digest, e := newToken()
	if e != nil {
		t.Fatal(e)
	}
	rollback := errors.New("caller rollback")
	queue := func(purpose string, rollbackAfter bool) error {
		return db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `INSERT INTO identity_challenges(id,person_id,email_id,purpose,token_digest,expires_at) VALUES($1,$2,$3,$4,$5,transaction_timestamp()+interval '30 minutes')`, challenge, p, c, purpose, digest[:]); e != nil {
				return e
			}
			if e := service.QueueExistingChallengeTx(ctx, tx, p, c, challenge, emailID(t), token); e != nil {
				return e
			}
			if rollbackAfter {
				return rollback
			}
			return nil
		})
	}
	if e := queue("password_reset", false); e != ErrChallengeUnavailable || counts() != before {
		t.Fatal("wrong-purpose queue did not roll back")
	}
	if e := queue("email_verification", true); e != rollback || counts() != before {
		t.Fatal("caller rollback left partial delivery")
	}
	if e := queue("email_verification", false); e != nil {
		t.Fatal("caller transaction queue failed")
	}
	if got := counts(); got != ([3]int{before[0] + 1, before[1] + 1, before[2] + 1}) {
		t.Fatal("caller transaction did not commit all rows")
	}
	exec(`UPDATE identity_challenges SET created_at=transaction_timestamp()-interval '2 hours',expires_at=transaction_timestamp()-interval '1 hour' WHERE id=$1`, challenge)
	if e := service.Confirm(ctx, challenge, token); e != ErrChallengeUnavailable {
		t.Fatal("expired confirmation accepted")
	}
	// The service has not acquired lifecycle authority over the injected runtime.
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error { var one int; return tx.QueryRowContext(ctx, `SELECT 1`).Scan(&one) }); e != nil {
		t.Fatal("caller-owned runtime no longer usable")
	}
}
