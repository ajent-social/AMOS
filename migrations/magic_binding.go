package migrations

// MagicBrowserBinding appends the optional purpose-restricted digest used by
// scanner-safe magic-link login. Generated reference apps reserve sequence 11.
func MagicBrowserBinding(sequence uint64) (Fragment, error) {
	sql, err := fragments.ReadFile("fragments/identity-magic-binding.sql")
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Namespace: "identity", Migrations: []Migration{{Sequence: sequence, Name: "magic_browser_binding", SQL: string(sql)}}}, nil
}
