# Authentication request protection

Compose `Guard.PublicJSON` before signup/sign-in credential handlers. It rejects
foreign origins and oversized or ambiguous JSON content types before durable
admission and expensive credential work. The shared password hasher independently
caps Argon2 concurrency. Install one stable owner-resolved HMAC key across replicas;
no raw IP or account input is stored in counters.

Compose `Guard.CookieMutation` inside authoritative session middleware. The guard
requires its current principal, the configured origin and an exact CSRF header or
single body form field. Query tokens confer no authority. The downstream handler
must independently authorize its operation and credential assurance. This wrapper
does not implement password changes or grant authority from an email address.

Only socket peer addresses are accepted. Forwarding headers are ignored. A
proxied production deployment needs separately qualified trusted proxy identity;
otherwise all traffic through one proxy shares the same conservative IP budget.
Provider callbacks need their own state/nonce/session binding and are not generic
exemptions from this guard.

Counters use database transaction time and atomic row updates. IP denial stops
before creating a supplied account counter; account denial remains independent of
account existence. Denied responses are generic 429 with a bounded retry hint;
database failure is 503. Expired counters are reused. Installation maintenance
must prune expired rows under its separately reviewed retention policy.

The migration is additive. Do not remove active counters during an upgrade,
change a deployment key casually, or infer live proxy/provider qualification from
local SQL tests. Test credential/context fixtures isolate the guard boundary;
production composition must resolve fresh session and account state.
