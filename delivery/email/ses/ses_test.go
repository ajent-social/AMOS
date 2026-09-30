package ses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/internal/receipt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/google/uuid"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func testConfig() Config {
	return Config{Region: "us-east-1", CredentialRef: "secretref://aws/ses-sender", FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", ConfigurationSet: "amos-email", RequestTimeout: 2 * time.Second}
}
func testMessage(t *testing.T) email.Message {
	t.Helper()
	r, err := email.NewRenderer(email.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: email.MaxBodyBytes})
	if err != nil {
		t.Fatal(err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	m, err := r.Render(id, email.Request{Template: email.TemplateSignIn, MaterialRef: "vault://flow/1", ExpiresInSeconds: 600}, email.PrivateMaterial{Recipient: "person@example.test", ActionURL: "https://app.example.test/auth?token=synthetic-token"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func testSender(t *testing.T, status int, response string, calls *int, captured *string) *Sender {
	t.Helper()
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		*calls++
		if !strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("AWS request was not signed")
		}
		if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/v2/email/outbound-emails") {
			t.Errorf("unexpected SES API request %s %s", req.Method, req.URL.Path)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read signed request: %v", err)
		}
		*captured = string(body)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response)), Request: req}, nil
	})
	awsCfg := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("SYNTHETICACCESS", "synthetic-secret", ""), HTTPClient: &http.Client{Transport: rt}, RetryMaxAttempts: 1}
	return newSender(awsCfg, testConfig(), nil)
}

func TestSendUsesSignedSESv2SingleRecipientAndReturnsAcceptedReceipt(t *testing.T) {
	calls := 0
	captured := ""
	s := testSender(t, 200, `{"MessageId":"synthetic-message-1"}`, &calls, &captured)
	message := testMessage(t)
	receipt, err := s.Send(context.Background(), message)
	if err != nil || receipt.State() != email.ReceiptAccepted || receipt.MessageID() != "synthetic-message-1" {
		t.Fatalf("receipt=%v err=%v", receipt.State(), err)
	}
	if calls != 1 {
		t.Fatalf("SES calls=%d want 1", calls)
	}
	if !strings.Contains(captured, "person@example.test") || !strings.Contains(captured, "synthetic-token") || !strings.Contains(captured, message.JobID().String()) {
		t.Fatal("SES request missing intended message fields")
	}
}

func TestMissingSESReceiptIsUnknownAndSDKDoesNotRetry5xx(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "empty receipt", status: 200, body: `{}`},
		{name: "server failure", status: 500, body: `{"message":"private provider response"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			captured := ""
			s := testSender(t, tc.status, tc.body, &calls, &captured)
			receipt, err := s.Send(context.Background(), testMessage(t))
			if !errors.Is(err, email.ErrOutcomeUnknown) || receipt.State() != email.ReceiptUnknown {
				t.Fatalf("receipt=%v err=%v", receipt.State(), err)
			}
			if calls != 1 {
				t.Fatalf("SDK retried unknown outcome: calls=%d", calls)
			}
			if strings.Contains(err.Error(), "private provider response") {
				t.Fatal("provider response text leaked")
			}
		})
	}
}

type resolverDouble struct{ provider aws.CredentialsProvider }

func (r resolverDouble) ResolveAWS(context.Context, string) (aws.CredentialsProvider, error) {
	return r.provider, nil
}

func TestNewRequiresExplicitCredentialReference(t *testing.T) {
	cfg := testConfig()
	if _, err := New(context.Background(), cfg, nil, nil); !errors.Is(err, email.ErrSetupRequired) {
		t.Fatalf("missing resolver error=%v", err)
	}
	resolver := resolverDouble{provider: credentials.NewStaticCredentialsProvider("SYNTHETICACCESS", "synthetic-secret", "")}
	cfg.CredentialRef = ""
	if _, err := New(context.Background(), cfg, resolver, nil); !errors.Is(err, email.ErrSetupRequired) {
		t.Fatalf("missing reference error=%v", err)
	}
	for _, invalid := range []string{"", " bad", "bad\nvalue"} {
		cfg = testConfig()
		cfg.ConfigurationSet = invalid
		if _, err := New(context.Background(), cfg, resolver, nil); !errors.Is(err, email.ErrSetupRequired) {
			t.Errorf("invalid configuration set was accepted: %v", err)
		}
	}
}

type eventSourceDouble struct {
	event email.VerifiedEvent
	found bool
	err   error
}

func (e eventSourceDouble) LookupVerified(context.Context, uuid.UUID) (email.VerifiedEvent, bool, error) {
	return e.event, e.found, e.err
}

func TestReconcileRequiresVerifiedMatchingProviderEvent(t *testing.T) {
	jobID, _ := uuid.NewV7()
	awsCfg := aws.Config{Region: "us-east-1"}
	unknown := newSender(awsCfg, testConfig(), eventSourceDouble{})
	if r, err := unknown.Reconcile(context.Background(), jobID); !errors.Is(err, email.ErrOutcomeUnknown) || r.State() != email.ReceiptUnknown {
		t.Fatalf("missing provider event result=%v err=%v", r.State(), err)
	}
	unverified := receipt.VerifiedProviderEvent(jobID, receipt.AcceptedResult("synthetic-id"), false)
	noTrust := newSender(awsCfg, testConfig(), eventSourceDouble{event: unverified, found: true})
	if r, err := noTrust.Reconcile(context.Background(), jobID); !errors.Is(err, email.ErrOutcomeUnknown) || r.State() != email.ReceiptUnknown {
		t.Fatalf("unverified event result=%v err=%v", r.State(), err)
	}
	verified := receipt.VerifiedProviderEvent(jobID, receipt.AcceptedResult("synthetic-id"), true)
	trusted := newSender(awsCfg, testConfig(), eventSourceDouble{event: verified, found: true})
	if r, err := trusted.Reconcile(context.Background(), jobID); err != nil || r.State() != email.ReceiptAccepted || r.MessageID() != "synthetic-id" {
		t.Fatalf("verified event result=%v err=%v", r.State(), err)
	}
	wrongJob := receipt.VerifiedProviderEvent(uuid.New(), receipt.AcceptedResult("synthetic-id"), true)
	wrong := newSender(awsCfg, testConfig(), eventSourceDouble{event: wrongJob, found: true})
	if r, err := wrong.Reconcile(context.Background(), jobID); !errors.Is(err, email.ErrOutcomeUnknown) || r.State() != email.ReceiptUnknown {
		t.Fatalf("mismatched event result=%v err=%v", r.State(), err)
	}
}
