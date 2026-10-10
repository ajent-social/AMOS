package billingcheckout

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const intentCookie = "amos_billing_checkout_intent"

var (
	priceKeyPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// State must come from current, workspace-scoped subscription projection data.
// Checkout redirects and query parameters alone never produce StatePaid.
type State string

const (
	StatePending     State = "pending"
	StateUnknown     State = "unknown"
	StateUnavailable State = "unavailable"
	StateFailed      State = "failed"
	StatePaid        State = "paid"
)

type Choice struct {
	PriceKey    string
	Plan        string
	Interval    string
	Currency    string
	AmountMinor int64
}

type PaymentView struct {
	State       State
	Payer       string
	Plan        string
	Currency    string
	AmountMinor int64
	ActionPath  string
}

type PageView struct {
	Payer   string
	Current PaymentView
	Choices []Choice
}

type CheckoutResult struct {
	State       string
	IntentID    string
	RedirectURL string
}

// Service implementations read choices from the immutable catalog, delegate
// StartCheckout to the permission-checked T5.6 action, and derive PaymentStatus
// from the current scoped subscription projection. Each method reauthorizes the
// request principal and workspace. Checkout intent status alone is not payment.
type Service interface {
	Page(context.Context, *http.Request) (PageView, error)
	StartCheckout(context.Context, *http.Request, string, string) (CheckoutResult, error)
	PaymentStatus(context.Context, *http.Request, string) (PaymentView, error)
}

type RequestCheck interface {
	Token(*http.Request) (string, error)
	Valid(*http.Request) bool
}

type Config struct {
	Service       Service
	RequestCheck  RequestCheck
	CheckoutHosts []string
}

type Handler struct {
	service Service
	check   RequestCheck
	hosts   map[string]struct{}
	page    *template.Template
}

func New(cfg Config) (*Handler, error) {
	if cfg.Service == nil || cfg.RequestCheck == nil || len(cfg.CheckoutHosts) == 0 {
		return nil, errors.New("billing checkout UI dependencies are required")
	}
	hosts := make(map[string]struct{}, len(cfg.CheckoutHosts))
	for _, host := range cfg.CheckoutHosts {
		if !validHost(host) {
			return nil, errors.New("invalid hosted checkout allowlist")
		}
		if _, exists := hosts[host]; exists {
			return nil, errors.New("duplicate hosted checkout allowlist entry")
		}
		hosts[host] = struct{}{}
	}
	page, err := template.New("billing-checkout").Funcs(template.FuncMap{"price": formatPrice}).Parse(pageHTML)
	if err != nil {
		return nil, fmt.Errorf("parse billing checkout page: %w", err)
	}
	return &Handler{service: cfg.Service, check: cfg.RequestCheck, hosts: hosts, page: page}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil || h.check == nil || r == nil {
		writeError(w)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Vary", "Cookie, HX-Request, HX-Target")
	switch {
	case r.URL.Path == "/billing" && r.Method == http.MethodGet:
		h.showBilling(w, r)
	case r.URL.Path == "/billing/checkout" && r.Method == http.MethodPost:
		h.start(w, r)
	case r.URL.Path == "/billing/return" && r.Method == http.MethodGet:
		h.showReturn(w, r)
	case r.URL.Path == "/billing/return/retry" && r.Method == http.MethodPost:
		h.retry(w, r)
	case r.URL.Path == "/billing/return/status" && r.Method == http.MethodGet:
		h.status(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) showBilling(w http.ResponseWriter, r *http.Request) {
	token, err := h.check.Token(r)
	view, viewErr := h.service.Page(r.Context(), r)
	if err != nil || token == "" || viewErr != nil || validatePage(view) != nil {
		h.render(w, http.StatusServiceUnavailable, model{Title: "Billing unavailable", Heading: "Billing is temporarily unavailable", Notice: "Your billing details could not be loaded. Please try again shortly.", Unavailable: true})
		return
	}
	key, err := newIdempotencyKey()
	if err != nil {
		h.render(w, http.StatusServiceUnavailable, model{Title: "Billing unavailable", Heading: "Billing is temporarily unavailable", Notice: "A secure checkout request could not be prepared.", Unavailable: true})
		return
	}
	h.render(w, http.StatusOK, model{Title: "Billing", Heading: "Choose a plan", Notice: "Choose a plan for the active workspace.", Payer: view.Payer, Current: view.Current, Choices: view.Choices, HasChoices: true, CSRF: token, IdempotencyKey: key, ActionPath: paidAction(view.Current)})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	fields, ok := h.readForm(w, r, "_csrf", "price_key", "idempotency_key")
	if !ok || !h.check.Valid(r) || fields["_csrf"] == "" {
		h.render(w, http.StatusBadRequest, model{Title: "Checkout unavailable", Heading: "Checkout request could not be accepted", Notice: "Return to billing and try again.", Unavailable: true})
		return
	}
	view, err := h.service.Page(r.Context(), r)
	if err != nil || validatePage(view) != nil {
		h.render(w, http.StatusServiceUnavailable, model{Title: "Billing unavailable", Heading: "Billing is temporarily unavailable", Notice: "Your billing details could not be loaded. Please try again shortly.", Unavailable: true})
		return
	}
	choice, found := findChoice(view.Choices, fields["price_key"])
	if !found || !idempotencyPattern.MatchString(fields["idempotency_key"]) {
		h.render(w, http.StatusBadRequest, model{Title: "Checkout unavailable", Heading: "Checkout request could not be accepted", Notice: "Choose an available plan and try again.", Unavailable: true})
		return
	}
	result, err := h.service.StartCheckout(r.Context(), r, choice.PriceKey, fields["idempotency_key"])
	if err != nil || !validCheckoutResult(result, h.hosts) {
		h.render(w, http.StatusServiceUnavailable, model{Title: "Checkout status unknown", Heading: "Checkout status needs checking", Notice: stateMessage(StateUnknown), State: StateUnknown})
		return
	}
	h.setIntent(w, result.IntentID)
	if result.State == "redirect" {
		http.Redirect(w, r, result.RedirectURL, http.StatusSeeOther)
		return
	}
	state := StateUnknown
	if result.State == "pending" {
		state = StatePending
	}
	if result.State == "failed" {
		state = StateFailed
	}
	h.render(w, http.StatusAccepted, model{Title: "Checkout status", Heading: "Checkout status", Notice: stateMessage(state), State: state, HasRetry: state == StateUnknown || state == StatePending})
}

func (h *Handler) showReturn(w http.ResponseWriter, r *http.Request) {
	id := checkoutIntent(r)
	if id == "" {
		h.render(w, http.StatusOK, model{Title: "Payment status", Heading: "No checkout is being checked", Notice: "Return to billing to choose a plan."})
		return
	}
	token, err := h.check.Token(r)
	if err != nil || token == "" {
		h.render(w, http.StatusServiceUnavailable, model{Title: "Payment status unavailable", Heading: "Payment status is unavailable", Notice: stateMessage(StateUnavailable), State: StateUnavailable, Unavailable: true})
		return
	}
	payment, err := h.service.PaymentStatus(r.Context(), r, id)
	if err != nil || validatePayment(payment) != nil {
		payment = PaymentView{State: StateUnavailable}
	}
	if payment.State == StatePaid || payment.State == StateFailed {
		h.clearIntent(w)
	}
	h.render(w, http.StatusOK, returnModel(payment, token))
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	fields, ok := h.readForm(w, r, "_csrf")
	if !ok || !h.check.Valid(r) || fields["_csrf"] == "" {
		h.render(w, http.StatusBadRequest, model{Title: "Payment status unavailable", Heading: "Payment status could not be checked", Notice: stateMessage(StateUnavailable), State: StateUnavailable, Unavailable: true})
		return
	}
	id := checkoutIntent(r)
	if id == "" {
		h.render(w, http.StatusBadRequest, model{Title: "Payment status", Heading: "No checkout is being checked", Notice: "Return to billing to choose a plan."})
		return
	}
	payment, err := h.service.PaymentStatus(r.Context(), r, id)
	if err != nil || validatePayment(payment) != nil {
		payment = PaymentView{State: StateUnavailable}
	}
	if payment.State == StatePaid || payment.State == StateFailed {
		h.clearIntent(w)
	}
	token, err := h.check.Token(r)
	if err != nil || token == "" {
		token = fields["_csrf"]
	}
	h.render(w, http.StatusOK, returnModel(payment, token))
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	id := checkoutIntent(r)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, statusResponse{State: StateUnavailable})
		return
	}
	payment, err := h.service.PaymentStatus(r.Context(), r, id)
	if err != nil || validatePayment(payment) != nil {
		writeJSON(w, http.StatusServiceUnavailable, statusResponse{State: StateUnavailable})
		return
	}
	result := statusResponse{State: payment.State}
	switch payment.State {
	case StatePaid:
		result.Payer, result.Plan, result.Currency = payment.Payer, payment.Plan, payment.Currency
		result.AmountMinor, result.ActionPath = payment.AmountMinor, payment.ActionPath
		h.clearIntent(w)
	case StateFailed:
		h.clearIntent(w)
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) readForm(w http.ResponseWriter, r *http.Request, names ...string) (map[string]string, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil || len(r.Form) != len(names) || len(r.PostForm) != len(names) {
		return nil, false
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		values := r.PostForm[name]
		if len(values) != 1 || values[0] == "" {
			return nil, false
		}
		out[name] = values[0]
	}
	for name := range r.PostForm {
		found := false
		for _, allowed := range names {
			if name == allowed {
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return out, true
}

func (h *Handler) render(w http.ResponseWriter, status int, view model) {
	var body bytes.Buffer
	if err := h.page.Execute(&body, view); err != nil {
		writeError(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.Copy(w, &body)
}

func returnModel(payment PaymentView, token string) model {
	return model{Title: "Payment status", Heading: "Payment status", Notice: stateMessage(payment.State), State: payment.State, CSRF: token, HasRetry: payment.State != StatePaid && payment.State != StateFailed, Payer: payment.Payer, Current: payment, ActionPath: paidAction(payment)}
}

func checkoutIntent(r *http.Request) string {
	for _, c := range r.Cookies() {
		if c.Name == intentCookie && validIntentID(c.Value) {
			return c.Value
		}
	}
	return ""
}

func (h *Handler) setIntent(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{Name: intentCookie, Value: id, Path: "/billing/return", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 900})
}

func (h *Handler) clearIntent(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: intentCookie, Path: "/billing/return", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func validIntentID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id.Version() == 7 && id != uuid.Nil && id.String() == raw
}

func validCheckoutResult(r CheckoutResult, hosts map[string]struct{}) bool {
	if !validIntentID(r.IntentID) {
		return false
	}
	switch r.State {
	case "redirect":
		u, err := url.Parse(r.RedirectURL)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Hostname() == "" || u.Port() != "" {
			return false
		}
		_, ok := hosts[strings.ToLower(u.Hostname())]
		return ok
	case "pending", "unknown", "failed":
		return r.RedirectURL == ""
	default:
		return false
	}
}

func validatePage(v PageView) error {
	if strings.TrimSpace(v.Payer) == "" || len(v.Choices) == 0 || len(v.Choices) > 24 {
		return errors.New("invalid billing page")
	}
	if err := validatePayment(v.Current); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(v.Choices))
	for _, c := range v.Choices {
		if !priceKeyPattern.MatchString(c.PriceKey) || strings.TrimSpace(c.Plan) == "" || (c.Interval != "month" && c.Interval != "year") || c.Currency != "USD" || c.AmountMinor <= 0 {
			return errors.New("invalid catalog choice")
		}
		if _, ok := seen[c.PriceKey]; ok {
			return errors.New("duplicate catalog choice")
		}
		seen[c.PriceKey] = struct{}{}
	}
	return nil
}

func validatePayment(p PaymentView) error {
	switch p.State {
	case StatePending, StateUnknown, StateUnavailable, StateFailed:
		if p.ActionPath != "" {
			return errors.New("unpaid projection contains action")
		}
	case StatePaid:
		if p.Payer == "" || p.Plan == "" || p.Currency != "USD" || p.AmountMinor <= 0 || !safeLocalPath(p.ActionPath) {
			return errors.New("invalid paid projection")
		}
	default:
		return errors.New("invalid projection state")
	}
	if p.Currency != "" && p.Currency != "USD" {
		return errors.New("unsupported currency")
	}
	return nil
}

func findChoice(choices []Choice, key string) (Choice, bool) {
	for _, c := range choices {
		if c.PriceKey == key {
			return c, true
		}
	}
	return Choice{}, false
}

func paidAction(p PaymentView) string {
	if p.State == StatePaid && safeLocalPath(p.ActionPath) {
		return p.ActionPath
	}
	return ""
}

func safeLocalPath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n") {
		return false
	}
	u, err := url.Parse(p)
	return err == nil && !u.IsAbs() && u.Host == "" && !strings.Contains(p, "\\")
}

func validHost(host string) bool {
	if host == "" || host != strings.ToLower(host) || strings.TrimSpace(host) != host || strings.ContainsAny(host, ":/@?#*[]") {
		return false
	}
	u, err := url.Parse("https://" + host)
	return err == nil && u.Hostname() == host && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

func newIdempotencyKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate checkout idempotency key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func formatPrice(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}

func stateMessage(s State) string {
	switch s {
	case StatePaid:
		return "Payment is confirmed for the active workspace."
	case StatePending:
		return "Payment is still pending confirmation. Paid access remains unavailable."
	case StateUnknown:
		return "The checkout outcome is unknown and is being reconciled. Paid access remains unavailable."
	case StateFailed:
		return "Checkout did not complete. Return to billing to try again."
	default:
		return "Payment status is temporarily unavailable. Paid access remains unavailable."
	}
}

func writeError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, "Billing is temporarily unavailable.")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type statusResponse struct {
	State       State  `json:"state"`
	Payer       string `json:"payer,omitempty"`
	Plan        string `json:"plan,omitempty"`
	Currency    string `json:"currency,omitempty"`
	AmountMinor int64  `json:"amount_minor,omitempty"`
	ActionPath  string `json:"action_path,omitempty"`
}

type model struct {
	Title          string
	Heading        string
	Notice         string
	State          State
	Payer          string
	Current        PaymentView
	Choices        []Choice
	CSRF           string
	IdempotencyKey string
	HasChoices     bool
	HasRetry       bool
	Unavailable    bool
	ActionPath     string
}

const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title><link rel="stylesheet" href="/assets/base.css"></head>
<body><a class="skip-link" href="#main-content">Skip to main content</a><header class="site-header"><a href="/" class="brand">AMOS</a><nav aria-label="Primary"><a href="/workspaces">Workspaces</a><a href="/account">Account</a><a href="/billing">Billing</a></nav></header>
<main id="main-content"><h1>{{.Heading}}</h1>{{if .Payer}}<p>Active workspace payer: <strong>{{.Payer}}</strong></p>{{end}}{{if .Notice}}<p id="payment-notice" role="{{if .Unavailable}}alert{{else}}status{{end}}" aria-live="polite">{{.Notice}}</p>{{end}}
{{if and .Current.Plan (eq .Current.State "paid")}}<section aria-labelledby="current-plan"><h2 id="current-plan">Current plan</h2><p>{{.Current.Plan}} &middot; {{.Current.Currency}} {{price .Current.AmountMinor}} / {{if eq .Current.State "paid"}}active{{else}}checking{{end}}</p></section>{{end}}
{{if .ActionPath}}<p><a class="button" href="{{.ActionPath}}">Continue to your paid workspace</a></p>{{end}}
{{if .HasChoices}}<form action="/billing/checkout" method="post"><input type="hidden" name="_csrf" value="{{.CSRF}}"><input type="hidden" name="idempotency_key" value="{{.IdempotencyKey}}"><fieldset><legend>Available plans</legend>{{range .Choices}}<label><input type="radio" name="price_key" value="{{.PriceKey}}" required>{{.Plan}} &middot; {{.Currency}} {{price .AmountMinor}} / {{.Interval}}</label>{{end}}</fieldset><button type="submit">Continue to secure checkout</button></form>{{end}}
{{if .HasRetry}}<form action="/billing/return/retry" method="post"><input type="hidden" name="_csrf" value="{{.CSRF}}"><button type="submit">Check payment status again</button></form>{{end}}
{{if .State}}<p id="payment-state" data-state="{{.State}}">{{.State}}</p><p id="payment-details" aria-live="polite"></p>{{if .HasRetry}}<section id="payment-poll" data-url="/billing/return/status" data-attempts="6" aria-live="polite"></section>{{end}}{{end}}
</main><script>
(() => {
 const poll = document.getElementById("payment-poll");
 if (!poll) return;
 const status = document.getElementById("payment-state");
 const notice = document.getElementById("payment-notice");
 const details = document.getElementById("payment-details");
 let remaining = Number(poll.dataset.attempts);
 const check = async () => {
  while (remaining-- > 0) {
   try {
    const response = await fetch(poll.dataset.url, {credentials:"same-origin", headers:{Accept:"application/json"}});
    const result = await response.json();
    if (!response.ok) throw new Error("status unavailable");
    status.textContent = result.state;
    status.dataset.state = result.state;
    if (result.state === "paid") {
     notice.textContent = "Payment is confirmed for the active workspace.";
     details.textContent = result.plan + " - " + result.currency + " " + (result.amount_minor / 100).toFixed(2);
     const link = document.createElement("a");
     link.href = result.action_path;
     link.textContent = "Continue to your paid workspace";
     poll.replaceChildren(link);
     return;
    }
    if (result.state === "failed") return;
   } catch { status.textContent = "unavailable"; status.dataset.state = "unavailable"; notice.textContent = "Payment status is temporarily unavailable. Paid access remains unavailable."; }
   await new Promise(resolve => setTimeout(resolve, 1200));
  }
  poll.textContent = "We have not confirmed payment yet. You can check again.";
 };
 void check();
})();
</script></body></html>`
