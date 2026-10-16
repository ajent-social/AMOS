# Optional native password-page presentation

Amendment v1.26. This additive presentation contract preserves the native
password lifecycle and existing zero-configuration behavior. It supplements
T6.14; it grants no production-host or authorization capability.

## Public shape

```go
// package authpassword
type Theme struct {
    BrandName string
    LogoURL string
    StylesheetURL string
}
// Config adds Theme *Theme.
// apphost.LocalConfig adds AuthTheme *authpassword.Theme.
```

A nil theme preserves the existing document and restrictive CSP. A configured
theme requires a nonblank UTF-8 brand name (at most 100 Unicode code points,
no control characters). Logo and stylesheet are optional. Each nonempty asset
URL must be a canonical absolute application-local path: one leading slash,
ASCII unreserved path segments, no empty/dot/dot-dot segment, percent encoding,
backslash, scheme, host, credentials, query or fragment. Root alone is rejected.
Invalid configuration returns an error before serving. Constructors copy the
value; later caller mutation cannot alter rendered pages. The host validates
presentation before opening dependencies and passes the same copied value to
the native password pages.

Theme content is owner-authored presentation. A stylesheet is executable
presentation authority and must be served from application-owned static routes;
this capability never accepts per-request theme selection. Paths being local
is not authorization to serve arbitrary filesystem content.

## Rendering and compatibility

All native password pages and rendered outcomes share the theme, including
signup, signin, verification, recovery, reset, validation/denial/unavailable and
completed/expired-link pages. Text and URL attributes remain escaped through
html/template. Brand markup links to fixed `/`, optional logo has fixed 32 by
32 dimensions and empty alternative text beside visible brand text, and the
document title adds the brand. Shared classes are `amos-auth`, `amos-auth-brand`,
`amos-auth-main`, `amos-auth-form`, `amos-auth-message`, `amos-auth-links`.
The main landmark has stable id `main-content`; message announcements retain
role=status. Form fields remain labelled. No inline script/style, raw HTML,
custom credential handler, form/action override or template injection is added.

For configured themes only, add `style-src 'self'; font-src 'self'; img-src
'self'` to the existing `default-src 'none'; form-action 'self'; base-uri 'none';
frame-ancestors 'none'` CSP. Scripts remain disallowed. The library does not
fetch assets. The consumer supplies same-origin responses and correct content
types for its stylesheet, fonts and logo.

Preserve native methods/status/redirects, guarded backend delegation, action
paths, input names, challenges, Origin/CSRF checks, no-store, referrer policy,
nosniff and HEAD without body. Nil-theme HTML remains byte-compatible. Unsupported
method/unknown-route transport errors need not become themed lifecycle pages.
The existing return-path allowlist and local-only listener restrictions remain.

## Acceptance and source assignment

After independent design review and landing, the integrator owns
`ui/authpassword/theme.go`, its tests, the minimal `pages.go` changes,
`apphost/local.go` propagation and directly relevant host integration tests.
No credential, workspace, storage, migration or module files are delegated.

Verify malicious URLs/controls and invalid UTF-8 fail; escaped brand text cannot
inject markup; constructor inputs are copied; nil-theme output/CSP is unchanged;
all rendered page outcomes and HEAD retain controls; the real NewLocal lifecycle
serves the configured presentation while native signup/verification/signin and
signout still execute against PostgreSQL. Browser checks must inspect real themed
markup and CSS loading, keyboard focus and narrow viewport behavior. Source,
actual-service and browser evidence are recorded separately. An isolated negative
mutation must demonstrate that the malicious-URL test detects missing validation.
Independent exact-head source review, guarded rebase merge and landed verification
precede reporting the seam available to consumers.
