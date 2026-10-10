package apphost

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/checkout"
	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	"github.com/ajent-social/amos/ui/billingcheckout"
	workspacectx "github.com/ajent-social/amos/workspace/context"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

// LocalBillingConfig contains only server-owned billing dependencies. Selection
// keys refer to immutable catalog entries; no caller amount or payment state is
// accepted here.
type LocalBillingConfig struct {
	Catalog               *catalog.Catalog
	Provider              checkout.Provider
	ContinuationReader    checkout.ConfirmedCheckoutReader
	ProviderAccountID     string
	AccountMode           provider.AccountMode
	CheckoutHosts         []string
	SuccessURL, CancelURL string
	SelectionKeys         []string
	PaidFeature           string
	PostPaymentAction     string
}

type localBillingService struct {
	pool          *sql.DB
	cfg           LocalBillingConfig
	action        http.Handler
	selectionKeys map[string]struct{}
}

func newLocalBillingHandler(db *storage.DB, pool *sql.DB, sessions *session.Service, cfg *LocalBillingConfig, local LocalConfig) (http.Handler, error) {
	if db == nil || pool == nil || sessions == nil || cfg == nil || cfg.Catalog == nil ||
		cfg.Provider == nil || cfg.ContinuationReader == nil || cfg.ProviderAccountID == "" ||
		(cfg.AccountMode != provider.AccountTest && cfg.AccountMode != provider.AccountLive) ||
		len(cfg.SelectionKeys) == 0 || len(cfg.SelectionKeys) > 24 || len(cfg.CheckoutHosts) == 0 ||
		!validLocalActionPath(cfg.PostPaymentAction) || strings.TrimSpace(cfg.PaidFeature) == "" {
		return nil, errors.New("local billing configuration is incomplete")
	}
	keys := make(map[string]struct{}, len(cfg.SelectionKeys))
	for _, key := range cfg.SelectionKeys {
		if strings.TrimSpace(key) != key || key == "" {
			return nil, errors.New("local billing selection key is invalid")
		}
		if _, exists := keys[key]; exists {
			return nil, errors.New("local billing selection key is duplicated")
		}
		plan, _, err := cfg.Catalog.ResolveSelection(key, time.Now())
		if err != nil || !hasCatalogFeature(plan.Features, cfg.PaidFeature) {
			return nil, errors.New("local billing selection lacks the configured paid feature")
		}
		if plan.Scope.InstallationID != local.InstallationID.String() ||
			plan.Scope.ApplicationID != local.ApplicationID.String() ||
			plan.Scope.EnvironmentID != local.EnvironmentID.String() ||
			plan.Scope.Provider != "stripe" || plan.Scope.ProviderAccountID != cfg.ProviderAccountID ||
			plan.Scope.AccountMode != cfg.AccountMode {
			return nil, errors.New("local billing catalog scope does not match application configuration")
		}
		keys[key] = struct{}{}
	}
	repository, err := checkout.NewSQLRepository(db, func(tx *sql.Tx) (checkout.TransactionStore, error) {
		return billingstore.New(tx)
	})
	if err != nil {
		return nil, err
	}
	action, err := checkout.New(checkout.Config{
		Repository: repository, Provider: cfg.Provider, ContinuationReader: cfg.ContinuationReader,
		Catalog: cfg.Catalog, CheckoutHosts: cfg.CheckoutHosts, ProviderAccountID: cfg.ProviderAccountID,
		AccountMode: cfg.AccountMode, SuccessURL: cfg.SuccessURL, CancelURL: cfg.CancelURL,
	})
	if err != nil {
		return nil, err
	}
	copiedConfig := *cfg
	copiedConfig.CheckoutHosts = append([]string(nil), cfg.CheckoutHosts...)
	copiedConfig.SelectionKeys = append([]string(nil), cfg.SelectionKeys...)
	service := &localBillingService{pool: pool, cfg: copiedConfig, action: action, selectionKeys: keys}
	ui, err := billingcheckout.New(billingcheckout.Config{
		Service: service, RequestCheck: workspaceRequestCheck{sessions}, CheckoutHosts: append([]string(nil), cfg.CheckoutHosts...),
	})
	if err != nil {
		return nil, err
	}
	return ui, nil
}

