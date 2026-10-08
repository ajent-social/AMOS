// Package writerproof holds closed native identity evidence for one W1 attempt.
// Factories are trusted identity-internal boundaries; snapshots do not attest
// that arbitrary callers actually verified a credential or reread final rows.
package writerproof

import (
	"errors"
	"sync"
	"time"

	"github.com/ajent-social/amos/identity/internal/authproof"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("writer evidence unavailable")
	ErrDenied      = errors.New("writer evidence denied")
)

type Action uint8

const (
	Register Action = iota + 1
	PasswordSignIn
	EmailRequest
	EmailConfirm
	ResetRequest
	ResetComplete
	PasswordChange
	MagicRequest
	MagicConfirm
	MFABegin
	MFAConfirm
	MFAChallenge
	FederationBeginLogin
	FederationBeginLink
	FederationCallbackLogin
	FederationCallbackLink
	Renew
	Signout
	Revoke
	Assurance
)

type Subject struct {
	Person uuid.UUID
	Realm  aw.Realm
	Epoch  int64
}
type Contact struct {
	ID            uuid.UUID
	ComparisonKey string
	VerifiedAt    time.Time
}
type PasswordCheck struct {
	Subject                  Subject
	Contact                  Contact
	CredentialID             uuid.UUID
	VerifiedHash, StagedHash string
	VerifiedAt, ValidUntil   time.Time
}
type ChallengeCheck struct {
	Subject                          Subject
	Contact                          Contact
	ID                               uuid.UUID
	TokenDigest, BrowserDigest       [32]byte
	CreatedAt, ExpiresAt, VerifiedAt time.Time
	DifferentDeviceConfirmed         bool
}
type ActorCheck struct {
	Subject                                   Subject
	SessionID                                 uuid.UUID
	Digest                                    [32]byte
	Method                                    string
	AuthenticatedAt, IdleUntil, AbsoluteUntil time.Time
	Assurance                                 string
	AssuranceUntil                            time.Time
}
type FactorCheck struct {
	ID                                                 uuid.UUID
	State                                              string
	Epoch, LastStep, AcceptedStep                      int64
	CiphertextDigest                                   [32]byte
	FailedAttempts                                     int
	LockedUntil, PendingUntil, VerifiedAt, WindowUntil time.Time
}
type FlowCheck struct {
	ID, ConnectionID                                                  uuid.UUID
	Realm                                                             aw.Realm
	Provider, Issuer, Subject, ReturnTo                               string
	CallbackURL, CodeChallenge, CodeChallengeMethod, AuthorizationURL string
	StateDigest, BrowserDigest, NonceDigest                           [32]byte
	CreatedAt, ExpiresAt                                              time.Time
}
type ProviderCheck struct {
	ConnectionID                                                       uuid.UUID
	Provider, Issuer, Subject                                          string
	NonceDigest                                                        [32]byte
	ValidationStartedAt, ValidatedAt, ProviderValidUntil, ReceiptUntil time.Time
}
type CounterReason uint8

const (
	BadCode CounterReason = iota + 1
	Replay
)

type CounterTransition struct {
	FactorID                                       uuid.UUID
	Reason                                         CounterReason
	BeforeAttempts, AfterAttempts                  int
	BeforeLockedUntil, AfterLockedUntil, UpdatedAt time.Time
}
type Evidence struct{ data *evidenceData }
type Issuance struct{ data *issuanceData }
type Permit struct{ data *finalizationData }
type evidenceKind uint8

const (
	passwordKind evidenceKind = iota + 1
	challengeKind
	actorKind
	enrollmentKind
	totpKind
	flowKind
	providerKind
	counterKind
)

