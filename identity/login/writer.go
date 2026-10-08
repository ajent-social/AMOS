package login

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/internal/passwordwork"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/workspace/personal"
	"github.com/google/uuid"
)

// NewWithWriter copies configuration and retains the exact private root and
// concrete native dependencies. Active-root admission is enforced by Run/Read
// before SQL; no constructor probe or alternate transaction runner is used.
func NewWithWriter(root *aw.Root, cfg TxConfig) (*Service, error) {
	if root == nil || cfg.Passwords == nil || cfg.Email == nil || cfg.Sessions == nil || !validID(cfg.InstallationID) || !validID(cfg.ApplicationID) || !validID(cfg.EnvironmentID) {
		return nil, ErrConfiguration
	}
	if cfg.ChallengeLifetime == 0 {
		cfg.ChallengeLifetime = DefaultChallengeLifetime
	}
	if cfg.ChallengeLifetime != email.DefaultChallengeLifetime {
		return nil, ErrConfiguration
	}
	return &Service{root: root, cfg: cfg}, nil
}
func (s *Service) realm() aw.Realm {
	return aw.Realm{Installation: s.cfg.InstallationID, Application: s.cfg.ApplicationID, Environment: s.cfg.EnvironmentID}
}
func loginFinish(a *aw.Attempt, outcome aw.Outcome) aw.Outcome {
	if err := a.Finish(outcome); err != nil {
		return aw.UnavailableRollback
	}
	return outcome
}
func loginProofFailure(a *aw.Attempt, err error) aw.Outcome {
	if errors.Is(err, wp.ErrDenied) {
		return loginFinish(a, aw.DeniedRollback)
	}
	return loginFinish(a, aw.UnavailableRollback)
}
func loginStoreFailure(err error) aw.Outcome {
	// Store mutation failures have already finished their attempt. Never finish it twice.
	if errors.Is(err, store.ErrSessionUnavailable) || errors.Is(err, store.ErrPersonUnavailable) || errors.Is(err, store.ErrChallengeUnavailable) {
		return aw.DeniedRollback
	}
	return aw.UnavailableRollback
}
func uniqueRows(rows []aw.Row) []aw.Row {
	result := make([]aw.Row, 0, len(rows))
	seen := make(map[aw.Row]bool, len(rows))
	for _, r := range rows {
		if !seen[r] {
			result = append(result, r)
			seen[r] = true
		}
	}
	return result
}
func acquire(ctx context.Context, a *aw.Attempt, phases ...aw.Phase) error {
	for _, p := range phases {
		if err := a.Acquire(ctx, p); err != nil {
			return err
		}
	}
	return nil
}
func unavailable(w http.ResponseWriter, r *http.Request) {
	write(w, r, http.StatusServiceUnavailable, "dependency.unavailable", "Service unavailable.")
}

