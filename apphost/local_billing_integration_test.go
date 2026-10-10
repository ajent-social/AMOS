package apphost

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/checkout"
	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

type localBillingProviderStub struct {
	mu           sync.Mutex
	createCalls  int
	recoverCalls int
	keys         []string
}

func (s *localBillingProviderStub) CreateCheckout(_ context.Context, req provider.CheckoutRequest) (provider.Outcome[provider.CheckoutSession], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	s.keys = append(s.keys, req.IdempotencyKey)
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: time.Now().UTC()}, nil
}
func (s *localBillingProviderStub) RecoverCheckout(_ context.Context, req provider.CheckoutRequest, _ billingstore.Intent) (provider.Outcome[provider.CheckoutSession], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoverCalls++
	s.keys = append(s.keys, req.IdempotencyKey)
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: time.Now().UTC()}, nil
}
func (*localBillingProviderStub) ReadConfirmedCheckout(context.Context, provider.CheckoutRequest, billingstore.Intent) (provider.Outcome[provider.CheckoutSession], error) {
	return provider.Outcome[provider.CheckoutSession]{State: provider.SubscriptionUnknown, ObservedAt: time.Now().UTC()}, nil
}
func (s *localBillingProviderStub) calls() (int, int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createCalls, s.recoverCalls, append([]string(nil), s.keys...)
}

var _ checkout.Provider = (*localBillingProviderStub)(nil)
var _ checkout.ConfirmedCheckoutReader = (*localBillingProviderStub)(nil)

