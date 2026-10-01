// Package checkout exposes the authenticated workspace checkout boundary.
// Provider work begins only after the durable repository has committed the
// immutable request intent and its claim.
package checkout

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/billing/stripecheckout"
	"github.com/ajent-social/amos/identity"
	identitystore "github.com/ajent-social/amos/identity/store"
	workspacectx "github.com/ajent-social/amos/workspace/context"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

const (
	Operation       = stripecheckout.CheckoutIntentOperation
	maxRequestBytes = 8 << 10
	intentLease     = 2 * time.Minute
)

var (
	ErrInvalidConfig   = errors.New("checkout handler configuration is invalid")
	ErrInvalidRequest  = errors.New("checkout request is invalid")
	ErrUnauthorized    = errors.New("checkout is not authorized")
	ErrCustomerMissing = errors.New("workspace billing customer is not configured")
	ErrCheckoutExpired = errors.New("checkout continuation has expired")
	keyPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
)

type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

// TransactionStore is the durable billing-store surface used by SQLRepository.
// NewStore adapts billing/store.New and its transaction-scoped implementation.
type TransactionStore interface {
	EnsureWorkspaceAccount(context.Context, uuid.UUID, provider.Binding) (billingstore.WorkspaceAccount, error)
	FindCustomerBinding(context.Context, uuid.UUID, provider.Binding) (billingstore.CustomerBinding, error)
	CreateIntent(context.Context, billingstore.CreateIntentInput) (billingstore.Intent, bool, error)
	ClaimIntent(context.Context, billingstore.ClaimIntentInput) (billingstore.ClaimResult, error)
	ResolveIntent(context.Context, billingstore.ResolveIntentInput) (billingstore.Intent, error)
	ReconcileIntent(context.Context, billingstore.ReconcileIntentInput) (billingstore.Intent, error)
	FindIntent(context.Context, provider.Binding, uuid.UUID) (billingstore.Intent, error)
}

type StoreFactory func(*sql.Tx) (TransactionStore, error)

