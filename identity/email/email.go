// Package email coordinates purpose-bound email verification. Raw challenges
// are kept out of identity rows and generic outbox payloads; issue operations
// require an owner-qualified protected-material writer.
package email

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var (
	ErrInvalidRequest       = errors.New("invalid email verification request")
	ErrUnavailable          = errors.New("email verification is unavailable")
	ErrChallengeUnavailable = errors.New("verification challenge is unavailable")
	errDuplicateDeliveryKey = errors.New("email verification request already queued")
)

const (
	DefaultChallengeLifetime = 30 * time.Minute
	MinChallengeLifetime     = time.Minute
	MaxChallengeLifetime     = 24 * time.Hour
	MaxIssueRequestsPerHour  = 3
	MaxConfirmBodyBytes      = 4096
	verificationPurpose      = store.ChallengeEmailVerification
)

type Config struct {
	DevelopmentLoopback bool
	InstallationID      uuid.UUID
	ApplicationID       uuid.UUID
	ApplicationOrigin   string
	ChallengeLifetime   time.Duration
}

// ProtectedMaterialWriter must store encrypted/protected delivery material in
// the same PostgreSQL transaction as the challenge and outbox row. There is no
// default implementation; absence means verification delivery is unavailable.
type ProtectedMaterialWriter interface {
	PutVerificationMaterial(context.Context, *sql.Tx, deliveryemail.SecretReference, deliveryemail.PrivateMaterial, time.Time) error
}

// TxRunner is the transaction capability consumed by email verification.
// Implementations own transaction completion and database lifetime.
type TxRunner interface {
	WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}

type Service struct {
	db             TxRunner
	outbox         *sqlstore.Store
	renderer       *deliveryemail.Renderer
	materials      ProtectedMaterialWriter
	origin         *url.URL
	installationID uuid.UUID
	applicationID  uuid.UUID
	ttl            time.Duration
}

type Acknowledgement struct {
	Received bool
}

type Page struct {
	Available bool
}

func New(db *storage.DB, outbox *sqlstore.Store, renderer *deliveryemail.Renderer, materials ProtectedMaterialWriter, cfg Config) (*Service, error) {
	return NewWithTxRunner(db, outbox, renderer, materials, cfg)
}

// NewWithTxRunner constructs a service without probing or taking ownership of db.
// Trusted composition must bind identity, outbox and materials to one database.
func NewWithTxRunner(db TxRunner, outbox *sqlstore.Store, renderer *deliveryemail.Renderer, materials ProtectedMaterialWriter, cfg Config) (*Service, error) {
	if nilTxRunner(db) || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || cfg.ChallengeLifetime < MinChallengeLifetime || cfg.ChallengeLifetime > MaxChallengeLifetime || cfg.ChallengeLifetime%time.Second != 0 {
		return nil, ErrInvalidRequest
	}
	origin, err := parseOrigin(cfg.ApplicationOrigin, cfg.DevelopmentLoopback)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	configured := 0
	if outbox != nil {
		configured++
	}
	if renderer != nil {
		configured++
	}
	if materials != nil {
		configured++
	}
	if configured != 0 && configured != 3 {
		return nil, ErrInvalidRequest
	}
	if renderer != nil {
		challengeID, idErr := uuid.NewV7()
		materialID, materialErr := uuid.NewV7()
		if idErr != nil || materialErr != nil {
			return nil, ErrUnavailable
		}
		ref := deliveryemail.SecretReference("material:" + materialID.String())
		probeURL := buildActionURL(origin, challengeID, strings.Repeat("A", 43))
		if _, err := renderer.Render(challengeID, deliveryemail.Request{Template: deliveryemail.TemplateVerifyEmail, MaterialRef: ref, ExpiresInSeconds: int64(cfg.ChallengeLifetime / time.Second)}, deliveryemail.PrivateMaterial{Recipient: "verification-probe@example.invalid", ActionURL: probeURL}); err != nil {
			return nil, ErrInvalidRequest
		}
	}
	return &Service{db: db, outbox: outbox, renderer: renderer, materials: materials, origin: origin, installationID: cfg.InstallationID, applicationID: cfg.ApplicationID, ttl: cfg.ChallengeLifetime}, nil
}

