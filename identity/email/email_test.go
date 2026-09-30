package email

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestT3_6_PreviewDoesNotConsumeAndConfirmActivatesBoundContactOnce(t *testing.T) {
	db := newEmailDB(t)
	installationID, applicationID := emailID(t), emailID(t)
	account, token := createEmailAccount(t, db, installationID, applicationID, store.ChallengeEmailVerification, time.Hour)
	service, err := New(db, nil, nil, nil, Config{InstallationID: installationID, ApplicationID: applicationID, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime})
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.Preview(context.Background(), account.ChallengeID, token)
	if err != nil || !page.Available {
		t.Fatalf("Preview() = (%+v, %v), want available", page, err)
	}
	var consumed sql.NullTime
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT consumed_at FROM identity_challenges WHERE id=$1`, account.ChallengeID).Scan(&consumed)
	}); err != nil {
		t.Fatal(err)
	}
	if consumed.Valid {
		t.Fatal("GET preview consumed the challenge")
	}
	if err := service.Confirm(context.Background(), account.ChallengeID, "wrong-token"); err != ErrChallengeUnavailable {
		t.Fatalf("wrong token error = %v, want challenge unavailable", err)
	}
	if err := service.Confirm(context.Background(), account.ChallengeID, token); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if err := service.Confirm(context.Background(), account.ChallengeID, token); err != ErrChallengeUnavailable {
		t.Fatalf("replay error = %v, want challenge unavailable", err)
	}
	var state string
	var verified sql.NullTime
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT p.state,e.verified_at FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id WHERE p.id=$1 AND e.id=$2`, account.PersonID, account.EmailID).Scan(&state, &verified)
	}); err != nil {
		t.Fatal(err)
	}
	if state != "active" || !verified.Valid {
		t.Fatalf("confirmed state=%q verified=%v, want active and verified", state, verified.Valid)
	}
}

func TestT3_6_RejectsWrongPurposeAndExpiredChallenges(t *testing.T) {
	db := newEmailDB(t)
	installationID, applicationID := emailID(t), emailID(t)
	wrongPurpose, wrongToken := createEmailAccount(t, db, installationID, applicationID, store.ChallengePasswordReset, time.Hour)
	expired, expiredToken := createEmailAccount(t, db, installationID, applicationID, store.ChallengeEmailVerification, -time.Minute)
	service, err := New(db, nil, nil, nil, Config{InstallationID: installationID, ApplicationID: applicationID, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime})
	if err != nil {
		t.Fatal(err)
	}
	for name, fixture := range map[string]struct {
		account store.PendingAccount
		token   string
	}{"wrong purpose": {wrongPurpose, wrongToken}, "expired": {expired, expiredToken}} {
		t.Run(name, func(t *testing.T) {
			if err := service.Confirm(context.Background(), fixture.account.ChallengeID, fixture.token); err != ErrChallengeUnavailable {
				t.Fatalf("Confirm() error = %v, want challenge unavailable", err)
			}
		})
	}
}

func TestT3_6_ChallengeCannotCrossInstallationOrApplicationRealm(t *testing.T) {
	db, raw := newEmailDBWithJobs(t)
	ownerInstallation, ownerApplication := emailID(t), emailID(t)
	account, token := createEmailAccount(t, db, ownerInstallation, ownerApplication, store.ChallengeEmailVerification, time.Hour)
	wrongInstallation, wrongApplication := emailID(t), emailID(t)
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: 4096})
	if err != nil {
		t.Fatalf("build safe email renderer: %v", err)
	}
	outbox, err := sqlstore.New(raw, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatalf("build outbox store: %v", err)
	}
	for _, realm := range []struct {
		name           string
		installationID uuid.UUID
		applicationID  uuid.UUID
	}{
		{name: "different installation", installationID: wrongInstallation, applicationID: ownerApplication},
		{name: "different application", installationID: ownerInstallation, applicationID: wrongApplication},
	} {
		t.Run(realm.name, func(t *testing.T) {
			service, err := New(db, outbox, renderer, &memoryMaterialWriter{}, Config{InstallationID: realm.installationID, ApplicationID: realm.applicationID, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime})
			if err != nil {
				t.Fatalf("build other-realm verification service: %v", err)
			}
			preview, err := service.Preview(context.Background(), account.ChallengeID, token)
			if err != nil || preview.Available {
				t.Fatalf("other-realm Preview() = (%+v, %v), want unavailable", preview, err)
			}
			if err := service.Confirm(context.Background(), account.ChallengeID, token); err != ErrChallengeUnavailable {
				t.Fatalf("other-realm Confirm() = %v, want generic challenge unavailable", err)
			}
			before := countChallenges(t, db, account.PersonID)
			ack, err := service.IssueVerification(context.Background(), account.PersonID, account.EmailID, emailID(t))
			if err != nil || !ack.Received {
				t.Fatalf("other-realm IssueVerification() = (%+v, %v), want generic acknowledgement", ack, err)
			}
			if after := countChallenges(t, db, account.PersonID); after != before {
				t.Fatalf("other-realm issue created %d challenges, want unchanged count %d", after, before)
			}
		})
	}
}

