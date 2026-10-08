package email

import (
	"context"
	"database/sql"
	"errors"
	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/google/uuid"
	"time"
)

func NewWithWriter(root *aw.Root, outbox *sqlstore.TxWriter, renderer *deliveryemail.Renderer, materials *materialstore.Writer, cfg Config) (*Service, error) {
	if root == nil || !validID(cfg.EnvironmentID) {
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
	s, err := configuredService(renderer, cfg)
	if err != nil {
		return nil, err
	}
	s.root = root
	s.writerOutbox = outbox
	s.writerMaterials = materials
	return s, nil
}
func (s *Service) writerRealm() aw.Realm {
	return aw.Realm{Installation: s.installationID, Application: s.applicationID, Environment: s.environmentID}
}
func (s *Service) writerReady() bool {
	return s != nil && s.root != nil && s.writerOutbox != nil && s.writerMaterials != nil && s.renderer != nil && s.origin != nil
}
func emailFailure(a *aw.Attempt, err error) error {
	outcome := aw.UnavailableRollback
	if errors.Is(err, ErrChallengeUnavailable) {
		outcome = aw.DeniedRollback
	}
	if e := a.Finish(outcome); e != nil {
		return ErrUnavailable
	}
	return err
}
func emailOutcome(err error) aw.Outcome {
	if errors.Is(err, ErrChallengeUnavailable) {
		return aw.DeniedRollback
	}
	return aw.UnavailableRollback
}
func terminalIdentity(err error) error {
	if errors.Is(err, store.ErrChallengeUnavailable) || errors.Is(err, store.ErrPersonUnavailable) {
		return ErrChallengeUnavailable
	}
	return ErrUnavailable
}
func writerRow(a *aw.Attempt, table aw.Table, id uuid.UUID) (aw.Row, error) {
	rows, e := a.PlannedRows(table)
	if e != nil {
		return aw.Row{}, ErrUnavailable
	}
	for _, r := range rows {
		if r.ID == id && (r.Access == aw.ExistingUpdate || r.Access == aw.ReservedInsert) {
			return r, nil
		}
	}
	return aw.Row{}, ErrUnavailable
}

type challengeSnapshot struct {
	person, email, id            uuid.UUID
	state, address, key, purpose string
	epoch                        int64
	created, expires             time.Time
	verified, consumed           sql.NullTime
	digest                       []byte
}

func (s *Service) challengeSnapshot(ctx context.Context, tx *sql.Tx, id uuid.UUID) (challengeSnapshot, error) {
	var v challengeSnapshot
	err := tx.QueryRowContext(ctx, `SELECT p.id,e.id,c.id,p.state,p.security_epoch,e.display_address,e.comparison_key,e.verified_at,c.purpose,c.token_digest,c.created_at,c.expires_at,c.consumed_at
 FROM identity_challenges c JOIN identity_persons p ON p.id=c.person_id JOIN identity_emails e ON e.id=c.email_id AND e.person_id=p.id
 WHERE c.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND e.installation_id=$2 AND e.application_id=$3`, id, s.installationID, s.applicationID).Scan(&v.person, &v.email, &v.id, &v.state, &v.epoch, &v.address, &v.key, &v.verified, &v.purpose, &v.digest, &v.created, &v.expires, &v.consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrChallengeUnavailable
	}
	if err != nil {
		return v, ErrUnavailable
	}
	switch v.state {
	case "pending_verification", "active", "self_disabled", "administratively_disabled", "deletion_pending":
	default:
		return v, ErrUnavailable
	}
	if v.epoch < 0 || len(v.digest) != 32 || v.key == "" || len(v.key) > 320 || v.created.IsZero() || !v.expires.After(v.created) || (v.verified.Valid && v.verified.Time.IsZero()) || (v.consumed.Valid && (v.consumed.Time.Before(v.created) || !v.consumed.Time.Before(v.expires))) {
		return v, ErrUnavailable
	}
	return v, nil
}
func (s *Service) snapshotEvidence(a *aw.Attempt, action wp.Action, v challengeSnapshot, verified time.Time) (wp.Evidence, error) {
	if len(v.digest) != 32 || v.epoch < 0 {
		return wp.Evidence{}, ErrUnavailable
	}
	return wp.Challenge(a, action, wp.ChallengeCheck{Subject: wp.Subject{Person: v.person, Realm: s.writerRealm(), Epoch: v.epoch}, Contact: wp.Contact{ID: v.email, ComparisonKey: v.key, VerifiedAt: v.verified.Time}, ID: v.id, TokenDigest: [32]byte(v.digest), CreatedAt: v.created, ExpiresAt: v.expires, VerifiedAt: verified})
}
func (s *Service) QueueExistingChallengeWriter(ctx context.Context, a *aw.Attempt, person, email, challenge, request uuid.UUID, raw string) error {
	if !s.writerReady() || ctx == nil || !validID(person) || !validID(email) || !validID(challenge) || !validID(request) {
		return emailFailure(a, ErrUnavailable)
	}
	realm, e := a.Realm()
	if e != nil || realm != s.writerRealm() {
		return emailFailure(a, ErrUnavailable)
	}
	_, digest, e := parseToken(raw)
	if e != nil {
		return emailFailure(a, ErrChallengeUnavailable)
	}
	rows := make([]aw.Row, 0, 3)
	for _, want := range []struct {
		table aw.Table
		id    uuid.UUID
	}{{aw.Persons, person}, {aw.Emails, email}, {aw.Challenges, challenge}} {
		r, e := writerRow(a, want.table, want.id)
		if e != nil {
			return emailFailure(a, e)
		}
		rows = append(rows, r)
	}
	tx, e := a.ParticipantTx(ctx, aw.W, rows)
	if e != nil {
		return emailFailure(a, ErrUnavailable)
	}
	v, e := s.challengeSnapshot(ctx, tx, challenge)
	if e != nil {
		return emailFailure(a, e)
	}
	var now time.Time
	if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now); e != nil {
		return emailFailure(a, ErrUnavailable)
	}
	if v.person != person || v.email != email || v.purpose != verificationPurpose || !equalDigest(v.digest, digest[:]) || v.state != "pending_verification" || v.verified.Valid || v.consumed.Valid || !now.Before(v.expires) {
		return emailFailure(a, ErrChallengeUnavailable)
	}
	if v.created.IsZero() || now.Before(v.created) || !v.expires.After(v.created) {
		return emailFailure(a, ErrUnavailable)
	}
	count, e := recentChallengeCount(ctx, tx, s.installationID, s.applicationID, person, email)
	if e != nil {
		return emailFailure(a, ErrUnavailable)
	}
	if count > MaxIssueRequestsPerHour {
		return emailFailure(a, ErrChallengeUnavailable)
	}
	delivery := aw.Delivery{MaterialID: challenge, JobKey: "email-verification:" + request.String()}
	ref := deliveryemail.SecretReference("material:" + challenge.String())
	material := deliveryemail.PrivateMaterial{Recipient: v.address, ActionURL: buildActionURL(s.origin, challenge, raw)}
	if e = s.writerMaterials.PutVerificationWriter(ctx, a, delivery, ref, material, v.expires); e != nil {
		return ErrUnavailable
	}
	_, e = deliveryemail.EnqueueWriter(ctx, a, delivery, s.writerOutbox, s.renderer, s.installationID, s.applicationID, delivery.JobKey, deliveryemail.Request{Template: deliveryemail.TemplateVerifyEmail, MaterialRef: ref, ExpiresInSeconds: int64(s.ttl / time.Second)}, v.expires)
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
func equalDigest(a, b []byte) bool {
	if len(a) != 32 || len(b) != 32 {
		return false
	}
	var difference byte
	for i := range a {
		difference |= a[i] ^ b[i]
	}
	return difference == 0
}
func (s *Service) previewWriter(ctx context.Context, id uuid.UUID, raw string) (Page, error) {
	if ctx == nil {
		return Page{}, ErrUnavailable
	}
	if !validID(id) {
		return Page{}, nil
	}
	_, digest, e := parseToken(raw)
	if e != nil {
		return Page{}, nil
	}
	var available bool
	e = s.root.Read(ctx, func(ctx context.Context, tx *sql.Tx) error {
		v, e := s.challengeSnapshot(ctx, tx, id)
		if errors.Is(e, ErrChallengeUnavailable) {
			return nil
		}
		if e != nil {
			return e
		}
		var now time.Time
		if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now); e != nil {
			return ErrUnavailable
		}
		available = v.state == "pending_verification" && !v.verified.Valid && !v.consumed.Valid && v.purpose == verificationPurpose && equalDigest(v.digest, digest[:]) && !now.Before(v.created) && now.Before(v.expires)
		return nil
	})
	if e != nil {
		return Page{}, ErrUnavailable
	}
	return Page{Available: available}, nil
}
func sameChallenge(a, b challengeSnapshot) bool {
	return a.person == b.person && a.email == b.email && a.id == b.id && a.state == b.state && a.epoch == b.epoch && a.address == b.address && a.key == b.key && a.purpose == b.purpose && a.created.Equal(b.created) && a.expires.Equal(b.expires) && a.verified == b.verified && a.consumed == b.consumed && equalDigest(a.digest, b.digest)
}
func (s *Service) finishEmail(ctx context.Context, a *aw.Attempt, tx *sql.Tx, v challengeSnapshot, evidence wp.Evidence) (wp.Permit, error) {
	f, e := a.DrainAndSample(ctx)
	if e != nil {
		return wp.Permit{}, emailFailure(a, ErrUnavailable)
	}
	current, e := s.challengeSnapshot(ctx, tx, v.id)
	if e != nil {
		return wp.Permit{}, emailFailure(a, e)
	}
	if !sameChallenge(v, current) {
		return wp.Permit{}, emailFailure(a, ErrUnavailable)
	}
	if v.state == "active" {
		var personal bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE installation_id=$1 AND application_id=$2 AND kind='personal' AND state='active' AND personal_owner_id=$3)`, s.installationID, s.applicationID, v.person).Scan(&personal); e != nil {
			return wp.Permit{}, emailFailure(a, ErrUnavailable)
		}
		if !personal {
			return wp.Permit{}, emailFailure(a, ErrChallengeUnavailable)
		}
	}
	permit, e := wp.Finalize(a, evidence, f)
	if e != nil {
		reason := ErrUnavailable
		if errors.Is(e, wp.ErrDenied) {
			reason = ErrChallengeUnavailable
		}
		return wp.Permit{}, emailFailure(a, reason)
	}
	if e = a.Finish(aw.Success); e != nil {
		return wp.Permit{}, ErrUnavailable
	}
	return permit, nil
}
func releaseEmail(c aw.Completion, p wp.Permit, b aw.Binding) error {
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

func (s *Service) issueWriter(ctx context.Context, person, email, request uuid.UUID) (Acknowledgement, error) {
	if ctx == nil || !validID(person) || !validID(email) || !validID(request) {
		return Acknowledgement{}, ErrInvalidRequest
	}
	if !s.writerReady() {
		return Acknowledgement{}, ErrUnavailable
	}
	challenge, e := store.NewID()
	if e != nil {
		return Acknowledgement{}, ErrUnavailable
	}
	raw, digest, e := newToken()
	if e != nil {
		return Acknowledgement{}, ErrUnavailable
	}
	var permit wp.Permit
	var binding aw.Binding
	noop := false
	completion, e := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		fail := func(e error) aw.Outcome { return emailOutcome(emailFailure(a, e)) }
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return fail(ErrUnavailable)
		}
		var exists bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id WHERE p.id=$1 AND e.id=$2 AND p.installation_id=$3 AND p.application_id=$4 AND e.installation_id=$3 AND e.application_id=$4 AND p.state='pending_verification' AND e.verified_at IS NULL)`, person, email, s.installationID, s.applicationID).Scan(&exists)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if !exists {
			noop = true
			return fail(ErrChallengeUnavailable)
		}
		rows := []aw.Row{{Table: aw.Persons, ID: person, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: email, Access: aw.ExistingUpdate}, {Table: aw.Challenges, ID: challenge, Access: aw.ReservedInsert}}
		workspaceRows, e := s.workspaceRows(ctx, tx, person, false)
		if e != nil {
			return fail(e)
		}
		rows = append(rows, workspaceRows...)
		delivery := aw.Delivery{MaterialID: challenge, JobKey: "email-verification:" + request.String()}
		plan, e := aw.NewPlan(s.writerRealm(), rows, []aw.Delivery{delivery})
		if e != nil {
			return fail(ErrUnavailable)
		}
		if e = a.SealPlan(plan); e != nil {
			return fail(ErrUnavailable)
		}
		for phase := aw.P; phase <= aw.W; phase++ {
			if e = a.Acquire(ctx, phase); e != nil {
				if errors.Is(e, aw.ErrDenied) {
					return fail(ErrChallengeUnavailable)
				}
				return fail(ErrUnavailable)
			}
		}
		tx, e = a.ParticipantTx(ctx, aw.W, rows)
		if e != nil {
			return fail(ErrUnavailable)
		}
		// Recheck the held contact; the earlier plain discovery is not authorization.
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id WHERE p.id=$1 AND e.id=$2 AND p.installation_id=$3 AND p.application_id=$4 AND e.installation_id=$3 AND e.application_id=$4 AND p.state='pending_verification' AND e.verified_at IS NULL)`, person, email, s.installationID, s.applicationID).Scan(&exists)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if !exists {
			noop = true
			return fail(ErrChallengeUnavailable)
		}
		count, e := recentChallengeCount(ctx, tx, s.installationID, s.applicationID, person, email)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if count >= MaxIssueRequestsPerHour {
			noop = true
			return fail(ErrChallengeUnavailable)
		}
		b, e := a.StartedAt()
		if e != nil {
			return fail(ErrUnavailable)
		}
		identityStore, e := store.NewWriter(a)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if e = identityStore.CreateChallenge(ctx, store.Challenge{ID: challenge, PersonID: person, EmailID: email, Purpose: verificationPurpose, Digest: digest[:], ExpiresAt: b.Add(s.ttl)}); e != nil {
			return emailOutcome(terminalIdentity(e))
		}
		v, e := s.challengeSnapshot(ctx, tx, challenge)
		if e != nil {
			return fail(e)
		}
		var verification time.Time
		if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&verification); e != nil {
			return fail(ErrUnavailable)
		}
		evidence, e := s.snapshotEvidence(a, wp.EmailRequest, v, verification)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if e = s.QueueExistingChallengeWriter(ctx, a, person, email, challenge, request, raw); e != nil {
			return emailOutcome(e)
		}
		binding, e = a.Binding()
		if e != nil {
			return fail(ErrUnavailable)
		}
		permit, e = s.finishEmail(ctx, a, tx, v, evidence)
		if e != nil {
			return emailOutcome(e)
		}
		return aw.Success
	})
	if e != nil {
		if errors.Is(e, aw.ErrDenied) && noop {
			return Acknowledgement{Received: true}, nil
		}
		return Acknowledgement{}, ErrUnavailable
	}
	if e = releaseEmail(completion, permit, binding); e != nil {
		return Acknowledgement{}, e
	}
	return Acknowledgement{Received: true}, nil
}
func (s *Service) confirmWriter(ctx context.Context, id uuid.UUID, raw string) error {
	if ctx == nil || !validID(id) {
		return ErrChallengeUnavailable
	}
	_, digest, e := parseToken(raw)
	if e != nil {
		return ErrChallengeUnavailable
	}
	var permit wp.Permit
	var binding aw.Binding
	completion, e := s.root.Run(ctx, func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		fail := func(e error) aw.Outcome { return emailOutcome(emailFailure(a, e)) }
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return fail(ErrUnavailable)
		}
		discovered, e := s.challengeSnapshot(ctx, tx, id)
		if e != nil {
			return fail(e)
		}
		if discovered.purpose != verificationPurpose || !equalDigest(discovered.digest, digest[:]) {
			return fail(ErrChallengeUnavailable)
		}
		rows := []aw.Row{{Table: aw.Persons, ID: discovered.person, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: discovered.email, Access: aw.ExistingUpdate}, {Table: aw.Challenges, ID: id, Access: aw.ExistingUpdate}}
		workspaceRows, e := s.workspaceRows(ctx, tx, discovered.person, true)
		if e != nil {
			return fail(e)
		}
		rows = append(rows, workspaceRows...)
		plan, e := aw.NewPlan(s.writerRealm(), rows, nil)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if e = a.SealPlan(plan); e != nil {
			return fail(ErrUnavailable)
		}
		for phase := aw.P; phase <= aw.W; phase++ {
			if e = a.Acquire(ctx, phase); e != nil {
				if errors.Is(e, aw.ErrDenied) {
					return fail(ErrChallengeUnavailable)
				}
				return fail(ErrUnavailable)
			}
		}
		tx, e = a.ParticipantTx(ctx, aw.W, rows)
		if e != nil {
			return fail(ErrUnavailable)
		}
		before, e := s.challengeSnapshot(ctx, tx, id)
		if e != nil {
			return fail(e)
		}
		if before.person != discovered.person || before.email != discovered.email || before.purpose != verificationPurpose || !equalDigest(before.digest, digest[:]) || before.state != "pending_verification" || before.verified.Valid || before.consumed.Valid {
			return fail(ErrChallengeUnavailable)
		}
		var verification time.Time
		if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&verification); e != nil {
			return fail(ErrUnavailable)
		}
		evidence, e := s.snapshotEvidence(a, wp.EmailConfirm, before, verification)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if !verification.Before(before.expires) {
			return fail(ErrChallengeUnavailable)
		}
		identityStore, e := store.NewWriter(a)
		if e != nil {
			return fail(ErrUnavailable)
		}
		if e = identityStore.MarkEmailVerified(ctx, before.person, before.email); e != nil {
			return emailOutcome(terminalIdentity(e))
		}
		if e = a.RecordMutation(aw.ContactWrite, []aw.Row{rows[0]}); e != nil {
			return fail(ErrUnavailable)
		}
		changed, e := tx.ExecContext(ctx, `UPDATE identity_persons SET state='active',updated_at=transaction_timestamp() WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND state='pending_verification' AND security_epoch=$4`, before.person, s.installationID, s.applicationID, before.epoch)
		if e != nil {
			return fail(ErrUnavailable)
		}
		n, e := changed.RowsAffected()
		if e != nil {
			return fail(ErrUnavailable)
		}
		if n != 1 {
			return fail(ErrChallengeUnavailable)
		}
		consumed, e := identityStore.ConsumeChallenge(ctx, id, verificationPurpose, digest[:])
		if e != nil {
			return emailOutcome(terminalIdentity(e))
		}
		if consumed.PersonID != before.person || consumed.EmailID != before.email {
			return fail(ErrUnavailable)
		}
		after, e := s.challengeSnapshot(ctx, tx, id)
		if e != nil {
			return fail(e)
		}
		expected := before
		expected.state = "active"
		expected.verified = after.verified
		expected.consumed = after.consumed
		if !after.verified.Valid || !after.consumed.Valid || !sameChallenge(expected, after) || after.consumed.Time.Before(before.created) || !after.consumed.Time.Before(before.expires) {
			return fail(ErrUnavailable)
		}
		binding, e = a.Binding()
		if e != nil {
			return fail(ErrUnavailable)
		}
		permit, e = s.finishEmail(ctx, a, tx, after, evidence)
		if e != nil {
			return emailOutcome(e)
		}
		return aw.Success
	})
	if errors.Is(e, aw.ErrDenied) {
		return ErrChallengeUnavailable
	}
	if e != nil {
		return ErrUnavailable
	}
	return releaseEmail(completion, permit, binding)
}

// Discover the complete owner-trigger set under G. These are plain reads; the
// root acquires each exact P/W row in global order before any contact mutation.
func (s *Service) workspaceRows(ctx context.Context, tx *sql.Tx, person uuid.UUID, requirePersonal bool) ([]aw.Row, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,kind,state,personal_owner_id FROM workspaces WHERE installation_id=$1 AND application_id=$2 AND (personal_owner_id=$3 OR id IN(SELECT workspace_id FROM workspace_memberships WHERE installation_id=$1 AND application_id=$2 AND person_id=$3)) ORDER BY id LIMIT 1025`, s.installationID, s.applicationID, person)
	if e != nil {
		return nil, ErrUnavailable
	}
	var workspaceIDs []uuid.UUID
	personal := false
	for rows.Next() {
		var id uuid.UUID
		var kind, state string
		var owner uuid.NullUUID
		if e = rows.Scan(&id, &kind, &state, &owner); e != nil {
			_ = rows.Close()
			return nil, ErrUnavailable
		}
		if kind == "personal" && state == "active" && owner.Valid && owner.UUID == person {
			personal = true
		}
		workspaceIDs = append(workspaceIDs, id)
	}
	e = rows.Err()
	closeErr := rows.Close()
	if e != nil || closeErr != nil || len(workspaceIDs) > 1024 {
		return nil, ErrUnavailable
	}
	if requirePersonal && !personal {
		return nil, ErrChallengeUnavailable
	}
	result := []aw.Row{}
	persons := map[uuid.UUID]bool{person: true}
	memberships := 0
	for _, id := range workspaceIDs {
		result = append(result, aw.Row{Table: aw.Workspaces, ID: id, Access: aw.ExistingUpdate})
		members, e := tx.QueryContext(ctx, `SELECT id,person_id FROM workspace_memberships WHERE workspace_id=$1 AND installation_id=$2 AND application_id=$3 ORDER BY id LIMIT 1025`, id, s.installationID, s.applicationID)
		if e != nil {
			return nil, ErrUnavailable
		}
		for members.Next() {
			var member, owner uuid.UUID
			if e = members.Scan(&member, &owner); e != nil {
				_ = members.Close()
				return nil, ErrUnavailable
			}
			result = append(result, aw.Row{Table: aw.Memberships, ID: member, Access: aw.ExistingUpdate})
			persons[owner] = true
			memberships++
		}
		e = members.Err()
		closeErr = members.Close()
		if e != nil || closeErr != nil || memberships > 1024 {
			return nil, ErrUnavailable
		}
		var owner uuid.NullUUID
		if e = tx.QueryRowContext(ctx, `SELECT personal_owner_id FROM workspaces WHERE id=$1`, id).Scan(&owner); e != nil {
			return nil, ErrUnavailable
		}
		if owner.Valid {
			persons[owner.UUID] = true
		}
	}
	for id := range persons {
		if id != person {
			result = append(result, aw.Row{Table: aw.Persons, ID: id, Access: aw.ExistingUpdate})
		}
	}
	return result, nil
}
