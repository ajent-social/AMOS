package migrations

// MFAProtection enables durable factor-specific admission; reference apps
// reserve sequence 14 after factor storage and session assurance migrations.
func MFAProtection(sequence uint64) (Fragment, error) {
	sql, err := fragments.ReadFile("fragments/identity-mfa-protection.sql")
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Namespace: "identity", Migrations: []Migration{{Sequence: sequence, Name: "mfa_protection", SQL: string(sql)}}}, nil
}
