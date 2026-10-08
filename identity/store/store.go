// Package store persists AMOS identity state in an existing PostgreSQL
// transaction. Callers must use storage.DB.WithTx so multi-row operations
// either commit together or leave no usable partial identity.
package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidInput            = errors.New("invalid identity store input")
	ErrIdentifierConflict      = errors.New("identity identifier already exists")
	ErrEmailAlreadyUsed        = errors.New("email address is already in use")
	ErrExternalIdentityBound   = errors.New("external identity is already bound")
	ErrCredentialAlreadyExists = errors.New("credential already exists")
	ErrChallengeUnavailable    = errors.New("challenge is unavailable")
	ErrSessionUnavailable      = errors.New("session is unavailable")
	ErrPersonUnavailable       = errors.New("person is unavailable")
	ErrPersistence             = errors.New("identity persistence failed")
)

const (
	AccountPendingVerification = "pending_verification"
	ChallengeEmailVerification = "email_verification"
	ChallengePasswordReset     = "password_reset"
	ChallengeEmailMagicLink    = "email_magic_link"
	ChallengeEmailChange       = "email_change"
)

// Store is scoped to one caller-owned transaction. It is intentionally not
// safe to retain after that transaction commits or rolls back.
type Store struct {
	tx *sql.Tx
}

// New binds identity operations to an existing transaction.
func New(tx *sql.Tx) (*Store, error) {
	if tx == nil {
		return nil, ErrInvalidInput
	}
	return &Store{tx: tx}, nil
}

// NewID returns a canonical RFC UUIDv7 ID for trusted application use.
func NewID() (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate identity ID: %w", err)
	}
	if !validID(id) {
		return uuid.Nil, ErrInvalidInput
	}
	return id, nil
}

// PendingAccount contains only a password verifier and digests of opaque
// challenge tokens. It must never receive a raw password or raw token.
type PendingAccount struct {
	PersonID        uuid.UUID
	EmailID         uuid.UUID
	CredentialID    uuid.UUID
	ChallengeID     uuid.UUID
	InstallationID  uuid.UUID
	ApplicationID   uuid.UUID
	EmailAddress    string
	PasswordHash    string
	ChallengeDigest []byte
	ChallengeExpiry time.Time
}

