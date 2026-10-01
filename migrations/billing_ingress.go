package migrations

// BillingWebhookIngress returns the additive normalized ingress migration at
// the caller's next application sequence. The integrator reserves sequence 9
// for generated reference apps whose todo foundation is sequence 8.
func BillingWebhookIngress(sequence uint64) (Fragment, error) {
	sql, err := fragments.ReadFile("fragments/billing-webhook-ingress.sql")
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{Namespace: "billing", Migrations: []Migration{{Sequence: sequence, Name: "verified_webhook_ingress", SQL: string(sql)}}}, nil
}
