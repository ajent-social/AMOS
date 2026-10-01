// Package stripecheckout adapts Stripe's official Go SDK to AMOS's narrowly
// scoped customer and hosted Checkout contracts. It performs no automatic
// network retries; ambiguous writes require a matching durable unknown intent.
package stripecheckout

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	stripe "github.com/stripe/stripe-go/v87"
)

const (
	CheckoutIntentOperation = "checkout.session"
	CustomerIntentOperation = "customer.create"
	StripeAPIVersion        = "2026-09-30.endive"
)

var (
	ErrInvalidConfig  = errors.New("invalid Stripe checkout configuration")
	ErrInvalidRequest = errors.New("invalid Stripe checkout request")
	ErrWrongAccount   = errors.New("stripe response did not match configured billing account")
)

type Config struct {
	APIKey            string
	ProviderAccountID string
	AccountMode       provider.AccountMode
	Catalog           *catalog.Catalog
	ReturnHosts       []string
	Timeout           time.Duration
	// ConnectAccountID scopes requests through Stripe-Account. Leave empty
	// when the API key itself belongs to the configured merchant account.
	ConnectAccountID string
	// APIBaseURL is only accepted for loopback SDK fixtures in test mode.
	APIBaseURL string
	Now        func() time.Time
}

type Adapter struct {
	client         *stripe.Client
	accountID      string
	mode           provider.AccountMode
	catalog        *catalog.Catalog
	returnHosts    map[string]struct{}
	connectAccount string
	now            func() time.Time
}

func New(cfg Config) (*Adapter, error) {
	if cfg.AccountMode != provider.AccountTest && cfg.AccountMode != provider.AccountLive ||
		!validAccountID(cfg.ProviderAccountID) || !validKeyMode(cfg.APIKey, cfg.AccountMode) ||
		cfg.Catalog == nil || len(cfg.ReturnHosts) == 0 || stripe.APIVersion != StripeAPIVersion {
		return nil, ErrInvalidConfig
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Timeout < time.Second || cfg.Timeout > 30*time.Second {
		return nil, ErrInvalidConfig
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	hosts := make(map[string]struct{}, len(cfg.ReturnHosts))
	for _, host := range cfg.ReturnHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" || strings.Contains(host, "/") || strings.Contains(host, ":") || strings.Contains(host, "@") {
			return nil, ErrInvalidConfig
		}
		hosts[host] = struct{}{}
	}
	if len(hosts) == 0 {
		return nil, ErrInvalidConfig
	}
	if cfg.ConnectAccountID != "" && (!validStripeID(cfg.ConnectAccountID) || cfg.ConnectAccountID != cfg.ProviderAccountID) {
		return nil, ErrInvalidConfig
	}
	var apiURL *string
	if cfg.APIBaseURL != "" {
		if cfg.AccountMode != provider.AccountTest || !loopbackURL(cfg.APIBaseURL) {
			return nil, ErrInvalidConfig
		}
		apiURL = &cfg.APIBaseURL
	}
	noRetries := int64(0)
	enableTelemetry := false
	// Go's transport may replay idempotency-key POSTs on reused connections
	// independently of SDK retries. Fresh connections prevent that implicit replay.
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSHandshakeTimeout: 10 * time.Second}
	// SDK diagnostics can contain provider URLs and object identifiers. Callers
	// receive sanitized adapter errors instead of raw SDK logging.
	backendConfig := &stripe.BackendConfig{LeveledLogger: &stripe.LeveledLogger{Level: stripe.LevelNull}, URL: apiURL, HTTPClient: &http.Client{Timeout: cfg.Timeout, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, MaxNetworkRetries: &noRetries, EnableTelemetry: &enableTelemetry}
	client := stripe.NewClient(cfg.APIKey, stripe.WithBackends(stripe.NewBackendsWithConfig(backendConfig)))
	return &Adapter{client: client, accountID: cfg.ProviderAccountID, mode: cfg.AccountMode, catalog: cfg.Catalog, returnHosts: hosts, connectAccount: cfg.ConnectAccountID, now: cfg.Now}, nil
}

