// Package email owns safe, bounded email intent rendering and delivery semantics.
package email

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/mail"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"
	"unicode"

	"github.com/ajent-social/amos/delivery/email/internal/receipt"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
)

var (
	ErrSetupRequired  = errors.New("email provider setup is required")
	ErrUnavailable    = errors.New("email provider is unavailable")
	ErrInvalidRequest = errors.New("invalid email request")
	ErrRejected       = errors.New("email provider rejected the message")
	ErrOutcomeUnknown = errors.New("email provider outcome is unknown")
)

const (
	Kind              = "email.send.v1"
	MaxRecipientBytes = 320
	MaxActionURLBytes = 2048
	MaxSubjectBytes   = 256
	MaxBodyBytes      = 64 << 10
	MinExpirySeconds  = 60
	MaxExpirySeconds  = 24 * 60 * 60
)

type Receipt = receipt.Receipt
type ReceiptState = receipt.State
type VerifiedEvent = receipt.VerifiedEvent

const (
	ReceiptUnknown  = receipt.Unknown
	ReceiptAccepted = receipt.Accepted
	ReceiptRejected = receipt.Rejected
)

type TemplateID string

const (
	TemplateSignIn        TemplateID = "sign_in_link"
	TemplateVerifyEmail   TemplateID = "verify_email"
	TemplatePasswordReset TemplateID = "password_reset"
)

type Request struct {
	Template         TemplateID      `json:"template"`
	MaterialRef      SecretReference `json:"material_ref"`
	ExpiresInSeconds int64           `json:"expires_in_seconds"`
}

type SecretReference string

// PrivateMaterial is resolved only for the send operation. Never persist plaintext or log it.
type PrivateMaterial struct {
	Recipient string
	ActionURL string
}

type RenderConfig struct {
	DevelopmentLoopback bool
	FromAddress         string
	ApplicationOrigin   string
	MaxBodyBytes        int
}

type Renderer struct {
	from    string
	origin  *url.URL
	maxBody int
	text    map[TemplateID]*texttemplate.Template
	html    map[TemplateID]*template.Template
}

type templateData struct {
	ActionURL        string
	ExpiresInSeconds int64
}

var subjects = map[TemplateID]string{
	TemplateSignIn:        "Your AMOS sign-in link",
	TemplateVerifyEmail:   "Verify your AMOS email address",
	TemplatePasswordReset: "Reset your AMOS password",
}

const textTemplateSource = `{{.ActionURL}}

This link expires in {{.ExpiresInSeconds}} seconds. If you did not request it, you can ignore this email.
`
const htmlTemplateSource = `<p><a href="{{.ActionURL}}">Continue</a></p><p>This link expires in {{.ExpiresInSeconds}} seconds. If you did not request it, you can ignore this email.</p>`

func NewRenderer(cfg RenderConfig) (*Renderer, error) {
	if cfg.MaxBodyBytes <= 0 || cfg.MaxBodyBytes > MaxBodyBytes || !validAddress(cfg.FromAddress) {
		return nil, ErrSetupRequired
	}
	origin, err := ParseApplicationOrigin(cfg.ApplicationOrigin, cfg.DevelopmentLoopback)
	if err != nil {
		return nil, ErrSetupRequired
	}
	origin.Path = ""
	texts := make(map[TemplateID]*texttemplate.Template, len(subjects))
	htmls := make(map[TemplateID]*template.Template, len(subjects))
	for id := range subjects {
		textValue, err := texttemplate.New(string(id)).Option("missingkey=error").Parse(textTemplateSource)
		if err != nil {
			return nil, ErrSetupRequired
		}
		htmlValue, err := template.New(string(id)).Option("missingkey=error").Parse(htmlTemplateSource)
		if err != nil {
			return nil, ErrSetupRequired
		}
		texts[id], htmls[id] = textValue, htmlValue
	}
	return &Renderer{from: cfg.FromAddress, origin: origin, maxBody: cfg.MaxBodyBytes, text: texts, html: htmls}, nil
}

type Message struct {
	jobID    uuid.UUID
	to       string
	from     string
	subject  string
	textBody string
	htmlBody string
}

func (m Message) JobID() uuid.UUID  { return m.jobID }
func (m Message) Recipient() string { return m.to }
func (m Message) Sender() string    { return m.from }
func (m Message) Subject() string   { return m.subject }
func (m Message) TextBody() string  { return m.textBody }
func (m Message) HTMLBody() string  { return m.htmlBody }

