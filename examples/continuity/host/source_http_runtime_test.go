package host

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// These are new ordinary HTTP checks over synthetic account/session setup and
// actual native middleware. They do not exercise a complete native enrollment
// journey, failed commit, writer schedules, or a public listener.
func TestPrivateSourceHTTPRuntimeRequiredService(t *testing.T) {
	f := privateReadFixture(t)
	if f == nil {
		return
	}
	handler := f.core.sourceHandler()
	request := func(method, path string, cookie, fragment bool, ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil).WithContext(ctx)
		r.Host = "reader.example.test"
		if cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: f.token})
		}
		if fragment {
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Target", "continuity-source-content")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	t.Run("native admitted full literal search", func(t *testing.T) {
		w := request(http.MethodGet, sourcePath+"?q=50%25_", true, false, f.ctx)
		if w.Code != 200 || !strings.Contains(w.Body.String(), f.sourceID.String()) || strings.Contains(w.Body.String(), f.foreignID.String()) || !strings.Contains(w.Body.String(), "<html") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("actual HTTP list failed: status %d", w.Code)
		}
	})
	t.Run("native admitted escaped detail fragment", func(t *testing.T) {
		w := request(http.MethodGet, sourcePath+"/"+f.sourceID.String(), true, true, f.ctx)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "&lt;script&gt;") || strings.Contains(w.Body.String(), "<script>untrusted") || strings.Contains(w.Body.String(), "<html") || !strings.Contains(w.Body.String(), "continuity-source-content") {
			t.Fatalf("actual HTTP detail failed: status %d", w.Code)
		}
	})
	t.Run("native admitted HEAD completes without body", func(t *testing.T) {
		w := request(http.MethodHead, sourcePath+"/"+f.sourceID.String(), true, false, f.ctx)
		n, err := strconv.Atoi(w.Header().Get("Content-Length"))
		if w.Code != 200 || w.Body.Len() != 0 || err != nil || n < 1 {
			t.Fatalf("actual HTTP HEAD failed: status %d", w.Code)
		}
	})
	t.Run("foreign detail has no source output", func(t *testing.T) {
		w := request(http.MethodGet, sourcePath+"/"+f.foreignID.String(), true, true, f.ctx)
		if w.Code != 404 || strings.Contains(w.Body.String(), "Literal 50") || strings.Contains(w.Body.String(), f.foreignID.String()) {
			t.Fatalf("foreign HTTP detail failed: status %d", w.Code)
		}
	})
	t.Run("missing native credential has no source output", func(t *testing.T) {
		w := request(http.MethodGet, sourcePath, false, false, f.ctx)
		if w.Code != 401 || strings.Contains(w.Body.String(), f.sourceID.String()) || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("missing credential HTTP response failed: status %d", w.Code)
		}
	})
	t.Run("canceled original request has no source output", func(t *testing.T) {
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		w := request(http.MethodGet, sourcePath, true, false, ctx)
		if w.Code != 503 || strings.Contains(w.Body.String(), f.sourceID.String()) {
			t.Fatalf("canceled HTTP response failed: status %d", w.Code)
		}
	})
	t.Run("actual stored digest corruption is unavailable", func(t *testing.T) {
		err := f.core.database.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(f.ctx, `UPDATE public.continuity_sources SET sha256=$6 WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND workspace_id=$4 AND id=$5`, f.cfg.InstallationID, f.cfg.ApplicationID, f.cfg.EnvironmentID, f.workspaceID, f.sourceID, strings.Repeat("0", 64))
			return err
		})
		if err != nil {
			t.Fatal("owned synthetic digest setup failed")
		}
		w := request(http.MethodGet, sourcePath+"/"+f.sourceID.String(), true, false, f.ctx)
		if w.Code != 503 || strings.Contains(w.Body.String(), "Literal 50") || strings.Contains(w.Body.String(), f.sourceID.String()) {
			t.Fatalf("corrupt digest HTTP response failed: status %d", w.Code)
		}
	})
}
