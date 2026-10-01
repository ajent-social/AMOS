package webhook

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	"github.com/google/uuid"
	stripe "github.com/stripe/stripe-go/v87"
	stripewebhook "github.com/stripe/stripe-go/v87/webhook"
)

const (
	currentSecret  = "whsec_test_current_secret"
	previousSecret = "whsec_test_previous_secret"
)

var (
	endpointInstallation = uuid.MustParse("018f0000-0000-7000-8000-000000000001")
	endpointApplication  = uuid.MustParse("018f0000-0000-7000-8000-000000000002")
	endpointEnvironment  = uuid.MustParse("018f0000-0000-7000-8000-000000000003")
	endpointWorkspace    = uuid.MustParse("018f0000-0000-7000-8000-000000000004")
)

type memoryRouter struct {
	route          ResolvedRoute
	err            error
	calls          int
	claim          EventClaims
	mutateMetadata bool
}

func (m *memoryRouter) Resolve(_ context.Context, _ EndpointScope, claims EventClaims) (ResolvedRoute, error) {
	m.calls++
	m.claim = claims
	if m.mutateMetadata {
		claims.Metadata["amos_workspace_id"] = endpointWorkspace.String()
	}
	return m.route, m.err
}

type memoryInbox struct {
	input   VerifiedWebhookInput
	receipt VerifiedWebhookReceipt
	err     error
	calls   int
}

func (m *memoryInbox) PersistVerifiedWebhook(_ context.Context, input VerifiedWebhookInput) (VerifiedWebhookReceipt, error) {
	m.calls++
	m.input = input
	return m.receipt, m.err
}

func testScope() EndpointScope {
	return EndpointScope{InstallationID: endpointInstallation, ApplicationID: endpointApplication, EnvironmentID: endpointEnvironment,
		Provider: "stripe", ProviderAccountID: "acct_test", AccountMode: provider.AccountTest}
}

func testBinding() provider.Binding {
	return provider.Binding{InstallationID: endpointInstallation.String(), EnvironmentID: endpointEnvironment.String(), WorkspaceID: endpointWorkspace.String(),
		Provider: "stripe", AccountID: "acct_test", AccountMode: provider.AccountTest}
}

func testRouter() *memoryRouter {
	return &memoryRouter{route: ResolvedRoute{ApplicationID: endpointApplication, Binding: testBinding()}}
}

func testHandler(t *testing.T, router *memoryRouter, inbox *memoryInbox, secrets ...SigningSecret) *Handler {
	t.Helper()
	if len(secrets) == 0 {
		secrets = []SigningSecret{{Value: currentSecret}}
	}
	h, err := New(Config{Scope: testScope(), Secrets: secrets, Router: router, Inbox: inbox})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func eventBody(t *testing.T) []byte {
	t.Helper()
	payload := map[string]any{
		"id": "evt_test_0001", "object": "event", "api_version": stripe.APIVersion,
		"type": "customer.subscription.updated", "livemode": false, "created": time.Now().Unix(),
		"data": map[string]any{"object": map[string]any{
			"id": "sub_test_123", "object": "subscription", "customer": "cus_test_123",
			"metadata": map[string]string{
				"amos_installation_id": endpointInstallation.String(), "amos_environment_id": endpointEnvironment.String(),
				"amos_provider_account_id": "acct_test", "amos_account_mode": string(provider.AccountTest), "amos_workspace_id": endpointWorkspace.String(),
			},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func serveSigned(t *testing.T, h http.Handler, body []byte, secret string, timestamp time.Time) *httptest.ResponseRecorder {
	t.Helper()
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{Payload: body, Secret: secret, Timestamp: timestamp})
	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(body)))
	r.Header.Set("Stripe-Signature", signed.Header)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestT5_7_ValidSignaturePersistsBeforeAcknowledgementAndSupportsRotation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secret string
	}{
		{name: "current secret", secret: currentSecret},
		{name: "previous secret during rotation", secret: previousSecret},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, inbox := testRouter(), &memoryInbox{receipt: VerifiedWebhookReceipt{State: "received"}}
			h := testHandler(t, router, inbox, SigningSecret{Value: currentSecret}, SigningSecret{Value: previousSecret, RetireAt: time.Now().Add(time.Hour)})
			body := eventBody(t)
			w := serveSigned(t, h, body, tc.secret, time.Now())
			if w.Code != http.StatusNoContent || inbox.calls != 1 || router.calls != 1 {
				t.Fatalf("valid receipt response=%d inbox=%d router=%d body=%s", w.Code, inbox.calls, router.calls, w.Body.String())
			}
			if inbox.input.ID == uuid.Nil || inbox.input.EventID != "evt_test_0001" || inbox.input.EventType != "customer.subscription.updated" || inbox.input.Binding != testBinding() || inbox.input.CustomerRef != "cus_test_123" || inbox.input.SubscriptionRef != "sub_test_123" || inbox.input.PayloadSHA256 != sha256.Sum256(body) {
				t.Fatalf("persisted event did not carry verified normalized scope: %+v", inbox.input)
			}
			if inbox.input.QuarantineReason != QuarantineNone || inbox.input.Endpoint != testScope() {
				t.Fatalf("unexpected receipt route: %+v", inbox.input)
			}
		})
	}
}

