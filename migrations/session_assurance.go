package migrations

// SessionAssurance appends durable, time-bounded assurance metadata. Reference
// applications reserve sequence 12; applying it is required before enabling
// session.Config.PersistAssurance.
func SessionAssurance(sequence uint64) (Fragment, error) {
	sql, err := fragments.ReadFile("fragments/session-assurance.sql")
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Namespace: "identity", Migrations: []Migration{{Sequence: sequence, Name: "session_assurance", SQL: string(sql)}}}, nil
}
