package mfa

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestNativeWriterPureLegacyIsolation(t *testing.T) {
	if os.Getenv("AMOS_MFA_PROFILE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeWriterPureLegacyIsolation$")
		cmd.Env = append(os.Environ(), "AMOS_MFA_PROFILE_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("profile child failed: %v %s", err, output)
		}
		return
	}
	// An unopened zero runtime is used only to freeze the process profile. No
	// Root.Run/Read or database operation is attempted in this pure test.
	root, err := aw.New(&storage.RuntimeDB{})
	if err != nil {
		t.Fatal(err)
	}
	if err = aw.ActivateW1(root); err != nil {
		t.Fatal(err)
	}
	if got, err := NewStore(&sql.Tx{}); got != nil || err == nil {
		t.Fatal("legacy factor constructor entered W1")
	}
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	scope := Scope{id(), id(), id(), id()}
	retained := &Store{tx: &sql.Tx{}}
	if _, err = retained.Find(context.Background(), scope, id()); !errors.Is(err, ErrFactorStore) {
		t.Fatal("retained legacy read reached SQL")
	}
	if err = retained.RecordFailure(context.Background(), scope, id(), time.Now()); !errors.Is(err, ErrFactorStore) {
		t.Fatal("retained legacy counter reached SQL")
	}
	if got, err := NewWithTxRunner(&storage.RuntimeDB{}, writerTestConfig()); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("legacy service admitted in W1")
	}
}
