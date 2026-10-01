package apphost

import (
	"context"
	"database/sql"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/recovery"
	"github.com/google/uuid"
)

// localPasswordPolicy enables only independent personal email/password recovery
// in the explicitly local development host. Organization/enterprise policy is
// unavailable here and cannot silently be replaced with personal recovery.
type localPasswordPolicy struct{ installation, application uuid.UUID }

func (p localPasswordPolicy) AuthorizePasswordReset(ctx context.Context, tx *sql.Tx, person uuid.UUID) error {
	if ctx == nil || tx == nil {
		return recovery.ErrUnavailable
	}
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identity_persons p
 WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='active'
 AND EXISTS(SELECT 1 FROM workspaces w WHERE w.personal_owner_id=p.id AND w.installation_id=p.installation_id AND w.application_id=p.application_id AND w.kind='personal' AND w.state='active')
 AND NOT EXISTS(SELECT 1 FROM workspace_memberships m JOIN workspaces w ON w.id=m.workspace_id WHERE m.person_id=p.id AND m.installation_id=p.installation_id AND m.application_id=p.application_id AND m.state<>'left' AND w.kind='organization')
 AND NOT EXISTS(SELECT 1 FROM identity_external_bindings b WHERE b.person_id=p.id AND b.provider='enterprise_oidc'))`, person, p.installation, p.application).Scan(&allowed)
	if err != nil {
		return recovery.ErrUnavailable
	}
	if !allowed {
		return recovery.ErrPolicyDenied
	}
	return nil
}
func (p localPasswordPolicy) AuthorizePasswordChange(ctx context.Context, tx *sql.Tx, principal identity.Principal) error {
	if principal.InstallationID() != p.installation || principal.ApplicationID() != p.application {
		return recovery.ErrPolicyDenied
	}
	var current bool
	if ctx == nil || tx == nil {
		return recovery.ErrUnavailable
	}
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND state='active' AND security_epoch=$4)`, principal.PersonID(), p.installation, p.application, principal.SecurityEpoch()).Scan(&current); err != nil {
		return recovery.ErrUnavailable
	}
	if !current {
		return recovery.ErrPolicyDenied
	}
	return p.AuthorizePasswordReset(ctx, tx, principal.PersonID())
}

// localMFAPolicy admits only independent personal accounts in this evaluation
// profile. Organization and enterprise policy require their planned evaluator.
type localMFAPolicy struct{ localPasswordPolicy }

func (p localMFAPolicy) Ready(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_totp_factors WHERE false`).Scan(&count); err != nil {
		return err
	}
	return nil
}
func (p localMFAPolicy) AuthorizeMFA(ctx context.Context, tx *sql.Tx, principal identity.Principal, action string) error {
	switch action {
	case "totp.read", "totp.enroll", "totp.confirm", "totp.challenge":
	default:
		return recovery.ErrPolicyDenied
	}
	return p.AuthorizePasswordChange(ctx, tx, principal)
}
