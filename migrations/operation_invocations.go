package migrations

// OperationInvocations is the additive invocation/capacity/audit fragment.
// The integrator allocates sequence 17 in the existing reference composition.
// It does not enable an executor or grant an application allocation privileges.
func OperationInvocations(sequence uint64) (Fragment, error) {
	if sequence == 0 {
		return Fragment{}, ErrInvalidRegistry
	}
	sql, err := fragments.ReadFile("fragments/operation-invocations.sql")
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Namespace: "operation", Migrations: []Migration{{Sequence: sequence, Name: "invocations", SQL: string(sql)}}}, nil
}