// CheckAccount verifies credential authority through Stripe rather than echoed
// metadata. Callers may use it for readiness; every mutation also verifies it.
func (a *Adapter) CheckAccount(ctx context.Context) error {
	if ctx == nil || a == nil || a.client == nil {
		return ErrInvalidConfig
	}
	params := &stripe.AccountRetrieveParams{}
	if a.connectAccount != "" {
		params.SetStripeAccount(a.connectAccount)
	}
	account, err := a.client.V1Accounts.Retrieve(ctx, params)
	if err != nil || account == nil || account.ID != a.accountID {
		return &provider.ProviderError{Class: provider.ErrorUnavailable, Retryable: false}
	}
	return nil
}

func (a *Adapter) EnsureCustomer(ctx context.Context, req provider.CustomerRequest) (provider.Customer, error) {
	if ctx == nil || a == nil || a.client == nil || !a.validBinding(req.Binding) || !validIdempotencyKey(req.IdempotencyKey) {
		return provider.Customer{}, provider.ErrInvalidRequest
	}
	metadata := bindingMetadata(req.Binding)
	if err := a.CheckAccount(ctx); err != nil {
		return provider.Customer{}, err
	}
	params := &stripe.CustomerCreateParams{Metadata: metadata}
	params.SetIdempotencyKey(req.IdempotencyKey)
	if a.connectAccount != "" {
		params.SetStripeAccount(a.connectAccount)
	}
	customer, err := a.client.V1Customers.Create(ctx, params)
	if err != nil {
		return provider.Customer{}, toCustomerError(err)
	}
	if customer == nil || !validStripeID(customer.ID) || customer.Livemode != (a.mode == provider.AccountLive) || !metadataMatches(customer.Metadata, metadata) {
		return provider.Customer{}, errors.Join(ErrWrongAccount, &provider.ProviderError{Class: provider.ErrorUnknownOutcome})
	}
	return provider.Customer{Ref: provider.ObjectRef{Binding: req.Binding, ID: customer.ID}}, nil
}

// RecoverCustomer repeats a customer-create request only when the caller has
// loaded the matching durable unknown intent from billing.store.
func (a *Adapter) RecoverCustomer(ctx context.Context, req provider.CustomerRequest, intent billingstore.Intent) (provider.Customer, error) {
	if ctx == nil || a == nil || !a.validBinding(req.Binding) || intent.Binding != req.Binding ||
		intent.Operation != CustomerIntentOperation || intent.State != "unknown" || intent.IdempotencyKey != req.IdempotencyKey ||
		intent.Version < 1 || intent.ProviderObjectRef != "" || intent.PayloadSHA256 != CustomerPayloadHash(req) {
		return provider.Customer{}, provider.ErrInvalidRequest
	}
	if !safeReplayAge(intent.CreatedAt, a.now()) {
		return provider.Customer{}, &provider.ProviderError{Class: provider.ErrorUnknownOutcome, Retryable: false}
	}
	return a.EnsureCustomer(ctx, req)
}

// CustomerPayloadHash is the immutable durable-intent digest for a customer
// operation; persist it before calling EnsureCustomer.
func CustomerPayloadHash(req provider.CustomerRequest) [sha256.Size]byte {
	data, _ := json.Marshal(struct {
		Binding        provider.Binding `json:"binding"`
		IdempotencyKey string           `json:"idempotency_key"`
	}{req.Binding, req.IdempotencyKey})
	return sha256.Sum256(data)
}

func (a *Adapter) CreateCheckout(ctx context.Context, req provider.CheckoutRequest) (provider.Outcome[provider.CheckoutSession], error) {
	if ctx == nil || a == nil || a.client == nil || !a.validBinding(req.Binding) || req.Customer.Validate() != nil || req.Customer.Binding != req.Binding || !validIdempotencyKey(req.IdempotencyKey) || !validReturnURL(req.SuccessURL, a.returnHosts) || !validReturnURL(req.CancelURL, a.returnHosts) {
		return provider.Outcome[provider.CheckoutSession]{}, provider.ErrInvalidRequest
	}
	return a.createCheckout(ctx, req)
}