type CheckoutRequest struct {
	Binding        provider.Binding
	Customer       provider.ObjectRef
	PriceKey       string
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

type Prepared struct {
	Intent   billingstore.Intent
	Request  CheckoutRequest
	Claimed  bool
	Customer provider.ObjectRef
}

type Repository interface {
	Prepare(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, CheckoutRequest) (Prepared, error)
	Resolve(context.Context, CheckoutRequest, billingstore.Intent, provider.Outcome[provider.CheckoutSession]) (billingstore.Intent, error)
	Find(context.Context, provider.Binding, uuid.UUID) (billingstore.Intent, error)
}

type Provider interface {
	CreateCheckout(context.Context, provider.CheckoutRequest) (provider.Outcome[provider.CheckoutSession], error)
	RecoverCheckout(context.Context, provider.CheckoutRequest, billingstore.Intent) (provider.Outcome[provider.CheckoutSession], error)
}

// ConfirmedCheckoutReader retrieves the existing provider checkout session.
// It must never create a new checkout or retry a mutation.
type ConfirmedCheckoutReader interface {
	ReadConfirmedCheckout(context.Context, provider.CheckoutRequest, billingstore.Intent) (provider.Outcome[provider.CheckoutSession], error)
}

type Authority struct {
	ActorKind          string
	PersonID           uuid.UUID
	InstallationID     uuid.UUID
	ApplicationID      uuid.UUID
	EnvironmentID      uuid.UUID
	AssuranceLevel     identity.AssuranceLevel
	AssuranceExpiresAt time.Time
	Workspace          workspacestore.Workspace
	Membership         *workspacestore.Membership
	Permissions        []string
}

type AuthorityResolver func(context.Context) (Authority, bool)

type Config struct {
	Repository         Repository
	Provider           Provider
	ContinuationReader ConfirmedCheckoutReader
	Catalog            *catalog.Catalog
	CheckoutHosts      []string
	ProviderAccountID  string
	AccountMode        provider.AccountMode
	SuccessURL         string
	CancelURL          string
	ResolveAuthority   AuthorityResolver
	Now                func() time.Time
}

type Handler struct {
	cfg           Config
	checkoutHosts map[string]struct{}
}

func New(cfg Config) (*Handler, error) {
	if cfg.Repository == nil || cfg.Provider == nil || cfg.Catalog == nil || cfg.ProviderAccountID == "" ||
		(cfg.AccountMode != provider.AccountTest && cfg.AccountMode != provider.AccountLive) ||
		cfg.SuccessURL == "" || cfg.CancelURL == "" || len(cfg.CheckoutHosts) == 0 {
		return nil, ErrInvalidConfig
	}
	checkoutHosts := make(map[string]struct{}, len(cfg.CheckoutHosts))
	for _, host := range cfg.CheckoutHosts {
		if !validCheckoutHost(host) {
			return nil, ErrInvalidConfig
		}
		if _, exists := checkoutHosts[host]; exists {
			return nil, ErrInvalidConfig
		}
		checkoutHosts[host] = struct{}{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ResolveAuthority == nil {
		cfg.ResolveAuthority = contextAuthority
	}
	cfg.CheckoutHosts = append([]string(nil), cfg.CheckoutHosts...)
	return &Handler{cfg: cfg, checkoutHosts: checkoutHosts}, nil
}

// SQLRepository commits intent creation and claim before the HTTP handler can
// make a provider request. The StoreFactory is provided by the composition root.
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

func (r *SQLRepository) Prepare(ctx context.Context, workspaceAccountID, intentID, auditID uuid.UUID, req CheckoutRequest) (out Prepared, resultErr error) {
	if r == nil || r.db == nil || r.newStore == nil || ctx == nil || !validID(workspaceAccountID) || !validID(intentID) || !validID(auditID) || req.Binding.Validate() != nil {
		return Prepared{}, billingstore.ErrInvalidInput
	}
	err := r.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, err := r.newStore(tx)
		if err != nil {
			return err
		}
		account, err := s.EnsureWorkspaceAccount(ctx, workspaceAccountID, req.Binding)
		if err != nil {
			return err
		}
		customer, err := s.FindCustomerBinding(ctx, account.ID, req.Binding)
		if err != nil {
			return err
		}
		if customer.State != "active" || customer.AccountID != account.ID || customer.Binding != req.Binding || customer.CustomerRef == "" {
			return ErrCustomerMissing
		}
		req.Customer = provider.ObjectRef{Binding: req.Binding, ID: customer.CustomerRef}
		payload := stripecheckout.CheckoutPayloadHash(toProviderRequest(req))
		intent, _, err := s.CreateIntent(ctx, billingstore.CreateIntentInput{
			ID: intentID, AuditReferenceID: auditID, AccountID: account.ID,
			Binding: req.Binding, Operation: Operation, IdempotencyKey: req.IdempotencyKey,
			PayloadSHA256: payload,
		})
		if err != nil {
			return err
		}
		out.Intent, out.Request, out.Customer = intent, req, req.Customer
		if intent.State == "pending" || intent.State == "claimed" {
			nextAuditID, idErr := identitystore.NewID()
			if idErr != nil {
				return idErr
			}
			claim, claimErr := s.ClaimIntent(ctx, billingstore.ClaimIntentInput{ID: intent.ID, AuditReferenceID: nextAuditID, Binding: req.Binding, ExpectedVersion: intent.Version, Lease: intentLease})
			if claimErr != nil {
				return claimErr
			}
			out.Intent, out.Claimed = claim.Intent, claim.Claimed
		}
		return nil
	})
	if err != nil {
		return Prepared{}, err
	}
	return out, nil
}

func (r *SQLRepository) Resolve(ctx context.Context, req CheckoutRequest, previous billingstore.Intent, outcome provider.Outcome[provider.CheckoutSession]) (billingstore.Intent, error) {
	if r == nil || r.db == nil || r.newStore == nil || ctx == nil || req.Binding.Validate() != nil || previous.ID == uuid.Nil || validateOutcome(req, outcome) != nil {
		return billingstore.Intent{}, billingstore.ErrInvalidInput
	}
	state, providerRef := storeOutcome(outcome)
	var updated billingstore.Intent
	err := r.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, err := r.newStore(tx)
		if err != nil {
			return err
		}
		if previous.State == "unknown" {
			if state == "unknown" {
				updated = previous
				return nil
			}
			auditID, idErr := identitystore.NewID()
			if idErr != nil {
				return idErr
			}
			updated, err = s.ReconcileIntent(ctx, billingstore.ReconcileIntentInput{ID: previous.ID, AuditReferenceID: auditID, Binding: req.Binding, ExpectedVersion: previous.Version, State: state, ProviderObjectRef: providerRef})
			return err
		}
		auditID, idErr := identitystore.NewID()
		if idErr != nil {
			return idErr
		}
		updated, err = s.ResolveIntent(ctx, billingstore.ResolveIntentInput{ID: previous.ID, AuditReferenceID: auditID, ClaimToken: previous.ClaimToken, Binding: req.Binding, ExpectedVersion: previous.Version, State: state, ProviderObjectRef: providerRef})
		return err
	})
	if err != nil {
		return billingstore.Intent{}, err
	}
	return updated, nil
}

