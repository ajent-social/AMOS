package recovery

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
	"reflect"
	"time"
)

func NewWithWriter(root *aw.Root, sessions *session.Service, outbox *sqlstore.TxWriter, materials *materialstore.Writer, cfg TxConfig) (*Service, error) {
	if root == nil || sessions == nil || outbox == nil || materials == nil || cfg.Outbox != nil || cfg.Materials != nil {
		return nil, ErrConfiguration
	}
	policy, ok := cfg.Policy.(WriterPolicy)
	if !ok || nilRecoveryDependency(policy) {
		return nil, ErrConfiguration
	}
	s, e := configuredRecovery(cfg)
	if e != nil {
		return nil, e
	}
	s.primary, e = primaryproof.New(cfg.Passwords)
	if e != nil {
		return nil, ErrConfiguration
	}
	s.root = root
	s.sessions = sessions
	s.writerPolicy = policy
	s.writerOutbox = outbox
	s.writerMaterials = materials
	return s, nil
}
func (s *Service) realm() aw.Realm {
	return aw.Realm{Installation: s.cfg.InstallationID, Application: s.cfg.ApplicationID, Environment: s.cfg.EnvironmentID}
}
func recoveryOutcome(e error) aw.Outcome {
	if errors.Is(e, ErrChallengeUnavailable) || errors.Is(e, ErrPolicyDenied) || errors.Is(e, ErrStepUpRequired) || errors.Is(e, password.ErrInvalid) {
		return aw.DeniedRollback
	}
	return aw.UnavailableRollback
}
func recoveryFailure(a *aw.Attempt, e error) error {
	if err := a.Finish(recoveryOutcome(e)); err != nil {
		return ErrUnavailable
	}
	return e
}
func recoveryStoreError(e error) error {
	if errors.Is(e, store.ErrSessionUnavailable) || errors.Is(e, store.ErrPersonUnavailable) || errors.Is(e, store.ErrChallengeUnavailable) {
		return ErrChallengeUnavailable
	}
	return ErrUnavailable
}
func releaseRecovery(c aw.Completion, p wp.Permit, b aw.Binding) error {
	r, e := c.TakeRelease(b)
	if e != nil || !p.MatchesRelease(r) {
		return ErrUnavailable
	}
	out, e := r.Outcome()
	if e != nil || out != aw.Success {
		return ErrUnavailable
	}
	return nil
}

type accountSnapshot struct {
	person, email, credential uuid.UUID
	epoch                     int64
	address, key, hash        string
	verified                  time.Time
}

func (s *Service) account(ctx context.Context, tx *sql.Tx, person, email uuid.UUID, address string) (accountSnapshot, error) {
	var v accountSnapshot
	e := tx.QueryRowContext(ctx, `SELECT p.id,e.id,COALESCE(c.id,'00000000-0000-0000-0000-000000000000'::uuid),p.security_epoch,e.display_address,e.comparison_key,e.verified_at,COALESCE(c.verifier_hash,'') FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id LEFT JOIN identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL
 WHERE p.installation_id=$1 AND p.application_id=$2 AND p.state='active' AND e.installation_id=$1 AND e.application_id=$2 AND e.verified_at IS NOT NULL AND (($3::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND p.id=$3 AND ($4::uuid='00000000-0000-0000-0000-000000000000'::uuid OR e.id=$4)) OR ($3::uuid='00000000-0000-0000-0000-000000000000'::uuid AND e.comparison_key=$5)) ORDER BY e.id LIMIT 1`, s.cfg.InstallationID, s.cfg.ApplicationID, person, email, address).Scan(&v.person, &v.email, &v.credential, &v.epoch, &v.address, &v.key, &v.verified, &v.hash)
	if errors.Is(e, sql.ErrNoRows) {
		return v, ErrChallengeUnavailable
	}
	if e != nil || v.epoch < 0 || v.verified.IsZero() {
		return v, ErrUnavailable
	}
	return v, nil
}
func accountRows(v accountSnapshot) []aw.Row {
	rows := []aw.Row{{Table: aw.Persons, ID: v.person, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: v.email, Access: aw.ExistingUpdate}}
	if v.credential != uuid.Nil {
		rows = append(rows, aw.Row{Table: aw.Credentials, ID: v.credential, Access: aw.ExistingUpdate})
	}
	return rows
}
func sameAccount(a, b accountSnapshot) bool {
	return a.person == b.person && a.email == b.email && a.credential == b.credential && a.epoch == b.epoch && a.address == b.address && a.key == b.key && a.hash == b.hash && a.verified.Equal(b.verified)
}

