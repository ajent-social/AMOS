package materialstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func setup(t *testing.T) (*Store, Config) {
	t.Helper()
	_, schema := testkit.NewPostgres(t)
	dsn, e := testkit.DatabaseURL()
	if e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := storage.Open(context.Background(), u.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	b, e := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "fragments", "email-material.sql"))
	if e != nil {
		t.Fatal(e)
	}
	registry, e := migrations.NewRegistry(migrations.Fragment{Namespace: "email", Migrations: []migrations.Migration{{Sequence: 1, Name: "protected_material", SQL: string(b)}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = storage.Migrate(context.Background(), db, registry); e != nil {
		t.Fatal(e)
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		t.Fatal(e)
	}
	id := func() uuid.UUID {
		v, e := uuid.NewV7()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	cfg := Config{DB: db, InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), ActiveKeyID: "v1", Keys: map[string][]byte{"v1": key}, ApplicationOrigin: "https://app.example.test"}
	s, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return s, cfg
}
func reference(t *testing.T) email.SecretReference {
	t.Helper()
	id, e := uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	return email.SecretReference("material:" + id.String())
}
func material() email.PrivateMaterial {
	return email.PrivateMaterial{Recipient: "synthetic@example.test", ActionURL: "https://app.example.test/verify-email?token=synthetic-private-challenge"}
}
func write(t *testing.T, s *Store, ref email.SecretReference) {
	t.Helper()
	e := s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return s.PutVerificationMaterial(context.Background(), tx, ref, material(), time.Now().UTC().Add(30*time.Minute))
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestProtectedMaterialEncryptedRoundTripAndRollback(t *testing.T) {
	s, _ := setup(t)
	ref := reference(t)
	write(t, s, ref)
	m, e := s.ResolveDelivery(context.Background(), ref)
	if e != nil || m != material() {
		t.Fatal("roundtrip failed", e)
	}
	var ciphertext []byte
	e = s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT ciphertext FROM email_delivery_material`).Scan(&ciphertext)
	})
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(ciphertext, []byte(material().Recipient)) || bytes.Contains(ciphertext, []byte("synthetic-private-challenge")) {
		t.Fatal("plaintext material persisted")
	}
	aborted := reference(t)
	sentinel := errors.New("deliberate transaction cancellation")
	e = s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if e := s.PutVerificationMaterial(context.Background(), tx, aborted, material(), time.Now().UTC().Add(time.Minute)); e != nil {
			return e
		}
		return sentinel
	})
	if e == nil {
		t.Fatal("rollback ignored")
	}
	if _, e = s.ResolveDelivery(context.Background(), aborted); e != ErrUnavailable {
		t.Fatal("rolled-back material resolved")
	}
}
func TestProtectedMaterialRejectsScopeMetadataCiphertextAndExpiryTampering(t *testing.T) {
	for _, change := range []string{"expiry", "ciphertext", "key", "nonce"} {
		t.Run(change, func(t *testing.T) {
			s, cfg := setup(t)
			ref := reference(t)
			write(t, s, ref)
			cfg.EnvironmentID, _ = uuid.NewV7()
			other, e := New(cfg)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = other.ResolveDelivery(context.Background(), ref); e != ErrUnavailable {
				t.Fatal("cross-environment material disclosed")
			}
			statement := map[string]string{"expiry": `UPDATE email_delivery_material SET expires_at=expires_at+interval '1 minute'`, "ciphertext": `UPDATE email_delivery_material SET ciphertext=set_byte(ciphertext,0,get_byte(ciphertext,0)#1)`, "key": `UPDATE email_delivery_material SET key_id='absent'`, "nonce": `UPDATE email_delivery_material SET nonce=set_byte(nonce,0,get_byte(nonce,0)#1)`}[change]
			e = s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error { _, e := tx.Exec(statement); return e })
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.ResolveDelivery(context.Background(), ref); e != ErrUnavailable {
				t.Fatal("tampered material disclosed")
			}
		})
	}
	s, _ := setup(t)
	ref := reference(t)
	write(t, s, ref)
	e := s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, e := tx.Exec(`UPDATE email_delivery_material SET created_at=transaction_timestamp()-interval '2 hour',expires_at=transaction_timestamp()-interval '1 hour'`)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveDelivery(context.Background(), ref); e != ErrUnavailable {
		t.Fatal("expired material disclosed")
	}
	n, e := s.PruneExpired(context.Background(), 1)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
func TestProtectedMaterialRotationAndInputBounds(t *testing.T) {
	s, cfg := setup(t)
	ref := reference(t)
	write(t, s, ref)
	newKey := make([]byte, 32)
	if _, e := rand.Read(newKey); e != nil {
		t.Fatal(e)
	}
	cfg.Keys["v2"] = newKey
	cfg.ActiveKeyID = "v2"
	rotated, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = rotated.ResolveDelivery(context.Background(), ref); e != nil {
		t.Fatal("retained key failed", e)
	}
	cfg.Keys = map[string][]byte{"v2": newKey}
	withoutOld, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = withoutOld.ResolveDelivery(context.Background(), ref); e != ErrUnavailable {
		t.Fatal("missing old key accepted")
	}
	for _, bad := range []email.PrivateMaterial{{Recipient: "x@example.test\r\nBcc: evil", ActionURL: material().ActionURL}, {Recipient: material().Recipient, ActionURL: "https://evil.example.test/verify-email"}, {Recipient: material().Recipient, ActionURL: "https://app.example.test/signin"}} {
		e = s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
			return s.PutVerificationMaterial(context.Background(), tx, reference(t), bad, time.Now().UTC().Add(time.Minute))
		})
		if e == nil {
			t.Fatal("invalid material accepted")
		}
	}
	if _, e = s.PruneExpired(context.Background(), 1001); e != ErrConfiguration {
		t.Fatal("unbounded deletion accepted")
	}
	if e = s.db.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolveDelivery(context.Background(), ref); e != ErrUnavailable {
		t.Fatal("outage ignored")
	}
}

