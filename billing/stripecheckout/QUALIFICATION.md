# Stripe checkout adapter qualification

Official stripe-go v87.0.0 and API 2026-09-30.endive are pinned by the integrator.
This original AMOS adapter consumes immutable billing.store intents; no private
upstream implementation or upstream consumer qualification is asserted.

The controller must persist and exclusively claim the exact request digest
before calling the adapter, and load the matching unknown intent before explicit
recovery. SDK and HTTP transport automatic write retries are disabled. Recovery
replays only within a conservative 23-hour window from durable intent creation;
older, missing or future creation times remain unknown for reconciliation or
manual review. Stripe may prune idempotency keys after at least 24 hours.
See [Stripe idempotency](https://docs.stripe.com/api/idempotent_requests).

The same credential and Connect context retrieve the authenticated account and
must match configured merchant identity before every mutation. Echoed metadata
alone is insufficient. Checkout requires a present matching customer, approved
catalog selection, fixed quantity one, scoped metadata and validated HTTPS URLs.
A successful browser return does not grant paid entitlement.

## Obtained evidence

Independent review identified merchant identity, old replay and missing customer
response gaps; the integrator corrected and independently rereviewed them.
The named fixture suite and vet passed; lint reported zero issues. Genuine
mutations removing account equality, replay age bound, required customer and
fresh transport each failed the corresponding regression. Restored named tests
passed. The transport mutation showed Go can replay a POST over a reused socket
even when SDK retries are zero; the dedicated transport disables connection reuse,
HTTP/2 and redirects. Fixture messages contain synthetic values only.

These are adapter boundary checks. Real Stripe account/price provisioning,
webhook reconciliation, subscription projection, durable controller wiring and
live provider qualification remain separate gates.