type evidenceData struct {
	mu            sync.Mutex
	binding       aw.Binding
	started       time.Time
	action        Action
	kind          evidenceKind
	subject       Subject
	passwordCheck PasswordCheck
	challenge     ChallengeCheck
	actor         ActorCheck
	factor        FactorCheck
	flow          FlowCheck
	provider      ProviderCheck
	counter       CounterTransition
	primary       *evidenceData
	issued        *issuanceData
	finalized     bool
}
type issuanceData struct {
	evidence           *evidenceData
	verifiedCredential authproof.VerifiedCredential
	consumed           bool // guarded by evidence.mu; copying Issuance never resets it
}
type finalizationData struct {
	binding   aw.Binding
	action    Action
	outcome   aw.Outcome
	finalTime time.Time
	issuance  *issuanceData
}

func validID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }
func nonzero(d [32]byte) bool   { return d != [32]byte{} }
func validSubject(s Subject) bool {
	return validID(s.Person) && validID(s.Realm.Installation) && validID(s.Realm.Application) && validID(s.Realm.Environment) && s.Epoch >= 0
}
func method(m string) bool {
	switch m {
	case "email_password", "email_magic_link", "google", "github", "apple", "passkey", "enterprise_oidc":
		return true
	default:
		return false
	}
}
func realm(a *aw.Attempt, want aw.Realm) (aw.Binding, time.Time, error) {
	r, err := a.Realm()
	if err != nil || r != want {
		return aw.Binding{}, time.Time{}, ErrUnavailable
	}
	b, err := a.Binding()
	if err != nil {
		return aw.Binding{}, time.Time{}, ErrUnavailable
	}
	start, err := a.StartedAt()
	if err != nil || start.IsZero() {
		return aw.Binding{}, time.Time{}, ErrUnavailable
	}
	return b, start, nil
}
func newEvidence(a *aw.Attempt, action Action, kind evidenceKind, subject Subject) (*evidenceData, error) {
	if action < Register || action > Assurance || !validSubject(subject) {
		return nil, ErrUnavailable
	}
	b, start, err := realm(a, subject.Realm)
	if err != nil {
		return nil, err
	}
	return &evidenceData{binding: b, started: start, action: action, kind: kind, subject: subject}, nil
}
func checkRows(a *aw.Attempt, phase aw.Phase, rows ...aw.Row) error {
	if err := a.CheckRows(phase, rows); err != nil {
		return ErrUnavailable
	}
	return nil
}
func personRow(s Subject) aw.Row {
	return aw.Row{Table: aw.Persons, ID: s.Person, Access: aw.ExistingUpdate}
}
func same(a, b Subject) bool { return a == b }

