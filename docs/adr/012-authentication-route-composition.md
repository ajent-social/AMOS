# ADR 012: explicit authentication route composition

Status: accepted for implementation. Contract clarification v1.3.

Generated applications need to bind implemented identity handlers without
opening reserved AMOS prefixes to business route registration. `app.Options`
therefore accepts a finite `IdentityHandlers` object at startup. Its fields map
only to the exact signup/sign-in pages, signup/sign-in/signout POST operations,
and email verification preview/confirm routes. Missing handlers remain visibly
unavailable. Built-in health and readiness retain their ownership.

This is composition by the application owner, not an authentication proof or a
new transport exemption. Each supplied handler must still enforce its existing
credential, session, origin, CSRF and current-state checks. Business registration
continues rejecting reserved prefixes, case variants and wildcard shadowing.
Path normalization still runs before shared dispatch. The seam has no wildcard
or arbitrary shared route field. Later identity flows require explicit reviewed
extensions. It grants no live provider or deployment qualification.