func validLocalActionPath(raw string) bool {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && !u.IsAbs() && u.Host == "" && !strings.Contains(u.Path, "\\\\")
}

func (s *localBillingService) Page(ctx context.Context, req *http.Request) (billingcheckout.PageView, error) {
	scope, err := s.scope(req)
	if err != nil {
		return billingcheckout.PageView{}, err
	}
	current, err := s.PaymentStatus(ctx, req, "")
	if err != nil {
		return billingcheckout.PageView{}, err
	}
	view := billingcheckout.PageView{Payer: "Current workspace", Current: current}
	now := time.Now()
	for _, key := range s.cfg.SelectionKeys {
		plan, price, err := s.cfg.Catalog.ResolveSelection(key, now)
		if err != nil {
			return billingcheckout.PageView{}, err
		}
		if plan.Scope.InstallationID != scope.InstallationID || plan.Scope.ApplicationID != scope.ApplicationID ||
			plan.Scope.EnvironmentID != scope.EnvironmentID || plan.Scope.Provider != scope.Provider ||
			plan.Scope.ProviderAccountID != scope.ProviderAccountID || plan.Scope.AccountMode != scope.AccountMode {
			return billingcheckout.PageView{}, errors.New("billing catalog scope does not match current application")
		}
		view.Choices = append(view.Choices, billingcheckout.Choice{
			PriceKey: key, Plan: plan.Key, Interval: string(price.Interval),
			Currency: plan.Currency, AmountMinor: price.AmountMinor,
		})
	}
	return view, nil
}