func nilTxRunner(db TxRunner) bool {
	if db == nil {
		return true
	}
	v := reflect.ValueOf(db)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// IssueVerification creates a new challenge for a pending, unverified contact.
// Missing/already-verified contacts and a reached issue budget return the same
// acknowledgement, avoiding account/contact enumeration.
func (s *Service) IssueVerification(ctx context.Context, personID, emailID, requestID uuid.UUID) (Acknowledgement, error) {
	if s == nil || s.db == nil || ctx == nil || !validID(personID) || !validID(emailID) || !validID(requestID) {
		return Acknowledgement{}, ErrInvalidRequest
	}
	if !s.deliveryReady() {
		return Acknowledgement{}, ErrUnavailable
	}
	ack := Acknowledgement{}
	err := s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		_, found, err := lockPendingContact(ctx, tx, s.installationID, s.applicationID, personID, emailID)
		if err != nil {
			return ErrUnavailable
		}
		if !found {
			ack.Received = true
			return nil
		}
		count, err := recentChallengeCount(ctx, tx, s.installationID, s.applicationID, personID, emailID)
		if err != nil {
			return ErrUnavailable
		}
		if count >= MaxIssueRequestsPerHour {
			ack.Received = true
			return nil
		}
		challengeID, err := store.NewID()
		if err != nil {
			return ErrUnavailable
		}
		token, digest, err := newToken()
		if err != nil {
			return ErrUnavailable
		}
		var now time.Time
		if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
			return ErrUnavailable
		}
		expiresAt := now.UTC().Add(s.ttl)
		identityStore, err := store.New(tx)
		if err != nil {
			return ErrUnavailable
		}
		if err := identityStore.CreateChallenge(ctx, store.Challenge{ID: challengeID, PersonID: personID, EmailID: emailID, Purpose: verificationPurpose, Digest: digest[:], ExpiresAt: expiresAt}); err != nil {
			return mapIdentityError(err)
		}
		_, _, queued, err := s.queueChallengeTx(ctx, tx, personID, emailID, challengeID, token, digest[:], requestID, expiresAt)
		if err != nil {
			if errors.Is(err, jobs.ErrIdempotencyConflict) {
				return errDuplicateDeliveryKey
			}
			return err
		}
		if !queued {
			return ErrUnavailable
		}
		ack.Received = true
		return nil
	})
	if err != nil {
		if errors.Is(err, errDuplicateDeliveryKey) {
			return Acknowledgement{Received: true}, nil
		}
		return Acknowledgement{}, ErrUnavailable
	}
	return ack, nil
}

// QueueExistingChallengeTx attaches delivery to a challenge already created
// in the caller's account-registration transaction. Callers must return any
// error so the account, challenge, material envelope, and outbox all roll back.
func (s *Service) QueueExistingChallengeTx(ctx context.Context, tx *sql.Tx, personID, emailID, challengeID, requestID uuid.UUID, rawToken string) error {
	if s == nil || ctx == nil || tx == nil || !validID(personID) || !validID(emailID) || !validID(challengeID) || !validID(requestID) {
		return ErrInvalidRequest
	}
	if !s.deliveryReady() {
		return ErrUnavailable
	}
	_, digest, err := parseToken(rawToken)
	if err != nil {
		return ErrChallengeUnavailable
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return ErrUnavailable
	}
	expiresAt := now.UTC().Add(s.ttl)
	_, _, queued, err := s.queueChallengeTx(ctx, tx, personID, emailID, challengeID, rawToken, digest[:], requestID, expiresAt)
	if err != nil {
		return err
	}
	if !queued {
		return ErrChallengeUnavailable
	}
	return nil
}

