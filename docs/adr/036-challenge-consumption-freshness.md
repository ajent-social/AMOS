# ADR 036: Sample challenge expiry after the row lock

Status: proposed; independent exact-head review and landing precede source.

The shared identity challenge consumer currently compares expiry against
transaction-start time while its update can wait. A post-lock database-time
check is required to avoid consuming a challenge whose validity elapsed during
the wait. Adopt the bounded [v1.22 proposal](../contracts/challenge-consumption-freshness.md):
lock by exact ID/purpose/digest, sample database time, then conditionally consume
using that same instant. Preserve method/result/error shapes and caller-owned
transactions, with an explicit READ COMMITTED compatibility restriction.

This corrects one store primitive. Moving all producer logic into this method
would broaden its authority and conflict with separately owned flows. Merely
substituting a clock expression inside the waiting UPDATE would not establish
post-wait sequencing. Complete producer lock graphs, email's inline consume,
current session provenance and resource authority remain separately gated.

No schema change, provider call, installation mutation, executor admission or
product acceptance is authorized by this proposal. Real waiter schedules and
independent negative/restored source verification must establish the correction.
