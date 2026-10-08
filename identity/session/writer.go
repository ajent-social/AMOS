package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/ajent-social/amos/identity"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

// NewWithWriter retains only the private root. Root.Run/Read enforce active
// process ownership before SQL; construction performs no transaction probe.
func NewWithWriter(root *aw.Root, cfg Config) (*Service, error) {
	if root == nil {
		return nil, ErrInvalidConfiguration
	}
	s, err := configured(cfg)
	if err != nil {
		return nil, err
	}
	s.root = root
	return s, nil
}

type WriterRequest struct{ data *writerRequestData }
type writerKey struct{}
type writerRequestData struct {
	mu                          sync.Mutex
	service                     *Service
	done                        <-chan struct{}
	deadline                    time.Time
	admission                   *currentAdmission
	contextOnly                 bool
	action                      wp.Action
	digest                      []byte
	origin, referer, csrf       string
	attempt                     *aw.Attempt
	discovered, staged          bool
	priorID, priorPerson, newID uuid.UUID
	token, csrfToken            string
}
type Staged struct{ data *stagedData }
type stagedData struct {
	mu                sync.Mutex
	checked, finalOK  bool
	tx                *sql.Tx
	request           WriterRequest
	newID, priorID    uuid.UUID
	inserted, rotated currentRow

	service  *Service
	attempt  *aw.Attempt
	binding  aw.Binding
	issuance wp.Issuance
	issued   Issued
}

func singleCookie(r *http.Request, name string) (*http.Cookie, error) {
	if r == nil {
		return nil, ErrUnauthenticated
	}
	var found *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == name {
			if found != nil {
				return nil, ErrUnauthenticated
			}
			found = c
		}
	}
	if found == nil {
		return nil, http.ErrNoCookie
	}
	return found, nil
}

// AdmitWriterRequest copies browser fields and installs a private request token
// in this exact request's existing bounded context. It grants no credential.
func (s *Service) AdmitWriterRequest(r *http.Request) (WriterRequest, error) {
	if s == nil || s.root == nil || r == nil {
		return WriterRequest{}, ErrUnavailable
	}
	ctx := r.Context()
	end, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil || !time.Now().Before(end) {
		return WriterRequest{}, ErrUnavailable
	}
	if ctx.Value(writerKey{}) != nil {
		return WriterRequest{}, ErrUnavailable
	}
	c, err := singleCookie(r, s.cookieName)
	if err != nil && !errors.Is(err, http.ErrNoCookie) {
		return WriterRequest{}, ErrUnauthenticated
	}
	if len(r.Header.Values("Origin")) > 1 || len(r.Header.Values("Referer")) > 1 || len(r.Header.Values("X-CSRF-Token")) > 1 {
		return WriterRequest{}, ErrUnauthenticated
	}
	d := &writerRequestData{service: s, done: ctx.Done(), deadline: end, origin: r.Header.Get("Origin"), referer: r.Header.Get("Referer"), csrf: csrfFromRequest(nil, r)}
	if c != nil {
		d.digest, _ = tokenDigest(c.Value)
	}
	if _, ok := ctx.Value(currentKey{}).(*currentAdmission); ok {
		d.admission, err = s.current(ctx)
		if err != nil {
			return WriterRequest{}, err
		}
	}
	*r = *r.WithContext(context.WithValue(ctx, writerKey{}, d))
	return WriterRequest{data: d}, nil
}
func (s *Service) AdmitWriterContext(ctx context.Context, action wp.Action) (WriterRequest, error) {
	if s == nil || s.root == nil || (action != wp.MFABegin && action != wp.PasswordChange) {
		return WriterRequest{}, ErrUnauthenticated
	}
	v, err := s.current(ctx)
	if err != nil {
		return WriterRequest{}, err
	}
	return WriterRequest{data: &writerRequestData{service: s, done: v.done, deadline: v.deadline, admission: v, contextOnly: true, action: action}}, nil
}
func (s *Service) writerRequest(ctx context.Context, r WriterRequest) (*writerRequestData, error) {
	d := r.data
	if s == nil || s.root == nil || ctx == nil || ctx.Err() != nil || d == nil || d.service != s {
		return nil, ErrUnavailable
	}
	select {
	case <-d.done:
		return nil, ErrUnavailable
	default:
	}
	end, ok := ctx.Deadline()
	if !ok || end.After(d.deadline) || !time.Now().Before(d.deadline) {
		return nil, ErrUnavailable
	}
	if d.admission != nil {
		v, err := s.current(ctx)
		if err != nil || v != d.admission {
			return nil, ErrUnauthenticated
		}
	}
	if !d.contextOnly && ctx.Value(writerKey{}) != d {
		return nil, ErrUnauthenticated
	}
	return d, nil
}
func (s *Service) WriterPrincipalMatches(r WriterRequest, p identity.Principal) bool {
	d := r.data
	if s == nil || d == nil || d.service != s || d.admission == nil || d.admission.service != s || p != d.admission.principal || !time.Now().Before(d.deadline) {
		return false
	}
	select {
	case <-d.done:
		return false
	default:
		return true
	}
}
func issuing(action wp.Action) bool {
	switch action {
	case wp.PasswordSignIn, wp.MagicConfirm, wp.MFAConfirm, wp.MFAChallenge, wp.FederationCallbackLogin:
		return true
	}
	return false
}
func writerFailure(a *aw.Attempt, err error) error {
	outcome := aw.UnavailableRollback
	if errors.Is(err, ErrUnauthenticated) {
		outcome = aw.DeniedRollback
	}
	if e := a.Finish(outcome); e != nil {
		return err
	}
	return err
}

