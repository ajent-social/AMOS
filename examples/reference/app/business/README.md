# Reference todo business handlers

`business.openapi.yaml` is validated and compiled by the pinned T2.3 Go types
generator. `apigen/api_generated.go` is generated output; the separate
`business` package implements its handler interfaces and checks generated
output against the contract during tests.

The request's `workspaceId` is an untrusted selector. Each handler first reads
the principal attached by identity session middleware, then checks current
active person, workspace, and membership rows using that principal's tenant and
person IDs. It locks those rows for the short resource transaction so a
concurrent membership revocation waits until the operation finishes. Resource
queries include installation, application, workspace, and resource IDs. Todo
records are shared with active members of their workspace; creator identity is
retained for audit and is not the access boundary.

Updates and deletes require the current resource revision. A stale revision
returns `ErrRevisionConflict`; a missing resource returns a non-enumerating
`ErrTodoUnavailable`. Mutation operations declare automatic retries forbidden;
this example does not persist idempotency records. The runtime policy layer must
enforce the generated `todos:read` and `todos:write` permissions before calling
these handlers.

The current type generator does not support path/query parameter models, so
the reference contract carries the workspace selector in its typed request
body and uses a dedicated `POST /todos/search` list operation. This keeps the
example generated from the actual contract rather than hand-maintaining a
parallel request model. Transport routing and application startup composition
remain downstream work.
