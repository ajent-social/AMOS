package render

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func validView() ViewModel {
	return ViewModel{Page: Page{ID: "profile", Version: ContractVersion}, Title: "Profile", Heading: "Your profile", ProfileName: "Ada", Form: &Form{Action: "/ui", Method: "POST", Label: "Display name", Field: "name", Value: "Ada", Submit: "Save"}}
}

func TestDefaultEscapesUserDataAndUsesDocumentFallback(t *testing.T) {
	renderer, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	view := validView()
	view.ProfileName = `<script>globalThis.pwned=1</script>`
	req := httptest.NewRequest("GET", "/ui", nil)
	w := httptest.NewRecorder()
	if err := renderer.Render(context.Background(), w, req, view.Page, view); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "<script>globalThis") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("profile text was not escaped: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<!doctype html>") {
		t.Fatal("missing full document fallback")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache policy=%q", w.Header().Get("Cache-Control"))
	}
}

func TestDefaultFragmentsRequireDeclaredHTMXTarget(t *testing.T) {
	renderer, _ := NewDefault()
	for _, tc := range []struct{ htmx, target, wantFragment string }{{"true", MainTarget, "yes"}, {"true", "other", "no"}, {"false", MainTarget, "no"}} {
		req := httptest.NewRequest("GET", "/ui", nil)
		req.Header.Set("HX-Request", tc.htmx)
		req.Header.Set("HX-Target", tc.target)
		w := httptest.NewRecorder()
		view := validView()
		if err := renderer.Render(context.Background(), w, req, view.Page, view); err != nil {
			t.Fatal(err)
		}
		fragment := strings.Contains(w.Body.String(), "<!doctype html>") == false
		if fragment != (tc.wantFragment == "yes") {
			t.Fatalf("metadata %q/%q yielded fragment=%v", tc.htmx, tc.target, fragment)
		}
		if w.Header().Get("Vary") == "" {
			t.Fatal("missing Vary header")
		}
	}
}

func TestDefaultRejectsExternalActionURL(t *testing.T) {
	renderer, _ := NewDefault()
	view := validView()
	view.Form.Action = "https://example.invalid/"
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui", nil)
	if err := renderer.Render(context.Background(), w, req, view.Page, view); err != ErrInvalidView {
		t.Fatalf("error=%v", err)
	}
}

func TestSafeLocalPathRejectsAmbiguousDestinations(t *testing.T) {
	for _, path := range []string{"https://example.invalid", "//example.invalid", "/../signout", "/a/./b", "/%2f/other"} {
		if safeLocalPath(path) {
			t.Errorf("unsafe path accepted: %q", path)
		}
	}
	if !safeLocalPath("/account/profile?tab=details") {
		t.Fatal("safe local path rejected")
	}
}