func (r *SQLRepository) Find(ctx context.Context, binding provider.Binding, id uuid.UUID) (out billingstore.Intent, resultErr error) {
	if r == nil || r.db == nil || r.newStore == nil || ctx == nil || binding.Validate() != nil || !validID(id) {
		return billingstore.Intent{}, billingstore.ErrInvalidInput
	}
	err := r.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		s, err := r.newStore(tx)
		if err != nil {
			return err
		}
		out, err = s.FindIntent(ctx, binding, id)
		return err
	})
	if err != nil {
		return billingstore.Intent{}, err
	}
	return out, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || r == nil {
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.URL.Path == "/billing/checkout" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "request.method_not_allowed")
			return
		}
		h.start(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/billing/checkout/") {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "request.method_not_allowed")
			return
		}
		h.status(w, r)
		return
	}
	writeError(w, http.StatusNotFound, "request.not_found")
}

type startRequest struct {
	PriceKey string `json:"price_key"`
}

type response struct {
	State       string     `json:"state"`
	IntentID    string     `json:"intent_id,omitempty"`
	RedirectURL string     `json:"redirect_url,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	access, ok := h.cfg.ResolveAuthority(r.Context())
	if !ok || !authorized(access, h.cfg.Now(), false) {
		writeError(w, http.StatusForbidden, "billing.permission_denied")
		return
	}
	if !authorized(access, h.cfg.Now(), true) {
		writeError(w, http.StatusForbidden, "billing.step_up_required")
		return
	}
	key := idempotencyKey(r)
	if key == "" {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "request.invalid")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	var input startRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil || input.PriceKey == "" || !validSelection(input.PriceKey) {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	binding, err := h.binding(access)
	if err != nil {
		writeError(w, http.StatusForbidden, "billing.permission_denied")
		return
	}
	plan, _, err := h.cfg.Catalog.ResolveSelection(input.PriceKey, h.cfg.Now())
	if err != nil || plan.Scope.InstallationID != binding.InstallationID || plan.Scope.ApplicationID != bindingApplication(access).String() || plan.Scope.EnvironmentID != binding.EnvironmentID || plan.Scope.Provider != binding.Provider || plan.Scope.ProviderAccountID != binding.AccountID || plan.Scope.AccountMode != binding.AccountMode {
		writeError(w, http.StatusUnprocessableEntity, "billing.selection_unavailable")
		return
	}
	request := CheckoutRequest{Binding: binding, PriceKey: input.PriceKey, SuccessURL: h.cfg.SuccessURL, CancelURL: h.cfg.CancelURL, IdempotencyKey: key}
	workspaceAccountID, err := identitystore.NewID()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	intentID, err := identitystore.NewID()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	auditID, err := identitystore.NewID()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	prepared, err := h.cfg.Repository.Prepare(r.Context(), workspaceAccountID, intentID, auditID, request)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	if prepared.Intent.State == "confirmed" {
		if h.cfg.ContinuationReader == nil {
			writeError(w, http.StatusServiceUnavailable, "billing.continuation_unavailable")
			return
		}
		outcome, readErr := h.cfg.ContinuationReader.ReadConfirmedCheckout(r.Context(), toProviderRequest(prepared.Request), prepared.Intent)
		if readErr != nil {
			if errors.Is(readErr, ErrCheckoutExpired) {
				writeError(w, http.StatusConflict, "billing.checkout_expired")
				return
			}
			writeError(w, http.StatusServiceUnavailable, "billing.continuation_unavailable")
			return
		}
		if err := h.validateOutcome(prepared.Request, outcome); err != nil || outcome.State != provider.SubscriptionConfirmed || outcome.ProviderObject.ID != prepared.Intent.ProviderObjectRef {
			if errors.Is(err, ErrCheckoutExpired) {
				writeError(w, http.StatusConflict, "billing.checkout_expired")
			} else {
				writeError(w, http.StatusServiceUnavailable, "billing.continuation_unavailable")
			}
			return
		}
		h.respondOutcome(w, prepared.Request, prepared.Intent, outcome)
		return
	}
	if prepared.Intent.State == "failed" {
		writeJSON(w, http.StatusConflict, response{State: "failed", IntentID: prepared.Intent.ID.String(), CreatedAt: timePtr(prepared.Intent.CreatedAt)})
		return
	}
	if prepared.Intent.State == "unknown" {
		outcome, callErr := h.cfg.Provider.RecoverCheckout(r.Context(), toProviderRequest(prepared.Request), prepared.Intent)
		if callErr != nil {
			outcome = provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: h.cfg.Now().UTC()}
			_, _ = h.cfg.Repository.Resolve(r.Context(), prepared.Request, prepared.Intent, outcome)
			writeJSON(w, http.StatusAccepted, response{State: "unknown", IntentID: prepared.Intent.ID.String(), CreatedAt: timePtr(prepared.Intent.CreatedAt)})
			return
		}
		if h.validateOutcome(prepared.Request, outcome) != nil {
			h.markUnknown(w, r, prepared)
			return
		}
		updated, resolveErr := h.cfg.Repository.Resolve(r.Context(), prepared.Request, prepared.Intent, outcome)
		if resolveErr != nil {
			writeError(w, http.StatusServiceUnavailable, "billing.unknown")
			return
		}
		h.respondOutcome(w, prepared.Request, updated, outcome)
		return
	}
	if !prepared.Claimed {
		writeJSON(w, http.StatusAccepted, response{State: "pending", IntentID: prepared.Intent.ID.String(), CreatedAt: timePtr(prepared.Intent.CreatedAt)})
		return
	}
	outcome, callErr := h.cfg.Provider.CreateCheckout(r.Context(), toProviderRequest(prepared.Request))
	if callErr != nil {
		outcome = provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: h.cfg.Now().UTC()}
		if _, resolveErr := h.cfg.Repository.Resolve(r.Context(), prepared.Request, prepared.Intent, outcome); resolveErr != nil {
			writeError(w, http.StatusServiceUnavailable, "billing.unknown")
			return
		}
		writeJSON(w, http.StatusAccepted, response{State: "unknown", IntentID: prepared.Intent.ID.String(), CreatedAt: timePtr(prepared.Intent.CreatedAt)})
		return
	}
	if h.validateOutcome(prepared.Request, outcome) != nil {
		h.markUnknown(w, r, prepared)
		return
	}
	updated, err := h.cfg.Repository.Resolve(r.Context(), prepared.Request, prepared.Intent, outcome)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "billing.unknown")
		return
	}
	h.respondOutcome(w, prepared.Request, updated, outcome)
}

func (h *Handler) markUnknown(w http.ResponseWriter, r *http.Request, prepared Prepared) {
	outcome := provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: h.cfg.Now().UTC()}
	if _, err := h.cfg.Repository.Resolve(r.Context(), prepared.Request, prepared.Intent, outcome); err != nil {
		writeError(w, http.StatusServiceUnavailable, "billing.unknown")
		return
	}
	writeJSON(w, http.StatusAccepted, response{State: "unknown", IntentID: prepared.Intent.ID.String(), CreatedAt: timePtr(prepared.Intent.CreatedAt)})
}

func (h *Handler) respondOutcome(w http.ResponseWriter, req CheckoutRequest, intent billingstore.Intent, outcome provider.Outcome[provider.CheckoutSession]) {
	if err := h.validateOutcome(req, outcome); err != nil {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	switch outcome.State {
	case provider.SubscriptionConfirmed:
		if outcome.Value.Ref.ID != intent.ProviderObjectRef {
			writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
			return
		}
		writeJSON(w, http.StatusOK, response{State: "redirect", IntentID: intent.ID.String(), RedirectURL: outcome.Value.URL, CreatedAt: timePtr(intent.CreatedAt)})
	case provider.SubscriptionPending:
		writeJSON(w, http.StatusAccepted, response{State: "pending", IntentID: intent.ID.String(), CreatedAt: timePtr(intent.CreatedAt)})
	case provider.SubscriptionUnknown:
		writeJSON(w, http.StatusAccepted, response{State: "unknown", IntentID: intent.ID.String(), CreatedAt: timePtr(intent.CreatedAt)})
	case provider.SubscriptionFailed:
		writeJSON(w, http.StatusConflict, response{State: "failed", IntentID: intent.ID.String(), CreatedAt: timePtr(intent.CreatedAt)})
	default:
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
	}
}

func validateOutcome(req CheckoutRequest, outcome provider.Outcome[provider.CheckoutSession]) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	if outcome.State != provider.SubscriptionConfirmed {
		if outcome.ProviderObject != (provider.ObjectRef{}) {
			return provider.ErrMalformedResult
		}
		return nil
	}
	session := outcome.Value
	if session.Ref.Validate() != nil || session.Ref.Binding != req.Binding ||
		outcome.ProviderObject.Validate() != nil || outcome.ProviderObject.Binding != req.Binding ||
		outcome.ProviderObject.ID != session.Ref.ID || session.URL == "" {
		return provider.ErrMalformedResult
	}
	u, err := url.Parse(session.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return provider.ErrMalformedResult
	}
	return nil
}

func (h *Handler) validateOutcome(req CheckoutRequest, outcome provider.Outcome[provider.CheckoutSession]) error {
	if err := validateOutcome(req, outcome); err != nil {
		return err
	}
	if outcome.State != provider.SubscriptionConfirmed {
		return nil
	}
	if outcome.Value.ExpiresAt.IsZero() || !outcome.Value.ExpiresAt.After(h.cfg.Now()) {
		return ErrCheckoutExpired
	}
	u, err := url.Parse(outcome.Value.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return provider.ErrMalformedResult
	}
	if _, ok := h.checkoutHosts[strings.ToLower(u.Hostname())]; !ok {
		return provider.ErrMalformedResult
	}
	return nil
}

func validCheckoutHost(host string) bool {
	if host == "" || strings.ToLower(host) != host || strings.TrimSpace(host) != host || strings.ContainsAny(host, ":/@?#*[]") {
		return false
	}
	u, err := url.Parse("https://" + host)
	return err == nil && u.Hostname() == host && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	access, ok := h.cfg.ResolveAuthority(r.Context())
	if !ok || !authorized(access, h.cfg.Now(), false) {
		writeError(w, http.StatusForbidden, "billing.permission_denied")
		return
	}
	if r.URL.RawQuery != "" || strings.Contains(r.URL.Path, "%") {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, "/billing/checkout/")
	id, err := uuid.Parse(raw)
	if err != nil || id.String() != raw || !validID(id) {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	binding, err := h.binding(access)
	if err != nil {
		writeError(w, http.StatusForbidden, "billing.permission_denied")
		return
	}
	intent, err := h.cfg.Repository.Find(r.Context(), binding, id)
	if err != nil {
		h.writeRepositoryError(w, err)
		return
	}
	state := statusState(intent.State)
	if state == "" {
		writeError(w, http.StatusServiceUnavailable, "billing.unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response{State: state, IntentID: intent.ID.String(), CreatedAt: timePtr(intent.CreatedAt)})
}

func (h *Handler) binding(access Authority) (provider.Binding, error) {
	workspace := access.Workspace
	if access.ActorKind != "person" || workspace.State != workspacestore.WorkspaceActive || workspace.ID == uuid.Nil ||
		workspace.Scope.InstallationID != access.InstallationID || workspace.Scope.ApplicationID != access.ApplicationID ||
		access.EnvironmentID == uuid.Nil || access.PersonID == uuid.Nil {
		return provider.Binding{}, ErrUnauthorized
	}
	switch workspace.Kind {
	case workspacestore.KindPersonal:
		if workspace.PersonalOwnerID != access.PersonID {
			return provider.Binding{}, ErrUnauthorized
		}
	case workspacestore.KindOrganization:
		if access.Membership == nil || access.Membership.PersonID != access.PersonID || access.Membership.WorkspaceID != workspace.ID || access.Membership.State != workspacestore.MembershipActive || access.Membership.Role != workspacestore.RoleOwner || !hasPermission(access.Permissions, "billing.manage") {
			return provider.Binding{}, ErrUnauthorized
		}
	default:
		return provider.Binding{}, ErrUnauthorized
	}
	return provider.Binding{InstallationID: access.InstallationID.String(), EnvironmentID: access.EnvironmentID.String(), WorkspaceID: workspace.ID.String(), Provider: "stripe", AccountID: h.cfg.ProviderAccountID, AccountMode: h.cfg.AccountMode}, nil
}

func authorized(access Authority, now time.Time, requireStepUp bool) bool {
	if now.IsZero() || access.ActorKind != "person" || access.PersonID == uuid.Nil {
		return false
	}
	if access.Workspace.State != workspacestore.WorkspaceActive {
		return false
	}
	switch access.Workspace.Kind {
	case workspacestore.KindPersonal:
		if access.Workspace.PersonalOwnerID != access.PersonID {
			return false
		}
	case workspacestore.KindOrganization:
		if access.Membership == nil || access.Membership.PersonID != access.PersonID || access.Membership.WorkspaceID != access.Workspace.ID || access.Membership.Role != workspacestore.RoleOwner || access.Membership.State != workspacestore.MembershipActive || !hasPermission(access.Permissions, "billing.manage") {
			return false
		}
	default:
		return false
	}
	if requireStepUp {
		if (access.AssuranceLevel != identity.AAL2 && access.AssuranceLevel != identity.AAL3) || !access.AssuranceExpiresAt.After(now) {
			return false
		}
	}
	return true
}

func contextAuthority(ctx context.Context) (Authority, bool) {
	if ctx == nil {
		return Authority{}, false
	}
	principal, ok := identity.PrincipalFromContext(ctx)
	if !ok {
		return Authority{}, false
	}
	selection, ok := workspacectx.FromContext(ctx)
	if !ok {
		return Authority{}, false
	}
	return Authority{
		ActorKind: principal.Actor().Kind(), PersonID: principal.PersonID(),
		InstallationID: principal.InstallationID(), ApplicationID: principal.ApplicationID(), EnvironmentID: principal.EnvironmentID(),
		AssuranceLevel: principal.Assurance().Level(), AssuranceExpiresAt: principal.Assurance().ExpiresAt(),
		Workspace: selection.Workspace, Membership: selection.Membership, Permissions: selection.Permissions,
	}, true
}

func toProviderRequest(req CheckoutRequest) provider.CheckoutRequest {
	return provider.CheckoutRequest{Binding: req.Binding, Customer: req.Customer, PriceKey: req.PriceKey, SuccessURL: req.SuccessURL, CancelURL: req.CancelURL, IdempotencyKey: req.IdempotencyKey}
}

func storeOutcome(outcome provider.Outcome[provider.CheckoutSession]) (string, string) {
	switch outcome.State {
	case provider.SubscriptionConfirmed:
		return "confirmed", outcome.ProviderObject.ID
	case provider.SubscriptionFailed:
		return "failed", ""
	default:
		return "unknown", ""
	}
}

func statusState(state string) string {
	switch state {
	case "pending", "claimed":
		return "pending"
	case "unknown":
		return "unknown"
	case "confirmed":
		return "ready"
	case "failed":
		return "failed"
	default:
		return ""
	}
}

func (h *Handler) writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billingstore.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "billing.idempotency_conflict")
	case errors.Is(err, billingstore.ErrIntentUnavailable):
		writeError(w, http.StatusNotFound, "billing.intent_unavailable")
	case errors.Is(err, billingstore.ErrAccountUnavailable), errors.Is(err, ErrCustomerMissing):
		writeError(w, http.StatusServiceUnavailable, "billing.setup_required")
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, billingstore.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "request.invalid")
	default:
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
	}
}

func idempotencyKey(r *http.Request) string {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 || values[0] != strings.TrimSpace(values[0]) || !keyPattern.MatchString(values[0]) {
		return ""
	}
	return values[0]
}

func validSelection(s string) bool { return len(s) <= 128 && !strings.ContainsAny(s, "\r\n\x00") }
func hasPermission(permissions []string, want string) bool {
	for _, permission := range permissions {
		if permission == want {
			return true
		}
	}
	return false
}
func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == strings.ToLower(id.String())
}
func bindingApplication(access Authority) uuid.UUID { return access.ApplicationID }
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t.UTC()
	return &v
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, struct {
		Code string `json:"code"`
	}{Code: code})
}

// RequestDigest is the immutable digest recorded by the SQL repository.
func RequestDigest(req CheckoutRequest) [sha256.Size]byte {
	return stripecheckout.CheckoutPayloadHash(toProviderRequest(req))
}
