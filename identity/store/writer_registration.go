package store

import (
	"context"
	"errors"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

// CreatePendingAccount stages its parent at P before acquiring/inserting C,
// then acquires/inserts H. All identifiers were reserved before P acquisition.
func (s *Store) CreatePendingAccount(ctx context.Context, v PendingAccount) error {
	if s == nil || s.attempt == nil {
		p, e := s.legacy(ctx)
		if e != nil {
			return e
		}
		return p.createPendingAccount(ctx, v)
	}
	if e := s.scope(v.InstallationID, v.ApplicationID, [16]byte{}); e != nil {
		return e
	}
	email, e := normalizeEmail(v.EmailAddress)
	if e != nil {
		return s.failed(e)
	}
	if !validID(v.PersonID) || !validID(v.EmailID) || !validID(v.CredentialID) || !validID(v.ChallengeID) || len(v.ChallengeDigest) != 32 || !validArgon2idHash(v.PasswordHash) {
		return s.failed(ErrInvalidInput)
	}
	b, e := s.attempt.StartedAt()
	if e != nil || !v.ChallengeExpiry.After(b) {
		return s.failed(ErrInvalidInput)
	}
	rows := []aw.Row{row(aw.Persons, v.PersonID, aw.ReservedInsert)}
	p, e := s.participant(ctx, aw.P, rows, aw.RegistrationWrite, rows)
	if e != nil {
		return e
	}
	result, e := p.tx.ExecContext(ctx, `INSERT INTO public.identity_persons(id,installation_id,application_id,state,created_at,updated_at) VALUES($1,$2,$3,'pending_verification',$4,$4)`, v.PersonID, v.InstallationID, v.ApplicationID, b)
	if e = dbResult(result, e); e != nil {
		return s.failed(e)
	}
	if e = s.attempt.Acquire(ctx, aw.C); e != nil {
		if errors.Is(e, aw.ErrDenied) {
			return s.failed(ErrPersonUnavailable)
		}
		return s.failed(ErrPersistence)
	}
	rows = []aw.Row{row(aw.Emails, v.EmailID, aw.ReservedInsert)}
	p, e = s.participant(ctx, aw.C, rows, aw.RegistrationWrite, rows)
	if e != nil {
		return e
	}
	result, e = p.tx.ExecContext(ctx, `INSERT INTO public.identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, v.EmailID, v.PersonID, v.InstallationID, v.ApplicationID, v.EmailAddress, email, b)
	if e != nil {
		return s.failed(mapConflict(e, "identity_emails_scope_address_key", ErrEmailAlreadyUsed))
	}
	if e = dbResult(result, nil); e != nil {
		return s.failed(e)
	}
	rows = []aw.Row{row(aw.Credentials, v.CredentialID, aw.ReservedInsert)}
	p, e = s.participant(ctx, aw.C, rows, aw.RegistrationWrite, rows)
	if e != nil {
		return e
	}
	result, e = p.tx.ExecContext(ctx, `INSERT INTO public.identity_credentials(id,person_id,method,verifier_hash,created_at) VALUES($1,$2,'email_password',$3,$4)`, v.CredentialID, v.PersonID, v.PasswordHash, b)
	if e = dbResult(result, e); e != nil {
		return s.failed(e)
	}
	if e = s.attempt.Acquire(ctx, aw.H); e != nil {
		if errors.Is(e, aw.ErrDenied) {
			return s.failed(ErrPersonUnavailable)
		}
		return s.failed(ErrPersistence)
	}
	rows = []aw.Row{row(aw.Challenges, v.ChallengeID, aw.ReservedInsert)}
	p, e = s.participant(ctx, aw.H, rows, aw.ChallengeWrite, rows)
	if e != nil {
		return e
	}
	result, e = p.tx.ExecContext(ctx, `INSERT INTO public.identity_challenges(id,person_id,email_id,purpose,token_digest,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, v.ChallengeID, v.PersonID, v.EmailID, ChallengeEmailVerification, v.ChallengeDigest, b, v.ChallengeExpiry)
	return s.failed(dbResult(result, e))
}
