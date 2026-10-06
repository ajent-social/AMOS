// Package personalmanage exposes the authenticated personal-workspace billing
// inspection and provider portal boundary. Provider references remain internal.
package personalmanage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/identity"
	identitystore "github.com/ajent-social/amos/identity/store"
	workspacectx "github.com/ajent-social/amos/workspace/context"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

const ProofFreshness = 5 * time.Minute

var (
	ErrInvalidConfig   = errors.New("personal billing configuration is invalid")
	ErrUnauthorized    = errors.New("personal billing is not authorized")
	ErrCustomerMissing = errors.New("personal billing customer is not configured")
)

type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

type TransactionStore interface {
	EnsureWorkspaceAccount(context.Context, uuid.UUID, provider.Binding) (billingstore.WorkspaceAccount, error)
	FindCustomerBinding(context.Context, uuid.UUID, provider.Binding) (billingstore.CustomerBinding, error)
}

type StoreFactory func(*sql.Tx) (TransactionStore, error)

type CustomerRepository interface {
	FindCustomer(context.Context, provider.Binding) (billingstore.CustomerBinding, error)
}

type Provider interface {
	GetSubscription(context.Context, provider.SubscriptionRequest) (provider.Outcome[provider.SubscriptionSnapshot], error)
	CreatePortal(context.Context, provider.PortalRequest) (provider.PortalSession, error)
}

// RequestCheck validates same-origin and CSRF proof for external portal entry.
// It is mandatory and deliberately has no permissive fallback.
type RequestCheck interface {
	Valid(*http.Request) bool
}

type Authority struct {
	ActorKind, AuthenticationMethod                        string
	PersonID, InstallationID, ApplicationID, EnvironmentID uuid.UUID
	AuthenticatedAt                                        time.Time
	Workspace                                              workspacestore.Workspace
}

type AuthorityResolver func(context.Context) (Authority, bool)

type Config struct {
	Repository        CustomerRepository
	Provider          Provider
	ProviderAccountID string
	AccountMode       provider.AccountMode
	ReturnURL         string
	ReturnHosts       []string
	PortalHosts       []string
	RequestCheck      RequestCheck
	ResolveAuthority  AuthorityResolver
	Now               func() time.Time
}

type Handler struct {
	cfg         Config
	returnHosts map[string]struct{}
}

func New(cfg Config) (*Handler, error) {
	if cfg.Repository == nil || nilInterface(cfg.Provider) || nilInterface(cfg.RequestCheck) || cfg.ProviderAccountID == "" ||
		(cfg.AccountMode != provider.AccountTest && cfg.AccountMode != provider.AccountLive) ||
		!validReturnURL(cfg.ReturnURL, cfg.ReturnHosts) || len(cfg.PortalHosts) == 0 {
		return nil, ErrInvalidConfig
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ResolveAuthority == nil {
		cfg.ResolveAuthority = contextAuthority
	}
	hosts := make(map[string]struct{}, len(cfg.PortalHosts))
	for _, host := range cfg.PortalHosts {
		if host == "" || strings.ToLower(host) != host || strings.ContainsAny(host, ":/@?#*[]") {
			return nil, ErrInvalidConfig
		}
		hosts[host] = struct{}{}
	}
	return &Handler{cfg: cfg, returnHosts: hosts}, nil
}

// SQLRepository loads the active provider customer only after deriving the
// complete namespace from trusted current workspace authority.
type SQLRepository struct {
	db       TxRunner
	newStore StoreFactory
}

func NewSQLRepository(db TxRunner, newStore StoreFactory) (*SQLRepository, error) {
	if db == nil || newStore == nil {
		return nil, ErrInvalidConfig
	}
	return &SQLRepository{db: db, newStore: newStore}, nil
}

func (r *SQLRepository) FindCustomer(ctx context.Context, binding provider.Binding) (out billingstore.CustomerBinding, resultErr error) {
	if r == nil || r.db == nil || r.newStore == nil || ctx == nil || binding.Validate() != nil {
		return out, billingstore.ErrInvalidInput
	}
	err := r.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, err := r.newStore(tx)
		if err != nil {
			return err
		}
		accountID, err := identitystore.NewID()
		if err != nil {
			return err
		}
		account, err := s.EnsureWorkspaceAccount(ctx, accountID, binding)
		if err != nil {
			return err
		}
		out, err = s.FindCustomerBinding(ctx, account.ID, binding)
		if err != nil {
			return err
		}
		if out.State != "active" || out.AccountID != account.ID || out.Binding != binding || out.CustomerRef == "" {
			return ErrCustomerMissing
		}
		return nil
	})
	return out, err
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Request-ID", newRequestID())
	if r.URL.RawQuery != "" || strings.Contains(r.URL.Path, "%") {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	access, ok := h.cfg.ResolveAuthority(r.Context())
	if !ok && !hasVerifiedPrincipal(r.Context()) {
		writeError(w, http.StatusUnauthorized, "authentication.required")
		return
	}
	if r.Method == http.MethodPost && (r.URL.Path == "/billing/personal/portal" || r.URL.Path == "/billing/personal/cancel") && !h.cfg.RequestCheck.Valid(r) {
		writeError(w, http.StatusForbidden, "request.csrf_denied")
		return
	}
	if !ok || !h.authorized(access) {
		writeError(w, http.StatusForbidden, "billing.permission_denied")
		return
	}
	binding, err := h.binding(access)
	if err != nil {
		writeError(w, http.StatusForbidden, "billing.permission_denied")
		return
	}
	switch {
	case r.URL.Path == "/billing/personal" && r.Method == http.MethodGet:
		h.inspect(w, r, binding)
	case (r.URL.Path == "/billing/personal/portal" || r.URL.Path == "/billing/personal/cancel") && r.Method == http.MethodPost:
		h.portal(w, r, binding)
	default:
		writeError(w, http.StatusNotFound, "request.not_found")
	}
}