type resetSnapshot struct {
	id, person, email uuid.UUID
	digest            []byte
	created, expires  time.Time
	consumed          sql.NullTime
}

func (s *Service) reset(ctx context.Context, tx *sql.Tx, id uuid.UUID, digest []byte) (resetSnapshot, error) {
	var v resetSnapshot
	e := tx.QueryRowContext(ctx, `SELECT c.id,c.person_id,c.email_id,c.token_digest,c.created_at,c.expires_at,c.consumed_at FROM identity_challenges c JOIN identity_persons p ON p.id=c.person_id JOIN identity_emails e ON e.id=c.email_id AND e.person_id=p.id WHERE c.id=$1 AND c.purpose=$2 AND c.token_digest=$3 AND p.installation_id=$4 AND p.application_id=$5 AND e.installation_id=$4 AND e.application_id=$5`, id, passwordResetPurpose, digest, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&v.id, &v.person, &v.email, &v.digest, &v.created, &v.expires, &v.consumed)
	if errors.Is(e, sql.ErrNoRows) {
		return v, ErrChallengeUnavailable
	}
	if e != nil || len(v.digest) != 32 || v.created.IsZero() || !v.expires.After(v.created) {
		return v, ErrUnavailable
	}
	return v, nil
}
func sameReset(a, b resetSnapshot) bool {
	return a.id == b.id && a.person == b.person && a.email == b.email && bytes.Equal(a.digest, b.digest) && a.created.Equal(b.created) && a.expires.Equal(b.expires) && a.consumed == b.consumed
}
func (s *Service) previewWriter(ctx context.Context, id uuid.UUID, digest []byte) (bool, error) {
	if ctx == nil || !validID(id) || len(digest) != 32 {
		return false, ErrChallengeUnavailable
	}
	available := false
	e := s.root.Read(ctx, func(ctx context.Context, tx *sql.Tx) error {
		r, e := s.reset(ctx, tx, id, digest)
		if errors.Is(e, ErrChallengeUnavailable) {
			return nil
		}
		if e != nil {
			return e
		}
		if _, e = s.account(ctx, tx, r.person, r.email, ""); errors.Is(e, ErrChallengeUnavailable) {
			return nil
		} else if e != nil {
			return e
		}
		var now time.Time
		if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now); e != nil {
			return ErrUnavailable
		}
		available = !r.consumed.Valid && !now.Before(r.created) && now.Before(r.expires)
		return nil
	})
	if e != nil {
		return false, ErrUnavailable
	}
	return available, nil
}