func (s *Service) registerWriter(w http.ResponseWriter, r *http.Request) {
	var in registrationRequest
	if !decode(w, r, &in) {
		write(w, r, http.StatusBadRequest, "request.invalid", "Invalid request.")
		return
	}
	address, ok := normalizeAddress(in.Email)
	if !ok {
		write(w, r, http.StatusBadRequest, "identity.registration_invalid", "Invalid registration details.")
		return
	}
	hash, err := passwordwork.Hash(r.Context(), s.cfg.Passwords, "register:"+address, in.Password)
	if err != nil {
		if errors.Is(err, password.ErrInvalid) {
			write(w, r, http.StatusBadRequest, "identity.registration_invalid", "Invalid registration details.")
		} else {
			unavailable(w, r)
		}
		return
	}
	ids := make([]uuid.UUID, 6)
	for i := range ids {
		ids[i], err = store.NewID()
		if err != nil {
			unavailable(w, r)
			return
		}
	}
	personID, emailID, credentialID, challengeID, workspaceID, requestID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		unavailable(w, r)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	delivery := aw.Delivery{MaterialID: challengeID, JobKey: "email-verification:" + requestID.String()}
	rows := []aw.Row{{Table: aw.Persons, ID: personID, Access: aw.ReservedInsert}, {Table: aw.Emails, ID: emailID, Access: aw.ReservedInsert}, {Table: aw.Credentials, ID: credentialID, Access: aw.ReservedInsert}, {Table: aw.Challenges, ID: challengeID, Access: aw.ReservedInsert}, {Table: aw.Workspaces, ID: workspaceID, Access: aw.ReservedInsert}}
	plan, err := aw.NewPlan(s.realm(), rows, []aw.Delivery{delivery})
	if err != nil {
		unavailable(w, r)
		return
	}
	var permit wp.Permit
	var binding aw.Binding
	duplicate := false
	completion, err := s.root.Run(r.Context(), func(ctx context.Context, a *aw.Attempt) aw.Outcome {
		tx, e := a.DiscoveryTx(ctx)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		// G stabilizes address absence; an existing address remains non-enumerating.
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.identity_emails WHERE installation_id=$1 AND application_id=$2 AND comparison_key=$3)`, s.cfg.InstallationID, s.cfg.ApplicationID, address).Scan(&duplicate); e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if duplicate {
			return loginFinish(a, aw.DeniedRollback)
		}
		if e = a.SealPlan(plan); e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if e = a.Acquire(ctx, aw.P); e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		b, e := a.StartedAt()
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		expiry := b.Add(s.cfg.ChallengeLifetime)
		st, e := store.NewWriter(a)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		pending, e := st.CreatePendingRegistration(ctx, store.PendingAccount{PersonID: personID, EmailID: emailID, CredentialID: credentialID, ChallengeID: challengeID, InstallationID: s.cfg.InstallationID, ApplicationID: s.cfg.ApplicationID, EmailAddress: address, PasswordHash: hash, ChallengeDigest: digest[:], ChallengeExpiry: expiry})
		if e != nil {
			return loginStoreFailure(e)
		}
		if e = acquire(ctx, a, aw.S, aw.W); e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		bootstrap, e := personal.NewWriter(a)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if _, e = bootstrap.BootstrapPending(ctx, pending, workspaceID); e != nil {
			if errors.Is(e, personal.ErrPersonUnavailable) || errors.Is(e, personal.ErrWorkspaceRepairRequired) || errors.Is(e, personal.ErrOwnerMismatch) {
				return aw.DeniedRollback
			}
			return aw.UnavailableRollback
		}
		if e = s.cfg.Email.QueueExistingChallengeWriter(ctx, a, personID, emailID, challengeID, requestID, token); e != nil {
			if errors.Is(e, email.ErrChallengeUnavailable) {
				return aw.DeniedRollback
			}
			return aw.UnavailableRollback
		}
		// All mutation and delivery work is complete. The final row comparison is
		// plain SQL under the held plan; no new lock or credential is introduced.
		tx, e = a.ParticipantTx(ctx, aw.W, rows)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		evidence, e := wp.Challenge(a, wp.Register, wp.ChallengeCheck{Subject: wp.Subject{Person: personID, Realm: s.realm(), Epoch: 0}, Contact: wp.Contact{ID: emailID, ComparisonKey: address}, ID: challengeID, TokenDigest: digest, CreatedAt: b, ExpiresAt: expiry, VerifiedAt: b})
		if e != nil {
			return loginProofFailure(a, e)
		}
		binding, e = a.Binding()
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		f, e := a.DrainAndSample(ctx)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		var matches bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.identity_persons p JOIN public.identity_emails e ON e.person_id=p.id JOIN public.identity_credentials c ON c.person_id=p.id JOIN public.identity_challenges h ON h.person_id=p.id AND h.email_id=e.id JOIN public.workspaces w ON w.personal_owner_id=p.id WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='pending_verification' AND p.security_epoch=0 AND e.id=$4 AND e.installation_id=$2 AND e.application_id=$3 AND e.comparison_key=$5 AND e.verified_at IS NULL AND c.id=$6 AND c.method='email_password' AND c.verifier_hash=$7 AND c.revoked_at IS NULL AND h.id=$8 AND h.purpose=$9 AND h.token_digest=$10 AND h.created_at=$11 AND h.expires_at=$12 AND h.consumed_at IS NULL AND w.id=$13 AND w.installation_id=$2 AND w.application_id=$3 AND w.kind='personal' AND w.state='active')`, personID, s.cfg.InstallationID, s.cfg.ApplicationID, emailID, address, credentialID, hash, challengeID, store.ChallengeEmailVerification, digest[:], b, expiry, workspaceID).Scan(&matches)
		if e != nil {
			return loginFinish(a, aw.UnavailableRollback)
		}
		if !matches {
			return loginFinish(a, aw.DeniedRollback)
		}
		permit, e = wp.Finalize(a, evidence, f)
		if e != nil {
			return loginProofFailure(a, e)
		}
		return loginFinish(a, aw.Success)
	})
	if duplicate && errors.Is(err, aw.ErrDenied) {
		write(w, r, http.StatusAccepted, "identity.registration_accepted", "If the address can be registered, a verification message will be sent.")
		return
	}
	if err != nil {
		unavailable(w, r)
		return
	}
	release, err := completion.TakeRelease(binding)
	if err != nil || !permit.MatchesRelease(release) {
		unavailable(w, r)
		return
	}
	write(w, r, http.StatusAccepted, "identity.registration_accepted", "If the address can be registered, a verification message will be sent.")
}
