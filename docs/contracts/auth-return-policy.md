# Native sign-in return destinations

Amendment v1.27 adds immutable consumer-owned return destinations to the native
password UI. It supplements T6.3 and keeps authentication/resource authority in
the existing handlers and services.

## Public shape

```go
// package authpassword
type ReturnPolicy struct {
    Default string
    Allowed []string
}
func (ReturnPolicy) Validate() error
// Config adds ReturnPolicy *ReturnPolicy.
// apphost.LocalConfig adds AuthReturnPolicy *authpassword.ReturnPolicy.
```

A nil policy retains all existing safeReturn behavior, including the legacy
`/todos` default and existing allowlist. A configured policy requires 1–32 unique
allowed paths and an explicitly included Default. Paths are at most 2,048 bytes,
canonical application-local absolute paths: `/` or nonempty ASCII-unreserved
segments separated by one slash. Reject dot/dot-dot/empty segments, trailing
slashes other than root, schemes/hosts/credentials, percent escapes, backslashes,
controls, queries and fragments. Matching is case-sensitive and exact.
Constructors reject invalid configuration, copy the policy and Allowed slice,
and never mutate caller memory. Host validation/copy precedes dependency opening.
The zero-value struct is invalid when explicitly configured.

## Request behavior

The existing query/form parser decodes transport fields once. The resulting
`next` value selects only an exact configured allowed path; any empty, unknown
or noncanonical selection falls back to the validated Default. No further URL
unescaping, path normalization, prefix matching, query preservation or external
redirect is performed. GET sign-in places that safe value in the existing hidden
`next` input. Successful POST sign-in returns the existing 303 to the safe path;
wrong-credential forms preserve the same safe selection. With configured policy,
missing-field sign-in forms also preserve it. Duplicate/invalid form fields retain
the current parser rejection. Public redirects never carry credential/challenge
values. Nil-policy behavior remains compatible.

Owner-authored destinations must be backed by appropriate application handlers.
This policy grants no ability to register reserved routes, bypass session or
workspace authorization, or infer current organization membership. Application
owners qualify the destination's own access controls. Signup, verification,
recovery, reset, signout, cookies, Origin/CSRF, protection, headers and status
semantics remain unchanged. Theme and return policy are independently optional.

## Source assignment and acceptance

After independently reviewed design landing, the integrator owns only new
`ui/authpassword/return_policy.go` and tests, minimal `pages.go` call-site changes,
`apphost/local.go` propagation and directly related host/browser tests. No identity,
workspace, runtime reservation, migration, module or production-host changes.

Prove nil-policy compatibility; constructor validation and deep copying; allowed,
empty, unknown, external, protocol-relative, escaped/traversal, query/fragment and
prefix-confusion selection; failed/missing credential preservation; unchanged
Origin/form rejection and 303/cookie semantics. Exercise an actual PostgreSQL
NewLocal sign-in to a session-protected consumer route without the legacy todos
alias. Real browser checks prove the native hidden field and no-JavaScript
form redirect; synthetic browser authority must be labelled. An isolated mutation
that trusts arbitrary next values must fail the unsafe-destination regression,
then exact restoration must pass. Independent source review, guarded rebase and
landed checks precede publishing the seam.
