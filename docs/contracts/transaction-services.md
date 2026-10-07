# Additive transaction-only service construction (v1.16)

Status: frozen after independent exact-head review and PR30, landed at `de3461086262d2c68a7f7bb770c8858f92a30bab`. No production host or role qualification follows.

A new minor amendment adds transaction-only construction to identity/session, identity/email, identity/login, identity/recovery, identity/mfa, identity/protection and delivery/email/materialstore. Existing New signatures, Config field names/types/order, return types, defaults and valid development behavior remain supported. The amendment changes no stored data, SQL migration, authentication policy, HTTP route or wire contract.

Each consuming package declares this exact interface:

```go
type TxRunner interface {
    WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error
}
```

It is structurally compatible with storage.TxRunner. Both storage.DB and storage.RuntimeDB pointer handles satisfy it. The interface adds no Pool, Close, PingContext, BeginTx, Migrate, credential or configuration method. Services store only the transaction capability for database work and never type-assert it to a richer handle, embed a storage handle, open a second connection or take ownership of closing it. This is capability minimization, not a SQL sandbox: trusted callbacks still receive *sql.Tx, and the database role must deny administrative SQL.

Add these public entry points; preserve the existing entry points:

```go
// package session
func NewWithTxRunner(db TxRunner, cfg Config) (*Service, error)
// package identity/email
func NewWithTxRunner(db TxRunner, outbox *sqlstore.Store, renderer *deliveryemail.Renderer, materials ProtectedMaterialWriter, cfg Config) (*Service, error)
// package login, recovery, mfa (each package has its own types)
func NewWithTxRunner(db TxRunner, cfg TxConfig) (*Service, error)
// package protection
func NewWithTxRunner(db TxRunner, cfg TxConfig) (*Limiter, error)
// package materialstore
func NewWithTxRunner(db TxRunner, cfg TxConfig) (*Store, error)
```

For each package introducing TxConfig, its exact fields, types, comments and order equal existing Config with only DB removed, as specified in the config appendix below. TxConfig neither embeds Config nor carries an alternate storage handle. Existing New explicitly maps its Config fields to TxConfig and forwards the concrete handle to one validation/initialization path. Session/email keep their existing DB-free Config. Service internals for login/recovery/MFA/protection keep private `db TxRunner` and `cfg TxConfig`; materialstore/session/email change their existing private db field to TxRunner. Existing exported service methods remain unchanged.

New transaction constructors reject a nil interface and every typed-nil implementation before invoking any dependency or transaction; they return the package's existing configuration sentinel and never panic for nil admission. Existing sentinels are session.ErrInvalidConfiguration, identity/email.ErrInvalidRequest, and ErrConfiguration for the other five packages. No ping/probe transaction, fallback connection or schema migration occurs in construction. Non-nil zero/closed storage handles are not certified ready by construction; subsequent operations preserve existing fail-closed error mapping. Do not call WithTx merely to test readiness.

Non-nil value implementations remain valid. A kind-guarded reflect check may reject nil pointers, maps, slices, functions, channels and interfaces without calling reflect.Value.IsNil on non-nilable kinds. The check does not recover arbitrary panics from caller-provided methods or reinterpret transaction callback panic semantics. The new guarantee concerns the new TxRunner input; broad changes to unrelated existing optional interfaces require separate review.

Preserve transaction options, callback boundaries, rollback/error behavior, realm checks, material encryption/purpose binding, session issuance-after-commit, current policy, MFA replay fencing and all existing fail-closed HTTP mappings. Mail-producing service construction still receives the existing concrete sqlstore.Store; transaction-only end-to-end composition requires that store to have been built through its separately frozen transaction constructor. Do not add an ad hoc outbox interface or raw pool accessor in this amendment. No constructor can infer same-database identity across arbitrary injected runners; trusted composition must bind all participants to the same intended database, and atomicity tests must verify it.


## Exact TxConfig fields

Package `identity/login`:

```go
type TxConfig struct {
	Passwords                                    *password.Hasher
	Email                                        *email.Service
	Sessions                                     *session.Service
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ChallengeLifetime                            time.Duration
}
```

Package `identity/recovery`:

```go
type TxConfig struct {
	Passwords                                    *password.Hasher
	Outbox                                       *sqlstore.Store
	Renderer                                     *deliveryemail.Renderer
	Materials                                    PasswordResetMaterialWriter
	Policy                                       Policy
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ApplicationOrigin                            string
	DevelopmentLoopback                          bool
	ChallengeLifetime                            time.Duration
}
```

Package `identity/mfa`:

```go
type TxConfig struct {
	Vault    SeedVault
	Primary  PrimaryProofVerifier
	Policy   Policy
	Sessions SessionIssuer
	Issuer   string
	Now      func() time.Time
}
```

Package `identity/protection`:

```go
type TxConfig struct {
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	// Key is stable across replicas and obtained through owner-controlled secret resolution.
	Key                   []byte
	Window                time.Duration
	IPLimit, AccountLimit int
}
```

Package `delivery/email/materialstore`:

```go
type TxConfig struct {
	DevelopmentLoopback                          bool
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	ActiveKeyID                                  string
	// Keys come from explicit owner secret resolution. Keep prior keys during
	// rotation until all retained material has expired; never persist key bytes.
	Keys              map[string][]byte
	ApplicationOrigin string
}
```

## Source and verification boundaries

First assignments are session and authentication protection only. Later leaf
assignments cover material storage, email, login, recovery and MFA; magic-link
requires a separate contract assignment before complete authentication composition.
Each source slice requires legacy function-type and Config compatibility tests,
nil/typed-nil admission tests with zero dependency calls, actual runtime-only TLS
PostgreSQL operations and rollback/cancellation behavior, scoped normal/race/vet/
lint, genuine negative/restored evidence, independent exact-head review and
landed checks. Missing service fails visibly. Existing development tests and
constructor doubles do not qualify the runtime role. No shared testkit, schema,
module, executable or apphost change is delegated by a leaf assignment.
