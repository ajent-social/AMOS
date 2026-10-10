# MCP operation mapping contract

Status: design contract for operation-to-tool mapping. This document does not
implement an MCP server, prove client interoperability, or authorize a tool call.

## Mapping identity and exposure

A mapping document uses contract identifier amos-mcp-mapping-v1 and contains
between 1 and 128 operation records. Operation IDs are the immutable IDs from
the AMOS operation registry. Tool names use lowercase ASCII letters, digits,
period, underscore, or hyphen, begin with a letter, and are at most 64
characters. Both operation IDs and tool names are unique within one mapping.
Descriptions are required, nonempty, and at most 2,048 characters.

An operation is exposed only when its frozen policy says eligible or challenge.
The never value is a permanent omission from the tool list. Protocol hints,
descriptions, schema annotations, and client-supplied metadata never authorize
an operation. Every exposed operation needs at least one permission, an explicit
minimum assurance, side-effect class, idempotency rule, workspace-selection
rule, resource-selection rule, and entitlement list (which may be empty).
The AMOS operation registry and current authorization policy remain the
authority at every invocation.

## Authorization and tenant binding

Permissions are enforceable AMOS permissions. Entitlements are current-state
entitlement names, not client claims. Minimum assurance is aal1, aal2, or aal3.
Side effects are read, write, or external. Idempotency is required, natural, or
forbidden. A client cannot supply an operation ID, permission, entitlement,
assurance level, or side-effect value to change policy.

workspaceSelection is active_principal or explicit_member. The first selects
the workspace already resolved in the trusted principal. The second may name a
workspace in input, but the server must independently resolve current
membership and policy for that principal. resourceSelection is none or
request_bound. A request_bound mapping names one required input field as
resourceField; the server must resolve that identifier under the selected
workspace and recheck current ownership and permission before execution. A
submitted resource or workspace identifier is a selector only, never authority.
Mappings with tenant-sensitive resources cannot omit this binding decision.

## Input schema limits

The input root is a closed object with at most 64 properties. Scalar properties
may be string, boolean, integer, or number. Arrays are limited to 100 items and
may contain only scalar items. String lengths are bounded at 4,096 characters;
enumerations are bounded at 64 values. Numeric ranges must be finite and
ordered. Property names use a restricted ASCII identifier form.

Nested objects, maps, unions, nullability, references, pattern properties,
arbitrary formats, and unbounded strings, numbers, or arrays are unsupported.
A generator must reject an unsupported shape with the operation ID and field
name. It must not drop, widen, coerce, or silently narrow a constraint. The
declared formats email, date-time, and uuid are descriptive JSON Schema
annotations here; a runtime must still perform the corresponding domain
validation.

## Determinism and failure

Mappings are sorted by operation ID before tool publication. Duplicate
operation IDs or tool names fail with diagnostics naming both conflicting
operations. Invalid policy, unsupported input shape, unknown fields, missing
tenant binding, or absent authorization metadata fail closed. No partial tool
list is published after any mapping error.

The JSON Schema enforces structural limits and conditional policy requirements.
Consumers must also run semantic validation for cross-field constraints the
schema cannot express, including ordered bounds, enum values matching their
field type, and resourceField resolving to a required top-level scalar input.
Failures must name the operation and field. Fixtures in the mapping contract
package check accepted/denied shapes and operation-specific diagnostics. They
are design evidence; runtime generation, policy enforcement, and deployed
interoperability require separate tasks and verification.
