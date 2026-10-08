package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
)

func TestNativeWriterPureRetainedLegacyIsolation(t *testing.T) {
	if os.Getenv("AMOS_LOGIN_PROFILE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeWriterPureRetainedLegacyIsolation$")
		cmd.Env = append(os.Environ(), "AMOS_LOGIN_PROFILE_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("profile child failed: %v %s", err, output)
		}
		return
	}
	root, err := aw.New(&storage.RuntimeDB{})
	if err != nil {
		t.Fatal(err)
	}
	if err = aw.ActivateW1(root); err != nil {
		t.Fatal(err)
	}
	if got, err := NewWithTxRunner(&storage.RuntimeDB{}, loginTxConfig(t)); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("legacy login constructor admitted in W1")
	}
	// The zero runtime and dependencies cannot execute SQL or password work. A
	// retained pre-cutover service must reject before touching either one.
	retained := &Service{db: &storage.RuntimeDB{}, cfg: loginTxConfig(t)}
	for _, entry := range []http.HandlerFunc{retained.register, retained.signIn} {
		req := httptest.NewRequest(http.MethodPost, "https://example.test/auth", strings.NewReader(`{"email":"person@example.test","password":"synthetic password"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		entry.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable || len(rec.Result().Cookies()) != 0 {
			t.Fatal("retained legacy login escaped W1 rejection")
		}
	}
}
