package host

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ajent-social/amos/examples/continuity/repository"
	"github.com/ajent-social/amos/examples/continuity/ui/sourceview"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

type sourceQuery struct {
	WorkspaceID        uuid.UUID
	ID                 string
	After, Query, Kind string
	Limit              int
	Fragment           bool
}

func validSourceQuery(q sourceQuery) bool {
	if q.WorkspaceID != uuid.Nil && (q.WorkspaceID.Version() != 7 || q.WorkspaceID.Variant() != uuid.RFC4122) {
		return false
	}
	if q.ID != "" {
		id, e := uuid.Parse(q.ID)
		return e == nil && id.String() == q.ID && id.Version() == 7 && id.Variant() == uuid.RFC4122 && q.After == "" && q.Query == "" && q.Kind == "" && q.Limit == 0
	}
	return len(q.After) <= 36 && len(q.Query) <= 480 && len(q.Kind) <= 14 && q.Limit >= 1 && q.Limit <= 100
}
func (c *readCore) personalRead(p identity.Principal, s workspacecontext.Selection) error {
	w := s.Workspace
	if p.Actor().Kind() != "person" || p.PersonID() == uuid.Nil || p.SecurityEpoch() < 0 || p.InstallationID() != c.realm.InstallationID || p.ApplicationID() != c.realm.ApplicationID || p.EnvironmentID() != c.realm.EnvironmentID {
		return errDenied
	}
	if w.Kind != workspacestore.KindPersonal {
		return errUnavailable
	}
	if w.Scope.InstallationID != p.InstallationID() || w.Scope.ApplicationID != p.ApplicationID() || w.PersonalOwnerID != p.PersonID() || w.State != workspacestore.WorkspaceActive || w.Epoch < 0 || s.Membership != nil || s.MembershipEpoch != 0 || len(s.Permissions) != 0 {
		return errDenied
	}
	return nil
}
func samePersonalSelection(a, b workspacecontext.Selection) bool {
	x, y := a.Workspace, b.Workspace
	return x.ID == y.ID && x.Scope == y.Scope && x.Kind == y.Kind && x.State == y.State && x.PersonalOwnerID == y.PersonalOwnerID && x.Epoch == y.Epoch && a.Membership == nil && b.Membership == nil && a.MembershipEpoch == 0 && b.MembershipEpoch == 0 && len(a.Permissions) == 0 && len(b.Permissions) == 0
}
func readClass(e error) error {
	switch {
	case errors.Is(e, session.ErrUnauthenticated), errors.Is(e, workspacecontext.ErrDenied):
		return errDenied
	case errors.Is(e, workspacecontext.ErrInvalidSelector), errors.Is(e, repository.ErrInvalid):
		return errInvalid
	case errors.Is(e, repository.ErrNotFound):
		return errNotFound
	default:
		return errUnavailable
	}
}

// source never reads a public Principal from context. Only this instance's
// privately admitted session can pass its native recheck. Bytes remain private
// until the exact Root.Read transaction has committed successfully.
func (c *readCore) source(ctx context.Context, q sourceQuery) ([]byte, error) {
	if c == nil || c.root == nil || c.sessions == nil || c.workspaces == nil || ctx == nil || ctx.Err() != nil {
		return nil, errUnavailable
	}
	if !validSourceQuery(q) {
		return nil, errInvalid
	}
	var buffered []byte
	reason := errUnavailable
	e := c.root.Read(ctx, func(ctx context.Context, tx *sql.Tx) error {
		fail := func(e error) error {
			reason = e
			if e == errDenied || e == errNotFound || e == errInvalid {
				return aw.ErrDenied
			}
			return aw.ErrUnavailable
		}
		principal, e := c.sessions.RecheckCurrentTx(ctx, tx)
		if e != nil {
			return fail(readClass(e))
		}
		selected, e := c.workspaces.ResolveCurrentTx(ctx, tx, principal, q.WorkspaceID)
		if e != nil {
			return fail(readClass(e))
		}
		if e = c.personalRead(principal, selected); e != nil {
			return fail(e)
		}
		repo, e := repository.New(tx, repository.Scope{InstallationID: principal.InstallationID(), ApplicationID: principal.ApplicationID(), EnvironmentID: principal.EnvironmentID(), WorkspaceID: selected.Workspace.ID})
		if e != nil {
			return fail(errUnavailable)
		}
		if q.ID == "" {
			values, e := repo.Sources(ctx, q.After, q.Query, q.Kind, q.Limit)
			if e != nil {
				return fail(readClass(e))
			}
			buffered, e = sourceview.RenderSources(sourceview.SourceList{After: q.After, Query: q.Query, Kind: q.Kind, Limit: q.Limit, Sources: values}, q.Fragment)
			if e != nil {
				return fail(errUnavailable)
			}
		} else {
			value, e := repo.Source(ctx, q.ID)
			if e != nil {
				return fail(readClass(e))
			}
			buffered, e = sourceview.RenderSource(value, q.Fragment)
			if e != nil {
				return fail(errUnavailable)
			}
		}
		if len(buffered) == 0 || len(buffered) > 512*1024 {
			return fail(errUnavailable)
		}
		if _, e = tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
			return fail(errUnavailable)
		}
		finalSelection, e := c.workspaces.ResolveCurrentTx(ctx, tx, principal, selected.Workspace.ID)
		if e != nil {
			return fail(readClass(e))
		}
		finalPrincipal, e := c.sessions.RecheckCurrentTx(ctx, tx)
		if e != nil {
			return fail(readClass(e))
		}
		// No SQL, renderer or callback follows the final session-owned DB instant.
		if !samePersonalSelection(selected, finalSelection) {
			return fail(errDenied)
		}
		if e = c.personalRead(finalPrincipal, finalSelection); e != nil {
			return fail(e)
		}
		return nil
	})
	if e != nil {
		if errors.Is(e, aw.ErrDenied) {
			return nil, reason
		}
		return nil, errUnavailable
	}
	return buffered, nil
}
