package migrations

// RuntimeBinding appends the immutable per-database deployment identity. The
// reference generator assigns sequence 10 after its todo and webhook entries.
func RuntimeBinding(sequence uint64) (Fragment, error) {
	sql, err := fragments.ReadFile("fragments/runtime-binding.sql")
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Namespace: "runtime", Migrations: []Migration{{Sequence: sequence, Name: "deployment_binding", SQL: string(sql)}}}, nil
}