// RecoverCheckout permits a single explicit idempotency replay only after the
// caller loads the durable unknown intent and proves its exact immutable payload.
// It does not retry automatically after transport failure.
func (a *Adapter) RecoverCheckout(ctx context.Context, req provider.CheckoutRequest, intent billingstore.Intent) (provider.Outcome[provider.CheckoutSession], error) {
	if ctx == nil || a == nil || !a.validBinding(req.Binding) || intent.Binding != req.Binding || intent.Operation != CheckoutIntentOperation || intent.State != "unknown" || intent.IdempotencyKey != req.IdempotencyKey || intent.Version < 1 || intent.ProviderObjectRef != "" || intent.PayloadSHA256 != CheckoutPayloadHash(req) {
		return provider.Outcome[provider.CheckoutSession]{}, provider.ErrInvalidRequest
	}
	if !safeReplayAge(intent.CreatedAt, a.now()) {
		return unknownOutcome(a.now()), &provider.ProviderError{Class: provider.ErrorUnknownOutcome, Retryable: false}
	}
	return a.CreateCheckout(ctx, req)
}

// CheckoutPayloadHash is the exact immutable request digest that callers must
// persist in billing.store before claiming and performing the provider call.
func CheckoutPayloadHash(req provider.CheckoutRequest) [sha256.Size]byte {
	payload := struct {
		Binding        provider.Binding `json:"binding"`
		Customer       string           `json:"customer"`
		Price          string           `json:"price"`
		SuccessURL     string           `json:"success_url"`
		CancelURL      string           `json:"cancel_url"`
		IdempotencyKey string           `json:"idempotency_key"`
		Quantity       int64            `json:"quantity"`
	}{
		Binding: req.Binding, Customer: req.Customer.ID, Price: req.PriceKey, SuccessURL: req.SuccessURL, CancelURL: req.CancelURL, IdempotencyKey: req.IdempotencyKey, Quantity: 1}
	data, _ := json.Marshal(payload)
	return sha256.Sum256(data)
}

func (a *Adapter) createCheckout(ctx context.Context, req provider.CheckoutRequest) (provider.Outcome[provider.CheckoutSession], error) {
	plan, price, err := a.catalog.ResolveSelection(req.PriceKey, a.now())
	if err != nil || plan.Scope.InstallationID != req.Binding.InstallationID || plan.Scope.EnvironmentID != req.Binding.EnvironmentID || plan.Scope.Provider != "stripe" || plan.Scope.ProviderAccountID != req.Binding.AccountID || plan.Scope.AccountMode != req.Binding.AccountMode {
		return provider.Outcome[provider.CheckoutSession]{}, provider.ErrInvalidRequest
	}
	metadata := bindingMetadata(req.Binding)
	metadata["amos_price_key"] = req.PriceKey
	metadata["amos_catalog_revision"] = a.catalog.Revision()
	if err := a.CheckAccount(ctx); err != nil {
		return unknownOutcome(a.now()), err
	}
	params := &stripe.CheckoutSessionCreateParams{Mode: stripe.String("subscription"), Customer: &req.Customer.ID,
		ClientReferenceID: &req.IdempotencyKey, SuccessURL: &req.SuccessURL, CancelURL: &req.CancelURL,
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{Price: &price.ProviderPriceKey, Quantity: stripe.Int64(1)}}, Metadata: metadata,
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{Metadata: bindingMetadata(req.Binding)}}
	params.SetIdempotencyKey(req.IdempotencyKey)
	if a.connectAccount != "" {
		params.SetStripeAccount(a.connectAccount)
	}
	session, err := a.client.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return checkoutError(err, a.now()), nil
	}
	if !a.validCheckoutResponse(session, req, metadata) {
		return unknownOutcome(a.now()), nil
	}
	result := provider.CheckoutSession{Ref: provider.ObjectRef{Binding: req.Binding, ID: session.ID}, URL: session.URL, ExpiresAt: time.Unix(session.ExpiresAt, 0).UTC()}
	ref := provider.ObjectRef{Binding: req.Binding, ID: session.ID}
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionConfirmed, Value: result, ProviderObject: ref, ObservedAt: a.now()}, nil
}