// queueChallengeTx checks the purpose/digest/contact binding before storing a
// protected secret and adding the email intent in this same transaction.
func (s *Service) queueChallengeTx(ctx context.Context, tx *sql.Tx, personID, emailID, challengeID uuid.UUID, rawToken string, digest []byte, requestID uuid.UUID, fallbackExpiry time.Time) (uuid.UUID, string, bool, error) {
	var installationID, applicationID uuid.UUID
	var address string
	var challengeExpiry time.Time
	err := tx.QueryRowContext(ctx, `SELECT p.installation_id,p.application_id,e.display_address,c.expires_at
		FROM identity_challenges c
		JOIN identity_persons p ON p.id=c.person_id
		JOIN identity_emails e ON e.id=c.email_id AND e.person_id=c.person_id
		WHERE c.id=$1 AND c.person_id=$2 AND c.email_id=$3 AND c.purpose=$4 AND c.token_digest=$5
		  AND p.installation_id=$6 AND p.application_id=$7
		  AND e.installation_id=$6 AND e.application_id=$7
		  AND c.consumed_at IS NULL AND c.expires_at > transaction_timestamp()
		  AND p.state='pending_verification' AND e.verified_at IS NULL
		FOR UPDATE OF c,p,e`, challengeID, personID, emailID, verificationPurpose, digest, s.installationID, s.applicationID).Scan(
		&installationID, &applicationID, &address, &challengeExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, "", false, ErrChallengeUnavailable
	}
	if err != nil {
		return uuid.Nil, "", false, ErrUnavailable
	}
	count, err := recentChallengeCount(ctx, tx, s.installationID, s.applicationID, personID, emailID)
	if err != nil {
		return uuid.Nil, "", false, ErrUnavailable
	}
	if count > MaxIssueRequestsPerHour {
		return uuid.Nil, "", false, nil
	}
	ref := deliveryemail.SecretReference("material:" + challengeID.String())
	link := buildActionURL(s.origin, challengeID, rawToken)
	material := deliveryemail.PrivateMaterial{Recipient: address, ActionURL: link}
	if err := s.materials.PutVerificationMaterial(ctx, tx, ref, material, challengeExpiry.UTC()); err != nil {
		return uuid.Nil, "", false, ErrUnavailable
	}
	request := deliveryemail.Request{Template: deliveryemail.TemplateVerifyEmail, MaterialRef: ref, ExpiresInSeconds: int64(s.ttl / time.Second)}
	deadline := challengeExpiry.UTC()
	if deadline.IsZero() {
		deadline = fallbackExpiry.UTC()
	}
	key := "email-verification:" + requestID.String()
	if _, err := deliveryemail.EnqueueTx(ctx, tx, s.outbox, s.renderer, installationID, applicationID, key, request, deadline); err != nil {
		return uuid.Nil, "", false, err
	}
	return challengeID, address, true, nil
}

