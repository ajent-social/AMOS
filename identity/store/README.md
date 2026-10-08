# Identity transaction participants

`New(tx)` selects the immutable Legacy process profile and refuses W1 before SQL.
`NewWriter(attempt)` retains a live sealed attempt. Every writer mutation/locking
operation authorizes exact planned rows through `ParticipantTx`; each mutation
records its journal entry before SQL. It cannot issue or elevate a session.
`NewIssuer` additionally requires a live same-attempt native issuance, checked
without consuming the credential getter owned by session staging. W1 session
insertion requires the same credential's typed assurance pair and performs one
INSERT with one journal entry. Elevated assurance is not added by a second write.
Legacy zero-pair insertion retains its SQL shape without optional columns.

Digest-based lookups first authorize the sealed existing inventory, then perform
a scoped plain lookup and reauthorize the returned exact ID before locking or
mutating it. Empty inventory returns the generic missing-target result without
SQL. Password reads use already acquired exact person, credential and contact
rows; W1 never introduces a joined person/credential lock.

Compound registration is called after P acquisition. It inserts the reserved
person before acquiring C and inserting contact/credential, then acquires H and
inserts the challenge. IDs come from the frozen caller plan; creation timestamps
use the root's B sample. `PendingRegistration.InAttempt` requires the original
binding and a still-live acquired reserved parent, not transaction-pointer equality.

Writer validation and SQL failures terminally invalidate the attempt. A semantic
missing session/person/challenge from a staged SQL operation finishes denied
rollback; the native root must return the matching outcome rather than trying to
finish a second time. Discovery-only missing targets do not imply a mutation.
Callers own complete cross-environment person-session revocation inventories;
these methods never truncate those plans or acquire an unplanned target.

Source checks and synthetic issuance tests do not qualify the complete native
credential graph, required-service schedules, private runtime composition or
host activation. Optional schema fragments must be independently provisioned;
these participants never repair schema or privileges.
