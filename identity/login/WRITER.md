# Native W1 producers

`NewWithWriter` is the private composition path for registration and password
sign-in. It retains the exact writer root, hasher, email service and session
service. The legacy constructors and retained legacy methods select the immutable
Legacy profile; they cannot perform SQL in W1. Constructors perform no SQL or
active-root probe. `Root.Run` and `Root.Read` enforce active ownership before SQL.
The host must construct every dependency from the same runtime and apply the real
protection Guard before reaching a native handler.

Registration hashes through bounded password work before G. The single attempt
creates P, C and H through the identity store, then personal workspace W and the
email service's material/job D participants. Pending registration never issues a
session. Password sign-in discovers under G, acquires the complete P/C set and
performs exactly one bounded verification against the held tuple. Optional rehash
is staged in that transaction. The complete person/contact/hash/epoch tuple is
compared again after the constraint drain, without repeating verification.

Magic request/confirmation use native challenge storage and typed delivery
participants. Confirmation binds the original digest, browser consent, contact,
request age and expiry. MFA uses the same native primary verifier, session-owned
actor admission and ordered factor store. Enrollment fixes pending expiry to the
original verification time. TOTP success and the sole-factor denial-counter branch
have distinct journals and completion outcomes. Status uses a session recheck and
a read-only factor store; it does not expire or activate factors.

All session-producing flows use the private staged-session fence at the exact
root final sample, then the method permit and committed one-use release. No
provisional cookie, enrollment seed or positive delivery acknowledgment is
published from the transaction callback. Read-only final comparisons use the
already acquired lexical transaction; they acquire no new row locks and perform
no mutation or password/vault work after F.

`TestNativeWriterRuntimeRequiredService` is required-service test source for the
composed native entry points. It fails when its precreated runtime-only TLS
fixture is absent and runs W1 in a separate process from Legacy tests. The fixture
must include the writer gate, workspace constraints, jobs/material, session
assurance, protection, MFA and magic browser-binding fragments. The test contains
synthetic activation and policy/key setup; that does not qualify the email-confirm
producer, production policy, external mail or any provider. No test creates schema.

Source compilation and pure tests do not qualify the actual wait, expiry,
rollback/commit, cross-environment revocation or full W1 schedules. Independent
exact-source review, separately authorized runtime checks and the private host's
same-database/cutover gates remain required. These changes do not admit current
workspace authority or increase product/task acceptance.