// Store participant errors already marked the terminal root outcome. Mapping
// them must neither Finish again nor resume SQL after a semantic denial.
func terminalStoreError(err error) error {
	if errors.Is(err, store.ErrSessionUnavailable) || errors.Is(err, store.ErrPersonUnavailable) || errors.Is(err, store.ErrChallengeUnavailable) {
		return ErrUnauthenticated
	}
	return ErrUnavailable
}

// DiscoverPrior runs only in G. Its result includes a cross-person prior-cookie
// owner, while a foreign environment's cookie cannot select a mutation target.
func (s *Service) DiscoverPrior(ctx context.Context, a *aw.Attempt, request WriterRequest, action wp.Action) ([]aw.Row, error) {
	d, err := s.writerRequest(ctx, request)
	if err != nil {
		return nil, writerFailure(a, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.discovered || action < wp.Register || action > wp.Assurance || (d.contextOnly && d.action != action) {
		return nil, writerFailure(a, ErrUnavailable)
	}
	tx, err := a.DiscoveryTx(ctx)
	if err != nil {
		return nil, writerFailure(a, ErrUnavailable)
	}
	d.discovered = true
	d.attempt = a
	d.action = action
	rows := []aw.Row{}
	if d.contextOnly {
		err = tx.QueryRowContext(ctx, `SELECT id,person_id FROM public.identity_sessions WHERE id=$1 AND person_id=$2 AND installation_id=$3 AND application_id=$4 AND environment_id=$5`, d.admission.session, d.admission.principal.PersonID(), s.cfg.InstallationID, s.cfg.ApplicationID, s.cfg.EnvironmentID).Scan(&d.priorID, &d.priorPerson)
	} else if len(d.digest) == 32 {
		err = tx.QueryRowContext(ctx, `SELECT id,person_id FROM public.identity_sessions WHERE token_digest=$1 AND installation_id=$2 AND application_id=$3 AND environment_id=$4 AND revoked_at IS NULL`, d.digest, s.cfg.InstallationID, s.cfg.ApplicationID, s.cfg.EnvironmentID).Scan(&d.priorID, &d.priorPerson)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if d.contextOnly {
			return nil, writerFailure(a, ErrUnauthenticated)
		}
		d.priorID = uuid.Nil
		d.priorPerson = uuid.Nil
	} else if err != nil {
		return nil, writerFailure(a, ErrUnavailable)
	}
	if d.priorID != uuid.Nil {
		rows = append(rows, aw.Row{Table: aw.Persons, ID: d.priorPerson, Access: aw.ExistingUpdate}, aw.Row{Table: aw.Sessions, ID: d.priorID, Access: aw.ExistingUpdate})
	}
	if issuing(action) {
		if d.contextOnly {
			return nil, writerFailure(a, ErrUnavailable)
		}
		raw := make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			return nil, writerFailure(a, ErrUnavailable)
		}
		d.newID, err = store.NewID()
		if err != nil {
			return nil, writerFailure(a, ErrUnavailable)
		}
		d.token = base64.RawURLEncoding.EncodeToString(raw)
		d.csrfToken = csrf(raw)
		rows = append(rows, aw.Row{Table: aw.Sessions, ID: d.newID, Access: aw.ReservedInsert})
	}
	return rows, nil
}
func (s *Service) StageWriter(ctx context.Context, a *aw.Attempt, proof wp.Issuance, request WriterRequest) (Staged, error) {
	d, err := s.writerRequest(ctx, request)
	if err != nil {
		return Staged{}, writerFailure(a, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.contextOnly || !d.discovered || d.staged || d.attempt != a || !issuing(d.action) || !validID(d.newID) {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	if err = proof.CheckAction(a, d.action); err != nil {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	credential, err := proof.Credential(a)
	if err != nil {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	if credential.InstallationID() != s.cfg.InstallationID || credential.ApplicationID() != s.cfg.ApplicationID || credential.EnvironmentID() != s.cfg.EnvironmentID {
		return Staged{}, writerFailure(a, ErrUnauthenticated)
	}
	st, err := store.NewIssuer(a, proof)
	if err != nil {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	rows := []aw.Row{{Table: aw.Persons, ID: credential.PersonID(), Access: aw.ExistingUpdate}, {Table: aw.Sessions, ID: d.newID, Access: aw.ReservedInsert}}
	if d.priorID != uuid.Nil {
		rows = append(rows, aw.Row{Table: aw.Sessions, ID: d.priorID, Access: aw.ExistingUpdate})
		if d.priorPerson != credential.PersonID() {
			rows = append(rows, aw.Row{Table: aw.Persons, ID: d.priorPerson, Access: aw.ExistingUpdate})
		}
	}
	tx, err := a.ParticipantTx(ctx, aw.S, rows)
	if err != nil {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now); err != nil {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	if now.Before(credential.AuthenticatedAt()) || !now.Before(credential.AssuranceExpires()) {
		return Staged{}, writerFailure(a, ErrUnauthenticated)
	}
	expires := credential.AuthenticatedAt().Add(maxAge)
	if credential.Assurance() == "aal1" && credential.AssuranceExpires().Before(expires) {
		expires = credential.AssuranceExpires()
	}
	if credential.Assurance() != "aal1" && (!s.cfg.PersistAssurance || credential.AssuranceExpires().After(credential.AuthenticatedAt().Add(15*time.Minute))) {
		return Staged{}, writerFailure(a, ErrUnauthenticated)
	}
	if d.priorID != uuid.Nil {
		// The exact prior row was discovered and acquired. Any participant
		// failure is terminal, including a semantic disappearance/revocation.
		err = st.RevokeSessionScoped(ctx, d.digest, store.SessionScope{InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID})
		if err != nil {
			return Staged{}, terminalStoreError(err)
		}
	}
	digest := sha256.Sum256([]byte(d.token))
	err = st.CreateSession(ctx, store.Session{ID: d.newID, PersonID: credential.PersonID(), InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EnvironmentID: s.cfg.EnvironmentID, TokenDigest: digest[:], SecurityEpoch: credential.SecurityEpoch(), AuthenticationMethod: credential.Method(), AuthenticatedAt: credential.AuthenticatedAt(), ExpiresAt: expires, AssuranceLevel: credential.Assurance(), AssuranceExpires: credential.AssuranceExpires()})
	if err != nil {
		return Staged{}, terminalStoreError(err)
	}
	binding, err := a.Binding()
	if err != nil {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	inserted, _, err := s.readCurrent(ctx, tx, credential.PersonID(), d.newID, false)
	if err != nil {
		return Staged{}, writerFailure(a, err)
	}
	if inserted.person != credential.PersonID() || inserted.epoch != credential.SecurityEpoch() || inserted.method != credential.Method() || !inserted.authenticated.Equal(credential.AuthenticatedAt()) || !inserted.absolute.Equal(expires) || !inserted.idle.Equal(minTime(expires, credential.AuthenticatedAt().Add(30*time.Minute))) || !bytes.Equal(inserted.digest, digest[:]) || inserted.level != credential.Assurance() || (inserted.level != "aal1" && (!inserted.elevated.Valid || !inserted.elevated.Time.Equal(credential.AssuranceExpires()))) {
		return Staged{}, writerFailure(a, ErrUnavailable)
	}
	var rotated currentRow
	if d.priorID != uuid.Nil {
		rotated, _, err = s.readCurrent(ctx, tx, d.priorPerson, d.priorID, false)
		if !errors.Is(err, ErrUnauthenticated) || !rotated.revoked.Valid {
			return Staged{}, writerFailure(a, ErrUnavailable)
		}
	}
	d.staged = true
	return Staged{data: &stagedData{service: s, attempt: a, binding: binding, issuance: proof, tx: tx, request: request, newID: d.newID, priorID: d.priorID, inserted: inserted, rotated: rotated, issued: Issued{Cookie: s.cookie(d.token, expires), CSRFToken: d.csrfToken, AssuranceExpires: credential.AssuranceExpires()}}}, nil
}
func (s *Service) PublishWriter(c aw.Completion, p wp.Permit, staged Staged) (Issued, error) {
	d := staged.data
	if d == nil {
		return Issued{}, ErrUnavailable
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.finalOK || s == nil || d.service != s || !p.Matches(d.attempt, d.issuance) || !c.Matches(d.attempt) {
		return Issued{}, ErrUnavailable
	}
	outcome, err := p.Outcome()
	if err != nil || outcome != aw.Success {
		return Issued{}, ErrUnavailable
	}
	release, err := c.TakeRelease(d.binding)
	if err != nil || !p.MatchesRelease(release) {
		return Issued{}, ErrUnavailable
	}
	result := d.issued
	cookie := *result.Cookie
	result.Cookie = &cookie
	return result, nil
}
