package store

import (
	"strings"
	"time"
)

// The assurance pair is optional only for the unchanged legacy insertion API.
func sessionAssurance(v Session, required bool) error {
	if v.AssuranceLevel == "" && v.AssuranceExpires.IsZero() {
		if required {
			return ErrInvalidInput
		}
		return nil
	}
	if v.AssuranceExpires.IsZero() || !v.AssuranceExpires.After(v.AuthenticatedAt) || v.AssuranceExpires.After(v.ExpiresAt) {
		return ErrInvalidInput
	}
	switch v.AssuranceLevel {
	case "aal1":
		if !v.AssuranceExpires.Equal(v.ExpiresAt) {
			return ErrInvalidInput
		}
	case "aal2", "aal3":
		if v.AssuranceExpires.After(v.AuthenticatedAt.Add(15 * time.Minute)) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// Optional columns occur only for an explicitly elevated pair. Legacy zero
// values and the base profile work without the optional assurance migration.
func sessionInsert(v Session) (string, []any) {
	columns := `id, person_id, installation_id, application_id, environment_id,
 token_digest, security_epoch, authentication_method, authenticated_at,
 expires_at, idle_expires_at, issued_at, last_seen_at`
	values := `$1, p.id, p.installation_id, p.application_id, $5,
 $6, p.security_epoch, $8, $9, $7,
 LEAST($7, issuance_clock.now + interval '30 minutes'), issuance_clock.now, issuance_clock.now`
	args := []any{v.ID, v.PersonID, v.InstallationID, v.ApplicationID, v.EnvironmentID, v.TokenDigest, v.ExpiresAt, v.AuthenticationMethod, v.AuthenticatedAt, v.SecurityEpoch}
	assuranceFence := ""
	if v.AssuranceLevel != "" {
		// W1 retains the original verification instant; insertion cannot refresh I.
		values = strings.Replace(values, "issuance_clock.now + interval '30 minutes'", "$9::timestamptz + interval '30 minutes'", 1)
		args = append(args, v.AssuranceExpires)
		assuranceFence = ` AND $11::timestamptz > issuance_clock.now AND $9::timestamptz + interval '30 minutes' > issuance_clock.now`
		if v.AssuranceLevel != "aal1" {
			columns += `, assurance_level, assurance_expires_at`
			values += `, $12, $11`
			args = append(args, v.AssuranceLevel)
		}
	}
	query := `WITH issuance_clock AS MATERIALIZED (SELECT clock_timestamp() AS now)
 INSERT INTO identity_sessions (` + columns + `) SELECT ` + values + `
 FROM identity_persons p CROSS JOIN issuance_clock
 WHERE p.id = $2 AND p.installation_id = $3 AND p.application_id = $4
 AND p.state = 'active' AND p.security_epoch = $10
 AND $7 > issuance_clock.now
 AND $7 <= issuance_clock.now + interval '12 hours'
 AND $9 <= issuance_clock.now` + assuranceFence
	return query, args
}
