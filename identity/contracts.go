package identity

import (
	"context"
	"time"

	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/google/uuid"
)

type ID = uuid.UUID

type AssuranceLevel string

const (
	AAL1 AssuranceLevel = "aal1"
	AAL2 AssuranceLevel = "aal2"
	AAL3 AssuranceLevel = "aal3"
)

// Principal is created only by trusted authentication plumbing. It carries
// identity and installation scope; workspace permissions are resolved later.
type Principal struct {
	installationID  uuid.UUID
	applicationID   uuid.UUID
	environmentID   uuid.UUID
	personID        uuid.UUID
	securityEpoch   int64
	authMethod      string
	authenticatedAt time.Time
	assurance       Assurance
}

type Assurance struct {
	level     AssuranceLevel
	expiresAt time.Time
}

type Actor struct {
	kind      string
	personID  uuid.UUID
	machineID uuid.UUID
}

func (a Actor) Kind() string         { return a.kind }
func (a Actor) PersonID() uuid.UUID  { return a.personID }
func (a Actor) MachineID() uuid.UUID { return a.machineID }

type Grant struct {
	workspaceID uuid.UUID
	permissions []string
	revision    string
	expiresAt   time.Time
}

func (g Grant) WorkspaceID() uuid.UUID { return g.workspaceID }
func (g Grant) Permissions() []string  { return append([]string(nil), g.permissions...) }
func (g Grant) Revision() string       { return g.revision }
func (g Grant) ExpiresAt() time.Time   { return g.expiresAt }

func (a Assurance) Level() AssuranceLevel        { return a.level }
func (a Assurance) ExpiresAt() time.Time         { return a.expiresAt }
func (p Principal) InstallationID() uuid.UUID    { return p.installationID }
func (p Principal) ApplicationID() uuid.UUID     { return p.applicationID }
func (p Principal) EnvironmentID() uuid.UUID     { return p.environmentID }
func (p Principal) PersonID() uuid.UUID          { return p.personID }
func (p Principal) SecurityEpoch() int64         { return p.securityEpoch }
func (p Principal) AuthenticationMethod() string { return p.authMethod }
func (p Principal) AuthenticatedAt() time.Time   { return p.authenticatedAt }
func (p Principal) Assurance() Assurance         { return p.assurance }
func (p Principal) Actor() Actor {
	if p.personID == uuid.Nil {
		return Actor{}
	}
	return Actor{kind: "person", personID: p.personID}
}
func (p Principal) Grants() []Grant { return nil }

type contextKey struct{}

func principalContext(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	p, ok := ctx.Value(contextKey{}).(Principal)
	if !ok || p.personID == uuid.Nil || p.installationID == uuid.Nil || p.applicationID == uuid.Nil || p.environmentID == uuid.Nil {
		return Principal{}, false
	}
	return p, true
}

// ContextWithVerifiedCredential is the narrow bridge used by identity's
// verified-authentication adapter. The value itself can only be created
// inside the identity subtree after credential verification.
func ContextWithVerifiedCredential(ctx context.Context, proof authproof.VerifiedCredential) context.Context {
	p := Principal{installationID: proof.InstallationID(), applicationID: proof.ApplicationID(), environmentID: proof.EnvironmentID(), personID: proof.PersonID(), securityEpoch: proof.SecurityEpoch(), authMethod: proof.Method(), authenticatedAt: proof.AuthenticatedAt(), assurance: Assurance{level: AssuranceLevel(proof.Assurance()), expiresAt: proof.AssuranceExpires()}}
	return principalContext(ctx, p)
}
