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
	ApplicationOrigin string
	ChallengeLifetime time.Duration
}

// ProtectedMaterialWriter must store encrypted/protected delivery material in
// the same PostgreSQL transaction as the challenge and outbox row. There is no
// default implementation; absence means verification delivery is unavailable.
type ProtectedMaterialWriter interface {
	PutVerificationMaterial(context.Context, *sql.Tx, deliveryemail.SecretReference, deliveryemail.PrivateMaterial, time.Time) error
}

type Service struct {
	db        *storage.DB
	outbox    *sqlstore.Store
	renderer  *deliveryemail.Renderer
	materials ProtectedMaterialWriter
	origin    *url.URL
	ttl       time.Duration
}

type Acknowledgement struct {
	Received bool
}

type Page struct {
	Available bool
}

func New(db *storage.DB, outbox *sqlstore.Store, renderer *deliveryemail.Renderer, materials ProtectedMaterialWriter, cfg Config) (*Service, error) {
	if db == nil || cfg.ChallengeLifetime < MinChallengeLifetime || cfg.ChallengeLifetime > MaxChallengeLifetime || cfg.ChallengeLifetime%time.Second != 0 {
		return nil, ErrInvalidRequest
	}
	origin, err := parseOrigin(cfg.ApplicationOrigin)
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
	return &Service{db: db, outbox: outbox, renderer: renderer, materials: materials, origin: origin, ttl: cfg.ChallengeLifetime}, nil
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
		_, _, found, err := lockPendingContact(ctx, tx, personID, emailID)
		if err != nil {
			return ErrUnavailable
		}
		if !found {
			ack.Received = true
			return nil
		}
		count, err := recentChallengeCount(ctx, tx, personID, emailID)
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
		  AND c.consumed_at IS NULL AND c.expires_at > transaction_timestamp()
		  AND p.state='pending_verification' AND e.verified_at IS NULL
		FOR UPDATE OF c,p,e`, challengeID, personID, emailID, verificationPurpose, digest).Scan(
		&installationID, &applicationID, &address, &challengeExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, "", false, ErrChallengeUnavailable
	}
	if err != nil {
		return uuid.Nil, "", false, ErrUnavailable
	}
	count, err := recentChallengeCount(ctx, tx, personID, emailID)
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
		  AND c.consumed_at IS NULL AND c.expires_at > transaction_timestamp()
		  AND p.state='pending_verification' AND e.verified_at IS NULL
	)`, challengeID, verificationPurpose, digest[:]).Scan(&available)
	})
	if err != nil {
		return Page{}, ErrUnavailable
	}
	return Page{Available: available}, nil
}

// Confirm consumes the challenge, verifies only its bound contact, and
// activates the pending person atomically. A wrong-purpose, expired, unknown,
// or replayed token has one non-enumerating error.
func (s *Service) Confirm(ctx context.Context, challengeID uuid.UUID, rawToken string) error {
	if s == nil || s.db == nil || ctx == nil || !validID(challengeID) {
		return ErrChallengeUnavailable
	}
	_, digest, err := parseToken(rawToken)
	if err != nil {
		return ErrChallengeUnavailable
	}
	err = s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		identityStore, err := store.New(tx)
		if err != nil {
			return ErrUnavailable
		}
		consumed, err := identityStore.ConsumeChallenge(ctx, challengeID, verificationPurpose, digest[:])
		if err != nil {
			if errors.Is(err, store.ErrChallengeUnavailable) {
				return ErrChallengeUnavailable
			}
			return ErrUnavailable
		}
		if err := identityStore.MarkEmailVerified(ctx, consumed.PersonID, consumed.EmailID); err != nil {
			if errors.Is(err, store.ErrPersonUnavailable) {
				return ErrChallengeUnavailable
			}
			return ErrUnavailable
		}
		result, err := tx.ExecContext(ctx, `UPDATE identity_persons
			SET state='active',updated_at=transaction_timestamp()
			WHERE id=$1 AND state='pending_verification'`, consumed.PersonID)
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
		if r.Header.Get("Origin") != s.origin.String() {
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

func lockPendingContact(ctx context.Context, tx *sql.Tx, personID, emailID uuid.UUID) (uuid.UUID, string, bool, error) {
	var installationID, applicationID uuid.UUID
	var address string
	err := tx.QueryRowContext(ctx, `SELECT p.installation_id,p.application_id,e.display_address
		FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id
		WHERE p.id=$1 AND e.id=$2 AND p.state='pending_verification' AND e.verified_at IS NULL
		FOR UPDATE OF p,e`, personID, emailID).Scan(&installationID, &applicationID, &address)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, "", false, nil
	}
	if err != nil {
		return uuid.Nil, "", false, err
	}
	return installationID, address, true, nil
}

func recentChallengeCount(ctx context.Context, tx *sql.Tx, personID, emailID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_challenges
		WHERE person_id=$1 AND email_id=$2 AND purpose=$3
		  AND created_at >= transaction_timestamp() - interval '1 hour'`, personID, emailID, verificationPurpose).Scan(&count)
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

func parseOrigin(value string) (*url.URL, error) {
	origin, err := url.Parse(value)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" || strings.ContainsAny(value, "\r\n\t ") {
		return nil, ErrInvalidRequest
	}
	origin.Path = ""
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
