# Native local application host: 2026-10-01

Source batch: `0a71fba`, based on the published password UI and TOTP services.

Independent headless Luna source review found no blocking issue in this exact source batch. The reviewer ran no tests; the checks below were obtained by the integrator.

## Obtained checks

- Fresh `go test -race ./apphost ./delivery/email/localcapture ./examples/reference/ui ./internal/runtime -count=1` passed against required PostgreSQL.
- Scoped `go vet` passed and pinned `golangci-lint` reported zero issues.
- Public artifact and planning checks passed; planning checks do not qualify product execution.
- Composed HTTP tests exercise signup, captured email confirmation, sign-in, personal todos, sign-out, CSRF/Origin denial, TOTP confirmation/replay denial and exact durable assurance expiry.
- Startup rejects a foreign environment, foreign quarantined receipt and missing session-assurance schema. The missing-column check actually changes the isolated test schema; startup must fail visibly.
- Mailbox checks reject remote origins, shared directories and symlinks, and verify private bounded idempotent messages.

## Qualification boundary

This is a loopback evaluation host with local email capture and local encrypted factor storage. It is not live email delivery, live Stripe, organization or enterprise authentication policy, managed key storage, cloud deployment or complete SaaS release qualification. Generator/CLI shipping is a separate batch. No planned architecture or later authentication, billing, infrastructure and operational stage is removed.
