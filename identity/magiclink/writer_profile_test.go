package magiclink

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func TestNativeWriterPureRetainedLegacyIsolation(t *testing.T) {
	if os.Getenv("AMOS_MAGIC_PROFILE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeWriterPureRetainedLegacyIsolation$")
		cmd.Env = append(os.Environ(), "AMOS_MAGIC_PROFILE_CHILD=1")
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
	if got, err := New(Config{DB: &storage.DB{}}); got != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatal("legacy magic constructor admitted in W1")
	}
	// An unopened concrete runtime deliberately cannot execute a transaction.
	retained := &Service{cfg: Config{DB: &storage.DB{}}}
	for _, handler := range []http.Handler{retained.RequestHandler(), retained.PreviewHandler(), retained.ConfirmHandler()} {
		req := httptest.NewRequest(http.MethodPost, "https://example.test/magic-link", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable || len(rec.Result().Cookies()) != 0 {
			t.Fatal("retained legacy magic handler escaped W1 rejection")
		}
	}
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7())
	if err := retained.ready(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("retained readiness reached SQL")
	}
	if err := retained.request(ctx, "person@example.test", id, "", "", id); !errors.Is(err, ErrUnavailable) {
		t.Fatal("retained request reached SQL")
	}
	if ok, err := retained.preview(ctx, id, make([]byte, 32)); ok || !errors.Is(err, ErrUnavailable) {
		t.Fatal("retained preview reached SQL")
	}
	if retained.bindingMatches(ctx, id, make([]byte, 32)) {
		t.Fatal("retained binding reached SQL")
	}
	req := httptest.NewRequest(http.MethodPost, "https://example.test/magic-link", nil)
	if _, err := retained.confirm(req.Context(), id, make([]byte, 32), "", false, req); !errors.Is(err, ErrUnavailable) {
		t.Fatal("retained confirmation reached SQL")
	}
}