// Policy discovery is plain and finite under G. Final policy SQL may read only
// these held identities and G-protected absence predicates in the fixed native
// composition; arbitrary business policy implementations remain inadmissible.
func (s *Service) policyRows(ctx context.Context, tx *sql.Tx, person uuid.UUID) ([]aw.Row, error) {
	var result []aw.Row
	for _, query := range []struct {
		table aw.Table
		sql   string
	}{
		{aw.Bindings, `SELECT id FROM identity_external_bindings WHERE person_id=$1 ORDER BY id LIMIT 1025`},
		{aw.Memberships, `SELECT id FROM workspace_memberships WHERE person_id=$1 AND installation_id=$2 AND application_id=$3 ORDER BY id LIMIT 1025`},
		{aw.Workspaces, `SELECT id FROM workspaces WHERE installation_id=$2 AND application_id=$3 AND (personal_owner_id=$1 OR id IN(SELECT workspace_id FROM workspace_memberships WHERE person_id=$1 AND installation_id=$2 AND application_id=$3)) ORDER BY id LIMIT 1025`},
	} {
		args := []any{person}
		if query.table != aw.Bindings {
			args = append(args, s.cfg.InstallationID, s.cfg.ApplicationID)
		}
		rows, e := tx.QueryContext(ctx, query.sql, args...)
		if e != nil {
			return nil, ErrUnavailable
		}
		count := 0
		for rows.Next() {
			var id uuid.UUID
			if e = rows.Scan(&id); e != nil {
				_ = rows.Close()
				return nil, ErrUnavailable
			}
			result = append(result, aw.Row{Table: query.table, ID: id, Access: aw.ExistingUpdate})
			count++
		}
		e = rows.Err()
		closeErr := rows.Close()
		if e != nil || closeErr != nil || count > 1024 {
			return nil, ErrUnavailable
		}
	}
	return result, nil
}
func sessionRows(ctx context.Context, tx *sql.Tx, person uuid.UUID) ([]aw.Row, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id FROM identity_sessions WHERE person_id=$1 AND revoked_at IS NULL ORDER BY id LIMIT 4097`, person)
	if e != nil {
		return nil, ErrUnavailable
	}
	var result []aw.Row
	for rows.Next() {
		var id uuid.UUID
		if e = rows.Scan(&id); e != nil {
			_ = rows.Close()
			return nil, ErrUnavailable
		}
		result = append(result, aw.Row{Table: aw.Sessions, ID: id, Access: aw.ExistingUpdate})
	}
	e = rows.Err()
	closeErr := rows.Close()
	if e != nil || closeErr != nil || len(result) > 4096 {
		return nil, ErrUnavailable
	}
	return result, nil
}
func sealAcquire(ctx context.Context, a *aw.Attempt, realm aw.Realm, rows []aw.Row, delivery []aw.Delivery) error {
	plan, e := aw.NewPlan(realm, rows, delivery)
	if e != nil {
		return ErrUnavailable
	}
	if e = a.SealPlan(plan); e != nil {
		return ErrUnavailable
	}
	for phase := aw.P; phase <= aw.W; phase++ {
		if e = a.Acquire(ctx, phase); e != nil {
			if errors.Is(e, aw.ErrDenied) {
				return ErrChallengeUnavailable
			}
			return ErrUnavailable
		}
	}
	return nil
}
func (s *Service) applyPassword(ctx context.Context, a *aw.Attempt, tx *sql.Tx, v accountSnapshot, newHash string, sessions []aw.Row) (accountSnapshot, error) {
	if e := a.RecordMutation(aw.CredentialWrite, []aw.Row{{Table: aw.Credentials, ID: v.credential, Access: aw.ExistingUpdate}}); e != nil {
		return v, recoveryFailure(a, ErrUnavailable)
	}
	changed, e := tx.ExecContext(ctx, `UPDATE identity_credentials SET verifier_hash=$1 WHERE id=$2 AND person_id=$3 AND method='email_password' AND revoked_at IS NULL AND verifier_hash=$4`, newHash, v.credential, v.person, v.hash)
	if e != nil {
		return v, recoveryFailure(a, ErrUnavailable)
	}
	count, e := changed.RowsAffected()
	if e != nil || count != 1 {
		return v, recoveryFailure(a, ErrChallengeUnavailable)
	}
	st, e := store.NewWriter(a)
	if e != nil {
		return v, recoveryFailure(a, ErrUnavailable)
	}
	epoch, e := st.AdvanceSecurityEpoch(ctx, v.person)
	if e != nil {
		return v, recoveryStoreError(e)
	}
	if epoch != v.epoch+1 {
		return v, recoveryFailure(a, ErrUnavailable)
	}
	for _, row := range sessions {
		if e = a.RecordMutation(aw.SessionWrite, []aw.Row{row}); e != nil {
			return v, recoveryFailure(a, ErrUnavailable)
		}
		changed, e = tx.ExecContext(ctx, `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE id=$1 AND person_id=$2 AND revoked_at IS NULL`, row.ID, v.person)
		if e != nil {
			return v, recoveryFailure(a, ErrUnavailable)
		}
		count, e = changed.RowsAffected()
		if e != nil || count != 1 {
			return v, recoveryFailure(a, ErrChallengeUnavailable)
		}
	}
	v.epoch = epoch
	v.hash = newHash
	return v, nil
}
func revokedAll(ctx context.Context, tx *sql.Tx, person uuid.UUID, rows []aw.Row, f time.Time) error {
	var remaining int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_sessions WHERE person_id=$1 AND revoked_at IS NULL`, person).Scan(&remaining); e != nil {
		return ErrUnavailable
	}
	if remaining != 0 {
		return ErrUnavailable
	}
	for _, row := range rows {
		var at time.Time
		if e := tx.QueryRowContext(ctx, `SELECT revoked_at FROM identity_sessions WHERE id=$1 AND person_id=$2`, row.ID, person).Scan(&at); e != nil || at.IsZero() || f.Before(at) {
			return ErrUnavailable
		}
	}
	return nil
}

func nilRecoveryDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// WriterPolicy checks only completion of an already-authorized exact epoch transition.
// It cannot issue authority and must use plain reads in the supplied transaction.
type WriterPolicy interface {
	Policy
	AuthorizePasswordChangeCompletion(context.Context, *sql.Tx, identity.Principal, int64) error
}

func recoveryProofError(e error) error {
	if errors.Is(e, wp.ErrDenied) {
		return ErrChallengeUnavailable
	}
	return ErrUnavailable
}
