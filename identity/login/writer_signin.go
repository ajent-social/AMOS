package login

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ajent-social/amos/identity/internal/passwordwork"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

type passwordRow struct {
	person, contact, credentialID uuid.UUID
	epoch                         int64
	state, key, hash              string
	verified                      sql.NullTime
}

func (s *Service) passwordRow(ctx context.Context, tx *sql.Tx, address string) (passwordRow, error) {
	var v passwordRow
	err := tx.QueryRowContext(ctx, `SELECT p.id,p.security_epoch,p.state,e.id,e.comparison_key,e.verified_at,c.id,c.verifier_hash FROM public.identity_emails e JOIN public.identity_persons p ON p.id=e.person_id AND p.installation_id=e.installation_id AND p.application_id=e.application_id JOIN public.identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL WHERE e.installation_id=$1 AND e.application_id=$2 AND e.comparison_key=$3`, s.cfg.InstallationID, s.cfg.ApplicationID, address).Scan(&v.person, &v.epoch, &v.state, &v.contact, &v.key, &v.verified, &v.credentialID, &v.hash)
	return v, err
}
func (v passwordRow) shape() bool {
	return validID(v.person) && validID(v.contact) && validID(v.credentialID) && v.epoch >= 0 && v.key != "" && len(v.key) <= 320 && v.hash != "" && len(v.hash) <= 256
}
func (v passwordRow) same(other passwordRow) bool {
	return v.person == other.person && v.contact == other.contact && v.credentialID == other.credentialID && v.epoch == other.epoch && v.state == other.state && v.key == other.key && v.hash == other.hash && v.verified.Valid == other.verified.Valid && v.verified.Time.Equal(other.verified.Time)
}
func (s *Service) signInWriter(w http.ResponseWriter, r *http.Request) {
	var in signInRequest
	if !decode(w, r, &in) {
		write(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
		return
	}
	address, valid := normalizeAddress(in.Email)
	if !valid {
		address = "invalid@example.invalid"
	}
	request, err := s.cfg.Sessions.AdmitWriterRequest(r)
	if err != nil {
		unavailable(w, r)
		return
	}
	var staged session.Staged
	var permit wp.Permit
	completion, err := s.root.Run(r.Context(), func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		discovered, e := s.passwordRow(ctx, tx, address)
		known := e == nil
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if known && !discovered.shape() {
			return loginFinish(a, aw.UnavailableRollback)
		}
		rows, e := s.cfg.Sessions.DiscoverPrior(ctx, a, request, wp.PasswordSignIn)
		if e != nil {
			return aw.UnavailableRollback
		}
		if known {
			rows = append(rows, aw.Row{Table: aw.Persons, ID: discovered.person, Access: aw.ExistingUpdate}, aw.Row{Table: aw.Emails, ID: discovered.contact, Access: aw.ExistingUpdate}, aw.Row{Table: aw.Credentials, ID: discovered.credentialID, Access: aw.ExistingUpdate})
		}
		rows = uniqueRows(rows)
		plan, e := aw.NewPlan(s.realm(), rows, nil)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if e = a.SealPlan(plan); e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if e = acquire(ctx, a, aw.P, aw.C); e != nil {
			return loginRootFailure(a, e)
		}
		held := passwordRow{}
		if known {
			tx, e = a.ParticipantTx(ctx, aw.C, []aw.Row{{Table: aw.Persons, ID: discovered.person, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: discovered.contact, Access: aw.ExistingUpdate}, {Table: aw.Credentials, ID: discovered.credentialID, Access: aw.ExistingUpdate}})
			if e != nil {
				return loginFinish(a, aw.UnavailableRollback)
			}
			held, e = s.passwordRow(ctx, tx, address)
			if errors.Is(e, sql.ErrNoRows) {
				return loginFinish(a, aw.DeniedRollback)
			}
			if e != nil {
				return loginFinish(a, aw.UnavailableRollback)
			}
			if !held.shape() {
				return loginFinish(a, aw.UnavailableRollback)
			}
			if !held.same(discovered) {
				return loginFinish(a, aw.DeniedRollback)
			}
		}
		// Exactly one real or dummy Verify, with the original one-use Guard budget.
		result, e := passwordwork.Verify(ctx, s.cfg.Passwords, "signin:"+address, in.Password, held.hash, known)
		if e != nil && !errors.Is(e, password.ErrInvalid) {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if e != nil || !valid || !known || !result.Verified || held.state != "active" || !held.verified.Valid {
			return loginFinish(a, aw.DeniedRollback)
		}
		var verified time.Time
		if e = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&verified); e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		check := wp.PasswordCheck{Subject: wp.Subject{Person: held.person, Realm: s.realm(), Epoch: held.epoch}, Contact: wp.Contact{ID: held.contact, ComparisonKey: held.key, VerifiedAt: held.verified.Time}, CredentialID: held.credentialID, VerifiedHash: held.hash, StagedHash: result.Replacement, VerifiedAt: verified, ValidUntil: verified.Add(15 * time.Minute)}
		if result.Replacement != "" {
			target := []aw.Row{{Table: aw.Credentials, ID: held.credentialID, Access: aw.ExistingUpdate}}
			if e = a.RecordMutation(aw.CredentialWrite, target); e != nil {
				return loginFinish(a, aw.UnavailableRollback)
			}
			updated, e := tx.ExecContext(ctx, `UPDATE public.identity_credentials SET verifier_hash=$1 WHERE id=$2 AND person_id=$3 AND method='email_password' AND revoked_at IS NULL AND verifier_hash=$4`, result.Replacement, held.credentialID, held.person, held.hash)
			if e != nil {
				return loginFinish(a, aw.UnavailableRollback)
			}
			n, e := updated.RowsAffected()
			if e != nil || n != 1 {
				return loginFinish(a, aw.UnavailableRollback)
			}
			held.hash = result.Replacement
		}
		evidence, e := wp.Password(a, wp.PasswordSignIn, check)
		if e != nil {
			return loginRootFailure(a, e)
		}
		if e = acquire(ctx, a, aw.H, aw.S, aw.W); e != nil {
			return loginRootFailure(a, e)
		}
		issuance, e := wp.ForIssue(a, evidence)
		if e != nil {
			return loginRootFailure(a, e)
		}
		staged, e = s.cfg.Sessions.StageWriter(ctx, a, issuance, request)
		if e != nil {
			if errors.Is(e, session.ErrUnauthenticated) {
				return aw.DeniedRollback
			}
			return aw.UnavailableRollback
		}
		f, e := a.DrainAndSample(ctx)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		final, e := s.passwordRow(ctx, tx, address)
		if errors.Is(e, sql.ErrNoRows) {
			return loginFinish(a, aw.DeniedRollback)
		}
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if !held.same(final) {
			return loginFinish(a, aw.DeniedRollback)
		}
		if e = s.cfg.Sessions.CheckStagedWriter(ctx, a, staged, f); e != nil {
			if errors.Is(e, session.ErrUnauthenticated) {
				return aw.DeniedRollback
			}
			return aw.UnavailableRollback
		}
		permit, e = wp.Finalize(a, evidence, f)
		if e != nil {
			return loginRootFailure(a, e)
		}
		return loginFinish(a, aw.Success)
	})
	if err != nil {
		if errors.Is(err, aw.ErrDenied) {
			write(w, r, http.StatusUnauthorized, "auth.unauthenticated", "Email or password is incorrect.")
		} else {
			unavailable(w, r)
		}
		return
	}
	issued, err := s.cfg.Sessions.PublishWriter(completion, permit, staged)
	if err != nil {
		unavailable(w, r)
		return
	}
	http.SetCookie(w, issued.Cookie)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}{true, issued.CSRFToken})
}
