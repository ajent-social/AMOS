# Native recovery writer participation

`NewWithWriter` retains the shared root, native session reader, bounded password
verifier, durable material writer and outbox participant. It rejects pool-backed
material/outbox configuration and requires the reviewed explicit completion
policy interface. Constructors perform no SQL. Legacy constructors and methods
remain behind the immutable legacy profile.

Request discovers the current verified account and all outstanding reset
challenges under G, seals P/C/H/W and D, consumes the old challenges, and stages
one original-bound challenge plus encrypted material and durable email intent.
The rate cap uses original B. Unknown accounts and capped requests return only
the existing generic acknowledgement; SQL or delivery failure remains unavailable.

Completion performs a current reset preflight, releases that transaction, and
uses bounded pure password hashing on the original admitted request context.
The writer then holds the exact account, challenge, policy inventory and every
unrevoked session across environments, including expired sessions. It consumes
the challenge, replaces the exact old hash, advances epoch once and revokes the
complete session set. At F it rechecks the exact intended tuple, original token
bounds, current reset policy and all revocations before releasing completion.

Password change requires native private current-session admission, refreshes the
policy principal on the held actor rows, and performs exactly one bounded primary
verification after C. It retains original password and actor evidence, hashes the
new password within the original budget, and makes the same epoch/session
transition. The completion policy checks the intentional epoch-plus-one using the
unchanged original actor, without constructing a post-change principal or session.
All final reads use the retained transaction and original root context; no locks,
mutation or clock sampling occurs after F.

The local host policy shares its existing personal-account business predicate
between initial and final change checks. This does not qualify organization or
enterprise policy. Fixed composition, same-database capabilities, complete
required-service schedules, independent review and full product gates remain
required. The required runtime test uses actual guarded request/completion and a
real hasher with synthetic account setup and policy; it does not qualify a mail
provider, the full password-change journey, or production policy.
