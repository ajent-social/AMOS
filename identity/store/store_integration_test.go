package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestT3_2_DuplicateEmailRollsBackPersonAndConcurrentCreateWinsOnce(t *testing.T) {
	db := newIdentityDB(t)
	first := pendingAccount(t, "Alice@example.test")
	if err := createAccount(t, db, first); err != nil {
		t.Fatalf("create first account: %v", err)
	}

	duplicate := pendingAccount(t, dbEmailCaseVariant(first.EmailAddress))
	duplicate.InstallationID = first.InstallationID
	duplicate.ApplicationID = first.ApplicationID
	err := createAccount(t, db, duplicate)
	if !errors.Is(err, store.ErrEmailAlreadyUsed) {
		t.Fatalf("duplicate account error = %v, want ErrEmailAlreadyUsed", err)
	}
	assertCounts(t, db, 1, 1, 1, 1)

	// Both transactions race for the same normalized email. PostgreSQL's unique
	// constraint must choose one committed identity, regardless of timing.
	left := pendingAccount(t, "Race@example.test")
	right := pendingAccount(t, "race@EXAMPLE.TEST")
	left.InstallationID, left.ApplicationID = first.InstallationID, first.ApplicationID
	right.InstallationID, right.ApplicationID = first.InstallationID, first.ApplicationID
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, input := range []store.PendingAccount{left, right} {
		input := input
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
				identityStore, err := store.New(tx)
				if err != nil {
					return err
				}
				return identityStore.CreatePendingAccount(ctx, input)
			})
			results <- err
		}()
	}
	close(start)
	firstErr, secondErr := <-results, <-results
	successes, conflicts := 0, 0
	for _, createErr := range []error{firstErr, secondErr} {
		switch {
		case createErr == nil:
			successes++
		case errors.Is(createErr, store.ErrEmailAlreadyUsed):
			conflicts++
		default:
			t.Fatalf("concurrent create returned unexpected error: %v", createErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results: successes=%d conflicts=%d, want one each", successes, conflicts)
	}
	assertCounts(t, db, 2, 2, 2, 2)
}

func TestT3_2_FailedCreateAfterPersonInsertLeavesNoPartialAccount(t *testing.T) {
	db := newIdentityDB(t)
	first := pendingAccount(t, "rollback@example.test")
	if err := createAccount(t, db, first); err != nil {
		t.Fatalf("create first account: %v", err)
	}

	// The attempted transaction inserts its person row, then fails on the
	// scoped email uniqueness constraint. storage.DB.WithTx must roll back that
	// person row along with every other row in the account operation.
	second := pendingAccount(t, "ROLLBACK@example.test")
	second.InstallationID = first.InstallationID
	second.ApplicationID = first.ApplicationID
	err := createAccount(t, db, second)
	if !errors.Is(err, store.ErrEmailAlreadyUsed) {
		t.Fatalf("failing account error = %v, want ErrEmailAlreadyUsed", err)
	}
	assertCounts(t, db, 1, 1, 1, 1)
	if existsPerson(t, db, second.PersonID) {
		t.Fatal("failed transaction left its person row behind")
	}
}

func TestT3_2_ChallengeIsPurposeBoundAndConsumedOnce(t *testing.T) {
	db := newIdentityDB(t)
	account := pendingAccount(t, "challenge@example.test")
	if err := createAccount(t, db, account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	var consumed store.ConsumedChallenge
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		consumed, err = identityStore.ConsumeChallenge(context.Background(), account.ChallengeID,
			store.ChallengeEmailVerification, account.ChallengeDigest)
		if err != nil {
			return err
		}
		return identityStore.MarkEmailVerified(context.Background(), consumed.PersonID, consumed.EmailID)
	}); err != nil {
		t.Fatalf("consume verification challenge: %v", err)
	}
	if consumed.PersonID != account.PersonID || consumed.EmailID != account.EmailID {
		t.Fatal("challenge returned a different bound identity")
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		_, err = identityStore.ConsumeChallenge(context.Background(), account.ChallengeID,
			store.ChallengeEmailVerification, account.ChallengeDigest)
		return err
	}); !errors.Is(err, store.ErrChallengeUnavailable) {
		t.Fatalf("replayed challenge error = %v, want ErrChallengeUnavailable", err)
	}

	var verified bool
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(),
			`SELECT verified_at IS NOT NULL FROM identity_emails WHERE id = $1`, account.EmailID).Scan(&verified)
	}); err != nil {
		t.Fatalf("read verified contact: %v", err)
	}
	if !verified {
		t.Fatal("email challenge did not verify its bound contact")
	}
}

