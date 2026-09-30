# HTTP runtime

`app.New` creates the shared AMOS HTTP composition. Register module-owned business endpoints on the returned app, then pass `App.Handler()` to an embedding server or call `App.Serve(ctx, listener)` for a server whose lifetime follows a context. `Serve` gracefully drains active requests on cancellation up to the configured shutdown timeout; after the timeout it closes outstanding connections.

AMOS reserves `/signin`, `/signout`, `/account`, `/workspaces`, `/billing`, `/api`, `/mcp`, `/healthz`, and `/readyz`, including descendants. Reservation is case insensitive. Reserved routes remain unavailable with HTTP 503 until their owning modules are present, so a business module cannot accidentally claim those paths. `/healthz` reports process liveness. `/readyz` executes the configured dependency checks with a bounded context and returns 503 if any required dependency is unavailable. An app with no declared readiness checks has no external readiness dependency.

Business routes use an uppercase-normalized HTTP method and a canonical absolute path. A route may be exact (`/catalog`) or use a terminal prefix wildcard (`/reports/*`). Duplicate canonical method/path registrations and overlapping exact/prefix patterns for the same method fail registration. Ingress paths containing dot segments, duplicate separators, encoded slashes or backslashes, malformed escapes, or other ambiguous forms receive HTTP 400.

The runtime only dispatches requests. It does not construct caller identity, credentials, or authorization decisions. Those remain responsibilities of trusted authentication and policy components defined by the frozen cross-lane contracts. Error responses expose stable codes and do not include dependency error details.