// Preview performs a read-only validity check for a GET confirmation page. It
// never consumes or refreshes the challenge.
func (s *Service) Preview(ctx context.Context, challengeID uuid.UUID, rawToken string) (Page, error) {
	if s == nil || s.db == nil || ctx == nil {
		return Page{}, ErrUnavailable
	}
	if !validID(challengeID) {
		return Page{}, nil
	}
	_, digest, err := parseToken(rawToken)
	if err != nil {
		return Page{}, nil
	}
	var available bool
	err = s.db.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM identity_challenges c
		JOIN identity_persons p ON p.id=c.person_id
		JOIN identity_emails e ON e.id=c.email_id AND e.person_id=c.person_id
		WHERE c.id=$1 AND c.purpose=$2 AND c.token_digest=$3
		  AND p.installation_id=$4 AND p.application_id=$5
		  AND e.installation_id=$4 AND e.application_id=$5
		  AND c.consumed_at IS NULL AND c.expires_at > transaction_timestamp()
		  AND p.state='pending_verification' AND e.verified_at IS NULL
	)`, challengeID, verificationPurpose, digest[:], s.installationID, s.applicationID).Scan(&available)
	})
	if err != nil {
		return Page{}, ErrUnavailable
	}
	return Page{Available: available}, nil
}

// Confirm provisionally verifies the bound contact and activates its pending
// person, then consumes the challenge after those row waits in READ COMMITTED.
// Callback failure rolls back all writes; success is returned only after commit.
// A commit error is unavailable and may have an unknown outcome.
// Wrong-purpose, expired, unknown and replayed tokens share one error.
func (s *Service) Confirm(ctx context.Context, challengeID uuid.UUID, rawToken string) error {
	if s == nil || s.db == nil || ctx == nil || !validID(challengeID) {
		return ErrChallengeUnavailable
	}
	_, digest, err := parseToken(rawToken)
	if err != nil {
		return ErrChallengeUnavailable
	}
	err = s.db.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx *sql.Tx) error {
		var personID, emailID uuid.UUID
		err := tx.QueryRowContext(ctx, `SELECT c.person_id,c.email_id FROM identity_challenges c
			JOIN identity_persons p ON p.id=c.person_id
			JOIN identity_emails e ON e.id=c.email_id AND e.person_id=c.person_id
			WHERE c.id=$1 AND c.purpose=$2 AND c.token_digest=$3
			  AND c.consumed_at IS NULL
			  AND p.id=c.person_id AND p.state='pending_verification'
			  AND p.installation_id=$4 AND p.application_id=$5
			  AND e.id=c.email_id AND e.person_id=c.person_id
			  AND e.installation_id=$4 AND e.application_id=$5 AND e.verified_at IS NULL
			FOR UPDATE OF c`, challengeID, verificationPurpose, digest[:], s.installationID, s.applicationID).Scan(&personID, &emailID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrChallengeUnavailable
		}
		if err != nil {
			return ErrUnavailable
		}
		verified, err := tx.ExecContext(ctx, `UPDATE identity_emails
			SET verified_at=transaction_timestamp()
			WHERE id=$1 AND person_id=$2 AND installation_id=$3 AND application_id=$4 AND verified_at IS NULL`,
			emailID, personID, s.installationID, s.applicationID)
		if err != nil {
			return ErrUnavailable
		}
		verifiedCount, err := verified.RowsAffected()
		if err != nil {
			return ErrUnavailable
		}
		if verifiedCount != 1 {
			return ErrChallengeUnavailable
		}
		result, err := tx.ExecContext(ctx, `UPDATE identity_persons
			SET state='active',updated_at=transaction_timestamp()
			WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND state='pending_verification'`, personID, s.installationID, s.applicationID)
		if err != nil {
			return ErrUnavailable
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return ErrUnavailable
		}
		if updated != 1 {
			return ErrChallengeUnavailable
		}
		identityStore, err := store.New(tx)
		if err != nil {
			return ErrUnavailable
		}
		consumed, err := identityStore.ConsumeChallenge(ctx, challengeID, verificationPurpose, digest[:])
		if err != nil {
			return mapIdentityError(err)
		}
		if consumed.PersonID != personID || consumed.EmailID != emailID {
			return ErrChallengeUnavailable
		}
		return nil
	})
	if errors.Is(err, ErrChallengeUnavailable) {
		return ErrChallengeUnavailable
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// Handler exposes preview-only GET/HEAD and explicit same-origin POST confirm.
// It never logs or reflects raw request values into errors.
func (s *Service) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Service) serveHTTP(w http.ResponseWriter, r *http.Request) {
	setPrivateHeaders(w)
	if s == nil || s.db == nil || r == nil {
		http.Error(w, "Email verification is unavailable.", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		query := r.URL.Query()
		challengeValues, tokenValues := query["challenge"], query["token"]
		if len(query) != 2 || len(challengeValues) != 1 || len(tokenValues) != 1 {
			writePage(w, r.Method, http.StatusOK, "This verification link is invalid or expired.", "")
			return
		}
		challengeID, err := parseID(challengeValues[0])
		if err != nil {
			writePage(w, r.Method, http.StatusOK, "This verification link is invalid or expired.", "")
			return
		}
		page, err := s.Preview(r.Context(), challengeID, tokenValues[0])
		if err != nil {
			http.Error(w, "Email verification is unavailable.", http.StatusServiceUnavailable)
			return
		}
		if !page.Available {
			writePage(w, r.Method, http.StatusOK, "This verification link is invalid or expired.", "")
			return
		}
		writeConfirmationForm(w, r.Method, challengeID.String(), tokenValues[0])
	case http.MethodPost:
		if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != s.origin.String() {
			http.Error(w, "Verification request was denied.", http.StatusForbidden)
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength > MaxConfirmBodyBytes {
			http.Error(w, "Invalid verification request.", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxConfirmBodyBytes)
		if err := r.ParseForm(); err != nil || len(r.PostForm) != 2 || len(r.PostForm["challenge"]) != 1 || len(r.PostForm["token"]) != 1 {
			http.Error(w, "Invalid verification request.", http.StatusBadRequest)
			return
		}
		challengeID, err := parseID(r.PostForm.Get("challenge"))
		if err != nil {
			writePage(w, r.Method, http.StatusOK, "This verification link is invalid or expired.", "")
			return
		}
		err = s.Confirm(r.Context(), challengeID, r.PostForm.Get("token"))
		if errors.Is(err, ErrChallengeUnavailable) {
			writePage(w, r.Method, http.StatusOK, "This verification link is invalid or expired.", "")
			return
		}
		if err != nil {
			http.Error(w, "Email verification is unavailable.", http.StatusServiceUnavailable)
			return
		}
		writePage(w, r.Method, http.StatusOK, "Your email address is verified. You may continue.", "")
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
	}
}

func (s *Service) deliveryReady() bool {
	return s != nil && s.db != nil && s.outbox != nil && s.renderer != nil && s.materials != nil && s.origin != nil
}

func lockPendingContact(ctx context.Context, tx *sql.Tx, installationID, applicationID, personID, emailID uuid.UUID) (string, bool, error) {
	var address string
	err := tx.QueryRowContext(ctx, `SELECT e.display_address
		FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id
		WHERE p.id=$1 AND e.id=$2 AND p.installation_id=$3 AND p.application_id=$4
		  AND e.installation_id=$3 AND e.application_id=$4
		  AND p.state='pending_verification' AND e.verified_at IS NULL
		FOR UPDATE OF p,e`, personID, emailID, installationID, applicationID).Scan(&address)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return address, true, nil
}

func recentChallengeCount(ctx context.Context, tx *sql.Tx, installationID, applicationID, personID, emailID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_challenges c
		JOIN identity_persons p ON p.id=c.person_id
		JOIN identity_emails e ON e.id=c.email_id AND e.person_id=c.person_id
		WHERE c.person_id=$1 AND c.email_id=$2 AND c.purpose=$3
		  AND p.installation_id=$4 AND p.application_id=$5
		  AND e.installation_id=$4 AND e.application_id=$5
		  AND c.created_at >= transaction_timestamp() - interval '1 hour'`, personID, emailID, verificationPurpose, installationID, applicationID).Scan(&count)
	return count, err
}

