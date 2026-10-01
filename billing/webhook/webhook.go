// Package webhook accepts Stripe events only after SDK signature verification
// and durable, scoped receipt. It does not mutate entitlements or projections.
package webhook

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	identitystore "github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/jobs"
	"github.com/google/uuid"
	stripe "github.com/stripe/stripe-go/v87"
	stripewebhook "github.com/stripe/stripe-go/v87/webhook"
)

const (
	Path             = "/webhooks/stripe"
	DefaultBodyLimit = 1 << 20
	maxBodyLimit     = 2 << 20
	maxSignatureSize = 8 << 10
	maxSecrets       = 2
	maxRotationGrace = 24 * time.Hour
)

var (
	ErrInvalidConfig = errors.New("webhook handler configuration is invalid")
	ErrInvalidEvent  = errors.New("webhook event is invalid")
	ErrInboxConflict = billingstore.ErrInboxConflict
	eventIDPattern   = regexp.MustCompile(`^evt_[A-Za-z0-9][A-Za-z0-9_]{0,120}$`)
	refPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:./-]{0,127}$`)
	eventTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

type QuarantineReason = string

const (
	QuarantineNone             = ""
	QuarantineUnknownCustomer  = "unknown_customer"
	QuarantineMissingCustomer  = "missing_customer"
	QuarantineMetadataMismatch = "metadata_mismatch"
	QuarantineAccountMismatch  = "account_mismatch"
	QuarantineModeMismatch     = QuarantineAccountMismatch
	QuarantineUnsupportedEvent = "unsupported_event"
)

type EndpointScope = billingstore.EndpointScope

// Secrets are ordered current-first, previous-second during an explicitly
// bounded key-rotation window. Secret values are never included in events or
// errors.
type SigningSecret struct {
	Value    string
	RetireAt time.Time // required only for the previous secret; bounded to 24 hours
}

type Config struct {
	Scope              EndpointScope
	EventAccountID     string // empty only for an event from the configured platform account
	Secrets            []SigningSecret
	BodyLimit          int64
	SignatureTolerance time.Duration
	Now                func() time.Time
	Router             BindingRouter
	Inbox              DurableInbox
}

type EventClaims struct {
	ID              string
	Type            string
	EventAccountID  string
	LiveMode        bool
	CustomerRef     string
	SubscriptionRef string
	Metadata        map[string]string
}

type ResolvedRoute struct {
	ApplicationID    uuid.UUID
	Binding          provider.Binding
	QuarantineReason QuarantineReason
}

type BindingRouter interface {
	// Resolve uses configured endpoint scope and signed event claims to find the
	// active persisted customer binding. It must not route by metadata alone.
	Resolve(context.Context, EndpointScope, EventClaims) (ResolvedRoute, error)
}

type VerifiedWebhookInput = billingstore.VerifiedWebhookInput
type VerifiedWebhookReceipt = billingstore.VerifiedWebhookReceipt

type DurableInbox interface {
	// PersistVerifiedWebhook returns only after the receipt transaction commits.
	PersistVerifiedWebhook(context.Context, VerifiedWebhookInput) (VerifiedWebhookReceipt, error)
}

type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

type TransactionStore interface {
	FindCustomerBindingByRef(context.Context, billingstore.CustomerLookupInput) (billingstore.CustomerBinding, error)
	PersistVerifiedWebhook(context.Context, billingstore.VerifiedWebhookInput) (billingstore.VerifiedWebhookReceipt, error)
}

type StoreFactory func(*sql.Tx) (TransactionStore, error)

type SQLBindingRouter struct {
	db       TxRunner
	newStore StoreFactory
}

func NewSQLBindingRouter(db TxRunner, newStore StoreFactory) (*SQLBindingRouter, error) {
	if db == nil || newStore == nil {
		return nil, ErrInvalidConfig
	}
	return &SQLBindingRouter{db: db, newStore: newStore}, nil
}

func (r *SQLBindingRouter) Resolve(ctx context.Context, scope EndpointScope, claims EventClaims) (ResolvedRoute, error) {
	var route ResolvedRoute
	if r == nil || r.db == nil || r.newStore == nil || ctx == nil || !validEndpointScope(scope) || !refPattern.MatchString(claims.CustomerRef) {
		return ResolvedRoute{}, ErrInvalidEvent
	}
	err := r.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		s, err := r.newStore(tx)
		if err != nil {
			return err
		}
		customer, err := s.FindCustomerBindingByRef(ctx, billingstore.CustomerLookupInput{Endpoint: scope, CustomerRef: claims.CustomerRef})
		if errors.Is(err, billingstore.ErrAccountUnavailable) {
			route = ResolvedRoute{QuarantineReason: QuarantineUnknownCustomer}
			return nil
		}
		if err != nil {
			return err
		}
		route = ResolvedRoute{ApplicationID: scope.ApplicationID, Binding: customer.Binding}
		return nil
	})
	if err != nil {
		return ResolvedRoute{}, err
	}
	return route, nil
}

type SQLInbox struct {
	db       TxRunner
	newStore StoreFactory
	jobs     TxEnqueuer
}

// TxEnqueuer writes a durable job into the caller's receipt transaction.
type TxEnqueuer interface {
	EnqueueTx(context.Context, *sql.Tx, jobs.Intent) (jobs.Job, error)
}

func NewSQLInbox(db TxRunner, newStore StoreFactory, jobWriter TxEnqueuer) (*SQLInbox, error) {
	if db == nil || newStore == nil || jobWriter == nil {
		return nil, ErrInvalidConfig
	}
	return &SQLInbox{db: db, newStore: newStore, jobs: jobWriter}, nil
}

func (i *SQLInbox) PersistVerifiedWebhook(ctx context.Context, input VerifiedWebhookInput) (VerifiedWebhookReceipt, error) {
	if i == nil || i.db == nil || i.newStore == nil || i.jobs == nil || ctx == nil {
		return VerifiedWebhookReceipt{}, billingstore.ErrInvalidInput
	}
	var out VerifiedWebhookReceipt
	err := i.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		s, err := i.newStore(tx)
		if err != nil {
			return err
		}
		out, err = s.PersistVerifiedWebhook(ctx, input)
		if err != nil {
			return err
		}
		intent, err := reconcileIntent(input.Endpoint, out)
		if err != nil {
			return err
		}
		_, err = i.jobs.EnqueueTx(ctx, tx, intent)
		return err
	})
	if err != nil {
		return VerifiedWebhookReceipt{}, err
	}
	return out, nil
}

const reconcileJobKind = "billing.webhook.reconcile"

type reconcilePayload struct {
	IngressID         uuid.UUID `json:"ingress_id"`
	EnvironmentID     uuid.UUID `json:"environment_id"`
	Provider          string    `json:"provider"`
	ProviderAccountID string    `json:"provider_account_id"`
	AccountMode       string    `json:"account_mode"`
}

func reconcileIntent(endpoint EndpointScope, receipt VerifiedWebhookReceipt) (jobs.Intent, error) {
	if receipt.ID == uuid.Nil || receipt.ReceivedAt.IsZero() || !validEndpointScope(endpoint) {
		return jobs.Intent{}, ErrInvalidEvent
	}
	payload, err := json.Marshal(reconcilePayload{
		IngressID: receipt.ID, EnvironmentID: endpoint.EnvironmentID, Provider: endpoint.Provider,
		ProviderAccountID: endpoint.ProviderAccountID, AccountMode: string(endpoint.AccountMode),
	})
	if err != nil {
		return jobs.Intent{}, err
	}
	return jobs.Intent{
		InstallationID: endpoint.InstallationID,
		ApplicationID:  endpoint.ApplicationID,
		Key:            "billing:reconcile:" + receipt.ID.String(),
		Kind:           reconcileJobKind,
		Payload:        payload,
		ExternalEffect: false,
		Deadline:       receipt.ReceivedAt.UTC().Add(7 * 24 * time.Hour),
	}, nil
}

type Handler struct {
	cfg Config
}

func New(cfg Config) (*Handler, error) {
	if !validEndpointScope(cfg.Scope) || cfg.Router == nil || cfg.Inbox == nil ||
		!validOptionalAccountID(cfg.EventAccountID) || len(cfg.Secrets) < 1 || len(cfg.Secrets) > maxSecrets {
		return nil, ErrInvalidConfig
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	now := cfg.Now()
	if now.IsZero() {
		return nil, ErrInvalidConfig
	}
	seen := make(map[string]struct{}, len(cfg.Secrets))
	for index, secret := range cfg.Secrets {
		if len(secret.Value) < 16 || len(secret.Value) > 256 || strings.TrimSpace(secret.Value) != secret.Value {
			return nil, ErrInvalidConfig
		}
		if index == 0 && !secret.RetireAt.IsZero() || index == 1 && (secret.RetireAt.IsZero() || !secret.RetireAt.After(now) || secret.RetireAt.After(now.Add(maxRotationGrace))) {
			return nil, ErrInvalidConfig
		}
		if _, exists := seen[secret.Value]; exists {
			return nil, ErrInvalidConfig
		}
		seen[secret.Value] = struct{}{}
	}
	if cfg.BodyLimit == 0 {
		cfg.BodyLimit = DefaultBodyLimit
	}
	if cfg.BodyLimit < 1024 || cfg.BodyLimit > maxBodyLimit {
		return nil, ErrInvalidConfig
	}
	if cfg.SignatureTolerance == 0 {
		cfg.SignatureTolerance = stripewebhook.DefaultTolerance
	}
	if cfg.SignatureTolerance < time.Second || cfg.SignatureTolerance > 15*time.Minute {
		return nil, ErrInvalidConfig
	}
	cfg.Secrets = append([]SigningSecret(nil), cfg.Secrets...)
	return &Handler{cfg: cfg}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || r == nil {
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path != Path {
		writeError(w, http.StatusNotFound, "request.not_found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "request.method_not_allowed")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	signatures := r.Header.Values("Stripe-Signature")
	if len(signatures) != 1 || signatures[0] == "" || len(signatures[0]) > maxSignatureSize {
		writeError(w, http.StatusBadRequest, "webhook.signature_invalid")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.BodyLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request.too_large")
			return
		}
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "request.invalid")
		return
	}
	var event stripe.Event
	verified := false
	now := h.cfg.Now()
	for index, secret := range h.cfg.Secrets {
		if index > 0 && !now.Before(secret.RetireAt) {
			continue
		}
		event, err = stripewebhook.ConstructEventWithOptions(body, signatures[0], secret.Value,
			stripewebhook.ConstructEventOptions{Tolerance: h.cfg.SignatureTolerance})
		if err == nil {
			verified = true
			break
		}
	}
	if !verified {
		writeError(w, http.StatusBadRequest, "webhook.signature_invalid")
		return
	}
	claims, err := normalizeEvent(event)
	if err != nil {
		writeError(w, http.StatusBadRequest, "webhook.event_invalid")
		return
	}
	input, err := h.resolve(r.Context(), claims, body)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "dependency.unavailable")
		return
	}
	receipt, err := h.cfg.Inbox.PersistVerifiedWebhook(r.Context(), input)
	if errors.Is(err, billingstore.ErrAccountUnavailable) && input.QuarantineReason == QuarantineNone {
		input.Binding = provider.Binding{}
		input.QuarantineReason = QuarantineUnknownCustomer
		receipt, err = h.cfg.Inbox.PersistVerifiedWebhook(r.Context(), input)
	}
	if err != nil {
		if errors.Is(err, ErrInboxConflict) || errors.Is(err, billingstore.ErrInboxConflict) {
			writeError(w, http.StatusConflict, "webhook.event_conflict")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "webhook.receipt_unavailable")
		return
	}
	if receipt.QuarantineReason != QuarantineNone || receipt.State == "quarantined" || input.QuarantineReason != QuarantineNone {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resolve(ctx context.Context, claims EventClaims, body []byte) (VerifiedWebhookInput, error) {
	route := ResolvedRoute{}
	if claims.EventAccountID != h.cfg.EventAccountID {
		route.QuarantineReason = QuarantineAccountMismatch
	} else if claims.LiveMode != (h.cfg.Scope.AccountMode == provider.AccountLive) {
		route.QuarantineReason = QuarantineModeMismatch
	} else if !supportedEventType(claims.Type) {
		route.QuarantineReason = QuarantineUnsupportedEvent
	} else if claims.CustomerRef == "" {
		route.QuarantineReason = QuarantineMissingCustomer
	} else {
		verifiedMetadata := cloneMetadata(claims.Metadata)
		claims.Metadata = cloneMetadata(claims.Metadata)
		var err error
		route, err = h.cfg.Router.Resolve(ctx, h.cfg.Scope, claims)
		if err != nil {
			return VerifiedWebhookInput{}, err
		}
		if route.QuarantineReason == QuarantineNone {
			if route.ApplicationID != h.cfg.Scope.ApplicationID || !bindingMatchesScope(route.Binding, h.cfg.Scope) {
				return VerifiedWebhookInput{}, ErrInvalidEvent
			}
			if !metadataMatchesBinding(verifiedMetadata, route.Binding) {
				route = ResolvedRoute{QuarantineReason: QuarantineMetadataMismatch}
			}
		}
	}
	if !validQuarantineReason(route.QuarantineReason) {
		return VerifiedWebhookInput{}, ErrInvalidEvent
	}
	if route.QuarantineReason != QuarantineNone && route.Binding != (provider.Binding{}) {
		return VerifiedWebhookInput{}, ErrInvalidEvent
	}
	id, err := identitystore.NewID()
	if err != nil {
		return VerifiedWebhookInput{}, err
	}
	return VerifiedWebhookInput{
		ID: id, Endpoint: h.cfg.Scope, EventID: claims.ID, EventType: claims.Type,
		CustomerRef: claims.CustomerRef, SubscriptionRef: claims.SubscriptionRef,
		PayloadSHA256: sha256.Sum256(body), Binding: route.Binding,
		QuarantineReason: route.QuarantineReason,
	}, nil
}

func normalizeEvent(event stripe.Event) (EventClaims, error) {
	claims := EventClaims{ID: event.ID, Type: string(event.Type), EventAccountID: event.Account, LiveMode: event.Livemode}
	if !eventIDPattern.MatchString(claims.ID) || !eventTypePattern.MatchString(claims.Type) || event.Data == nil || len(event.Data.Raw) == 0 {
		return EventClaims{}, ErrInvalidEvent
	}
	var object struct {
		ID           string          `json:"id"`
		Object       string          `json:"object"`
		Customer     json.RawMessage `json:"customer"`
		Subscription json.RawMessage `json:"subscription"`
		Parent       struct {
			SubscriptionDetails struct {
				Subscription json.RawMessage `json:"subscription"`
			} `json:"subscription_details"`
		} `json:"parent"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(event.Data.Raw, &object); err != nil || !refPattern.MatchString(object.ID) {
		return EventClaims{}, ErrInvalidEvent
	}
	claims.CustomerRef = parseReference(object.Customer)
	claims.SubscriptionRef = parseReference(object.Subscription)
	if claims.SubscriptionRef == "" && object.Parent.SubscriptionDetails.Subscription != nil {
		claims.SubscriptionRef = parseReference(object.Parent.SubscriptionDetails.Subscription)
	}
	if object.Object == "subscription" {
		claims.SubscriptionRef = object.ID
	}
	claims.Metadata = normalizedMetadata(object.Metadata)
	return claims, nil
}

