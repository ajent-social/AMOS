# ADR049: keep unreviewed vulnerability evidence on restricted hold

Status: adopted design; source acceptance and private-channel verification remain
separate. Independent review and landing precede source dispatch.

Adopt [restricted routing v1.36](../security/vulnerability-routing.md) as the initial
local T14.7 core. An immutable pure decision always holds unreviewed evidence and
reports private handling unavailable. Finite reporter/analysis/signal observations
can prioritize a reason but cannot authorize public automation, private delivery
or lift an embargo. No raw diagnostic data or caller-provided verified-channel
switch enters this API.

This is deliberately narrower than complete routing: no existing publisher,
private transport or external intake is qualified. T14.7 remains in progress until
its downstream and private-channel gates are met. Future release/publication
capabilities require separately reviewed current authority and evidence.
