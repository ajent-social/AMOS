package materialstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/storage"
)

// The operator must precreate public.email_delivery_material from the exact
// migration fragment and supply a TLS runtime DML role. This test performs no DDL.
func materialRuntimeDB(t *testing.T) *storage.RuntimeDB {
	t.Helper()
	path := os.Getenv("AMOS_MATERIAL_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required material TLS PostgreSQL fixture absent: set AMOS_MATERIAL_RUNTIME_TEST_CONFIG to operator-provided runtime JSON")
	}
	f, e := os.Open(path)
	if e != nil {
		t.Fatal("required material runtime config unavailable")
	}
	defer func() {
		if e := f.Close(); e != nil {
			t.Error("runtime config close failed")
		}
	}()
	var cfg struct {
		Host     string `json:"host"`
		Port     uint16 `json:"port"`
		Database string `json:"database"`
		User     string `json:"user"`
		Password string `json:"password"`
		CAPath   string `json:"ca_path"`
	}
	dec := json.NewDecoder(io.LimitReader(f, 2<<20))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&cfg); e != nil {
		t.Fatal("required material runtime JSON invalid")
	}
	if e := dec.Decode(new(any)); e != io.EOF {
		t.Fatal("required material runtime JSON has trailing data")
	}
	roots, e := os.ReadFile(cfg.CAPath)
	if e != nil {
		t.Fatal("required material runtime CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, e := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User, Password: cfg.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if e != nil {
		t.Fatal("required material runtime TLS connection failed")
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error("runtime close failed")
		}
	})
	return db
}

