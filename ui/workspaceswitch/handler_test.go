package workspaceswitch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ajent-social/amos/identity"
	"github.com/google/uuid"
)

func TestT6_4_ConstructorRequiresDomainAndRequestChecks(t *testing.T) {
	check := requestCheck{}
	if _, err := New(nil, check); err == nil {
		t.Fatal("nil authoritative service accepted")
	}
	if _, err := New(service{}, nil); err == nil {
		t.Fatal("nil origin/CSRF checker accepted")
	}
}

func TestT6_4_UnauthenticatedRequestFailsBeforeService(t *testing.T) {
	called := false
	h, err := New(service{called: &called}, requestCheck{})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/workspaces", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || called {
		t.Fatalf("status=%d serviceCalled=%v, want 401 before service", w.Code, called)
	}
}

func TestT6_4_SelectorRequiresCanonicalUUIDv7(t *testing.T) {
	valid := "018f47a0-7b3c-7cc1-9a20-123456789abc"
	id, err := parseID(valid)
	if err != nil || id.String() != valid {
		t.Fatalf("canonical UUIDv7 rejected: id=%s err=%v", id, err)
	}
	for _, invalid := range []string{"018f47a0-7b3c-7cc1-9a20-123456789ABC", "00000000-0000-4000-8000-000000000001", "018f47a0-7b3c-7cc1-9a20-123456789abc/../x"} {
		if _, err := parseID(invalid); err == nil {
			t.Errorf("invalid selector %q accepted", invalid)
		}
	}
}

func TestT6_4_DeniedSnapshotNeverRendersForeignWorkspaceData(t *testing.T) {
	foreignID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abc")
	otherID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abd")
	current := Workspace{ID: foreignID, Name: "Foreign Current Label", Kind: "organization"}
	options := []Workspace{{ID: foreignID, Name: "Foreign Current Label", Kind: "organization"}, {ID: otherID, Name: "Foreign Option Label", Kind: "organization"}}
	snapshot := deniedSnapshotService{current: current, options: options}
	h, err := New(snapshot, requestCheck{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	gotCurrent, gotOptions, snapshotErr := snapshot.Choices(context.Background(), identity.Principal{}, uuid.Nil)
	h.renderWorkspacePage(w, gotCurrent, gotOptions, snapshotErr, "token", false)
	body := w.Body.String()
	if w.Code != http.StatusForbidden || !strings.Contains(body, "workspace.denied") {
		t.Fatalf("status=%d body=%q, want generic workspace denial", w.Code, body)
	}
	for _, valueToReject := range []string{foreignID.String(), otherID.String(), current.Name, options[1].Name} {
		if strings.Contains(body, valueToReject) {
			t.Errorf("denied response disclosed %q: %q", valueToReject, body)
		}
	}
}

func TestT6_4_StaleHintRetriesWithEmptyHintAndRendersAuthorizedChoices(t *testing.T) {
	staleID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abc")
	currentID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abd")
	current := Workspace{ID: currentID, Name: "Current workspace", Kind: "personal"}
	svc := &sequenceService{results: []choiceResult{
		{current: Workspace{ID: staleID, Name: "Stale foreign label", Kind: "organization"}, items: []Workspace{{ID: staleID, Name: "Stale foreign label", Kind: "organization"}}, err: ErrDenied},
		{current: current, items: []Workspace{current}},
	}}
	h, err := New(svc, requestCheck{})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
	r.AddCookie(&http.Cookie{Name: hintCookie, Value: staleID.String()})
	w := httptest.NewRecorder()
	h.renderChoices(w, r, identity.Principal{}, staleID, "token")

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), current.Name) || !strings.Contains(w.Body.String(), "Your selected workspace is no longer available") || strings.Contains(w.Body.String(), "Stale foreign label") {
		t.Fatalf("status=%d body=%q, want only current authorized remediation", w.Code, w.Body.String())
	}
	if len(svc.hints) != 2 || svc.hints[0] != staleID || svc.hints[1] != uuid.Nil {
		t.Fatalf("Choices hints=%v, want stale hint followed by empty hint", svc.hints)
	}
	cleared := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == hintCookie && cookie.MaxAge < 0 && cookie.Value == "" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("stale workspace hint cookie was not cleared")
	}
}