// CreatePendingAccount inserts a pending person, an unverified contact, a
// password verifier, and an email-verification challenge in the caller's
// transaction. Any conflict/error must be returned from the WithTx callback
// to roll the entire operation back.
func (s *Store) CreatePendingAccount(ctx context.Context, input PendingAccount) error {
	if s == nil || s.tx == nil || ctx == nil || !validID(input.PersonID) ||
		!validID(input.EmailID) || !validID(input.CredentialID) ||
		!validID(input.ChallengeID) || !validID(input.InstallationID) ||
		!validID(input.ApplicationID) || len(input.ChallengeDigest) != 32 ||
		!validArgon2idHash(input.PasswordHash) {
		return ErrInvalidInput
	}
	comparisonKey, err := normalizeEmail(input.EmailAddress)
	if err != nil {
		return err
	}

	_, err = s.tx.ExecContext(ctx, `
		INSERT INTO identity_persons (id, installation_id, application_id, state)
		VALUES ($1, $2, $3, $4)`,
		input.PersonID, input.InstallationID, input.ApplicationID, AccountPendingVerification)
	if err != nil {
		return mapConflict(err, "identity_persons_pkey", ErrIdentifierConflict)
	}

	_, err = s.tx.ExecContext(ctx, `
		INSERT INTO identity_emails (id, person_id, installation_id, application_id, display_address, comparison_key)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		input.EmailID, input.PersonID, input.InstallationID, input.ApplicationID, input.EmailAddress, comparisonKey)
	if err != nil {
		return mapConflict(err, "identity_emails_scope_address_key", ErrEmailAlreadyUsed)
	}

	_, err = s.tx.ExecContext(ctx, `
		INSERT INTO identity_credentials (id, person_id, method, verifier_hash)
		VALUES ($1, $2, 'email_password', $3)`, input.CredentialID, input.PersonID, input.PasswordHash)
	if err != nil {
		return mapConflict(err, "identity_credentials_person_method_key", ErrCredentialAlreadyExists)
	}

	result, err := s.tx.ExecContext(ctx, `
		INSERT INTO identity_challenges (id, person_id, email_id, purpose, token_digest, expires_at)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE $6 > transaction_timestamp()`,
		input.ChallengeID, input.PersonID, input.EmailID, ChallengeEmailVerification,
		input.ChallengeDigest, input.ChallengeExpiry)
	if err != nil {
		return mapConflict(err, "identity_challenges_token_digest_key", ErrIdentifierConflict)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if inserted != 1 {
		return ErrInvalidInput
	}
	return nil
}

// ExternalBinding is an already-verified provider result. This store does not
// perform provider verification or link by email; issuer and subject are
// stored and compared exactly.
type ExternalBinding struct {
	ID                   uuid.UUID
	PersonID             uuid.UUID
	ProviderConnectionID uuid.UUID
	Provider             string
	Issuer               string
	Subject              string
}

// LinkExternalIdentity binds one exact provider connection, issuer, and
// verified subject to a person. Duplicate bindings have a stable conflict.
func (s *Store) LinkExternalIdentity(ctx context.Context, binding ExternalBinding) error {
	if s == nil || s.tx == nil || ctx == nil || !validID(binding.ID) ||
		!validID(binding.PersonID) || !validID(binding.ProviderConnectionID) ||
		!validTokenPart(binding.Provider) || !validTokenPart(binding.Issuer) ||
		!validTokenPart(binding.Subject) {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, `
		INSERT INTO identity_external_bindings
			(id, person_id, provider_connection_id, provider, issuer, subject)
		SELECT $1, p.id, $3, $4, $5, $6
		FROM identity_persons p
		WHERE p.id = $2 AND p.state = 'active'`, binding.ID, binding.PersonID,
		binding.ProviderConnectionID, binding.Provider, binding.Issuer, binding.Subject)
	if err != nil {
		return mapConflict(err, "identity_external_bindings_subject_key", ErrExternalIdentityBound)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if rows != 1 {
		return ErrPersonUnavailable
	}
	return nil
}

// AdvanceSecurityEpoch invalidates sessions and grants that carry an earlier
// epoch. It never decrements or accepts a caller-supplied new value.
func (s *Store) AdvanceSecurityEpoch(ctx context.Context, personID uuid.UUID) (int64, error) {
	if s == nil || s.tx == nil || ctx == nil || !validID(personID) {
		return 0, ErrInvalidInput
	}
	var epoch int64
	err := s.tx.QueryRowContext(ctx, `
		UPDATE identity_persons
		SET security_epoch = security_epoch + 1, updated_at = transaction_timestamp()
		WHERE id = $1 AND security_epoch < 9223372036854775807
		RETURNING security_epoch`, personID).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPersonUnavailable
	}
	if err != nil {
		return 0, ErrPersistence
	}
	return epoch, nil
}

// Session contains only a digest of the browser token. The raw token belongs
// to the caller and must not be passed to or logged by the store.
type Session struct {
	ID                   uuid.UUID
	PersonID             uuid.UUID
	InstallationID       uuid.UUID
	ApplicationID        uuid.UUID
	EnvironmentID        uuid.UUID
	TokenDigest          []byte
	SecurityEpoch        int64
	AuthenticationMethod string
	AuthenticatedAt      time.Time
	ExpiresAt            time.Time
}

// AuthenticatedSession is returned only after the store has checked the
// current person state, session expiry/revocation, and epoch equality in SQL.
type AuthenticatedSession struct {
	ID                   uuid.UUID
	PersonID             uuid.UUID
	InstallationID       uuid.UUID
	ApplicationID        uuid.UUID
	EnvironmentID        uuid.UUID
	SecurityEpoch        int64
	AuthenticationMethod string
	AuthenticatedAt      time.Time
}

// SessionScope is supplied by trusted request composition, never decoded
// from an untrusted header or token alone.
type SessionScope struct {
	InstallationID uuid.UUID
	ApplicationID  uuid.UUID
	EnvironmentID  uuid.UUID
}

// CreateSession persists a session only when the person is active and the
// supplied epoch still matches the current account epoch. Expiry is bounded
// by the identity policy's 12-hour absolute lifetime.
func (s *Store) CreateSession(ctx context.Context, session Session) error {
	if s == nil || s.tx == nil || ctx == nil || !validID(session.ID) ||
		!validID(session.PersonID) || !validID(session.InstallationID) ||
		!validID(session.ApplicationID) || !validID(session.EnvironmentID) ||
		len(session.TokenDigest) != 32 || session.AuthenticatedAt.IsZero() ||
		session.SecurityEpoch < 0 || !validAuthenticationMethod(session.AuthenticationMethod) {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, `
		WITH issuance_clock AS MATERIALIZED (SELECT clock_timestamp() AS now)
		INSERT INTO identity_sessions
			(id, person_id, installation_id, application_id, environment_id,
			 token_digest, security_epoch, authentication_method, authenticated_at,
			 expires_at, idle_expires_at, issued_at, last_seen_at)
		SELECT $1, p.id, p.installation_id, p.application_id, $5,
			$6, p.security_epoch, $8, $9, $7,
			LEAST($7, issuance_clock.now + interval '30 minutes'), issuance_clock.now, issuance_clock.now
		FROM identity_persons p CROSS JOIN issuance_clock
		WHERE p.id = $2 AND p.installation_id = $3 AND p.application_id = $4
		  AND p.state = 'active' AND p.security_epoch = $10
		  AND $7 > issuance_clock.now
		  AND $7 <= issuance_clock.now + interval '12 hours'
		  AND $9 <= issuance_clock.now`,
		session.ID, session.PersonID, session.InstallationID, session.ApplicationID,
		session.EnvironmentID, session.TokenDigest, session.ExpiresAt,
		session.AuthenticationMethod, session.AuthenticatedAt, session.SecurityEpoch)
	if err != nil {
		return mapConflict(err, "identity_sessions_token_digest_key", ErrIdentifierConflict)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if rows != 1 {
		return ErrSessionUnavailable
	}
	return nil
}

// RevokeSession revokes the digest-addressed session once. It does not reveal
// whether a missing digest belonged to another person.
func (s *Store) RevokeSession(ctx context.Context, tokenDigest []byte) error {
	if s == nil || s.tx == nil || ctx == nil || len(tokenDigest) != 32 {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, `
		UPDATE identity_sessions
		SET revoked_at = transaction_timestamp()
		WHERE token_digest = $1 AND revoked_at IS NULL`, tokenDigest)
	if err != nil {
		return ErrPersistence
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if rows != 1 {
		return ErrSessionUnavailable
	}
	return nil
}

// FindActiveSession rechecks the authoritative account state and security
// epoch at READ COMMITTED after locking the scoped session row. The database
// clock is sampled only after that lock completes; no person writer ordering
// is implied. A stale session and an unknown token have the same result.
func (s *Store) FindActiveSession(ctx context.Context, tokenDigest []byte, scope SessionScope) (AuthenticatedSession, error) {
	if s == nil || s.tx == nil || ctx == nil || len(tokenDigest) != 32 ||
		!validID(scope.InstallationID) || !validID(scope.ApplicationID) || !validID(scope.EnvironmentID) {
		return AuthenticatedSession{}, ErrInvalidInput
	}
	var isolation string
	if err := s.tx.QueryRowContext(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil || isolation != "read committed" {
		return AuthenticatedSession{}, ErrPersistence
	}
	var lockedID uuid.UUID
	err := s.tx.QueryRowContext(ctx, `SELECT id FROM identity_sessions
		WHERE token_digest=$1 AND installation_id=$2 AND application_id=$3 AND environment_id=$4
		FOR UPDATE`, tokenDigest, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthenticatedSession{}, ErrSessionUnavailable
	}
	if err != nil {
		return AuthenticatedSession{}, ErrPersistence
	}
	var now time.Time
	if err := s.tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return AuthenticatedSession{}, ErrPersistence
	}
	var session AuthenticatedSession
	err = s.tx.QueryRowContext(ctx, `
		UPDATE identity_sessions s
		SET last_seen_at = $5::timestamptz,
		    idle_expires_at = LEAST(s.expires_at, $5::timestamptz + interval '30 minutes')
		FROM identity_persons p
		WHERE s.token_digest = $1 AND s.person_id = p.id
		  AND s.revoked_at IS NULL AND s.expires_at > $5::timestamptz
		  AND s.idle_expires_at > $5::timestamptz
		  AND p.state = 'active' AND p.security_epoch = s.security_epoch
		  AND s.installation_id = $2 AND s.application_id = $3 AND s.environment_id = $4
		  AND p.installation_id = s.installation_id
		  AND p.application_id = s.application_id
		RETURNING s.id, s.person_id, s.installation_id, s.application_id,
		          s.environment_id, s.security_epoch, s.authentication_method,
		          s.authenticated_at`, tokenDigest, scope.InstallationID,
		scope.ApplicationID, scope.EnvironmentID, now).Scan(
		&session.ID, &session.PersonID, &session.InstallationID, &session.ApplicationID,
		&session.EnvironmentID, &session.SecurityEpoch, &session.AuthenticationMethod,
		&session.AuthenticatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthenticatedSession{}, ErrSessionUnavailable
	}
	if err != nil {
		return AuthenticatedSession{}, ErrPersistence
	}
	return session, nil
}

// Challenge contains a purpose-bound opaque challenge digest and expiry.
type Challenge struct {
	ID        uuid.UUID
	PersonID  uuid.UUID
	EmailID   uuid.UUID
	Purpose   string
	Digest    []byte
	ExpiresAt time.Time
}

// CreateChallenge stores only a digest. The caller generates and delivers the
// raw random secret; it is never persisted by this method.
func (s *Store) CreateChallenge(ctx context.Context, challenge Challenge) error {
	if s == nil || s.tx == nil || ctx == nil || !validID(challenge.ID) ||
		!validID(challenge.PersonID) || !validID(challenge.EmailID) ||
		len(challenge.Digest) != 32 || !validChallengePurpose(challenge.Purpose) {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, `
		INSERT INTO identity_challenges (id, person_id, email_id, purpose, token_digest, expires_at)
		SELECT $1, e.person_id, e.id, $4, $5, $6
		FROM identity_emails e
		WHERE e.id = $3 AND e.person_id = $2 AND $6 > transaction_timestamp()`,
		challenge.ID, challenge.PersonID, challenge.EmailID, challenge.Purpose,
		challenge.Digest, challenge.ExpiresAt)
	if err != nil {
		return mapConflict(err, "identity_challenges_token_digest_key", ErrIdentifierConflict)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if rows != 1 {
		return ErrChallengeUnavailable
	}
	return nil
}

// ConsumedChallenge identifies the bound person and contact after an atomic
// successful consume. It contains no secret or provider response.
type ConsumedChallenge struct {
	PersonID uuid.UUID
	EmailID  uuid.UUID
}

// ConsumeChallenge atomically consumes a matching, unexpired, purpose-bound
// digest exactly once. The database clock is authoritative for expiry.
func (s *Store) ConsumeChallenge(ctx context.Context, id uuid.UUID, purpose string, digest []byte) (ConsumedChallenge, error) {
	if s == nil || s.tx == nil || ctx == nil || !validID(id) ||
		!validChallengePurpose(purpose) || len(digest) != 32 {
		return ConsumedChallenge{}, ErrInvalidInput
	}
	var consumed ConsumedChallenge
	err := s.tx.QueryRowContext(ctx, `
		UPDATE identity_challenges
		SET consumed_at = transaction_timestamp()
		WHERE id = $1 AND purpose = $2 AND token_digest = $3
		  AND consumed_at IS NULL AND expires_at > transaction_timestamp()
		RETURNING person_id, email_id`, id, purpose, digest).Scan(&consumed.PersonID, &consumed.EmailID)
	if errors.Is(err, sql.ErrNoRows) {
		return ConsumedChallenge{}, ErrChallengeUnavailable
	}
	if err != nil {
		return ConsumedChallenge{}, ErrPersistence
	}
	return consumed, nil
}

// MarkEmailVerified records a contact assertion after its proof challenge has
// been consumed in the same transaction.
func (s *Store) MarkEmailVerified(ctx context.Context, personID, emailID uuid.UUID) error {
	if s == nil || s.tx == nil || ctx == nil || !validID(personID) || !validID(emailID) {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, `
		UPDATE identity_emails SET verified_at = transaction_timestamp()
		WHERE id = $1 AND person_id = $2 AND verified_at IS NULL`, emailID, personID)
	if err != nil {
		return ErrPersistence
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if rows != 1 {
		return ErrPersonUnavailable
	}
	return nil
}

func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == strings.ToLower(id.String())
}

func validArgon2idHash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" ||
		parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=1" {
		return false
	}
	salt, errSalt := base64.RawStdEncoding.DecodeString(parts[4])
	digest, errDigest := base64.RawStdEncoding.DecodeString(parts[5])
	return errSalt == nil && errDigest == nil && len(salt) == 16 && len(digest) == 32 &&
		base64.RawStdEncoding.EncodeToString(salt) == parts[4] &&
		base64.RawStdEncoding.EncodeToString(digest) == parts[5]
}

func normalizeEmail(address string) (string, error) {
	if address == "" || !utf8.ValidString(address) {
		return "", ErrInvalidInput
	}
	for _, r := range address {
		if r > 0x7f || r < 0x21 || r == 0x7f {
			return "", ErrInvalidInput
		}
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Name != "" || parsed.Address != address ||
		strings.Count(address, "@") != 1 || strings.ContainsAny(address, "<>(),;:\\\"[]") {
		return "", ErrInvalidInput
	}
	return strings.ToLower(address), nil
}

func validTokenPart(value string) bool {
	if value == "" || len(value) > 2048 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return true
}

func validChallengePurpose(purpose string) bool {
	switch purpose {
	case ChallengeEmailVerification, ChallengePasswordReset, ChallengeEmailMagicLink, ChallengeEmailChange:
		return true
	default:
		return false
	}
}

func validAuthenticationMethod(method string) bool {
	switch method {
	case "email_password", "email_magic_link", "google", "github", "apple", "passkey", "enterprise_oidc", "password+totp":
		return true
	default:
		return false
	}
}

func mapConflict(err error, expectedConstraint string, conflict error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == expectedConstraint {
			return conflict
		}
		return ErrIdentifierConflict
	}
	return ErrPersistence
}