func TestMaterialRuntimeRequiredService(t *testing.T) {
	db := materialRuntimeDB(t)
	cfg := materialTxConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	scope := []any{cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := db.WithTx(c, nil, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(c, `DELETE FROM public.email_delivery_material WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3`, scope...)
			return e
		}); e != nil {
			t.Error("exact UUID scope material cleanup failed")
		}
	})
	if err := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		var roleOK, tableOK bool
		if err := tx.QueryRowContext(ctx, `SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&roleOK); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT to_regclass('email_delivery_material')=to_regclass('public.email_delivery_material') AND has_table_privilege(current_user,'public.email_delivery_material','SELECT,INSERT,UPDATE,DELETE')`).Scan(&tableOK); err != nil {
			return err
		}
		if !roleOK || !tableOK {
			return errors.New("runtime material prerequisites absent")
		}
		return nil
	}); err != nil {
		t.Fatal("required material DML role or precreated public table absent")
	}
	s, e := NewWithTxRunner(db, cfg)
	if e != nil {
		t.Fatal(e)
	}
	put := func(ref email.SecretReference, m email.PrivateMaterial, fn func(context.Context, *sql.Tx, email.SecretReference, email.PrivateMaterial, time.Time) error) {
		t.Helper()
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error { return fn(ctx, tx, ref, m, time.Now().Add(time.Hour)) }); e != nil {
			t.Fatal("caller transaction material write failed")
		}
	}
	resolve := func(store *Store, ref email.SecretReference, want email.PrivateMaterial) {
		t.Helper()
		got, e := store.ResolveDelivery(ctx, ref)
		if e != nil || got != want {
			t.Fatal("runtime material resolution failed")
		}
	}
	reject := func(store *Store, ref email.SecretReference) {
		t.Helper()
		got, e := store.ResolveDelivery(ctx, ref)
		if e != ErrUnavailable || got != (email.PrivateMaterial{}) {
			t.Fatal("unavailable material disclosed")
		}
	}
	exec := func(q string, ref email.SecretReference) {
		t.Helper()
		id, e := parseRef(ref)
		if e != nil {
			t.Fatal(e)
		}
		args := append(append([]any{}, scope...), id)
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, q, args...); return e }); e != nil {
			t.Fatal("scoped material DML failed")
		}
	}
	ref := reference(t)
	put(ref, material(), s.PutVerificationMaterial)
	resolve(s, ref, material())
	id, e := parseRef(ref)
	if e != nil {
		t.Fatal(e)
	}
	var ciphertext []byte
	if e := db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT ciphertext FROM public.email_delivery_material WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, id).Scan(&ciphertext)
	}); e != nil {
		t.Fatal("ciphertext read failed")
	}
	for _, plain := range []string{material().Recipient, material().ActionURL, "synthetic-private-challenge"} {
		if bytes.Contains(ciphertext, []byte(plain)) {
			t.Fatal("plaintext persisted")
		}
	}
	aborted := reference(t)
	rollback := errors.New("deliberate rollback")
	if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if e := s.PutVerificationMaterial(ctx, tx, aborted, material(), time.Now().Add(time.Hour)); e != nil {
			return e
		}
		return rollback
	}); !errors.Is(e, rollback) {
		t.Fatal("caller rollback not preserved")
	}
	reject(s, aborted)
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, e := s.ResolveDelivery(canceled, ref); e != ErrUnavailable {
		t.Fatal("canceled resolve succeeded")
	}
	canceledRef := reference(t)
	writeCtx, cancelWrite := context.WithCancel(ctx)
	err := db.WithTx(writeCtx, nil, func(tx *sql.Tx) error {
		if err := s.PutVerificationMaterial(writeCtx, tx, canceledRef, material(), time.Now().Add(time.Hour)); err != nil {
			return err
		}
		cancelWrite()
		return writeCtx.Err()
	})
	cancelWrite()
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled caller transaction was not canceled")
	}
	reject(s, canceledRef)
	// Every writer commits through the supplied transaction and enforces its purpose.
	for _, tc := range []struct {
		path     string
		template email.TemplateID
		write    func(context.Context, *sql.Tx, email.SecretReference, email.PrivateMaterial, time.Time) error
	}{
		{"/reset-password", email.TemplatePasswordReset, s.PutPasswordResetMaterial},
		{"/magic-link", email.TemplateSignIn, s.PutSignInMaterial},
		{"/verify-email", email.TemplateVerifyEmail, s.PutVerificationMaterial},
	} {
		m := material()
		m.ActionURL = "https://app.example.test" + tc.path + "?token=synthetic"
		r := reference(t)
		put(r, m, tc.write)
		if got, e := s.ResolveForTemplate(ctx, r, tc.template); e != nil || got != m {
			t.Fatal("correct purpose failed")
		}
		rolledRef := reference(t)
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if err := tc.write(ctx, tx, rolledRef, m, time.Now().Add(time.Hour)); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			t.Fatal("purpose writer rollback failed")
		}
		reject(s, rolledRef)
		wrong := email.TemplatePasswordReset
		if wrong == tc.template {
			wrong = email.TemplateVerifyEmail
		}
		if _, e := s.ResolveForTemplate(ctx, r, wrong); e != ErrUnavailable {
			t.Fatal("cross-purpose disclosure")
		}
		bad := m
		bad.ActionURL = "https://app.example.test/unsupported?token=synthetic"
		if e := db.WithTx(ctx, nil, func(tx *sql.Tx) error { return tc.write(ctx, tx, reference(t), bad, time.Now().Add(time.Hour)) }); e != ErrUnavailable {
			t.Fatal("wrong-purpose write admitted")
		}
	}
	for _, field := range []string{"installation", "application", "environment"} {
		foreign := cfg
		fresh := materialTxConfig(t)
		switch field {
		case "installation":
			foreign.InstallationID = fresh.InstallationID
		case "application":
			foreign.ApplicationID = fresh.ApplicationID
		case "environment":
			foreign.EnvironmentID = fresh.EnvironmentID
		}
		other, e := NewWithTxRunner(db, foreign)
		if e != nil {
			t.Fatal(e)
		}
		reject(other, ref)
		if n, e := other.PruneExpired(ctx, 1000); e != nil || n != 0 {
			t.Fatal("foreign prune affected scope")
		}
	}
	rotatedCfg := cfg
	rotatedCfg.ActiveKeyID = "v2"
	rotatedCfg.Keys = map[string][]byte{"v1": cfg.Keys["v1"], "v2": bytes.Repeat([]byte{33}, 32)}
	rotated, e := NewWithTxRunner(db, rotatedCfg)
	if e != nil {
		t.Fatal(e)
	}
	resolve(rotated, ref, material())
	newRef := reference(t)
	put(newRef, material(), rotated.PutVerificationMaterial)
	resolve(rotated, newRef, material())
	reject(s, newRef)
	rotatedCfg.Keys = map[string][]byte{"v2": rotatedCfg.Keys["v2"]}
	retired, e := NewWithTxRunner(db, rotatedCfg)
	if e != nil {
		t.Fatal(e)
	}
	reject(retired, ref)
	resolve(retired, newRef, material())
	for _, q := range []string{
		`UPDATE public.email_delivery_material SET ciphertext=set_byte(ciphertext,0,get_byte(ciphertext,0)#1) WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`,
		`UPDATE public.email_delivery_material SET nonce=set_byte(nonce,0,get_byte(nonce,0)#1) WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`,
		`UPDATE public.email_delivery_material SET key_id='absent' WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`,
		`UPDATE public.email_delivery_material SET expires_at=expires_at+interval '1 minute' WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`,
	} {
		r := reference(t)
		put(r, material(), s.PutVerificationMaterial)
		exec(q, r)
		reject(s, r)
	}
	for _, expiry := range []time.Time{time.Now().Add(-time.Hour), time.Now().Add(25 * time.Hour)} {
		if err := db.WithTx(ctx, nil, func(tx *sql.Tx) error { return s.PutVerificationMaterial(ctx, tx, reference(t), material(), expiry) }); err != ErrUnavailable {
			t.Fatal("invalid material lifetime admitted")
		}
	}
	expired := reference(t)
	put(expired, material(), s.PutVerificationMaterial)
	exec(`UPDATE public.email_delivery_material SET created_at=transaction_timestamp()-interval '2 hours',expires_at=transaction_timestamp()-interval '1 hour' WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND material_id=$4`, expired)
	reject(s, expired)
	if n, e := s.PruneExpired(ctx, 1); e != nil || n != 1 {
		t.Fatal("bounded expired prune failed", n, e)
	}
	if n, e := s.PruneExpired(ctx, 1000); e != nil || n != 0 {
		t.Fatal("prune removed current material", n, e)
	}
	resolve(s, ref, material())
}