func TestT6_4_SecondDeniedChoiceErrorDiscardsForeignDTO(t *testing.T) {
	foreignID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abc")
	otherID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abd")
	foreign := Workspace{ID: foreignID, Name: "Foreign current label", Kind: "organization"}
	option := Workspace{ID: otherID, Name: "Foreign option label", Kind: "organization"}
	svc := &sequenceService{results: []choiceResult{
		{current: foreign, items: []Workspace{foreign, option}, err: ErrDenied},
		{current: foreign, items: []Workspace{foreign, option}, err: ErrDenied},
	}}
	h, err := New(svc, requestCheck{})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
	r.AddCookie(&http.Cookie{Name: hintCookie, Value: foreignID.String()})
	w := httptest.NewRecorder()
	h.renderChoices(w, r, identity.Principal{}, foreignID, "token")

	body := w.Body.String()
	if w.Code != http.StatusForbidden || !strings.Contains(body, "workspace.denied") {
		t.Fatalf("status=%d body=%q, want generic denial", w.Code, body)
	}
	for _, valueToReject := range []string{foreignID.String(), otherID.String(), foreign.Name, option.Name} {
		if strings.Contains(body, valueToReject) {
			t.Errorf("denied response disclosed %q: %q", valueToReject, body)
		}
	}
	if len(svc.hints) != 2 || svc.hints[1] != uuid.Nil {
		t.Fatalf("Choices hints=%v, want empty-hint retry", svc.hints)
	}
}

func TestT6_4_InvalidCurrentChoicesDTOIsNotRendered(t *testing.T) {
	staleID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abc")
	invalidID := uuid.MustParse("018f47a0-7b3c-7cc1-9a20-123456789abd")
	invalid := Workspace{ID: invalidID, Name: "Unvalidated workspace name", Kind: "unknown"}
	svc := &sequenceService{results: []choiceResult{
		{current: Workspace{ID: staleID, Name: "Stale label", Kind: "organization"}, err: ErrDenied},
		{current: invalid, items: []Workspace{invalid}},
	}}
	h, err := New(svc, requestCheck{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.renderChoices(w, httptest.NewRequest(http.MethodGet, "/workspaces", nil), identity.Principal{}, staleID, "token")
	body := w.Body.String()
	if w.Code != http.StatusForbidden || !strings.Contains(body, "workspace.denied") {
		t.Fatalf("status=%d body=%q, want generic denial", w.Code, body)
	}
	for _, valueToReject := range []string{staleID.String(), invalidID.String(), "Stale label", invalid.Name} {
		if strings.Contains(body, valueToReject) {
			t.Errorf("invalid remediation disclosed %q: %q", valueToReject, body)
		}
	}
}

type choiceResult struct {
	current Workspace
	items   []Workspace
	err     error
}

type sequenceService struct {
	results []choiceResult
	hints   []uuid.UUID
}

func (s *sequenceService) Choices(_ context.Context, _ identity.Principal, hint uuid.UUID) (Workspace, []Workspace, error) {
	s.hints = append(s.hints, hint)
	if len(s.results) == 0 {
		return Workspace{}, nil, errors.New("unexpected Choices call")
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result.current, result.items, result.err
}

func (*sequenceService) Select(context.Context, identity.Principal, uuid.UUID) (Workspace, error) {
	return Workspace{}, ErrUnavailable
}

type service struct{ called *bool }

func (s service) Choices(_ context.Context, _ identity.Principal, _ uuid.UUID) (Workspace, []Workspace, error) {
	if s.called != nil {
		*s.called = true
	}
	return Workspace{}, nil, ErrUnavailable
}

type deniedSnapshotService struct {
	current Workspace
	options []Workspace
}

func (s deniedSnapshotService) Choices(context.Context, identity.Principal, uuid.UUID) (Workspace, []Workspace, error) {
	return s.current, s.options, ErrDenied
}
func (s deniedSnapshotService) Select(context.Context, identity.Principal, uuid.UUID) (Workspace, error) {
	return Workspace{}, ErrUnavailable
}
func (s service) Select(_ context.Context, _ identity.Principal, _ uuid.UUID) (Workspace, error) {
	if s.called != nil {
		*s.called = true
	}
	return Workspace{}, ErrUnavailable
}

type requestCheck struct{}

func (requestCheck) Valid(*http.Request) bool            { return false }
func (requestCheck) Token(*http.Request) (string, error) { return "token", nil }