func TestT5_7_TamperedSignatureAndStaleTimestampNeverPersist(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tamper    bool
		timestamp time.Time
	}{
		{name: "tampered body", tamper: true, timestamp: time.Now()},
		{name: "stale signature", timestamp: time.Now().Add(-time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, inbox := testRouter(), &memoryInbox{}
			h := testHandler(t, router, inbox)
			body := eventBody(t)
			if tc.tamper {
				original := append([]byte(nil), body...)
				body = append(body, ' ')
				signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{Payload: original, Secret: currentSecret, Timestamp: tc.timestamp})
				r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(body)))
				r.Header.Set("Stripe-Signature", signed.Header)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code < 400 || inbox.calls != 0 || router.calls != 0 {
					t.Fatalf("tampered webhook crossed signature boundary: status=%d inbox=%d router=%d", w.Code, inbox.calls, router.calls)
				}
				return
			}
			w := serveSigned(t, h, body, currentSecret, tc.timestamp)
			if w.Code < 400 || inbox.calls != 0 || router.calls != 0 {
				t.Fatalf("stale webhook crossed signature boundary: status=%d inbox=%d router=%d", w.Code, inbox.calls, router.calls)
			}
		})
	}
}

func TestT5_7_BodyLimitAndDuplicateSignatureHeadersReject(t *testing.T) {
	router, inbox := testRouter(), &memoryInbox{}
	h := testHandler(t, router, inbox)
	body := eventBody(t)
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{Payload: body, Secret: currentSecret, Timestamp: time.Now()})
	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(strings.Repeat("x", DefaultBodyLimit+1)))
	r.Header.Set("Stripe-Signature", signed.Header)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || inbox.calls != 0 {
		t.Fatalf("oversized body response=%d inbox=%d", w.Code, inbox.calls)
	}
	r = httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(body)))
	r.Header.Add("Stripe-Signature", signed.Header)
	r.Header.Add("Stripe-Signature", signed.Header)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code < 400 || inbox.calls != 0 {
		t.Fatalf("duplicate signature headers accepted: status=%d inbox=%d", w.Code, inbox.calls)
	}
}

func TestT5_7_DurableConflictQuarantineAndPersistenceFailureResponses(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*memoryRouter)
		inbox      VerifiedWebhookReceipt
		inboxErr   error
		wantStatus int
		wantRouter int
	}{
		{name: "same digest duplicate", inbox: VerifiedWebhookReceipt{Duplicate: true, State: "received"}, wantStatus: http.StatusNoContent, wantRouter: 1},
		{name: "conflicting digest", inboxErr: ErrInboxConflict, wantStatus: http.StatusConflict, wantRouter: 1},
		{name: "unknown customer quarantined", mutate: func(r *memoryRouter) { r.route = ResolvedRoute{QuarantineReason: QuarantineUnknownCustomer} }, inbox: VerifiedWebhookReceipt{State: "quarantined", QuarantineReason: QuarantineUnknownCustomer}, wantStatus: http.StatusAccepted, wantRouter: 1},
		{name: "database commit failure", inboxErr: errors.New("database unavailable"), wantStatus: http.StatusServiceUnavailable, wantRouter: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := testRouter()
			if tc.mutate != nil {
				tc.mutate(router)
			}
			inbox := &memoryInbox{receipt: tc.inbox, err: tc.inboxErr}
			h := testHandler(t, router, inbox)
			w := serveSigned(t, h, eventBody(t), currentSecret, time.Now())
			if w.Code != tc.wantStatus || inbox.calls != 1 || router.calls != tc.wantRouter {
				t.Fatalf("response=%d inbox=%d router=%d, want %d/1/%d, body=%s", w.Code, inbox.calls, router.calls, tc.wantStatus, tc.wantRouter, w.Body.String())
			}
			if tc.name == "database commit failure" && w.Code >= 200 && w.Code < 300 {
				t.Fatal("database failure was acknowledged")
			}
			if tc.name == "unknown customer quarantined" && inbox.input.Binding != (provider.Binding{}) {
				t.Fatal("quarantined event was assigned to a workspace")
			}
		})
	}
}

