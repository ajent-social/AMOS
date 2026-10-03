// Package billingcheckout renders the workspace billing page around an
// authoritative application supplied billing service. Browser return values
// are selectors only; only the service's fresh projection can confirm access.
package billingcheckout

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/ajent-social/amos/identity"
	"github.com/google/uuid"
)

var (
	ErrDenied      = errors.New("billing page denied")
	ErrUnavailable = errors.New("billing page unavailable")
)

const maxPolls = 12

//go:embed return.js
var returnScript embed.FS

type Offer struct {
	PriceKey string
	Plan     string
	Amount   int64
	Currency string
	Interval string
}

type PageState struct {
	Workspace string
	Payer     string
	Offers    []Offer
	Plan      string
	Currency  string
	Paid      bool
	CanManage bool
	CanAct    bool
	State     string
	IntentID  uuid.UUID
	RequestID string
}

// Service resolves every field from current principal, workspace membership,
// billing projection, catalog and tenant policy. It must not use browser claims.
type Service interface {
	Page(context.Context, identity.Principal) (PageState, error)
	Start(context.Context, identity.Principal, string, string) (StartResult, error)
	Status(context.Context, identity.Principal, uuid.UUID) (PageState, error)
	// BusinessAction repeats fresh subscription and tenant-permission checks at
	// the action boundary; PageState.CanAct is presentation data only.
	BusinessAction(context.Context, identity.Principal) error
}

type StartResult struct {
	IntentID    uuid.UUID
	RedirectURL string
}

type RequestCheck interface {
	Valid(*http.Request) bool
	Token(*http.Request) (string, error)
}

type Handler struct {
	service Service
	check   RequestCheck
	page    *template.Template
}

func New(service Service, check RequestCheck) (*Handler, error) {
	if service == nil || check == nil {
		return nil, ErrUnavailable
	}
	t, err := template.New("billing").Funcs(template.FuncMap{"div100": func(n int64) float64 { return float64(n) / 100 }}).Parse(pageHTML)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Handler{service: service, check: check, page: t}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil || h.check == nil || r == nil {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Add("Vary", "Cookie")
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth.unauthenticated")
		return
	}
	switch {
	case r.URL.Path == "/billing" && r.Method == http.MethodGet:
		h.renderPage(w, r, principal, uuid.Nil)
	case r.URL.Path == "/billing/checkout" && r.Method == http.MethodPost:
		h.start(w, r, principal)
	case strings.HasPrefix(r.URL.Path, "/billing/status/") && r.Method == http.MethodGet:
		h.status(w, r, principal)
	case r.URL.Path == "/billing/action" && r.Method == http.MethodPost:
		h.action(w, r, principal)
	case strings.HasPrefix(r.URL.Path, "/billing/return/") && r.Method == http.MethodGet:
		intent := parseIntent(strings.TrimPrefix(r.URL.Path, "/billing/return/"))
		if intent == uuid.Nil {
			writeError(w, http.StatusBadRequest, "request.invalid")
			return
		}
		h.renderPage(w, r, principal, intent)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusNotFound, "request.not_found")
	}
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	id := parseIntent(strings.TrimPrefix(r.URL.Path, "/billing/status/"))
	if id == uuid.Nil {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	s, err := h.service.Status(r.Context(), p, id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !validState(s) {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		State     string `json:"state"`
		Confirmed bool   `json:"confirmed"`
		Plan      string `json:"plan,omitempty"`
		Currency  string `json:"currency,omitempty"`
	}{State: s.State, Confirmed: s.Paid, Plan: s.Plan, Currency: s.Currency})
}

