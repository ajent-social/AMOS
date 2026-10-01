package email

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/delivery/email/internal/receipt"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
)

func testRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := NewRenderer(RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: MaxBodyBytes})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func validRequest() Request {
	return Request{Template: TemplateSignIn, MaterialRef: "material:018f22e7-8e71-7b4c-9a4c-6d7b8f15a3c2", ExpiresInSeconds: 600}
}

func validMaterial() PrivateMaterial {
	return PrivateMaterial{Recipient: "person@example.test", ActionURL: "https://app.example.test/verify-email?token=synthetic-token"}
}

func TestRendererRejectsRecipientTemplateAndOriginInjection(t *testing.T) {
	r := testRenderer(t)
	id, _ := uuid.NewV7()
	for _, req := range []Request{
		func() Request { v := validRequest(); v.Template = "unregistered"; return v }(),
		func() Request { v := validRequest(); v.MaterialRef = ""; return v }(),
		func() Request { v := validRequest(); v.ExpiresInSeconds = MaxExpirySeconds + 1; return v }(),
	} {
		if _, err := r.Render(id, req, validMaterial()); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("accepted invalid request %#v: %v", req, err)
		}
	}
	for _, m := range []PrivateMaterial{{Recipient: "first@example.test,second@example.test", ActionURL: validMaterial().ActionURL}, {Recipient: "Person <person@example.test>", ActionURL: validMaterial().ActionURL}, {Recipient: "person@example.test", ActionURL: "https://outside.example.test/reset"}} {
		if _, err := r.Render(id, validRequest(), m); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("accepted invalid private material: %v", err)
		}
	}
}

func TestRendererUsesEscapedBoundedTemplate(t *testing.T) {
	r := testRenderer(t)
	id, _ := uuid.NewV7()
	m := validMaterial()
	m.ActionURL = "https://app.example.test/auth/action?token=%22%3E%3Cscript%3E"
	msg, err := r.Render(id, validRequest(), m)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject() == "" || strings.Contains(msg.HTMLBody(), "<script>") {
		t.Fatalf("HTML template did not escape URL: %q", msg.HTMLBody())
	}
	if msg.JobID() != id || msg.Recipient() != m.Recipient {
		t.Fatalf("message lost job/recipient binding")
	}
}

type materialDouble struct {
	value PrivateMaterial
	err   error
	calls int
}

func (m *materialDouble) ResolveDelivery(context.Context, SecretReference) (PrivateMaterial, error) {
	m.calls++
	return m.value, m.err
}

type senderDouble struct {
	sent         Message
	send         Receipt
	sendErr      error
	reconcile    Receipt
	reconcileErr error
	calls        int
}

func (s *senderDouble) Send(_ context.Context, m Message) (Receipt, error) {
	s.calls++
	s.sent = m
	return s.send, s.sendErr
}
func (s *senderDouble) Reconcile(context.Context, uuid.UUID) (Receipt, error) {
	return s.reconcile, s.reconcileErr
}