func TestT5_7_WrongAccountModeAndMetadataQuarantineWithoutRoutingAuthority(t *testing.T) {
	for _, tc := range []struct {
		name         string
		body         []byte
		eventAccount string
		mode         provider.AccountMode
		want         QuarantineReason
	}{
		{name: "wrong event account", body: eventBody(t), eventAccount: "acct_other", mode: provider.AccountTest, want: QuarantineAccountMismatch},
		{name: "wrong live mode", body: eventBody(t), mode: provider.AccountLive, want: QuarantineModeMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, inbox := testRouter(), &memoryInbox{receipt: VerifiedWebhookReceipt{State: "quarantined", QuarantineReason: tc.want}}
			cfg := Config{Scope: testScope(), EventAccountID: tc.eventAccount, Secrets: []SigningSecret{{Value: currentSecret}}, Router: router, Inbox: inbox}
			cfg.Scope.AccountMode = tc.mode
			h, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			w := serveSigned(t, h, tc.body, currentSecret, time.Now())
			if w.Code != http.StatusAccepted || router.calls != 0 || inbox.input.QuarantineReason != tc.want {
				t.Fatalf("wrong endpoint event handling: status=%d router=%d route=%s body=%s", w.Code, router.calls, inbox.input.QuarantineReason, w.Body.String())
			}
		})
	}
	claims := EventClaims{ID: "evt_test_0001", Type: "customer.subscription.updated", LiveMode: false, CustomerRef: "cus_test_123",
		Metadata: map[string]string{"amos_workspace_id": endpointWorkspace.String()}}
	if metadataMatchesBinding(claims.Metadata, testBinding()) {
		t.Fatal("incomplete metadata matched a full persisted binding")
	}
}

func TestT5_7_MetadataMismatchAndMissingCustomerAreDurablyQuarantined(t *testing.T) {
	good := eventBody(t)
	missingCustomer := []byte(strings.Replace(string(good), `"customer":"cus_test_123",`, "", 1))
	wrongWorkspace := []byte(strings.Replace(string(good), endpointWorkspace.String(), "018f0000-0000-7000-8000-000000000005", 1))
	for _, tc := range []struct {
		name            string
		body            []byte
		quarantine      string
		wantRouterCalls int
	}{
		{name: "missing customer reference", body: missingCustomer, quarantine: QuarantineMissingCustomer, wantRouterCalls: 0},
		{name: "signed metadata disagrees with persisted binding", body: wrongWorkspace, quarantine: QuarantineMetadataMismatch, wantRouterCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, inbox := testRouter(), &memoryInbox{receipt: VerifiedWebhookReceipt{State: "quarantined", QuarantineReason: tc.quarantine}}
			h := testHandler(t, router, inbox)
			w := serveSigned(t, h, tc.body, currentSecret, time.Now())
			if w.Code != http.StatusAccepted || inbox.calls != 1 || router.calls != tc.wantRouterCalls || inbox.input.QuarantineReason != tc.quarantine || inbox.input.Binding != (provider.Binding{}) {
				t.Fatalf("quarantine response=%d router=%d inbox=%d reason=%q binding=%+v", w.Code, router.calls, inbox.calls, inbox.input.QuarantineReason, inbox.input.Binding)
			}
		})
	}
	mutatingRouter, inbox := testRouter(), &memoryInbox{receipt: VerifiedWebhookReceipt{State: "quarantined", QuarantineReason: QuarantineMetadataMismatch}}
	mutatingRouter.mutateMetadata = true
	w := serveSigned(t, testHandler(t, mutatingRouter, inbox), wrongWorkspace, currentSecret, time.Now())
	if w.Code != http.StatusAccepted || inbox.input.QuarantineReason != QuarantineMetadataMismatch {
		t.Fatalf("router mutation changed verified metadata claim: status=%d reason=%s", w.Code, inbox.input.QuarantineReason)
	}
}

func TestT5_7_ConfigRejectsUnboundedOrAmbiguousSecretRotation(t *testing.T) {
	for _, secrets := range [][]SigningSecret{
		{}, {{Value: currentSecret}, {Value: currentSecret}}, {{Value: "short"}},
		{{Value: currentSecret}, {Value: previousSecret}},
		{{Value: currentSecret}, {Value: previousSecret, RetireAt: time.Now().Add(25 * time.Hour)}},
		{{Value: currentSecret}, {Value: previousSecret, RetireAt: time.Now().Add(-time.Second)}},
		{{Value: currentSecret}, {Value: previousSecret, RetireAt: time.Now().Add(time.Hour)}, {Value: "whsec_third_secret_value"}},
	} {
		if _, err := New(Config{Scope: testScope(), Secrets: secrets, Router: testRouter(), Inbox: &memoryInbox{}}); err == nil {
			t.Fatalf("invalid secret rotation accepted: count=%d", len(secrets))
		}
	}
}

func TestT5_7_PreviousSecretStopsVerifyingAtConfiguredRetirement(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	router, inbox := testRouter(), &memoryInbox{receipt: VerifiedWebhookReceipt{State: "received"}}
	h, err := New(Config{Scope: testScope(), Secrets: []SigningSecret{{Value: currentSecret}, {Value: previousSecret, RetireAt: now.Add(time.Hour)}},
		Router: router, Inbox: inbox, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	body := eventBody(t)
	w := serveSigned(t, h, body, previousSecret, time.Now())
	if w.Code != http.StatusNoContent {
		t.Fatalf("previous secret rejected inside rotation window: %d %s", w.Code, w.Body.String())
	}
	now = now.Add(time.Hour)
	w = serveSigned(t, h, body, previousSecret, time.Now())
	if w.Code < 400 || inbox.calls != 1 {
		t.Fatalf("retired signing secret remained valid: status=%d inbox_calls=%d", w.Code, inbox.calls)
	}
}
