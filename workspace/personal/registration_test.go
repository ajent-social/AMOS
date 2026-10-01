package personal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	identitystore "github.com/ajent-social/amos/identity/store"
)

func TestRegistrationPendingAtomicBoundAndIdempotent(t *testing.T) {
	db, scope, _ := newPersonalDB(t)
	ctx := context.Background()
	input := identitystore.PendingAccount{PersonID: newID(t), EmailID: newID(t), CredentialID: newID(t), ChallengeID: newID(t), InstallationID: scope.InstallationID, ApplicationID: scope.ApplicationID, EmailAddress: "pending@example.test", PasswordHash: "$argon2id$v=19$m=65536,t=3,p=1$" + base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("s", 16))) + "$" + base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("h", 32))), ChallengeExpiry: time.Now().Add(time.Hour)}
	digest := sha256.Sum256([]byte("synthetic verification token"))
	input.ChallengeDigest = digest[:]
	var proof identitystore.PendingRegistration
	var result Result
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		store, e := identitystore.New(tx)
		if e != nil {
			return e
		}
		proof, e = store.CreatePendingRegistration(ctx, input)
		if e != nil {
			return e
		}
		participant, e := New(tx)
		if e != nil {
			return e
		}
		result, e = participant.BootstrapPending(ctx, proof, newID(t))
		if e != nil {
			return e
		}
		again, e := participant.BootstrapPending(ctx, proof, newID(t))
		if e != nil {
			return e
		}
		if !again.Existing || again.Workspace.ID != result.Workspace.ID {
			t.Fatal("same transaction replay created different workspace")
		}
		if _, e = participant.BootstrapPending(ctx, identitystore.PendingRegistration{}, newID(t)); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("zero capability accepted: %v", e)
		}
		var state string
		if e = tx.QueryRowContext(ctx, "SELECT state FROM identity_persons WHERE id=$1", input.PersonID).Scan(&state); e != nil {
			return e
		}
		if state != identitystore.AccountPendingVerification {
			t.Fatalf("registration activated person: %s", state)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		participant, e := New(tx)
		if e != nil {
			return e
		}
		_, e = participant.BootstrapPending(ctx, proof, newID(t))
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("cross transaction capability accepted: %v", e)
		}
		var count int
		e = tx.QueryRowContext(ctx, "SELECT count(*) FROM workspaces WHERE personal_owner_id=$1", input.PersonID).Scan(&count)
		if e == nil && count != 1 {
			t.Fatalf("workspace count=%d", count)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationPendingRollsBackBothRows(t *testing.T) {
	db, scope, _ := newPersonalDB(t)
	ctx := context.Background()
	person := newID(t)
	marker := errors.New("abort registration")
	err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		store, e := identitystore.New(tx)
		if e != nil {
			return e
		}
		digest := sha256.Sum256([]byte("rollback token"))
		proof, e := store.CreatePendingRegistration(ctx, identitystore.PendingAccount{PersonID: person, EmailID: newID(t), CredentialID: newID(t), ChallengeID: newID(t), InstallationID: scope.InstallationID, ApplicationID: scope.ApplicationID, EmailAddress: "rollback-pending@example.test", PasswordHash: "$argon2id$v=19$m=65536,t=3,p=1$" + base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("s", 16))) + "$" + base64.RawStdEncoding.EncodeToString([]byte(strings.Repeat("h", 32))), ChallengeDigest: digest[:], ChallengeExpiry: time.Now().Add(time.Hour)})
		if e != nil {
			return e
		}
		participant, e := New(tx)
		if e != nil {
			return e
		}
		if _, e = participant.BootstrapPending(ctx, proof, newID(t)); e != nil {
			return e
		}
		return marker
	})
	if !errors.Is(err, marker) {
		t.Fatal(err)
	}
	if err = db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var count int
		if e := tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM identity_persons WHERE id=$1)+(SELECT count(*) FROM workspaces WHERE personal_owner_id=$1)", person).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			t.Fatalf("rollback left %d rows", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
