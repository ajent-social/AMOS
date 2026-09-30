// Package authproof defines credential proof values available only to the
// identity subtree. Constructors must only be called after credential
// verification and a fresh authoritative account-state lookup.
package authproof

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type VerifiedCredential struct {
	personID, installationID, applicationID, environmentID uuid.UUID
	securityEpoch                                          int64
	method                                                 string
	authenticatedAt                                        time.Time
	assurance                                              string
	assuranceExpires                                       time.Time
}

func NewVerifiedCredential(personID, installationID, applicationID, environmentID uuid.UUID, epoch int64, method string, authenticatedAt time.Time, assurance string, assuranceExpires time.Time) (VerifiedCredential, error) {
	if !valid(personID) || !valid(installationID) || !valid(applicationID) || !valid(environmentID) || epoch < 0 || method == "" || authenticatedAt.IsZero() || (assurance != "aal1" && assurance != "aal2" && assurance != "aal3") || assuranceExpires.Before(authenticatedAt) {
		return VerifiedCredential{}, errors.New("invalid verified credential proof")
	}
	return VerifiedCredential{personID: personID, installationID: installationID, applicationID: applicationID, environmentID: environmentID, securityEpoch: epoch, method: method, authenticatedAt: authenticatedAt.UTC(), assurance: assurance, assuranceExpires: assuranceExpires.UTC()}, nil
}

func (p VerifiedCredential) PersonID() uuid.UUID         { return p.personID }
func (p VerifiedCredential) InstallationID() uuid.UUID   { return p.installationID }
func (p VerifiedCredential) ApplicationID() uuid.UUID    { return p.applicationID }
func (p VerifiedCredential) EnvironmentID() uuid.UUID    { return p.environmentID }
func (p VerifiedCredential) SecurityEpoch() int64        { return p.securityEpoch }
func (p VerifiedCredential) Method() string              { return p.method }
func (p VerifiedCredential) AuthenticatedAt() time.Time  { return p.authenticatedAt }
func (p VerifiedCredential) Assurance() string           { return p.assurance }
func (p VerifiedCredential) AssuranceExpires() time.Time { return p.assuranceExpires }
func valid(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
