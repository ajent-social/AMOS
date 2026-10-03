# AMOS

AMOS is an open-source, self-hosted foundation for building and operating paid SaaS applications that people and AI agents can use.

Each application owner runs their own installation. AMOS supplies the shared account, workspace, billing, API and operational capabilities so developers can focus on their business features.

## The intended developer experience

```sh
amos init
amos deploy
```

The interactive initializer and deployment commands are the target workflow. A config-driven local initializer is available below. The initializer will generate a Go application, default UI, GitHub Actions workflows and infrastructure as code. Deployment will bring the application online at its own domain.

AMOS owns identity, workspace, policy, audit, billing and API/MCP application capabilities. The business application owns its landing page and business routes on the same public domain. The default integrates business code into the Go application; a separate-service option will support other stacks. AMOS remains useful during external workflow-service outages.

## Planned capabilities

- Personal and organization workspaces, invitations, roles and account administration.
- Email/password, magic links, social login, passkeys, enterprise OIDC and MFA.
- Flat-rate, seat-based and usage-based subscriptions through a provider abstraction, starting with Stripe.
- Web interfaces and APIs, with MCP generated from OpenAPI so independently operated agents can connect. Mobile, SMS and end-user CLI clients come later.
- AWS deployment with required Cloudflare integration, offering managed containers/database and a small virtual machine profile.
- Deployment, monitoring, backups, recovery, immutable release and upgrade profiles, and owner controlled bounded domain maintenance.

Application code and Pulumi infrastructure programs use Go. The default UI uses server-rendered HTML, HTMX and plain JavaScript/CSS, and is replaceable. AMOS enforces application authorization; external agents retain their own governance.

## Implementation status

AMOS is under active development. It is not yet a complete runnable SaaS or a qualified production deployment.

Implemented foundations include real PostgreSQL test infrastructure and transactional migrations; configuration and policy validation; durable jobs and an outbox; email delivery and audit components; identity storage, sessions, password hashing, email verification and signup/sign-in components; workspace persistence, personal workspace bootstrap and request context; OpenAPI validation and Go code generation; a tenant-scoped reference todo service; the default UI renderer and protected template overrides; atomic initializer generation; and scoped billing intents and persistence.

The generated local application composes signup, explicit email verification, sign-in, a personal workspace, reference todos, password recovery and guarded TOTP services. The execution record currently accepts 57 of 257 planned tasks; this is a task count, not a completion percentage. Organization administration, complete subscription processing, deployment commands, cloud infrastructure, MCP, upgrades and operational recovery remain to be completed. Local generation is qualified only for the documented evaluation path; billing and production are not qualified. Local checks and provider fixtures do not qualify live services.

AMOS owns application identity, workspaces, policy, audit, billing and API/MCP boundaries. An external code-change lifecycle service owns PR safety and the executable author, independent review, bounded fix, re-review and landing tasks. A separate product workflow controller owns product delivery and qualification. AMOS may diagnose owner-local issues, generate proposals and upgrades, and run bounded domain jobs; it does not build a second task or PR orchestrator. Release and upgrade profiles are immutable and owner reviewed. Restricted implementation source is not copied into this public repository.

See the [current roadmap](docs/roadmap.md) and [execution record](docs/planning/execution-state.json) for task-level progress.

## Generate a local evaluation app

From this checkout, with the supported Go toolchain installed:

```sh
mkdir -p ../amos-apps
cat > initializer.json <<'JSON'
{
  "schemaVersion": 1,
  "appSlug": "todo-demo",
  "module": "example.test/todo-demo",
  "parentDir": "../amos-apps",
  "target": "todo-demo",
  "modules": ["identity", "workspace"],
  "publicOrigin": "http://127.0.0.1:8080",
  "businessMode": "integrated-go",
  "mode": "evaluation"
}
JSON
go run ./cmd/amos init --config initializer.json --framework-source .
go build -o ../amos-apps/amos-dev ./cmd/amos
cd ../amos-apps/todo-demo
mkdir -p .amos/bin
go build -o .amos/bin/app ./cmd/app
../amos-dev dev --project todo-demo --dir . --binary .amos/bin/app
```

The native development runner requires running Podman and an available PostgreSQL
image. It reads the private generated configuration, migrates and starts the Go
server, and preserves its database volume when stopped. The alternative
`./scripts/dev` requires an installed Podman Compose provider. Missing prerequisites fail visibly. If the database
is already running on that port, use its explicitly configured migration and
runtime URLs with `go run ./cmd/app migrate` and `go run ./cmd/app serve`.
The database must be a disposable local evaluation database.

Visit `http://127.0.0.1:8080/signup`. Verification and reset messages are captured
in private `.amos/mail` files; open the message's confirmation link and submit
the confirmation form. Keep `.amos` and `.env.local` private and retain the
generated framework bundle. Owner business code is in `app/business`, UI in
`ui`, and reference business migrations in `migrations`.

This is a loopback evaluation installation. Local capture does not send live
email. The Compose scripts have not been qualified in the current environment
because its Compose provider is absent; the generated executable, real
PostgreSQL and Chromium lifecycle have been qualified directly. Billing and
production generation fail explicitly until their composition is available.
See [generator evidence](docs/evidence/native-generator-20261001.md) and
[native runner evidence](docs/evidence/native-dev-supervision-20261001.md).
`amos-dev status --project todo-demo --dir .` inspects the local installation;
`amos-dev clean --plan --project todo-demo --dir .` is advisory only.

## Development and architecture

- [Development prerequisites](docs/development.md)
- [Migration behavior](docs/migrations.md)
- [Vision](docs/VISION.md)
- [Architecture RFC](docs/rfc/rfc-0001.md)
- [Complete staged implementation plan](docs/plan.md)
- [Contributor/session resume guide](docs/RESUME.md)
- [AMSL reuse strategy](docs/planning/amsl-boundary.md)

## License

Original AMOS code is licensed under [Apache 2.0](LICENSE). Third-party licenses and notices are preserved in [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES.md).
