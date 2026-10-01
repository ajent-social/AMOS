package migrations

import "embed"

//go:embed fragments/*.sql
var fragments embed.FS

// Core returns the fixed, globally ordered foundation and optional application
// fragments. Application sequence numbers begin at 8. Existing migration bytes
// and IDs are immutable; upgrades append entries, never reorder or rewrite them.
func Core(application ...Fragment) (Registry, error) {
	entries := []struct{ file, namespace, name string }{
		{"identity.sql", "identity", "foundation"},
		{"jobs.sql", "jobs", "foundation"},
		{"workspace.sql", "workspace", "foundation"},
		{"audit.sql", "audit", "foundation"},
		{"billing.sql", "billing", "foundation"},
		{"identity-protection.sql", "identity", "authentication_limits"},
		{"email-material.sql", "delivery", "protected_email_material"},
	}
	all := make([]Fragment, 0, len(entries)+len(application))
	for i, e := range entries {
		sql, err := fragments.ReadFile("fragments/" + e.file)
		if err != nil {
			return Registry{}, err
		}
		all = append(all, Fragment{Namespace: e.namespace, Migrations: []Migration{{Sequence: uint64(i + 1), Name: e.name, SQL: string(sql)}}})
	}
	all = append(all, application...)
	return NewRegistry(all...)
}