func (r *Renderer) Render(jobID uuid.UUID, req Request, material PrivateMaterial) (Message, error) {
	if r == nil || jobID == uuid.Nil || !validAddress(material.Recipient) || len(material.Recipient) > MaxRecipientBytes || req.ExpiresInSeconds < MinExpirySeconds || req.ExpiresInSeconds > MaxExpirySeconds {
		return Message{}, ErrInvalidRequest
	}
	if _, ok := subjects[req.Template]; !ok {
		return Message{}, ErrInvalidRequest
	}
	if !validSecretReference(req.MaterialRef) || !r.allowedActionURL(material.ActionURL) {
		return Message{}, ErrInvalidRequest
	}
	action, parseErr := url.Parse(material.ActionURL)
	purposeMatches := parseErr == nil
	if req.Template == TemplateVerifyEmail {
		purposeMatches = purposeMatches && action.Path == "/verify-email" && action.RawPath == ""
	}
	if req.Template == TemplatePasswordReset {
		purposeMatches = purposeMatches && action.Path == "/reset-password" && action.RawPath == ""
	}
	if !purposeMatches {
		return Message{}, ErrInvalidRequest
	}
	data := templateData{ActionURL: material.ActionURL, ExpiresInSeconds: req.ExpiresInSeconds}
	var textBuffer, htmlBuffer bytes.Buffer
	if err := r.text[req.Template].Execute(&textBuffer, data); err != nil {
		return Message{}, ErrInvalidRequest
	}
	if err := r.html[req.Template].Execute(&htmlBuffer, data); err != nil {
		return Message{}, ErrInvalidRequest
	}
	textBody, htmlBody := textBuffer.String(), htmlBuffer.String()
	if len(textBody) > r.maxBody || len(htmlBody) > r.maxBody {
		return Message{}, ErrInvalidRequest
	}
	return Message{jobID: jobID, to: material.Recipient, from: r.from, subject: subjects[req.Template], textBody: textBody, htmlBody: htmlBody}, nil
}

func (r *Renderer) allowedActionURL(raw string) bool {
	if len(raw) == 0 || len(raw) > MaxActionURLBytes || strings.ContainsAny(raw, "\r\n\t ") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == r.origin.Scheme && strings.EqualFold(u.Host, r.origin.Host) && u.User == nil && u.Fragment == "" && u.Host != ""
}

func validAddress(value string) bool {
	if value == "" || len(value) > MaxRecipientBytes || strings.ContainsAny(value, "\r\n\t ") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && parsed.Name == ""
}

func validSecretReference(value SecretReference) bool {
	s := string(value)
	const prefix = "material:"
	if !strings.HasPrefix(s, prefix) || len(s) != len(prefix)+36 || strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	idText := strings.TrimPrefix(s, prefix)
	id, err := uuid.Parse(idText)
	return err == nil && id.Version() == 7 && id.String() == idText
}

func (r *Renderer) validateRequest(req Request) error {
	if r == nil || !validSecretReference(req.MaterialRef) || req.ExpiresInSeconds < MinExpirySeconds || req.ExpiresInSeconds > MaxExpirySeconds {
		return ErrInvalidRequest
	}
	if _, ok := subjects[req.Template]; !ok {
		return ErrInvalidRequest
	}
	return nil
}

type EmailSender interface {
	Send(context.Context, Message) (Receipt, error)
	Reconcile(context.Context, uuid.UUID) (Receipt, error)
}

type MaterialResolver interface {
	ResolveDelivery(context.Context, SecretReference) (PrivateMaterial, error)
}

// PurposeMaterialResolver binds protected material to its authentication template.
type PurposeMaterialResolver interface {
	ResolveForTemplate(context.Context, SecretReference, TemplateID) (PrivateMaterial, error)
}
type VerifiedEventSource interface {
	LookupVerified(context.Context, uuid.UUID) (VerifiedEvent, bool, error)
}