type subscriptionResponse struct {
	SnapshotState string     `json:"snapshot_state"`
	PriceKey      string     `json:"price_key,omitempty"`
	Quantity      int64      `json:"quantity"`
	PeriodStart   *time.Time `json:"period_start,omitempty"`
	PeriodEnd     *time.Time `json:"period_end,omitempty"`
	ObservedAt    *time.Time `json:"observed_at,omitempty"`
}

func (h *Handler) inspect(w http.ResponseWriter, r *http.Request, binding provider.Binding) {
	customer, err := h.customer(r.Context(), binding)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out, err := h.cfg.Provider.GetSubscription(r.Context(), provider.SubscriptionRequest{Binding: binding, Customer: provider.ObjectRef{Binding: binding, ID: customer.CustomerRef}})
	if err != nil || validateSnapshot(binding, customer.CustomerRef, out) != nil {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	if out.State == provider.SubscriptionFailed {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	if out.State != provider.SubscriptionConfirmed {
		writeError(w, http.StatusServiceUnavailable, "billing.snapshot_unavailable")
		return
	}
	s := out.Value
	writeJSON(w, http.StatusOK, subscriptionResponse{SnapshotState: string(out.State), PriceKey: s.PriceKey, Quantity: s.Quantity,
		PeriodStart: timePtr(s.PeriodStart), PeriodEnd: timePtr(s.PeriodEnd), ObservedAt: timePtr(s.ObservedAt)})
}

func (h *Handler) portal(w http.ResponseWriter, r *http.Request, binding provider.Binding) {
	customer, err := h.customer(r.Context(), binding)
	if err != nil {
		h.writeError(w, err)
		return
	}
	session, err := h.cfg.Provider.CreatePortal(r.Context(), provider.PortalRequest{Binding: binding,
		Customer: provider.ObjectRef{Binding: binding, ID: customer.CustomerRef}, ReturnURL: h.cfg.ReturnURL})
	if err != nil || validatePortal(binding, session, h.returnHosts, h.cfg.Now()) != nil {
		writeError(w, http.StatusServiceUnavailable, "billing.portal_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "portal_ready", "redirect_url": session.URL})
}

func (h *Handler) customer(ctx context.Context, binding provider.Binding) (billingstore.CustomerBinding, error) {
	c, err := h.cfg.Repository.FindCustomer(ctx, binding)
	if err != nil {
		return billingstore.CustomerBinding{}, err
	}
	if c.Binding != binding {
		return billingstore.CustomerBinding{}, ErrUnauthorized
	}
	if c.State != "active" || c.CustomerRef == "" {
		return billingstore.CustomerBinding{}, ErrCustomerMissing
	}
	return c, nil
}

func (h *Handler) authorized(a Authority) bool {
	now := h.cfg.Now()
	return !now.IsZero() && a.ActorKind == "person" && a.PersonID != uuid.Nil &&
		a.Workspace.State == workspacestore.WorkspaceActive && a.Workspace.Kind == workspacestore.KindPersonal &&
		a.Workspace.PersonalOwnerID == a.PersonID && a.Workspace.Scope.InstallationID == a.InstallationID &&
		a.Workspace.Scope.ApplicationID == a.ApplicationID && a.InstallationID != uuid.Nil && a.ApplicationID != uuid.Nil &&
		a.EnvironmentID != uuid.Nil && a.AuthenticationMethod == "email_password" &&
		!a.AuthenticatedAt.IsZero() && !a.AuthenticatedAt.After(now) && now.Sub(a.AuthenticatedAt) <= ProofFreshness
}

func (h *Handler) binding(a Authority) (provider.Binding, error) {
	if !h.authorized(a) {
		return provider.Binding{}, ErrUnauthorized
	}
	b := provider.Binding{InstallationID: a.InstallationID.String(), EnvironmentID: a.EnvironmentID.String(), WorkspaceID: a.Workspace.ID.String(), Provider: "stripe", AccountID: h.cfg.ProviderAccountID, AccountMode: h.cfg.AccountMode}
	return b, b.Validate()
}

func contextAuthority(ctx context.Context) (Authority, bool) {
	if ctx == nil {
		return Authority{}, false
	}
	p, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return Authority{}, false
	}
	w, ok := workspacectx.FromContext(ctx)
	if !ok {
		return Authority{}, false
	}
	return Authority{ActorKind: p.Actor().Kind(), AuthenticationMethod: p.AuthenticationMethod(), PersonID: p.PersonID(),
		InstallationID: p.InstallationID(), ApplicationID: p.ApplicationID(), EnvironmentID: p.EnvironmentID(),
		AuthenticatedAt: p.AuthenticatedAt(), Workspace: w.Workspace}, true
}

func hasVerifiedPrincipal(ctx context.Context) bool {
	_, ok := identity.PrincipalFromContext(ctx)
	return ok
}

func validateSnapshot(binding provider.Binding, customer string, out provider.Outcome[provider.SubscriptionSnapshot]) error {
	if err := out.Validate(); err != nil {
		return err
	}
	if out.State != provider.SubscriptionConfirmed {
		return nil
	}
	s := out.Value
	if s.Ref.Validate() != nil || s.Ref.Binding != binding || s.State != provider.SubscriptionConfirmed ||
		s.ObservedAt.IsZero() || out.ObservedAt.IsZero() || s.PriceKey == "" || s.Quantity < 0 ||
		s.PeriodStart.IsZero() || s.PeriodEnd.IsZero() || !s.PeriodEnd.After(s.PeriodStart) ||
		out.ProviderObject != s.Ref || customer == "" {
		return provider.ErrMalformedResult
	}
	return nil
}

func validatePortal(binding provider.Binding, session provider.PortalSession, hosts map[string]struct{}, now time.Time) error {
	if session.Ref.Validate() != nil || session.Ref.Binding != binding || session.URL == "" || session.ExpiresAt.IsZero() || !session.ExpiresAt.After(now) {
		return provider.ErrMalformedResult
	}
	u, err := url.Parse(session.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return provider.ErrMalformedResult
	}
	if _, ok := hosts[strings.ToLower(u.Hostname())]; !ok {
		return provider.ErrMalformedResult
	}
	return nil
}

func validReturnURL(raw string, hosts []string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Port() != "" || u.Fragment != "" {
		return false
	}
	for _, host := range hosts {
		if strings.ToLower(host) == u.Hostname() && host == strings.ToLower(host) {
			return true
		}
	}
	return false
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t.UTC()
	return &v
}
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthorized):
		writeError(w, http.StatusForbidden, "billing.permission_denied")
	case errors.Is(err, billingstore.ErrAccountUnavailable), errors.Is(err, ErrCustomerMissing):
		writeError(w, http.StatusServiceUnavailable, "billing.setup_required")
	case errors.Is(err, billingstore.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "request.invalid")
	default:
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
	}
}
func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": "request could not be completed", "request_id": w.Header().Get("X-Request-ID")})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func nilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func:
		return rv.IsNil()
	}
	return false
}

func newRequestID() string {
	id, err := identitystore.NewID()
	if err != nil {
		return "unavailable"
	}
	return id.String()
}
