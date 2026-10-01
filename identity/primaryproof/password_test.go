package primaryproof

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/password/policy"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/google/uuid"
)

// This budget is a test double; transport admission is tested in protection.
type fixtureBudget struct{}

func (fixtureBudget) Allow(context.Context, string) error { return nil }

func TestCurrentPasswordRequiresVerifiedActiveMatchingEpoch(t *testing.T) {
	db, _ := testkit.NewPostgres(t)
	ctx := context.Background()
	schema, err := os.ReadFile("../../migrations/fragments/identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	blocklist, err := policy.New(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := password.New(blocklist, fixtureBudget{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	phrase := "current primary credential for factor enrollment"
	encoded, err := hasher.Hash(ctx, "fixture", phrase)
	if err != nil {
		t.Fatal(err)
	}
	nextID := func() uuid.UUID {
		t.Helper()
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	person, email, credential, challenge, installation, application, environment := nextID(), nextID(), nextID(), nextID(), nextID(), nextID(), nextID()
	digest := sha256.Sum256([]byte("primary fixture challenge"))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(tx)
	if err != nil {
		t.Fatal(err)
	}
	err = st.CreatePendingAccount(ctx, store.PendingAccount{PersonID: person, EmailID: email, CredentialID: credential, ChallengeID: challenge, InstallationID: installation, ApplicationID: application, EmailAddress: "factor@example.test", PasswordHash: encoded, ChallengeDigest: digest[:], ChallengeExpiry: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE identity_persons SET state='active' WHERE id=$1`, person); err != nil {
		t.Fatal(err)
	}
	if err = st.MarkEmailVerified(ctx, person, email); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	proof, err := authproof.NewVerifiedCredential(person, installation, application, environment, 0, "email_password", now, "aal1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	principal, ok := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(ctx, proof))
	if !ok {
		t.Fatal("fixture principal unavailable")
	}
	verifier, err := New(hasher)
	if err != nil {
		t.Fatal(err)
	}
	check := func(input string, want bool) {
		t.Helper()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
				t.Error(err)
			}
		}()
		verified, err := verifier.VerifyCurrentPassword(ctx, tx, principal, input)
		if want {
			if err != nil || verified.PersonID() != person || verified.Assurance() != "aal1" || verified.Method() != "email_password" {
				t.Fatal("verified primary proof unavailable")
			}
		} else if err == nil {
			t.Fatal("unverified current password minted proof")
		}
	}
	check(phrase, true)
	check("incorrect primary password for enrollment", false)
	if _, err := db.ExecContext(ctx, `UPDATE identity_persons SET security_epoch=security_epoch+1 WHERE id=$1`, person); err != nil {
		t.Fatal(err)
	}
	check(phrase, false)
}