func EnqueueTx(ctx context.Context, tx *sql.Tx, store *sqlstore.Store, renderer *Renderer, installationID, applicationID uuid.UUID, key string, req Request, deadline time.Time) (jobs.Job, error) {
	if renderer == nil || store == nil {
		return jobs.Job{}, ErrSetupRequired
	}
	if err := renderer.validateRequest(req); err != nil {
		return jobs.Job{}, err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return jobs.Job{}, ErrInvalidRequest
	}
	return store.EnqueueTx(ctx, tx, jobs.Intent{InstallationID: installationID, ApplicationID: applicationID, Key: key, Kind: Kind, Payload: payload, ExternalEffect: true, Deadline: deadline.UTC()})
}

type Dispatcher struct {
	renderer   *Renderer
	sender     EmailSender
	materials  MaterialResolver
	retryDelay time.Duration
}

func NewDispatcher(renderer *Renderer, sender EmailSender, materials MaterialResolver, retryDelay time.Duration) (*Dispatcher, error) {
	if renderer == nil || sender == nil || materials == nil {
		return nil, ErrSetupRequired
	}
	if retryDelay <= 0 || retryDelay > 7*24*time.Hour {
		return nil, ErrInvalidRequest
	}
	return &Dispatcher{renderer: renderer, sender: sender, materials: materials, retryDelay: retryDelay}, nil
}

func (d *Dispatcher) Execute(ctx context.Context, job jobs.Job, idempotencyKey uuid.UUID) jobs.Resolution {
	if d == nil || d.renderer == nil || d.sender == nil || !job.ExternalEffect || job.Kind != Kind || idempotencyKey != job.ID {
		return jobs.Resolution{Kind: jobs.ResolutionTerminal}
	}
	request, err := decodeRequest(job.Payload)
	if err != nil {
		return jobs.Resolution{Kind: jobs.ResolutionTerminal}
	}
	if err := d.renderer.validateRequest(request); err != nil {
		return jobs.Resolution{Kind: jobs.ResolutionTerminal}
	}
	var material PrivateMaterial
	if scoped, ok := d.materials.(PurposeMaterialResolver); ok {
		material, err = scoped.ResolveForTemplate(ctx, request.MaterialRef, request.Template)
	} else {
		material, err = d.materials.ResolveDelivery(ctx, request.MaterialRef)
	}
	if err != nil {
		return jobs.Resolution{Kind: jobs.ResolutionRetrySafe, RetryAfter: d.retryDelay}
	}
	message, err := d.renderer.Render(job.ID, request, material)
	if err != nil {
		return jobs.Resolution{Kind: jobs.ResolutionTerminal}
	}
	receipt, err := d.sender.Send(ctx, message)
	if err != nil && receipt.State() != ReceiptRejected {
		return jobs.Resolution{Kind: jobs.ResolutionUnknown}
	}
	return resolutionFor(receipt, d.retryDelay)
}

func (d *Dispatcher) Reconcile(ctx context.Context, job jobs.Job, idempotencyKey uuid.UUID) jobs.Resolution {
	if d == nil || d.sender == nil || !job.ExternalEffect || job.Kind != Kind || idempotencyKey != job.ID {
		return jobs.Resolution{Kind: jobs.ResolutionTerminal}
	}
	receipt, err := d.sender.Reconcile(ctx, job.ID)
	if err != nil {
		return jobs.Resolution{Kind: jobs.ResolutionUnknown}
	}
	if receipt.State() == ReceiptUnknown {
		return jobs.Resolution{Kind: jobs.ResolutionUnknown}
	}
	return resolutionFor(receipt, d.retryDelay)
}

func resolutionFor(r Receipt, retryDelay time.Duration) jobs.Resolution {
	switch r.State() {
	case ReceiptAccepted:
		if r.MessageID() == "" {
			return jobs.Resolution{Kind: jobs.ResolutionUnknown}
		}
		return jobs.Resolution{Kind: jobs.ResolutionSucceeded}
	case ReceiptRejected:
		if r.Retryable() {
			delay := r.RetryAfter()
			if delay <= 0 || delay > retryDelay {
				delay = retryDelay
			}
			return jobs.Resolution{Kind: jobs.ResolutionRetrySafe, RetryAfter: delay}
		}
		return jobs.Resolution{Kind: jobs.ResolutionTerminal}
	default:
		return jobs.Resolution{Kind: jobs.ResolutionUnknown}
	}
}

func decodeRequest(payload []byte) (Request, error) {
	var req Request
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return Request{}, ErrInvalidRequest
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Request{}, ErrInvalidRequest
	}
	return req, nil
}