func (h *Handler) action(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.URL.RawQuery != "" || mediaErr != nil || mediaType != "application/x-www-form-urlencoded" {
		writeError(w, http.StatusForbidden, "request.csrf_denied")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil || len(r.PostForm) != 1 || len(r.PostForm["_csrf"]) != 1 {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	if !h.check.Valid(r) {
		writeError(w, http.StatusForbidden, "request.csrf_denied")
		return
	}
	if err := h.service.BusinessAction(r.Context(), p); err != nil {
		writeDomainError(w, err)
		return
	}
	http.Redirect(w, r, "/workspace", http.StatusSeeOther)
}

func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, p identity.Principal, intent uuid.UUID) {
	state, err := h.service.Page(r.Context(), p)
	if intent != uuid.Nil {
		// The return URL can request a status refresh, but cannot influence the
		// entitlement fields. Status must re-read the current projection.
		state, err = h.service.Status(r.Context(), p, intent)
		state.IntentID = intent
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !validState(state) {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	token, tokenErr := h.check.Token(r)
	if tokenErr != nil || token == "" {
		writeError(w, http.StatusServiceUnavailable, "request.csrf_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	script, _ := returnScript.ReadFile("return.js")
	_ = h.page.Execute(w, view{State: state, Token: token, Returning: intent != uuid.Nil, Polls: maxPolls, Script: template.JS(script)})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.URL.RawQuery != "" || mediaErr != nil || mediaType != "application/x-www-form-urlencoded" {
		writeError(w, http.StatusForbidden, "request.csrf_denied")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil || len(r.PostForm) != 3 || len(r.PostForm["price_key"]) != 1 || len(r.PostForm["_csrf"]) != 1 || len(r.PostForm["idempotency_key"]) != 1 {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	if !h.check.Valid(r) {
		writeError(w, http.StatusForbidden, "request.csrf_denied")
		return
	}
	key := strings.TrimSpace(r.PostForm.Get("price_key"))
	idem := strings.TrimSpace(r.PostForm.Get("idempotency_key"))
	if key == "" || idem == "" {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	result, err := h.service.Start(r.Context(), p, key, idem)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if result.IntentID == uuid.Nil || result.IntentID.Version() != 7 || result.RedirectURL == "" {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	if _, err := safeRedirectHost(result.RedirectURL); err != nil {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	http.Redirect(w, r, result.RedirectURL, http.StatusSeeOther)
}

func validState(s PageState) bool {
	if strings.TrimSpace(s.Workspace) == "" || strings.TrimSpace(s.Payer) == "" {
		return false
	}
	if s.CanAct && (!s.Paid || !s.CanManage) {
		return false
	}
	if len(s.Offers) > 16 || (s.CanManage && (s.RequestID == "" || len(s.Offers) == 0)) {
		return false
	}
	seenPrices := make(map[string]struct{}, len(s.Offers))
	for _, o := range s.Offers {
		if o.PriceKey == "" || o.Plan == "" || o.Amount <= 0 || o.Currency != "USD" || (o.Interval != "month" && o.Interval != "year") {
			return false
		}
		if _, exists := seenPrices[o.PriceKey]; exists {
			return false
		}
		seenPrices[o.PriceKey] = struct{}{}
	}
	return true
}

func parseIntent(s string) uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil || u == uuid.Nil || u.Version() != 7 || u.String() != s {
		return uuid.Nil
	}
	return u
}

func safeRedirectHost(raw string) (string, error) {
	// Stripe-host validation is repeated by billing/checkout; keeping the UI
	// redirect validator restrictive prevents an adapter mistake becoming open redirect.
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "checkout.stripe.com" || u.User != nil || u.Port() != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/c/") {
		return "", ErrUnavailable
	}
	return "checkout.stripe.com", nil
}

func writeDomainError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrDenied) {
		writeError(w, http.StatusForbidden, "billing.permission_denied")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
}
func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"code":"` + code + `","message":"The request could not be authorized."}`))
}

type view struct {
	State     PageState
	Token     string
	Returning bool
	Polls     int
	Script    template.JS
}

const pageHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Billing</title><style>body{font:1rem system-ui;max-width:48rem;margin:2rem auto;padding:0 1rem}fieldset{margin:1rem 0}button{padding:.6rem 1rem}[hidden]{display:none}</style></head><body><main><nav aria-label="Account"><a href="/account">Account</a> · <a href="/recovery">Recovery</a></nav><h1>Workspace billing</h1><p>Workspace: <strong>{{.State.Workspace}}</strong></p><p>Payer: <strong>{{.State.Payer}}</strong></p><section id="payment-state" aria-live="polite" data-state="{{.State.State}}" data-intent="{{if .Returning}}{{.State.IntentID}}{{end}}" data-polls="{{.Polls}}" data-confirmed="{{.State.Paid}}">
{{if .State.Paid}}<p role="status">Payment confirmed</p><p>Plan: {{.State.Plan}} · {{.State.Currency}}</p>{{else if .Returning}}<p role="status" id="pending-message">Checking payment status. Your plan is not active yet.</p><button type="button" id="retry">Check again</button>{{else}}<p role="status">{{if .State.State}}{{.State.State}}{{else}}Choose a plan to continue.{{end}}</p>{{end}}
{{if .State.CanAct}}<form method="post" action="/billing/action"><input type="hidden" name="_csrf" value="{{.Token}}"><button type="submit" id="paid-action">Continue to workspace</button></form>{{else}}<button type="button" id="paid-action" disabled>Continue to workspace</button>{{end}}</section>
{{if .State.CanManage}}<form method="post" action="/billing/checkout"><input type="hidden" name="_csrf" value="{{.Token}}"><input type="hidden" name="idempotency_key" value="{{.State.RequestID}}"><fieldset><legend>Choose a plan</legend>{{range .State.Offers}}<label><input type="radio" name="price_key" value="{{.PriceKey}}" required>{{.Plan}} — {{printf "$%.2f" (div100 .Amount)}} / {{.Interval}}</label><br>{{end}}</fieldset><button type="submit">Continue to secure checkout</button></form>{{else}}<p>Only the current workspace billing owner can manage payment.</p>{{end}}</main><script>{{.Script}}</script></body></html>`