func TestT3_6_HandlerGETIsPreviewAndPOSTRequiresSameOrigin(t *testing.T) {
	db := newEmailDB(t)
	installationID, applicationID := emailID(t), emailID(t)
	account, token := createEmailAccount(t, db, installationID, applicationID, store.ChallengeEmailVerification, time.Hour)
	service, err := New(db, nil, nil, nil, Config{InstallationID: installationID, ApplicationID: applicationID, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime})
	if err != nil {
		t.Fatal(err)
	}
	preview := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/verify-email?challenge="+account.ChallengeID.String()+"&token="+url.QueryEscape(token), nil)
	service.Handler().ServeHTTP(preview, request)
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), "Confirm that you own this email address") {
		t.Fatalf("GET status=%d body missing confirmation form", preview.Code)
	}
	if err := service.Confirm(context.Background(), account.ChallengeID, token); err != nil {
		t.Fatalf("confirmation after GET preview: %v", err)
	}

	other, otherToken := createEmailAccount(t, db, installationID, applicationID, store.ChallengeEmailVerification, time.Hour)
	body := url.Values{"challenge": {other.ChallengeID.String()}, "token": {otherToken}}
	rejected := httptest.NewRecorder()
	post := httptest.NewRequest("POST", "/verify-email", strings.NewReader(body.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", "https://evil.example.test")
	service.Handler().ServeHTTP(rejected, post)
	if rejected.Code != 403 {
		t.Fatalf("cross-origin POST status=%d, want 403", rejected.Code)
	}
	if err := service.Confirm(context.Background(), other.ChallengeID, otherToken); err != nil {
		t.Fatalf("cross-origin POST consumed challenge: %v", err)
	}
}

func TestT3_6_IssueWritesOnlyOpaqueOutboxIntentAndRollsBackOnOutboxFailure(t *testing.T) {
	db, raw := newEmailDBWithJobs(t)
	installationID, applicationID := emailID(t), emailID(t)
	account, _ := createEmailAccount(t, db, installationID, applicationID, store.ChallengeEmailVerification, time.Hour)
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: 4096})
	if err != nil {
		t.Fatalf("build safe email renderer: %v", err)
	}
	writer := &memoryMaterialWriter{}
	goodOutbox, err := sqlstore.New(raw, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatalf("build outbox store: %v", err)
	}
	service, err := New(db, goodOutbox, renderer, writer, Config{InstallationID: installationID, ApplicationID: applicationID, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime})
	if err != nil {
		t.Fatalf("build verification service: %v", err)
	}
	ack, err := service.IssueVerification(context.Background(), account.PersonID, account.EmailID, emailID(t))
	if err != nil || !ack.Received {
		t.Fatalf("IssueVerification() = (%+v, %v), want generic acknowledgement", ack, err)
	}
	var payload string
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT payload::text FROM amos_jobs ORDER BY created_at DESC LIMIT 1`).Scan(&payload)
	}); err != nil {
		t.Fatalf("read persisted job payload: %v", err)
	}
	material := writer.latest()
	if material.Recipient == "" || material.ActionURL == "" || !strings.Contains(material.ActionURL, "token=") {
		t.Fatalf("protected writer did not receive expected material: %+v", material)
	}
	for _, sensitiveFixture := range []string{material.Recipient, material.ActionURL, strings.TrimPrefix(material.ActionURL[strings.Index(material.ActionURL, "token="):], "token=")} {
		if strings.Contains(payload, sensitiveFixture) {
			t.Fatal("persisted job payload contains protected recipient or action material")
		}
	}
	if !strings.Contains(payload, "material:") {
		t.Fatal("persisted job payload lacks opaque material reference")
	}

	limitedOutbox, err := sqlstore.New(raw, sqlstore.Config{MaxPayloadBytes: 1, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatalf("build constrained outbox: %v", err)
	}
	rollbackService, err := New(db, limitedOutbox, renderer, writer, Config{InstallationID: installationID, ApplicationID: applicationID, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime})
	if err != nil {
		t.Fatalf("build rollback verification service: %v", err)
	}
	second, _ := createEmailAccount(t, db, installationID, applicationID, store.ChallengeEmailVerification, time.Hour)
	before := countChallenges(t, db, second.PersonID)
	if _, err := rollbackService.IssueVerification(context.Background(), second.PersonID, second.EmailID, emailID(t)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("IssueVerification with undersized outbox = %v, want unavailable", err)
	}
	if after := countChallenges(t, db, second.PersonID); after != before {
		t.Fatalf("failed outbox transaction left %d challenges, want %d", after, before)
	}
}

func newEmailDB(t *testing.T) *storage.DB {
	db, _ := newEmailDBWithJobs(t)
	return db
}

func newEmailDBWithJobs(t *testing.T) (*storage.DB, *sql.DB) {
	t.Helper()
	raw, schema := testkit.NewPostgres(t)
	raw.SetMaxOpenConns(1)
	if _, err := raw.ExecContext(context.Background(), `SET search_path TO `+`"`+schema+`"`); err != nil {
		t.Fatalf("set isolated PostgreSQL schema: %v", err)
	}
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal("read local PostgreSQL test connection")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse local PostgreSQL test connection")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close email verification test storage: %v", err)
		}
	})
	sqlText, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatal("read identity migration fragment")
	}
	fragments := []migrations.Fragment{{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(sqlText)}}}}
	jobsSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "jobs.sql"))
	if err != nil {
		t.Fatal("read jobs migration fragment")
	}
	fragments = append(fragments, migrations.Fragment{Namespace: "jobs", Migrations: []migrations.Migration{{Sequence: 2, Name: "jobs_base", SQL: string(jobsSQL)}}})
	registry, err := migrations.NewRegistry(fragments...)
	if err != nil {
		t.Fatal("build isolated migration registry")
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply identity migration: %v", err)
	}
	return db, raw
}

func createEmailAccount(t *testing.T, db *storage.DB, installationID, applicationID uuid.UUID, purpose string, lifetime time.Duration) (store.PendingAccount, string) {
	t.Helper()
	personID, contactID, credentialID := emailID(t), emailID(t), emailID(t)
	challengeID := emailID(t)
	token, digest, err := newToken()
	if err != nil {
		t.Fatal("generate fixture challenge")
	}
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	passwordDigest := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	input := store.PendingAccount{PersonID: personID, EmailID: contactID, CredentialID: credentialID, ChallengeID: challengeID, InstallationID: installationID, ApplicationID: applicationID, EmailAddress: fmt.Sprintf("person-%s@example.test", personID.String()), PasswordHash: fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", salt, passwordDigest), ChallengeDigest: digest[:], ChallengeExpiry: time.Now().Add(time.Hour)}
	ctx := context.Background()
	if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		identity, err := store.New(tx)
		if err != nil {
			return err
		}
		if err := identity.CreatePendingAccount(ctx, input); err != nil {
			return err
		}
		if purpose != store.ChallengeEmailVerification || lifetime != time.Hour {
			expiresAt := time.Now().Add(lifetime)
			_, err := tx.ExecContext(ctx, `UPDATE identity_challenges SET purpose=$2,expires_at=$3,created_at=LEAST(created_at,$3::timestamptz - interval '1 second') WHERE id=$1`, challengeID, purpose, expiresAt)
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("create pending email fixture: %v", err)
	}
	return input, token
}

func emailID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate fixture UUIDv7")
	}
	return id
}

func countChallenges(t *testing.T, db *storage.DB, personID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM identity_challenges WHERE person_id=$1`, personID).Scan(&count)
	}); err != nil {
		t.Fatalf("count challenge rows: %v", err)
	}
	return count
}

type memoryMaterialWriter struct {
	mu       sync.Mutex
	material deliveryemail.PrivateMaterial
}

func (w *memoryMaterialWriter) PutVerificationMaterial(_ context.Context, _ *sql.Tx, _ deliveryemail.SecretReference, material deliveryemail.PrivateMaterial, _ time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.material = material
	return nil
}

func (w *memoryMaterialWriter) latest() deliveryemail.PrivateMaterial {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.material
}
