# Bounded diagnostic payload decoder

This component implements only the byte/envelope projection in the frozen
[v1.37 intake design](../../../docs/contracts/upstream-intake.md). `Decode`
consumes caller-supplied bytes and explicit HTTP metadata, checks the existing
finite diagnostic profile, and returns independent canonical JSON copies. It has
no listener, admission, receipt, provenance, storage, witness or publication
capability. Original T14.4 remains incomplete; external collection stays disabled.

Callers must enforce the real HTTP framing/header limit and original five-second
network/read deadline before invoking this synchronous byte function. It cannot
cancel a reader, charge attempts or measure network time. ContentLength must come
from authoritative parsed HTTP framing, not be fabricated from `len(wire)` to
make truncated input look complete; -1 means unknown and is rejected. Preserve
ALL Content-Type/Encoding/Length field values and case aliases in Headers before
any parser discards or folds duplicate evidence. If framing already removed the
Content-Length field, its authoritative parsed length is still required. This
function cannot detect metadata erased by its caller. It independently bounds
supplied header field-line bytes, compressed bytes and decoded bytes.

The endpoint must reject Authorization/enrollment inputs without anonymous
fallback and durably charge an admitted attempt BEFORE body processing under the
full intake contract. Source identity, overload, quota, clock, encrypted object
custody, restore witness, retention and private early deletion remain separate
unqualified gates. This component must not be used to bypass those gates. Its
result confirms finite syntax only; recognized component fields do not prove
executing-code provenance, honest metrics, consent or accepted storage.

Gzip decoding reads through the checksum/EOF with multistream disabled, checks
zero remaining bytes, and rejects concatenated members or trailing garbage.
Canonicalization reuses `diagnosticcontract.DecodeAndValidate` before fresh typed
decoding and fixed-order marshaling. Errors are one fixed sentinel with empty
output; raw invalid bytes are never hashed, retained or echoed. The function does
not perform network requests, callbacks, filesystem access or background work.
Inputs must not be concurrently mutated while decoding. Returned bytes are copies.

Verification uses deterministic tests, one bounded `FuzzDecode` target and scoped
race/vet/lint. No database or provider service is needed for this pure component;
those requirements still apply to future durable intake, not to this decoder.
For a compiled negative, change only `if reader.Len() != 0 {` to
`if reader.Len() < 0 {` in payload.go: `TestPayloadGzipRejectTrailing` must fail
for trailing garbage and concatenated members. Restore exact source and rerun.
Such a mutation qualifies this guard only, not HTTP intake or full T14.4.
