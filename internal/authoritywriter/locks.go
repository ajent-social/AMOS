package authoritywriter

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// Every statement is fixed native SQL. In particular session locks deliberately
// span environments for full-person revocation; native issue discovery remains
// environment scoped. Parent joins scope rows but lock only the selected child.
func rowQuery(table Table) (string, bool) {
	switch table {
	case Persons:
		return `SELECT r.id FROM public.identity_persons r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3`, false
	case Emails:
		return `SELECT r.id FROM public.identity_emails r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3`, false
	case Credentials:
		return `SELECT r.id FROM public.identity_credentials r JOIN public.identity_persons p ON p.id=r.person_id WHERE r.id=$1 AND p.installation_id=$2 AND p.application_id=$3`, false
	case Connections:
		return `SELECT r.id FROM public.identity_federation_connections r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3`, false
	case Bindings:
		return `SELECT r.id FROM public.identity_external_bindings r JOIN public.identity_persons p ON p.id=r.person_id WHERE r.id=$1 AND p.installation_id=$2 AND p.application_id=$3`, false
	case Challenges:
		return `SELECT r.id FROM public.identity_challenges r JOIN public.identity_persons p ON p.id=r.person_id WHERE r.id=$1 AND p.installation_id=$2 AND p.application_id=$3`, false
	case Factors:
		return `SELECT r.id FROM public.identity_totp_factors r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3 AND r.environment_id=$4`, true
	case Flows:
		return `SELECT r.id FROM public.identity_federation_flows r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3 AND r.environment_id=$4`, true
	case Sessions:
		return `SELECT r.id FROM public.identity_sessions r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3`, false
	case Workspaces:
		return `SELECT r.id FROM public.workspaces r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3`, false
	case Memberships:
		return `SELECT r.id FROM public.workspace_memberships r WHERE r.id=$1 AND r.installation_id=$2 AND r.application_id=$3`, false
	default:
		return "", false
	}
}
func lockRow(ctx context.Context, tx *sql.Tx, realm Realm, row Row) error {
	query, environment := rowQuery(row.Table)
	if query == "" {
		return ErrUnrooted
	}
	args := []any{row.ID, realm.Installation, realm.Application}
	if environment {
		args = append(args, realm.Environment)
	}
	switch row.Access {
	case ExistingUpdate:
		query += ` FOR UPDATE OF r`
	case ExistingShare:
		if row.Table != Connections {
			return ErrUnrooted
		}
		query += ` FOR SHARE OF r`
	case ReservedInsert:
	default:
		return ErrUnrooted
	}
	var found uuid.UUID
	err := tx.QueryRowContext(ctx, query, args...).Scan(&found)
	if row.Access == ReservedInsert {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
		return ErrDenied
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDenied
	}
	if err != nil || found != row.ID {
		return ErrUnavailable
	}
	return nil
}
