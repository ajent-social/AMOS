# Generated Go API types

`internal/codegen/gotypes.Generate` first calls the T2.1 OpenAPI validator and
only emits Go for a validated contract. The output is deterministic and has a
generated-file header. It contains schema types, typed operation request and
response types, handler interfaces, a generic `ErrorResponse`, and a copied
operation metadata registry. Business implementations belong in separately
maintained files and satisfy the generated interfaces at compile time.

Generator v1 deliberately supports a bounded OpenAPI Schema Object subset:
string, integer, number, boolean, homogeneous arrays, and closed objects with
explicit properties and `additionalProperties: false`; required properties;
and one non-null type optionally unioned with `null`. It supports local
`#/components/schemas/<name>` references and a single `application/json`
request or success response schema. Optional non-null fields use pointers.
Optional nullable fields use `OptionalNullable[T]` to preserve absent, null,
and value states. Required fields are checked during JSON decoding, unknown
object fields are rejected, and non-nullable properties reject JSON null.

The generator rejects unsupported schema keywords and shapes, external
references, recursive component types, unsupported formats, path or operation
parameters, multiple successful responses, and non-JSON/multiple media types.
It does not emit transports, routers, storage, authorization checks, or a
business implementation. Those are separate repository tasks. Changes to an
input schema are expected to produce compile errors in incompatible handwritten
implementations; the fixture tests this by changing an integer field to a
string while leaving its consumer source unchanged.
