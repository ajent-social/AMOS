# Native writer participant

`NewWriter` retains the original authority attempt. Mutations recheck realm,
sealed row inventory and completed acquisition before using existing SQL.
Mutating row locks require update or reserved-insert access; a share acquisition
cannot silently become an update lock. Each mutated workspace or membership is
journaled before SQL. State changes compare the complete current affected-person
set with the acquired plan. The shared gate prevents another participating writer
from changing that set between discovery and mutation.

The participant is lexical and serial, like the original transaction store.
It neither authenticates a principal nor supplies operation policy. The native
root must perform private session recheck and workspace authorization. Failure
of a mutating participant ends the attempt; callers return its matching rollback
outcome without attempting another Finish. Invalid plans and dependencies are
unavailable; missing current domain state is a denial. No SQL is admitted after
terminal state or the original budget.

Required-service tests use a separate process for immutable W1 activation and
fail if the TLS fixture configuration is absent. Their scope is workspace
persistence and rollback, not complete current-session/policy composition.