func parseReference(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		if refPattern.MatchString(value) {
			return value
		}
		return ""
	}
	var object struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &object) == nil && refPattern.MatchString(object.ID) {
		return object.ID
	}
	return ""
}

func normalizedMetadata(values map[string]string) map[string]string {
	out := make(map[string]string, 5)
	for _, key := range []string{"amos_installation_id", "amos_environment_id", "amos_provider_account_id", "amos_account_mode", "amos_workspace_id"} {
		if value := values[key]; value != "" && len(value) <= 128 && strings.TrimSpace(value) == value {
			out[key] = value
		}
	}
	return out
}
func cloneMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func metadataMatchesBinding(metadata map[string]string, binding provider.Binding) bool {
	for key, expected := range map[string]string{
		"amos_installation_id":     binding.InstallationID,
		"amos_environment_id":      binding.EnvironmentID,
		"amos_provider_account_id": binding.AccountID,
		"amos_account_mode":        string(binding.AccountMode),
		"amos_workspace_id":        binding.WorkspaceID,
	} {
		if metadata[key] != expected {
			return false
		}
	}
	return true
}

func bindingMatchesScope(binding provider.Binding, scope EndpointScope) bool {
	return binding.Validate() == nil && binding.InstallationID == scope.InstallationID.String() &&
		binding.EnvironmentID == scope.EnvironmentID.String() && binding.Provider == "stripe" &&
		binding.AccountID == scope.ProviderAccountID && binding.AccountMode == scope.AccountMode
}

func validEndpointScope(scope EndpointScope) bool {
	return validUUIDv7(scope.InstallationID) && validUUIDv7(scope.ApplicationID) && validUUIDv7(scope.EnvironmentID) &&
		scope.Provider == "stripe" &&
		refPattern.MatchString(scope.ProviderAccountID) && (scope.AccountMode == provider.AccountTest || scope.AccountMode == provider.AccountLive)
}

func validOptionalAccountID(value string) bool { return value == "" || refPattern.MatchString(value) }
func validUUIDv7(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validQuarantineReason(reason QuarantineReason) bool {
	switch reason {
	case QuarantineNone, QuarantineUnknownCustomer, QuarantineMissingCustomer, QuarantineMetadataMismatch,
		QuarantineAccountMismatch, QuarantineUnsupportedEvent:
		return true
	default:
		return false
	}
}

func supportedEventType(eventType string) bool {
	switch eventType {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded", "checkout.session.async_payment_failed",
		"customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted",
		"invoice.paid", "invoice.payment_failed", "invoice.finalized":
		return true
	default:
		return false
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{Code: code})
}