func Password(a *aw.Attempt, action Action, check PasswordCheck) (Evidence, error) {
	switch action {
	case PasswordSignIn, PasswordChange, MFABegin, MFAConfirm, MFAChallenge:
	default:
		return Evidence{}, ErrUnavailable
	}
	d, err := newEvidence(a, action, passwordKind, check.Subject)
	if err != nil {
		return Evidence{}, err
	}
	if !validContact(check.Contact, true) || !validID(check.CredentialID) || check.VerifiedHash == "" || len(check.VerifiedHash) > 256 || len(check.StagedHash) > 256 || check.VerifiedAt.IsZero() || check.VerifiedAt.Before(d.started) || check.Contact.VerifiedAt.After(check.VerifiedAt) || !check.ValidUntil.Equal(check.VerifiedAt.Add(15*time.Minute)) {
		return Evidence{}, ErrUnavailable
	}
	if err := checkRows(a, aw.C, personRow(check.Subject), aw.Row{Table: aw.Emails, ID: check.Contact.ID, Access: aw.ExistingUpdate}, aw.Row{Table: aw.Credentials, ID: check.CredentialID, Access: aw.ExistingUpdate}); err != nil {
		return Evidence{}, err
	}
	d.passwordCheck = check
	return Evidence{data: d}, nil
}
func validContact(c Contact, verified bool) bool {
	return validID(c.ID) && c.ComparisonKey != "" && len(c.ComparisonKey) <= 320 && (!verified || !c.VerifiedAt.IsZero())
}
func Challenge(a *aw.Attempt, action Action, check ChallengeCheck) (Evidence, error) {
	var inserting, verified bool
	switch action {
	case Register, EmailRequest:
		inserting = true
	case ResetRequest, MagicRequest:
		inserting = true
		verified = true
	case EmailConfirm:
	case ResetComplete, MagicConfirm:
		verified = true
	default:
		return Evidence{}, ErrUnavailable
	}
	d, err := newEvidence(a, action, challengeKind, check.Subject)
	if err != nil {
		return Evidence{}, err
	}
	if !validContact(check.Contact, verified) || !validID(check.ID) || !nonzero(check.TokenDigest) || check.CreatedAt.IsZero() || check.ExpiresAt.IsZero() || !check.ExpiresAt.After(check.CreatedAt) || check.VerifiedAt.IsZero() || check.VerifiedAt.Before(d.started) || check.VerifiedAt.Before(check.CreatedAt) || check.Contact.VerifiedAt.After(check.VerifiedAt) {
		return Evidence{}, ErrUnavailable
	}
	if action == EmailRequest && check.CreatedAt.Before(d.started) {
		inserting = false
	}
	if inserting && !check.CreatedAt.Equal(d.started) || !inserting && check.CreatedAt.After(d.started) || action == Register && !check.Contact.VerifiedAt.IsZero() {
		return Evidence{}, ErrUnavailable
	}
	if inserting {
		ttl := check.ExpiresAt.Sub(check.CreatedAt)
		maximum := 24 * time.Hour
		if action == MagicRequest {
			maximum = 30 * time.Minute
		}
		if ttl < time.Minute || ttl > maximum || ttl%time.Second != 0 || action == Register && ttl != 30*time.Minute {
			return Evidence{}, ErrUnavailable
		}
	}
	if (action == MagicRequest || action == MagicConfirm) && !nonzero(check.BrowserDigest) {
		return Evidence{}, ErrUnavailable
	}
	if action != MagicConfirm && check.DifferentDeviceConfirmed {
		return Evidence{}, ErrUnavailable
	}
	rows := []aw.Row{personRow(check.Subject), {Table: aw.Emails, ID: check.Contact.ID, Access: aw.ExistingUpdate}, {Table: aw.Challenges, ID: check.ID, Access: aw.ExistingUpdate}}
	if inserting {
		rows[2].Access = aw.ReservedInsert
	}
	if action == Register {
		rows[0].Access = aw.ReservedInsert
		rows[1].Access = aw.ReservedInsert
	}
	if err := checkRows(a, aw.H, rows...); err != nil {
		return Evidence{}, err
	}
	d.challenge = check
	return Evidence{data: d}, nil
}

func actorShape(c ActorCheck) bool {
	if !validSubject(c.Subject) || !validID(c.SessionID) || !nonzero(c.Digest) || !method(c.Method) || c.AuthenticatedAt.IsZero() || !c.IdleUntil.After(c.AuthenticatedAt) || c.AbsoluteUntil.Before(c.IdleUntil) {
		return false
	}
	switch c.Assurance {
	case "aal1":
		return c.AssuranceUntil.Equal(c.AbsoluteUntil)
	case "aal2", "aal3":
		return c.AssuranceUntil.After(c.AuthenticatedAt) && !c.AssuranceUntil.After(c.AbsoluteUntil)
	default:
		return false
	}
}
func actorRows(a *aw.Attempt, c ActorCheck) error {
	if !actorShape(c) {
		return ErrUnavailable
	}
	return checkRows(a, aw.S, personRow(c.Subject), aw.Row{Table: aw.Sessions, ID: c.SessionID, Access: aw.ExistingUpdate})
}
func Actor(a *aw.Attempt, action Action, check ActorCheck) (Evidence, error) {
	switch action {
	case Renew, Signout, Revoke, Assurance, PasswordChange:
	default:
		return Evidence{}, ErrUnavailable
	}
	d, err := newEvidence(a, action, actorKind, check.Subject)
	if err != nil {
		return Evidence{}, err
	}
	if err = actorRows(a, check); err != nil || check.AuthenticatedAt.After(d.started) {
		return Evidence{}, ErrUnavailable
	}
	d.actor = check
	return Evidence{data: d}, nil
}
