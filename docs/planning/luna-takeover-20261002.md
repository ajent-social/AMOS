# In-progress lane takeover

Owner requested parallel GPT-6-Luna completion of existing in-progress work.
Baseline: d3c1de5. The coordinator owns integration and shared contract, module,
migration, executable and execution-state updates. Existing trees remain intact.

| Lane | Task | Exclusive implementation paths | Completion boundary |
|---|---|---|---|
| federation | T3.10 | identity/federation/** | Browser/provider-bound one-use sign-in/link transaction and real callback tests |
| billing | T5.8 | billing/reconcile/** | Scheduled reconciliation and freshness/availability policy; official provider reads retained |
| cleanup | T7.7 | internal/devrunner/status*, internal/devrunner/cleanup* | Exact owned resource cleanup, stale-PID refusal, default volume retention |
| workspace | T6.4 | ui/workspaceswitch/**, tests/browser/workspace-switch.spec.ts | Composed tenant selection and Back/stale-authority browser checks |
| extension | T12.1 | internal/businesscontract/**, docs/contracts/business-extension.md, api/extensions/business.schema.json | Resolve integration design gates against current public APIs without unowned shared edits |

Workers use isolated external-SSD trees and task-specific external caches.
At most two heavy lanes per project; multi-package builds require load check
and shared lease. The coordinator allocates heavy verification. Missing services
fail visibly; fixtures cannot qualify live providers. Workers report exact
commits, real tests and negatives, limitations and proposed root wiring.
They do not push, merge main or update acceptance records.

## Integration follow-up ownership

- The queue-scope lane owns `jobs/sqlstore/**`: claim predicates must constrain
  maintenance updates and selection to installation/application and endpoint
  payload scope before leasing a job. The coordinator owns caller composition.
- The lifecycle lane owns `internal/devrunner/**`, including the coordinator's
  staged cleanup hardening: serialize resource startup and cleanup with a
  separate private project operation lock. Process updates retain their own lock.
- Workspace browser qualification additionally delegates
  `apphost/workspace_browser_test.go` and
  `tests/browser/workspace.playwright.config.ts` to the workspace lane.
- The coordinator allocates reconciliation migration 15 and federation migration
  16, retains explicit unavailable provider routes by default, and owns the
  finite callback composition field and generated migration registry.