func (a *Adapter) validCheckoutResponse(s *stripe.CheckoutSession, req provider.CheckoutRequest, metadata map[string]string) bool {
	if s == nil || !validStripeID(s.ID) || s.Livemode != (a.mode == provider.AccountLive) || s.ClientReferenceID != req.IdempotencyKey || string(s.Mode) != "subscription" || !metadataMatches(s.Metadata, metadata) || !validHostedURL(s.URL) || s.ExpiresAt <= a.now().Unix() || s.ExpiresAt > a.now().Add(24*time.Hour).Unix() {
		return false
	}
	if s.Customer == nil || s.Customer.ID != req.Customer.ID {
		return false
	}
	return true
}
func (a *Adapter) validBinding(b provider.Binding) bool {
	return a != nil && b.Validate() == nil && b.Provider == "stripe" && b.AccountID == a.accountID && b.AccountMode == a.mode
}
func bindingMetadata(b provider.Binding) map[string]string {
	return map[string]string{"amos_installation_id": b.InstallationID, "amos_environment_id": b.EnvironmentID, "amos_workspace_id": b.WorkspaceID, "amos_provider_account_id": b.AccountID, "amos_account_mode": string(b.AccountMode)}
}
func metadataMatches(got, want map[string]string) bool {
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}
func validReturnURL(raw string, hosts map[string]struct{}) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Fragment != "" {
		return false
	}
	_, ok := hosts[strings.ToLower(u.Hostname())]
	return ok && u.Port() == ""
}
func validHostedURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "checkout.stripe.com" && u.User == nil && u.Port() == "" && strings.HasPrefix(u.Path, "/c/")
}
func validAccountID(s string) bool { return validStripeID(s) }
func validStripeID(s string) bool {
	if len(s) < 3 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}
func validIdempotencyKey(s string) bool {
	if len(s) < 8 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("_.:-", r):
		default:
			return false
		}
	}
	return true
}
func validKeyMode(key string, mode provider.AccountMode) bool {
	if len(key) < 12 || len(key) > 255 {
		return false
	}
	if mode == provider.AccountTest {
		return strings.HasPrefix(key, "sk_test_") || strings.HasPrefix(key, "rk_test_")
	}
	return mode == provider.AccountLive && (strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_"))
}
func loopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}
func checkoutError(err error, now time.Time) provider.Outcome[provider.CheckoutSession] {
	var se *stripe.Error
	if errors.As(err, &se) && se.HTTPStatusCode >= 400 && se.HTTPStatusCode < 500 {
		return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionFailed, Failure: &provider.ProviderError{Class: provider.ErrorInvalid}}
	}
	return unknownOutcome(now)
}
func unknownOutcome(observed time.Time) provider.Outcome[provider.CheckoutSession] {
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: observed}
}
func toCustomerError(err error) error {
	var se *stripe.Error
	if errors.As(err, &se) && se.HTTPStatusCode >= 400 && se.HTTPStatusCode < 500 {
		return &provider.ProviderError{Class: provider.ErrorInvalid}
	}
	return &provider.ProviderError{Class: provider.ErrorUnknownOutcome, Retryable: false}
}

var _ provider.CustomerClient = (*Adapter)(nil)
var _ provider.CheckoutClient = (*Adapter)(nil)

// Stripe can prune idempotency keys at 24 hours. Use a conservative 23-hour
// replay window from the durable intent's creation, which precedes first send.
// Older or malformed ages stay unknown and require reconciliation/manual review.
func safeReplayAge(created, now time.Time) bool {
	return !created.IsZero() && !now.IsZero() && !created.After(now) && now.Sub(created) < 23*time.Hour
}