func (s *localBillingService) StartCheckout(ctx context.Context, req *http.Request, priceKey, idempotencyKey string) (billingcheckout.CheckoutResult, error) {
	if _, ok := s.selectionKeys[priceKey]; !ok || req == nil || s.action == nil {
		return billingcheckout.CheckoutResult{}, errors.New("billing selection is unavailable")
	}
	body, err := json.Marshal(struct {
		PriceKey string `json:"price_key"`
	}{PriceKey: priceKey})
	if err != nil {
		return billingcheckout.CheckoutResult{}, err
	}
	actionRequest := req.Clone(ctx)
	actionRequest.Method = http.MethodPost
	actionRequest.URL.Path = "/billing/checkout"
	actionRequest.URL.RawPath = ""
	actionRequest.URL.RawQuery = ""
	actionRequest.Body = io.NopCloser(bytes.NewReader(body))
	actionRequest.ContentLength = int64(len(body))
	actionRequest.Header = make(http.Header)
	actionRequest.Header.Set("Content-Type", "application/json")
	actionRequest.Header.Set("Idempotency-Key", idempotencyKey)
	response := httptest.NewRecorder()
	s.action.ServeHTTP(response, actionRequest)
	if response.Code < 200 || response.Code >= 300 {
		return billingcheckout.CheckoutResult{}, fmt.Errorf("billing action returned status %d", response.Code)
	}
	var result struct {
		State       string `json:"state"`
		IntentID    string `json:"intent_id"`
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil {
		return billingcheckout.CheckoutResult{}, errors.New("billing action returned an invalid response")
	}
	return billingcheckout.CheckoutResult{State: result.State, IntentID: result.IntentID, RedirectURL: result.RedirectURL}, nil
}

// PaymentStatus deliberately ignores checkout intent state. Only a fresh,
// current-workspace subscription projection can produce StatePaid.
func (s *localBillingService) PaymentStatus(ctx context.Context, req *http.Request, _ string) (billingcheckout.PaymentView, error) {
	scope, err := s.scope(req)
	if err != nil {
		return billingcheckout.PaymentView{}, err
	}
	projection, err := s.readProjection(ctx, scope)
	if err != nil {
		return billingcheckout.PaymentView{}, err
	}
	if projection == nil {
		return billingcheckout.PaymentView{State: billingcheckout.StateUnknown}, nil
	}
	decision := s.cfg.Catalog.Evaluate(scope, projection, s.cfg.PaidFeature, time.Now())
	if projection.Quantity <= 0 || decision.Outcome != catalog.OutcomeAllowed {
		return billingcheckout.PaymentView{State: billingcheckout.StateUnknown}, nil
	}
	return billingcheckout.PaymentView{
		State: billingcheckout.StatePaid, Payer: "Current workspace", Plan: decision.PlanKey,
		Currency: decision.Currency, AmountMinor: decision.AmountMinor, ActionPath: s.cfg.PostPaymentAction,
	}, nil
}

func (s *localBillingService) scope(req *http.Request) (catalog.Scope, error) {
	if req == nil {
		return catalog.Scope{}, errors.New("billing request is unavailable")
	}
	principal, ok := identity.PrincipalFromContext(req.Context())
	if !ok || principal.Actor().Kind() != "person" {
		return catalog.Scope{}, errors.New("billing identity is unavailable")
	}
	selection, ok := workspacectx.FromContext(req.Context())
	if !ok || selection.Workspace.ID == uuid.Nil || selection.Workspace.State != workspacestore.WorkspaceActive ||
		selection.Workspace.Scope.InstallationID != principal.InstallationID() ||
		selection.Workspace.Scope.ApplicationID != principal.ApplicationID() {
		return catalog.Scope{}, errors.New("billing workspace is unavailable")
	}
	workspace := selection.Workspace
	switch workspace.Kind {
	case workspacestore.KindPersonal:
		if workspace.PersonalOwnerID != principal.PersonID() {
			return catalog.Scope{}, errors.New("billing owner is unavailable")
		}
	case workspacestore.KindOrganization:
		membership := selection.Membership
		if membership == nil || membership.PersonID != principal.PersonID() ||
			membership.WorkspaceID != workspace.ID || membership.State != workspacestore.MembershipActive ||
			membership.Role != workspacestore.RoleOwner || !hasPermission(selection.Permissions, "billing.manage") {
			return catalog.Scope{}, errors.New("billing owner is unavailable")
		}
	default:
		return catalog.Scope{}, errors.New("billing workspace is unavailable")
	}
	return catalog.Scope{
		InstallationID: principal.InstallationID().String(), ApplicationID: principal.ApplicationID().String(),
		EnvironmentID: principal.EnvironmentID().String(), WorkspaceID: selection.Workspace.ID.String(),
		Provider: "stripe", ProviderAccountID: s.cfg.ProviderAccountID, AccountMode: s.cfg.AccountMode,
	}, nil
}

func (s *localBillingService) readProjection(ctx context.Context, scope catalog.Scope) (*billingstore.SubscriptionProjection, error) {
	var projection billingstore.SubscriptionProjection
	var start, end sql.NullTime
	err := s.pool.QueryRowContext(ctx, `
		SELECT p.id,p.billing_account_id,p.subscription_ref,p.state,p.price_key,p.quantity,
		       p.period_start,p.period_end,p.observed_at,p.version,p.updated_at
		FROM billing_subscription_projections p
		JOIN billing_workspace_accounts a ON a.id=p.billing_account_id
		WHERE p.installation_id=$1 AND p.application_id=$2 AND p.environment_id=$3
		  AND p.workspace_id=$4 AND p.provider=$5 AND p.provider_account_id=$6
		  AND p.account_mode=$7 AND a.state='active'
		ORDER BY p.observed_at DESC,p.updated_at DESC,p.id DESC
		LIMIT 1`,
		scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.WorkspaceID,
		scope.Provider, scope.ProviderAccountID, string(scope.AccountMode),
	).Scan(&projection.ID, &projection.AccountID, &projection.SubscriptionRef, &projection.State,
		&projection.PriceKey, &projection.Quantity, &start, &end, &projection.ObservedAt,
		&projection.Version, &projection.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("billing projection is unavailable")
	}
	if start.Valid {
		projection.PeriodStart = start.Time
	}
	if end.Valid {
		projection.PeriodEnd = end.Time
	}
	projection.Binding = provider.Binding{
		InstallationID: scope.InstallationID, EnvironmentID: scope.EnvironmentID,
		WorkspaceID: scope.WorkspaceID, Provider: scope.Provider, AccountID: scope.ProviderAccountID,
		AccountMode: scope.AccountMode,
	}
	return &projection, nil
}

func unavailableBillingHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Billing is unavailable.", http.StatusServiceUnavailable)
	})
}

func hasCatalogFeature(features []string, wanted string) bool {
	for _, feature := range features {
		if feature == wanted {
			return true
		}
	}
	return false
}

func hasPermission(permissions []string, wanted string) bool {
	for _, permission := range permissions {
		if permission == wanted {
			return true
		}
	}
	return false
}