func TestResetProtectedMaterialCannotCrossAuthenticationPurpose(t *testing.T) {
	s, _ := setup(t)
	ref := reference(t)
	reset := material()
	reset.ActionURL = "https://app.example.test/reset-password?token=synthetic"
	expiry := time.Now().UTC().Add(time.Hour)
	if e := s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error { return s.PutVerificationMaterial(context.Background(), tx, ref, reset, expiry) }); e == nil {
		t.Fatal("reset link entered verification writer")
	}
	if e := s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return s.PutPasswordResetMaterial(context.Background(), tx, ref, reset, expiry)
	}); e != nil {
		t.Fatal(e)
	}
	if got, e := s.ResolveForTemplate(context.Background(), ref, email.TemplatePasswordReset); e != nil || got != reset {
		t.Fatal("reset material unavailable", e)
	}
	if _, e := s.ResolveForTemplate(context.Background(), ref, email.TemplateVerifyEmail); e == nil {
		t.Fatal("reset material reused as verification")
	}
	if _, e := s.ResolveForTemplate(context.Background(), ref, email.TemplateSignIn); e == nil {
		t.Fatal("unsupported purpose admitted")
	}
	magic := material()
	magic.ActionURL = "https://app.example.test/magic-link?challenge=synthetic&token=synthetic"
	magicRef := reference(t)
	if err := s.db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		return s.PutSignInMaterial(context.Background(), tx, magicRef, magic, time.Now().Add(time.Hour))
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ResolveForTemplate(context.Background(), magicRef, email.TemplateSignIn); err != nil || got != magic {
		t.Fatal("magic material unavailable", err)
	}
	if _, err := s.ResolveForTemplate(context.Background(), magicRef, email.TemplatePasswordReset); err == nil {
		t.Fatal("magic material reused as reset")
	}
	verification := reference(t)
	write(t, s, verification)
	if _, e := s.ResolveForTemplate(context.Background(), verification, email.TemplatePasswordReset); e == nil {
		t.Fatal("verification material reused as reset")
	}
}