func TestT3_2_SessionChecksAccountEpochAndRevocation(t *testing.T) {
	db := newIdentityDB(t)
	account := pendingAccount(t, "session@example.test")
	if err := createAccount(t, db, account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	var sessionID uuid.UUID
	var sessionEnvironmentID, epochEnvironmentID uuid.UUID
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(),
			`UPDATE identity_persons SET state = 'active' WHERE id = $1`, account.PersonID); err != nil {
			return err
		}
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		sessionID, err = store.NewID()
		if err != nil {
			return err
		}
		epochSessionID, err := store.NewID()
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte("synthetic-session-secret"))
		epochDigest := sha256.Sum256([]byte("synthetic-epoch-session-secret"))
		sessionEnvironmentID = newID(t)
		epochEnvironmentID = newID(t)
		if err := identityStore.CreateSession(context.Background(), store.Session{
			ID: sessionID, PersonID: account.PersonID, InstallationID: account.InstallationID,
			ApplicationID: account.ApplicationID, EnvironmentID: sessionEnvironmentID, TokenDigest: digest[:],
			SecurityEpoch: 0, AuthenticationMethod: "email_password", AuthenticatedAt: time.Now().Add(-time.Second),
			ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			return err
		}
		return identityStore.CreateSession(context.Background(), store.Session{
			ID: epochSessionID, PersonID: account.PersonID, InstallationID: account.InstallationID,
			ApplicationID: account.ApplicationID, EnvironmentID: epochEnvironmentID, TokenDigest: epochDigest[:],
			SecurityEpoch: 0, AuthenticationMethod: "email_password", AuthenticatedAt: time.Now().Add(-time.Second),
			ExpiresAt: time.Now().Add(time.Hour),
		})
	}); err != nil {
		t.Fatalf("create active session: %v", err)
	}

	sessionDigest := sha256.Sum256([]byte("synthetic-session-secret"))
	epochDigest := sha256.Sum256([]byte("synthetic-epoch-session-secret"))
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		got, err := identityStore.FindActiveSession(context.Background(), sessionDigest[:], store.SessionScope{
			InstallationID: account.InstallationID, ApplicationID: account.ApplicationID,
			EnvironmentID: sessionEnvironmentID,
		})
		if err == nil && (got.ID != sessionID || got.PersonID != account.PersonID ||
			got.InstallationID != account.InstallationID || got.ApplicationID != account.ApplicationID ||
			got.SecurityEpoch != 0) {
			return fmt.Errorf("active session record did not match")
		}
		if err != nil {
			return err
		}
		wrongScope := store.SessionScope{
			InstallationID: account.InstallationID, ApplicationID: account.ApplicationID,
			EnvironmentID: newID(t),
		}
		if _, err := identityStore.FindActiveSession(context.Background(), sessionDigest[:], wrongScope); !errors.Is(err, store.ErrSessionUnavailable) {
			return fmt.Errorf("wrong environment session error = %v, want ErrSessionUnavailable", err)
		}
		return identityStore.RevokeSession(context.Background(), sessionDigest[:])
	}); err != nil {
		t.Fatalf("find then revoke active session: %v", err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		if _, err := identityStore.FindActiveSession(context.Background(), sessionDigest[:], store.SessionScope{
			InstallationID: account.InstallationID, ApplicationID: account.ApplicationID,
			EnvironmentID: sessionEnvironmentID,
		}); !errors.Is(err, store.ErrSessionUnavailable) {
			return fmt.Errorf("revoked session error = %v, want ErrSessionUnavailable", err)
		}
		if _, err := identityStore.AdvanceSecurityEpoch(context.Background(), account.PersonID); err != nil {
			return err
		}
		if _, err := identityStore.FindActiveSession(context.Background(), epochDigest[:], store.SessionScope{
			InstallationID: account.InstallationID, ApplicationID: account.ApplicationID,
			EnvironmentID: epochEnvironmentID,
		}); !errors.Is(err, store.ErrSessionUnavailable) {
			return fmt.Errorf("old epoch session error = %v, want ErrSessionUnavailable", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("advance epoch and deny stale session: %v", err)
	}
}

func TestT3_2_ExternalBindingUsesExactIssuerAndSubject(t *testing.T) {
	db := newIdentityDB(t)
	first := pendingAccount(t, "provider-one@example.test")
	second := pendingAccount(t, "provider-two@example.test")
	if err := createAccount(t, db, first); err != nil {
		t.Fatalf("create first account: %v", err)
	}
	if err := createAccount(t, db, second); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	activatePerson(t, db, first.PersonID)
	activatePerson(t, db, second.PersonID)
	connectionID := newID(t)
	binding := store.ExternalBinding{
		ID: newID(t), PersonID: first.PersonID, ProviderConnectionID: connectionID,
		Provider: "oidc", Issuer: "https://id.example.test/tenant", Subject: "Subject-A",
	}
	if err := linkBinding(t, db, binding); err != nil {
		t.Fatalf("link external identity: %v", err)
	}
	binding.ID = newID(t)
	binding.PersonID = second.PersonID
	if err := linkBinding(t, db, binding); !errors.Is(err, store.ErrExternalIdentityBound) {
		t.Fatalf("duplicate external binding error = %v, want ErrExternalIdentityBound", err)
	}
	binding.ID = newID(t)
	binding.Issuer = "https://id.example.test/Tenant"
	if err := linkBinding(t, db, binding); err != nil {
		t.Fatalf("case-distinct issuer should remain distinct: %v", err)
	}
}

func linkBinding(t *testing.T, db *storage.DB, binding store.ExternalBinding) error {
	t.Helper()
	return db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		return identityStore.LinkExternalIdentity(context.Background(), binding)
	})
}