func newToken() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	digest := sha256.Sum256([]byte(token))
	return token, digest, nil
}

func parseToken(token string) ([]byte, [32]byte, error) {
	if len(token) != 43 {
		return nil, [32]byte{}, ErrChallengeUnavailable
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return nil, [32]byte{}, ErrChallengeUnavailable
	}
	digest := sha256.Sum256([]byte(token))
	return raw, digest, nil
}

func parseID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || !validID(id) || id.String() != value {
		return uuid.Nil, ErrChallengeUnavailable
	}
	return id, nil
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func parseOrigin(value string, developmentLoopback bool) (*url.URL, error) {
	origin, err := deliveryemail.ParseApplicationOrigin(value, developmentLoopback)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	return origin, nil
}

func buildActionURL(origin *url.URL, challengeID uuid.UUID, token string) string {
	value := *origin
	value.Path = "/verify-email"
	query := url.Values{}
	query.Set("challenge", challengeID.String())
	query.Set("token", token)
	value.RawQuery = query.Encode()
	return value.String()
}

func mapIdentityError(err error) error {
	if errors.Is(err, store.ErrChallengeUnavailable) || errors.Is(err, store.ErrPersonUnavailable) {
		return ErrChallengeUnavailable
	}
	return ErrUnavailable
}

var pageTemplate = template.Must(template.New("email-verification").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Email verification</title><body><main><h1>Email verification</h1><p>{{.Message}}</p>{{if .Available}}<form method="post" action="/verify-email"><input type="hidden" name="challenge" value="{{.Challenge}}"><input type="hidden" name="token" value="{{.Token}}"><button type="submit">Confirm email address</button></form>{{end}}</main></body></html>`))

type pageData struct {
	Message, Challenge, Token string
	Available                 bool
}

func writeConfirmationForm(w http.ResponseWriter, method, challengeID, token string) {
	writeHTMLPage(w, method, pageData{Message: "Confirm that you own this email address.", Challenge: challengeID, Token: token, Available: true})
}

func writePage(w http.ResponseWriter, method string, status int, message, _ string) {
	writeHTMLPageWithStatus(w, method, status, pageData{Message: message})
}

func writeHTMLPage(w http.ResponseWriter, method string, data pageData) {
	writeHTMLPageWithStatus(w, method, http.StatusOK, data)
}

func writeHTMLPageWithStatus(w http.ResponseWriter, method string, status int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if method == http.MethodHead {
		return
	}
	if err := pageTemplate.Execute(w, data); err != nil {
		return
	}
}

func setPrivateHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
