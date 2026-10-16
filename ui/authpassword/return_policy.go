package authpassword

import "errors"

// ErrInvalidReturnPolicy means the owner-supplied navigation policy is invalid.
var ErrInvalidReturnPolicy = errors.New("invalid native auth return policy")

// ReturnPolicy declares exact application-owned sign-in destinations. It grants
// no session, workspace or resource authority. New copies the value and slice.
type ReturnPolicy struct {
	Default string
	Allowed []string
}

// Validate checks a bounded canonical local allowlist and its included default.
func (p ReturnPolicy) Validate() error {
	if len(p.Allowed) == 0 || len(p.Allowed) > 32 {
		return ErrInvalidReturnPolicy
	}
	seen := make(map[string]bool, len(p.Allowed))
	for _, path := range p.Allowed {
		if len(path) > 2048 || path != "/" && !canonicalAssetPath(path) || seen[path] {
			return ErrInvalidReturnPolicy
		}
		seen[path] = true
	}
	if !seen[p.Default] {
		return ErrInvalidReturnPolicy
	}
	return nil
}

func (p *Pages) returnPath(next string) string {
	if p.cfg.ReturnPolicy == nil {
		return safeReturn(next)
	}
	for _, path := range p.cfg.ReturnPolicy.Allowed {
		if next == path {
			return path
		}
	}
	return p.cfg.ReturnPolicy.Default
}