func TestLocalBillingUsesRealSessionAndScopedApplicationCatalog(t *testing.T) {
	dsn := localDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	cfg := localConfig(t, dsn, origin)
	providerScope := catalog.Scope{
		InstallationID: cfg.InstallationID.String(), ApplicationID: cfg.ApplicationID.String(),
		EnvironmentID: cfg.EnvironmentID.String(), Provider: "stripe",
		ProviderAccountID: "acct_test", AccountMode: provider.AccountTest,
	}
	catalogConfig, err := catalog.New(catalog.Config{
		Revision: "catalog-7",
		Plans: []catalog.Plan{{
			Key: "pro-v1", Revision: "2026-01", ProductFamily: "application", Model: catalog.ModelFlat,
			Scope: providerScope, Currency: "USD", MinorUnit: 2, EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Features: []string{"projects"},
			Prices:   []catalog.Price{{Key: "pro-month", ProviderPriceKey: "price_month", AmountMinor: 1200, Interval: catalog.IntervalMonth}, {Key: "pro-year", ProviderPriceKey: "price_year", AmountMinor: 12000, Interval: catalog.IntervalYear}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stub := &localBillingProviderStub{}
	cfg.Billing = &LocalBillingConfig{
		Catalog: catalogConfig, Provider: stub, ContinuationReader: stub,
		ProviderAccountID: "acct_test", AccountMode: provider.AccountTest,
		CheckoutHosts: []string{"checkout.example.test"},
		SuccessURL:    "https://checkout.example.test/success", CancelURL: "https://checkout.example.test/cancel",
		SelectionKeys: []string{"pro-month"}, PaidFeature: "projects", PostPaymentAction: "/projects",
	}
	ctx, cancel := context.WithCancel(context.Background())
	host, err := NewLocal(ctx, cfg)
	if err != nil {
		cancel()
		_ = listener.Close()
		t.Fatalf("compose local billing app: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case serveErr := <-done:
			if serveErr != nil {
				t.Error(serveErr)
			}
		case <-time.After(5 * time.Second):
			_ = listener.Close()
			t.Error("local billing app failed to stop")
		}
		if closeErr := host.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	anonymous := &http.Client{Timeout: 5 * time.Second}
	anonResponse, err := anonymous.Get(origin + "/billing")
	if err != nil {
		t.Fatal("anonymous billing request failed")
	}
	_ = anonResponse.Body.Close()
	if anonResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous billing status=%d want=%d", anonResponse.StatusCode, http.StatusUnauthorized)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	email := "billing-owner@example.test"
	generatedTestPassword := uuid.NewString() + "Workspace!"
	status, _ := response(t, client, http.MethodPost, origin+"/signup", origin, url.Values{"email": {email}, "password": {generatedTestPassword}})
	if status != http.StatusAccepted {
		t.Fatalf("signup status=%d", status)
	}
	verifyURL := localVerificationURL(t, cfg.MailDirectory)
	verify, err := url.Parse(verifyURL)
	if err != nil {
		t.Fatal(err)
	}
	status, _ = response(t, client, http.MethodGet, verifyURL, "", nil)
	if status != http.StatusOK {
		t.Fatalf("verification preview status=%d", status)
	}
	status, _ = response(t, client, http.MethodPost, origin+"/verify-email", origin, url.Values{
		"challenge": {verify.Query().Get("challenge")}, "token": {verify.Query().Get("token")},
	})
	if status != http.StatusOK {
		t.Fatalf("verification status=%d", status)
	}
	status, _ = response(t, client, http.MethodPost, origin+"/auth", origin, url.Values{"email": {email}, "password": {generatedTestPassword}})
	if status != http.StatusSeeOther {
		t.Fatalf("signin status=%d", status)
	}
	var personID, personalWorkspaceID uuid.UUID
	if err := host.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT p.id FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id WHERE p.installation_id=$1 AND p.application_id=$2 AND e.comparison_key=$3 AND p.state='active'`, cfg.InstallationID, cfg.ApplicationID, strings.ToLower(email)).Scan(&personID); err != nil {
			return err
		}
		store, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		workspace, err := store.FindPersonalWorkspace(ctx, workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, personID)
		if err == nil {
			personalWorkspaceID = workspace.ID
		}
		return err
	}); err != nil {
		t.Fatalf("resolve verified user's personal workspace: %v", err)
	}
	originURL, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(originURL, []*http.Cookie{{Name: "amos_workspace_hint", Value: personalWorkspaceID.String(), Path: "/", HttpOnly: true}})
	status, body := response(t, client, http.MethodGet, origin+"/billing", "", nil)
	if status != http.StatusOK || !strings.Contains(body, "pro-v1") || !strings.Contains(body, "1200") {
		t.Fatalf("authenticated billing page status=%d contains configured choice=%v body=%q", status, status == http.StatusOK && strings.Contains(body, "pro-v1"), body)
	}
	if strings.Contains(body, `href="/projects"`) {
		t.Fatal("missing subscription projection exposed the paid business action")
	}
	if err := seedLocalBillingAccount(t, ctx, host, cfg, personalWorkspaceID); err != nil {
		t.Fatalf("seed active workspace billing account: %v", err)
	}

	form := url.Values{
		"_csrf":           {localBillingFormValue(t, body, "_csrf")},
		"price_key":       {localBillingFormValue(t, body, "price_key")},
		"idempotency_key": {localBillingFormValue(t, body, "idempotency_key")},
	}
	if form.Get("price_key") != "pro-month" {
		t.Fatalf("checkout form selected unexpected server-owned price key %q", form.Get("price_key"))
	}
	status, retryBody, _ := localBillingCheckoutPost(t, client, origin, form)
	if status != http.StatusServiceUnavailable || !strings.Contains(retryBody, "Retry checkout safely") {
		t.Fatalf("AAL1 checkout attempt did not fail closed with a retry surface: status=%d body=%q", status, retryBody)
	}
	createCalls, recoverCalls, keys := stub.calls()
	if createCalls != 0 || recoverCalls != 0 || len(keys) != 0 {
		t.Fatalf("AAL1 checkout reached provider: create=%d recover=%d keys=%v", createCalls, recoverCalls, keys)
	}
	if err := elevateLocalBillingSession(t, ctx, host, personID); err != nil {
		t.Fatalf("prepare test session with completed AAL2 step-up: %v", err)
	}
	status, retryBody, intentCookie := localBillingCheckoutPost(t, client, origin, form)
	if status != http.StatusAccepted || !strings.Contains(retryBody, `data-state="unknown"`) || !strings.Contains(retryBody, "Check payment status again") {
		t.Fatalf("unknown checkout was not presented as pending recovery: status=%d body=%q", status, retryBody)
	}
	if intentCookie == nil || !validLocalBillingIntentID(intentCookie.Value) || !intentCookie.Secure {
		t.Fatalf("checkout response did not set a secure intent cookie: %+v", intentCookie)
	}
	status, retryBody, retryIntentCookie := localBillingCheckoutPost(t, client, origin, form)
	if status != http.StatusAccepted || !strings.Contains(retryBody, `data-state="unknown"`) {
		t.Fatalf("same-key provider recovery did not remain unknown: status=%d body=%q", status, retryBody)
	}
	createCalls, recoverCalls, keys = stub.calls()
	if createCalls != 1 || recoverCalls != 1 || len(keys) != 2 || keys[0] != form.Get("idempotency_key") || keys[1] != keys[0] {
		t.Fatalf("same-key retry calls create=%d recover=%d keys=%v", createCalls, recoverCalls, keys)
	}
	if retryIntentCookie == nil || retryIntentCookie.Value != intentCookie.Value {
		t.Fatalf("same-key retry changed checkout intent cookie: first=%+v retry=%+v", intentCookie, retryIntentCookie)
	}
	intentID := intentCookie.Value
	status, statusBody := localBillingStatus(t, client, origin, intentID)
	if status != http.StatusOK || !strings.Contains(statusBody, `"state":"unknown"`) {
		t.Fatalf("unconfirmed checkout status=%d body=%q", status, statusBody)
	}

	if err := seedLocalBillingProjection(t, ctx, host, cfg, personalWorkspaceID, "pending", time.Now().UTC().Add(-2*time.Second)); err != nil {
		t.Fatalf("seed pending projection: %v", err)
	}
	status, body = response(t, client, http.MethodGet, origin+"/billing", "", nil)
	if status != http.StatusOK || strings.Contains(body, `href="/projects"`) {
		t.Fatalf("pending projection was presented as paid: status=%d", status)
	}
	status, statusBody = localBillingStatus(t, client, origin, intentID)
	if status != http.StatusOK || !strings.Contains(statusBody, `"state":"unknown"`) {
		t.Fatalf("pending projection changed checkout status=%d body=%q", status, statusBody)
	}
	if err := seedLocalBillingProjection(t, ctx, host, cfg, personalWorkspaceID, "confirmed", time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatalf("seed confirmed projection: %v", err)
	}
	status, body = response(t, client, http.MethodGet, origin+"/billing", "", nil)
	if status != http.StatusOK || !strings.Contains(body, `href="/projects"`) {
		t.Fatalf("confirmed scoped projection did not unlock the configured action: status=%d", status)
	}
	status, statusBody = localBillingStatus(t, client, origin, intentID)
	if status != http.StatusOK || !strings.Contains(statusBody, `"state":"paid"`) || !strings.Contains(statusBody, `"action_path":"/projects"`) {
		t.Fatalf("confirmed projection did not recover checkout status=%d body=%q", status, statusBody)
	}
}

func elevateLocalBillingSession(t *testing.T, ctx context.Context, host *Host, personID uuid.UUID) error {
	t.Helper()
	return host.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE identity_sessions SET assurance_level='aal2', assurance_expires_at=transaction_timestamp()+interval '10 minutes' WHERE person_id=$1 AND revoked_at IS NULL`, personID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("updated %d active sessions, want one", count)
		}
		return nil
	})
}

func localBillingFormValue(t *testing.T, body, name string) string {
	t.Helper()
	marker := `name="` + name + `" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("form field %q not found", name)
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("form field %q has no closing value quote", name)
	}
	return body[start : start+end]
}

func validLocalBillingIntentID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

func localBillingCheckoutPost(t *testing.T, client *http.Client, origin string, form url.Values) (int, string, *http.Cookie) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, origin+"/billing/start-checkout", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", origin)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var intentCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "amos_billing_checkout_intent" {
			clone := *cookie
			intentCookie = &clone
			break
		}
	}
	return resp.StatusCode, string(body), intentCookie
}

func localBillingStatus(t *testing.T, client *http.Client, origin, intentID string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, origin+"/billing/return/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "amos_billing_checkout_intent", Value: intentID})
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func seedLocalBillingAccount(t *testing.T, ctx context.Context, host *Host, cfg LocalConfig, workspaceID uuid.UUID) error {
	t.Helper()
	binding := provider.Binding{
		InstallationID: cfg.InstallationID.String(), EnvironmentID: cfg.EnvironmentID.String(),
		WorkspaceID: workspaceID.String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest,
	}
	return host.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		store, err := billingstore.New(tx)
		if err != nil {
			return err
		}
		account, err := store.EnsureWorkspaceAccount(ctx, mustUUID(t), binding)
		if err != nil {
			return err
		}
		_, err = store.BindCustomer(ctx, mustUUID(t), account.ID, binding, "cus_test_local_billing")
		return err
	})
}

func seedLocalBillingProjection(t *testing.T, ctx context.Context, host *Host, cfg LocalConfig, workspaceID uuid.UUID, state string, observed time.Time) error {
	t.Helper()
	binding := provider.Binding{
		InstallationID: cfg.InstallationID.String(), EnvironmentID: cfg.EnvironmentID.String(),
		WorkspaceID: workspaceID.String(), Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest,
	}
	return host.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		store, err := billingstore.New(tx)
		if err != nil {
			return err
		}
		if _, err := store.EnsureWorkspaceAccount(ctx, mustUUID(t), binding); err != nil {
			return err
		}
		_, err = store.UpsertSubscriptionProjection(ctx, billingstore.ProjectionInput{
			ID: mustUUID(t), Binding: binding, SubscriptionRef: "sub_test_1", State: state,
			PriceKey: "price_month", Quantity: 1, PeriodStart: observed.Add(-time.Hour),
			PeriodEnd: observed.Add(time.Hour), ObservedAt: observed,
		})
		return err
	})
}
