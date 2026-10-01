package mfa

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestT3_15_RealPostgresPendingActivationAndStepReplay(t *testing.T) {
	db, query := newMFADatabase(t)
	scope := createActivePerson(t, db)
	factorID := newID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	ciphertext := []byte("test-only encrypted seed bytes with key identifier 000000000000000000000000")
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := NewStore(tx)
		if err != nil {
			return err
		}
		return st.CreatePending(context.Background(), Factor{ID: factorID, Scope: scope, SeedCiphertext: ciphertext, State: FactorPending}, 0, now)
	})
	if err != nil {
		t.Fatal(err)
	}
	factor := findFactor(t, db, scope, factorID)
	if factor.State != FactorPending || factor.PendingSecurityEpoch == nil || *factor.PendingSecurityEpoch != 0 || factor.ExpiresAt == nil || !factor.ExpiresAt.Equal(now.Add(DefaultPendingLifetime)) {
		t.Fatalf("pending factor state mismatch: %+v", factor)
	}
	if string(factor.SeedCiphertext) != string(ciphertext) {
		t.Fatal("factor store changed encrypted seed bytes")
	}
	wrongScope := scope
	wrongScope.PersonID = newID(t)
	if _, err := findFactorErr(t, db, wrongScope, factorID); !errors.Is(err, ErrFactorAbsent) {
		t.Fatalf("foreign person read err=%v, want absent", err)
	}
	step := time.Now().Unix() / int64(TOTPPeriod/time.Second)
	activeCiphertext := []byte("active test-only encrypted seed bytes with key identifier 000000000000000000")
	err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := NewStore(tx)
		if err != nil {
			return err
		}
		return st.ActivatePendingAndConsumeStep(context.Background(), scope, factorID, 0, step, activeCiphertext, time.Now().UTC())
	})
	if err != nil {
		t.Fatal(err)
	}
	active := findFactor(t, db, scope, factorID)
	if active.State != FactorActive || active.LastUsedStep != step || active.PendingSecurityEpoch != nil || active.ExpiresAt != nil || active.ActivatedAt == nil || string(active.SeedCiphertext) != string(activeCiphertext) {
		t.Fatalf("active factor state mismatch: %+v", active)
	}
	var accepted bool
	err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := NewStore(tx)
		if err != nil {
			return err
		}
		accepted, err = st.AcceptStep(context.Background(), scope, factorID, step, time.Now().UTC())
		return err
	})
	if err != nil || accepted {
		t.Fatalf("same accepted step replay accepted=%v err=%v", accepted, err)
	}
	err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := NewStore(tx)
		if err != nil {
			return err
		}
		accepted, err = st.AcceptStep(context.Background(), scope, factorID, step+1, time.Now().UTC())
		return err
	})
	if err != nil || !accepted {
		t.Fatalf("fresh step accepted=%v err=%v", accepted, err)
	}
	var rows int
	if err := query.QueryRow(`SELECT count(*) FROM identity_totp_factors WHERE person_id=$1 AND state='active'`, scope.PersonID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("persisted active factors=%d err=%v", rows, err)
	}
}

func TestT3_15_RealPostgresPendingEpochExpiryAndAttemptLock(t *testing.T) {
	db, _ := newMFADatabase(t)
	scope := createActivePerson(t, db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	firstID := newID(t)
	createPending(t, db, scope, firstID, 0, now)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE identity_persons SET security_epoch=1 WHERE id=$1`, scope.PersonID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := NewStore(tx)
		if err != nil {
			return err
		}
		return st.ActivatePendingAndConsumeStep(context.Background(), scope, firstID, 0, 123, []byte("test-only encrypted active seed with at least 32 bytes key id 0000000000"), now.Add(time.Second))
	}); !errors.Is(err, ErrFactorAbsent) {
		t.Fatalf("stale pending epoch activation err=%v, want absent", err)
	}
	secondID := newID(t)
	createPending(t, db, scope, secondID, 1, now.Add(DefaultPendingLifetime+time.Second))
	factor := findFactor(t, db, scope, secondID)
	if factor.State != FactorPending {
		t.Fatalf("expired pending row was not retired before replacement: %+v", factor)
	}
	for attempt := 0; attempt < DefaultMaxAttempts; attempt++ {
		if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
			st, err := NewStore(tx)
			if err != nil {
				return err
			}
			return st.RecordFailure(context.Background(), scope, secondID, time.Now().UTC())
		}); err != nil {
			t.Fatal(err)
		}
	}
	factor = findFactor(t, db, scope, secondID)
	if factor.FailedAttempts != DefaultMaxAttempts || !(&Store{}).Locked(factor, time.Now().UTC()) {
		t.Fatalf("attempt lock not persisted: %+v", factor)
	}
}

func TestT3_15_TOTPWindowAndInputBoundary(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "AMOS", AccountName: "person", Period: uint(TOTPPeriod / time.Second), SecretSize: 20, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	code, err := totp.GenerateCodeCustom(key.Secret(), now, totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	if step, ok := matchingStep(code, key.Secret(), now); !ok || step != now.Unix()/30 {
		t.Fatalf("valid current TOTP step mismatch: step=%d ok=%v", step, ok)
	}
	if validTOTPCode("12a456") || validTOTPCode("12345") || validTOTPCode("１２３４５６") {
		t.Fatal("non-ASCII, non-numeric, or short TOTP input accepted")
	}
}

func newMFADatabase(t *testing.T) (*storage.DB, *sql.DB) {
	t.Helper()
	query, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	values.Set("search_path", schema)
	parsed.RawQuery = values.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	identitySQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	protectionSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "identity-protection.sql"))
	if err != nil {
		t.Fatal(err)
	}
	assuranceFragment, err := migrations.SessionAssurance(3)
	if err != nil {
		t.Fatal(err)
	}
	mfaProtectionFragment, err := migrations.MFAProtection(4)
	if err != nil {
		t.Fatal(err)
	}
	mfaFragment, err := Fragment(5)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.NewRegistry(
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 1, Name: "identity_base", SQL: string(identitySQL)}}},
		migrations.Fragment{Namespace: "identity", Migrations: []migrations.Migration{{Sequence: 2, Name: "identity_protection", SQL: string(protectionSQL)}}},
		assuranceFragment,
		mfaProtectionFragment,
		mfaFragment,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	return db, query
}

func createActivePerson(t *testing.T, db *storage.DB) Scope {
	t.Helper()
	ids := Scope{InstallationID: newID(t), ApplicationID: newID(t), EnvironmentID: newID(t), PersonID: newID(t)}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, ids.PersonID, ids.InstallationID, ids.ApplicationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ids
}

func createPending(t *testing.T, db *storage.DB, scope Scope, id uuid.UUID, epoch int64, now time.Time) {
	t.Helper()
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := NewStore(tx)
		if err != nil {
			return err
		}
		return st.CreatePending(context.Background(), Factor{ID: id, Scope: scope, SeedCiphertext: []byte("test-only encrypted seed bytes with key identifier 000000000000000000"), State: FactorPending}, epoch, now)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func findFactor(t *testing.T, db *storage.DB, scope Scope, id uuid.UUID) Factor {
	t.Helper()
	factor, err := findFactorErr(t, db, scope, id)
	if err != nil {
		t.Fatal(err)
	}
	return factor
}

func findFactorErr(t *testing.T, db *storage.DB, scope Scope, id uuid.UUID) (Factor, error) {
	t.Helper()
	var factor Factor
	err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		st, err := NewStore(tx)
		if err != nil {
			return err
		}
		factor, err = st.Find(context.Background(), scope, id)
		return err
	})
	return factor, err
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