func TestDispatcherResolvesSecretOnlyInsideSendAndPreservesUnknown(t *testing.T) {
	r := testRenderer(t)
	secretMaterial := validMaterial()
	secretMaterial.ActionURL = "https://app.example.test/auth/verify?token=synthetic-one-time-bearer"
	materials := &materialDouble{value: secretMaterial}
	sender := &senderDouble{send: receipt.AcceptedResult("ses-message-1")}
	d, err := NewDispatcher(r, sender, materials, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := uuid.NewV7()
	payload, err := json.Marshal(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.Job{ID: jobID, Kind: Kind, ExternalEffect: true, Payload: payload}
	if got := d.Execute(context.Background(), job, jobID); got.Kind != jobs.ResolutionSucceeded {
		t.Fatalf("accepted receipt resolution=%s", got.Kind)
	}
	if materials.calls != 1 || sender.calls != 1 || sender.sent.HTMLBody() == "" || !strings.Contains(sender.sent.HTMLBody(), "synthetic-one-time-bearer") {
		t.Fatal("resolved secret material did not reach provider boundary")
	}
	if got := d.Reconcile(context.Background(), job, jobID); got.Kind != jobs.ResolutionUnknown {
		t.Fatalf("missing verified event resolved unknown outcome as %s", got.Kind)
	}
}

func TestMissingSecretMaterialResolverFailsSetupAndFailureDoesNotSend(t *testing.T) {
	r := testRenderer(t)
	sender := &senderDouble{}
	if _, err := NewDispatcher(r, sender, nil, time.Second); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("missing resolver error=%v", err)
	}
	materials := &materialDouble{err: errors.New("synthetic secret store detail")}
	d, err := NewDispatcher(r, sender, materials, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := uuid.NewV7()
	payload, _ := json.Marshal(validRequest())
	job := jobs.Job{ID: id, Kind: Kind, ExternalEffect: true, Payload: payload}
	got := d.Execute(context.Background(), job, id)
	if got.Kind != jobs.ResolutionRetrySafe || sender.calls != 0 {
		t.Fatalf("resolution=%s sends=%d", got.Kind, sender.calls)
	}
}

func TestMaterialReferenceRequiresOpaqueCanonicalUUIDv7(t *testing.T) {
	r := testRenderer(t)
	invalid := []SecretReference{
		"person@example.test",
		"mailto:person@example.test",
		"https://app.example.test/auth/reset?token=synthetic-bearer",
		"vault://secret-store/users/person@example.test",
		"material:018f22e7-8e71-4b4c-9a4c-6d7b8f15a3c2",
		"material:018F22E7-8E71-7B4C-9A4C-6D7B8F15A3C2",
	}
	for _, ref := range invalid {
		req := validRequest()
		req.MaterialRef = ref
		if err := r.validateRequest(req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("unsafe material reference accepted: %v", err)
		}
	}
	if err := r.validateRequest(validRequest()); err != nil {
		t.Fatalf("canonical opaque reference rejected: %v", err)
	}
}

func TestEnqueuePersistsOnlySecretReference(t *testing.T) {
	db, _ := testkit.NewPostgres(t)
	schema, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", "jobs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("apply test migration: %v", err)
		}
	}
	store, err := sqlstore.New(db, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 3, MaxReconciliationAttempts: 4, MaxLease: time.Second, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	installation, _ := uuid.NewV7()
	application, _ := uuid.NewV7()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.MaterialRef = "material:018f22e7-8e71-7b4c-9a4c-6d7b8f15a3c2"
	job, err := EnqueueTx(context.Background(), tx, store, testRenderer(t), installation, application, "login-flow-01", req, time.Now().UTC().Add(time.Hour))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var persisted string
	if err := db.QueryRow(`SELECT payload::text FROM amos_jobs WHERE id=$1`, job.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"synthetic-one-time-bearer", "https://app.example.test/auth/verify", "token=", "person@example.test"} {
		if strings.Contains(persisted, forbidden) {
			t.Fatalf("persisted email intent contains sensitive material %q", forbidden)
		}
	}
	if !strings.Contains(persisted, string(req.MaterialRef)) {
		t.Fatal("outbox payload did not preserve the opaque material reference")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM amos_jobs WHERE id=$1 AND external_effect=true AND kind=$2`, job.ID, Kind).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("durable email intent count=%d", count)
	}
}

func TestRendererRejectsCrossPurposeEvenWithLegacyResolver(t *testing.T) {
	renderer := testRenderer(t)
	id, e := uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		template TemplateID
		path     string
	}{{TemplateVerifyEmail, "/reset-password"}, {TemplatePasswordReset, "/verify-email"}} {
		request := validRequest()
		request.Template = tc.template
		material := validMaterial()
		material.ActionURL = "https://app.example.test" + tc.path + "?token=synthetic"
		if _, e := renderer.Render(id, request, material); e == nil {
			t.Fatal("authentication purpose crossed renderer")
		}
		legacy := &materialDouble{value: material}
		sender := &senderDouble{send: receipt.AcceptedResult("synthetic")}
		dispatcher, e := NewDispatcher(renderer, sender, legacy, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		payload, e := json.Marshal(request)
		if e != nil {
			t.Fatal(e)
		}
		job := jobs.Job{ID: id, Kind: Kind, ExternalEffect: true, Payload: payload}
		if out := dispatcher.Execute(context.Background(), job, id); out.Kind != jobs.ResolutionTerminal || sender.calls != 0 {
			t.Fatal("legacy resolver bypassed authentication purpose")
		}

	}
}