func createAccount(t *testing.T, db *storage.DB, input store.PendingAccount) error {
	t.Helper()
	return db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		identityStore, err := store.New(tx)
		if err != nil {
			return err
		}
		return identityStore.CreatePendingAccount(context.Background(), input)
	})
}

func activatePerson(t *testing.T, db *storage.DB, personID uuid.UUID) {
	t.Helper()
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(),
			`UPDATE identity_persons SET state = 'active' WHERE id = $1`, personID)
		return err
	})
	if err != nil {
		t.Fatalf("activate test person: %v", err)
	}
}

func newIdentityDB(t *testing.T) *storage.DB {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("parse PostgreSQL test URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatalf("open isolated PostgreSQL test schema: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close identity test storage: %v", err)
		}
	})
	sqlText, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatalf("read identity migration fragment: %v", err)
	}
	registry, err := migrations.NewRegistry(migrations.Fragment{
		Namespace:  "identity",
		Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(sqlText)}},
	})
	if err != nil {
		t.Fatalf("build isolated identity migration registry: %v", err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatalf("apply identity migration to isolated schema: %v", err)
	}
	return db
}

func pendingAccount(t *testing.T, email string) store.PendingAccount {
	t.Helper()
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	digest := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	challenge := sha256.Sum256([]byte("synthetic-challenge-" + email))
	return store.PendingAccount{
		PersonID: newID(t), EmailID: newID(t), CredentialID: newID(t), ChallengeID: newID(t),
		InstallationID: newID(t), ApplicationID: newID(t), EmailAddress: email,
		PasswordHash:    fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", salt, digest),
		ChallengeDigest: challenge[:], ChallengeExpiry: time.Now().Add(time.Hour),
	}
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatalf("generate UUIDv7 fixture ID: %v", err)
	}
	return id
}

