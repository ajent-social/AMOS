package magiclink

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
)

// NewWithWriter retains only the exact root and native pool-free participants.
// Run/Read check active-root ownership before SQL, not during construction.
func NewWithWriter(root *aw.Root, outbox *sqlstore.TxWriter, materials *materialstore.Writer, cfg Config) (*Service, error) {
	sessions, ok := cfg.Sessions.(*session.Service)
	if root == nil || outbox == nil || materials == nil || !ok || sessions == nil || cfg.DB != nil || cfg.Outbox != nil || cfg.Materials != nil || cfg.Renderer == nil || nilWriterPolicy(cfg.Policy) || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) {
		return nil, ErrConfiguration
	}
	s, err := configured(cfg)
	if err != nil {
		return nil, err
	}
	s.root = root
	s.writerOutbox = outbox
	s.writerMaterials = materials
	s.writerSessions = sessions
	return s, nil
}
func (s *Service) available() bool {
	return s != nil && (s.root != nil || s.cfg.DB != nil && aw.SelectLegacy() == nil)
}
func (s *Service) read(ctx context.Context, body func(context.Context, *sql.Tx) error) error {
	if !s.available() {
		return ErrUnavailable
	}
	if s.root != nil {
		return s.root.Read(ctx, body)
	}
	return s.cfg.DB.WithTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error { return body(ctx, tx) })
}
func (s *Service) realm() aw.Realm {
	return aw.Realm{Installation: s.cfg.InstallationID, Application: s.cfg.ApplicationID, Environment: s.cfg.EnvironmentID}
}
func magicFinish(a *aw.Attempt, outcome aw.Outcome) aw.Outcome {
	if err := a.Finish(outcome); err != nil {
		return aw.UnavailableRollback
	}
	return outcome
}
func magicProofFailure(a *aw.Attempt, err error) aw.Outcome {
	if errors.Is(err, wp.ErrDenied) {
		return magicFinish(a, aw.DeniedRollback)
	}
	return magicFinish(a, aw.UnavailableRollback)
}
func magicStoreFailure(err error) aw.Outcome {
	if errors.Is(err, store.ErrChallengeUnavailable) || errors.Is(err, store.ErrPersonUnavailable) || errors.Is(err, store.ErrSessionUnavailable) {
		return aw.DeniedRollback
	}
	return aw.UnavailableRollback
}
func magicRows(rows []aw.Row) []aw.Row {
	result := make([]aw.Row, 0, len(rows))
	seen := make(map[aw.Row]bool)
	for _, r := range rows {
		if !seen[r] {
			result = append(result, r)
			seen[r] = true
		}
	}
	return result
}
func magicAcquire(ctx context.Context, a *aw.Attempt) error {
	for _, p := range []aw.Phase{aw.P, aw.C, aw.H, aw.S, aw.W} {
		if err := a.Acquire(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

type magicContact struct {
	person, email       uuid.UUID
	epoch               int64
	state, key, address string
	verified            sql.NullTime
}

func (s *Service) contact(ctx context.Context, tx *sql.Tx, address string) (magicContact, error) {
	var c magicContact
	err := tx.QueryRowContext(ctx, `SELECT p.id,e.id,p.security_epoch,p.state,e.comparison_key,e.display_address,e.verified_at FROM public.identity_persons p JOIN public.identity_emails e ON e.person_id=p.id AND e.installation_id=p.installation_id AND e.application_id=p.application_id WHERE p.installation_id=$1 AND p.application_id=$2 AND e.comparison_key=$3`, s.cfg.InstallationID, s.cfg.ApplicationID, address).Scan(&c.person, &c.email, &c.epoch, &c.state, &c.key, &c.address, &c.verified)
	return c, err
}
func (c magicContact) live() bool {
	return validID(c.person) && validID(c.email) && c.epoch >= 0 && c.state == "active" && c.verified.Valid
}
func (c magicContact) same(other magicContact) bool {
	return c.person == other.person && c.email == other.email && c.epoch == other.epoch && c.state == other.state && c.key == other.key && c.address == other.address && c.verified.Valid == other.verified.Valid && c.verified.Time.Equal(other.verified.Time)
}
func (c magicContact) rows() []aw.Row {
	return []aw.Row{{Table: aw.Persons, ID: c.person, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: c.email, Access: aw.ExistingUpdate}}
}
func (s *Service) policyRows(ctx context.Context, tx *sql.Tx, person uuid.UUID) (result []aw.Row, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT w.id FROM public.workspaces w WHERE w.installation_id=$1 AND w.application_id=$2 AND (w.personal_owner_id=$3 OR EXISTS(SELECT 1 FROM public.workspace_memberships m WHERE m.workspace_id=w.id AND m.person_id=$3 AND m.installation_id=$1 AND m.application_id=$2))`, s.cfg.InstallationID, s.cfg.ApplicationID, person)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := rows.Close(); err == nil {
			err = e
		}
	}()
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, aw.Row{Table: aw.Workspaces, ID: id, Access: aw.ExistingUpdate})
	}
	return result, rows.Err()
}
func (s *Service) requestWriter(ctx context.Context, address string, challengeID uuid.UUID, token, binding string, requestID uuid.UUID) error {
	_, digest, err := parseToken(token)
	if err != nil {
		return ErrUnavailable
	}
	_, browser, err := parseToken(binding)
	if err != nil {
		return ErrUnavailable
	}
	delivery := aw.Delivery{MaterialID: challengeID, JobKey: "magic-link:" + requestID.String()}
	var permit wp.Permit
	var output aw.Binding
	noop := false
	completion, err := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		c, e := s.contact(ctx, tx, address)
		if errors.Is(e, sql.ErrNoRows) || e == nil && !c.live() {
			noop = true
			return magicFinish(a, aw.DeniedRollback)
		}
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		rows := append(c.rows(), aw.Row{Table: aw.Challenges, ID: challengeID, Access: aw.ReservedInsert})
		policy, e := s.policyRows(ctx, tx, c.person)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		rows = append(rows, policy...)
		plan, e := aw.NewPlan(s.realm(), rows, []aw.Delivery{delivery})
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = a.SealPlan(plan); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = magicAcquire(ctx, a); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		tx, e = a.ParticipantTx(ctx, aw.W, rows)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		current, e := s.contact(ctx, tx, address)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if !c.same(current) {
			return magicFinish(a, aw.DeniedRollback)
		}
		if e = s.cfg.Policy.Ready(ctx, tx); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = s.cfg.Policy.AuthorizeMagicLink(ctx, tx, c.person); e != nil {
			if errors.Is(e, ErrPolicyDenied) || errors.Is(e, ErrStepUpRequired) {
				noop = true
				return magicFinish(a, aw.DeniedRollback)
			}
			return magicFinish(a, aw.UnavailableRollback)
		}
		b, e := a.StartedAt()
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		var recent int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM public.identity_challenges WHERE person_id=$1 AND email_id=$2 AND purpose=$3 AND created_at >= $4::timestamptz - interval '1 hour'`, c.person, c.email, challengePurpose, b).Scan(&recent); e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if recent >= MaxIssueRequestsPerHour {
			noop = true
			return magicFinish(a, aw.DeniedRollback)
		}
		expiry := b.Add(s.cfg.ChallengeLifetime)
		st, e := store.NewWriter(a)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = st.CreateMagicChallenge(ctx, store.Challenge{ID: challengeID, PersonID: c.person, EmailID: c.email, Purpose: challengePurpose, Digest: digest[:], ExpiresAt: expiry}, browser[:]); e != nil {
			return magicStoreFailure(e)
		}
		ref := deliveryemail.SecretReference("material:" + challengeID.String())
		material := deliveryemail.PrivateMaterial{Recipient: c.address, ActionURL: actionURL(s.origin, challengeID, token)}
		if e = s.writerMaterials.PutSignInWriter(ctx, a, delivery, ref, material, expiry); e != nil {
			return aw.UnavailableRollback
		}
		req := deliveryemail.Request{Template: deliveryemail.TemplateSignIn, MaterialRef: ref, ExpiresInSeconds: int64(s.cfg.ChallengeLifetime / time.Second)}
		job, e := deliveryemail.EnqueueWriter(ctx, a, delivery, s.writerOutbox, s.cfg.Renderer, s.cfg.InstallationID, s.cfg.ApplicationID, delivery.JobKey, req, expiry)
		if e != nil {
			return aw.UnavailableRollback
		}
		evidence, e := wp.Challenge(a, wp.MagicRequest, wp.ChallengeCheck{Subject: wp.Subject{Person: c.person, Realm: s.realm(), Epoch: c.epoch}, Contact: wp.Contact{ID: c.email, ComparisonKey: c.key, VerifiedAt: c.verified.Time}, ID: challengeID, TokenDigest: digest, BrowserDigest: browser, CreatedAt: b, ExpiresAt: expiry, VerifiedAt: b})
		if e != nil {
			return magicProofFailure(a, e)
		}
		output, e = a.Binding()
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		f, e := a.DrainAndSample(ctx)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		// All D waits and the constraint drain precede plain final comparisons.
		current, e = s.contact(ctx, tx, address)
		if e != nil {
			return magicFinish(a, aw.UnavailableRollback)
		}
		if !c.same(current) {
			return magicFinish(a, aw.DeniedRollback)
		}
		if e = s.cfg.Policy.AuthorizeMagicLink(ctx, tx, c.person); e != nil {
			if errors.Is(e, ErrPolicyDenied) || errors.Is(e, ErrStepUpRequired) {
				return magicFinish(a, aw.DeniedRollback)
			}
			return magicFinish(a, aw.UnavailableRollback)
		}
		if e = checkMagicRow(ctx, tx, challengeID, c, digest, browser, b, expiry, time.Time{}); e != nil {
			return magicProofFailure(a, e)
		}
		if !f.Before(job.Deadline) {
			return magicFinish(a, aw.DeniedRollback)
		}
		permit, e = wp.Finalize(a, evidence, f)
		if e != nil {
			return magicProofFailure(a, e)
		}
		return magicFinish(a, aw.Success)
	})
	if noop && errors.Is(err, aw.ErrDenied) {
		return nil
	}
	if err != nil {
		return ErrUnavailable
	}
	release, err := completion.TakeRelease(output)
	if err != nil || !permit.MatchesRelease(release) {
		return ErrUnavailable
	}
	return nil
}
func checkMagicRow(ctx context.Context, tx *sql.Tx, id uuid.UUID, c magicContact, digest, browser [32]byte, created, expires, consumedAt time.Time) error {
	var person, email uuid.UUID
	var purpose string
	var gotDigest, gotBrowser []byte
	var gotCreated, gotExpiry time.Time
	var used sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT person_id,email_id,purpose,token_digest,browser_binding_digest,created_at,expires_at,consumed_at FROM public.identity_challenges WHERE id=$1`, id).Scan(&person, &email, &purpose, &gotDigest, &gotBrowser, &gotCreated, &gotExpiry, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return wp.ErrDenied
	}
	if err != nil {
		return wp.ErrUnavailable
	}
	if person != c.person || email != c.email || purpose != challengePurpose || !bytes.Equal(gotDigest, digest[:]) || !bytes.Equal(gotBrowser, browser[:]) || !gotCreated.Equal(created) || !gotExpiry.Equal(expires) || used.Valid != !consumedAt.IsZero() || used.Valid && !used.Time.Equal(consumedAt) {
		return wp.ErrDenied
	}
	return nil
}

type magicChallenge struct {
	contact          magicContact
	id               uuid.UUID
	digest, browser  []byte
	created, expires time.Time
	consumed         sql.NullTime
}

func (s *Service) challenge(ctx context.Context, tx *sql.Tx, id uuid.UUID, digest []byte) (magicChallenge, error) {
	var v magicChallenge
	v.id = id
	c := &v.contact
	err := tx.QueryRowContext(ctx, `SELECT p.id,e.id,p.security_epoch,p.state,e.comparison_key,e.display_address,e.verified_at,h.token_digest,h.browser_binding_digest,h.created_at,h.expires_at,h.consumed_at FROM public.identity_challenges h JOIN public.identity_persons p ON p.id=h.person_id JOIN public.identity_emails e ON e.id=h.email_id AND e.person_id=p.id AND e.installation_id=p.installation_id AND e.application_id=p.application_id WHERE h.id=$1 AND h.purpose=$2 AND h.token_digest=$3 AND p.installation_id=$4 AND p.application_id=$5`, id, challengePurpose, digest, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&c.person, &c.email, &c.epoch, &c.state, &c.key, &c.address, &c.verified, &v.digest, &v.browser, &v.created, &v.expires, &v.consumed)
	return v, err
}
func (v magicChallenge) shape() bool {
	ttl := v.expires.Sub(v.created)
	return validID(v.id) && validID(v.contact.person) && validID(v.contact.email) && v.contact.epoch >= 0 && len(v.digest) == 32 && len(v.browser) == 32 && !v.created.IsZero() && ttl >= time.Minute && ttl <= DefaultChallengeLifetime && ttl%time.Second == 0
}
func (v magicChallenge) same(other magicChallenge) bool {
	return v.contact.same(other.contact) && v.id == other.id && bytes.Equal(v.digest, other.digest) && bytes.Equal(v.browser, other.browser) && v.created.Equal(other.created) && v.expires.Equal(other.expires) && v.consumed.Valid == other.consumed.Valid && v.consumed.Time.Equal(other.consumed.Time)
}

func nilWriterPolicy(policy Policy) bool {
	if policy == nil {
		return true
	}
	v := reflect.ValueOf(policy)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