func dbEmailCaseVariant(address string) string {
	return strings.ToUpper(address)
}

func assertCounts(t *testing.T, db *storage.DB, people, emails, credentials, challenges int) {
	t.Helper()
	ctx := context.Background()
	var gotPeople, gotEmails, gotCredentials, gotChallenges int
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			SELECT (SELECT count(*) FROM identity_persons),
			       (SELECT count(*) FROM identity_emails),
			       (SELECT count(*) FROM identity_credentials),
			       (SELECT count(*) FROM identity_challenges)`).Scan(
			&gotPeople, &gotEmails, &gotCredentials, &gotChallenges)
	})
	if err != nil {
		t.Fatalf("count persisted identity rows: %v", err)
	}
	if gotPeople != people || gotEmails != emails || gotCredentials != credentials || gotChallenges != challenges {
		t.Fatalf("identity counts=(%d,%d,%d,%d), want (%d,%d,%d,%d)",
			gotPeople, gotEmails, gotCredentials, gotChallenges, people, emails, credentials, challenges)
	}
}

func existsPerson(t *testing.T, db *storage.DB, id uuid.UUID) bool {
	t.Helper()
	var exists bool
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM identity_persons WHERE id = $1)`, id).Scan(&exists)
	})
	if err != nil {
		t.Fatalf("query person existence: %v", err)
	}
	return exists
}

func TestT3_2_SessionIssuanceAfterCallerTransactionVerification(t *testing.T) {
	db := newIdentityDB(t)
	account := pendingAccount(t, "issuance-clock@example.test")
	if err := createAccount(t, db, account); err != nil {
		t.Fatal("create clock test account")
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `UPDATE identity_persons SET state='active' WHERE id=$1`, account.PersonID); err != nil {
			return err
		}
		var authenticated, started time.Time
		if err := tx.QueryRowContext(context.Background(), `SELECT clock_timestamp(),transaction_timestamp()`).Scan(&authenticated, &started); err != nil {
			return err
		}
		if !authenticated.After(started) {
			return errors.New("verification clock did not advance after transaction start")
		}
		st, err := store.New(tx)
		if err != nil {
			return err
		}
		fingerprint := sha256.Sum256([]byte("synthetic-current-proof"))
		id := newID(t)
		input := store.Session{ID: id, PersonID: account.PersonID, InstallationID: account.InstallationID, ApplicationID: account.ApplicationID, EnvironmentID: newID(t), TokenDigest: fingerprint[:], SecurityEpoch: 0, AuthenticationMethod: "email_password", AuthenticatedAt: authenticated, ExpiresAt: authenticated.Add(12 * time.Hour)}
		if err := st.CreateSession(context.Background(), input); err != nil {
			return err
		}
		var valid bool
		if err := tx.QueryRowContext(context.Background(), `SELECT authenticated_at<=issued_at AND last_seen_at=issued_at AND expires_at<=issued_at+interval '12 hours' AND idle_expires_at<=issued_at+interval '30 minutes' FROM identity_sessions WHERE id=$1`, id).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return errors.New("issued session clocks violate lifecycle bounds")
		}
		input.ID = newID(t)
		input.AuthenticatedAt = authenticated.Add(time.Hour)
		input.ExpiresAt = authenticated.Add(13 * time.Hour)
		if err := st.CreateSession(context.Background(), input); !errors.Is(err, store.ErrSessionUnavailable) {
			return errors.New("future session proof was not rejected")
		}
		return nil
	}); err != nil {
		t.Fatalf("caller transaction session issuance: %v", err)
	}
}
