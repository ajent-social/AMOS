#!/usr/bin/env python3
"""Render the public AMOS plan from reviewed, dependency-resolved planning data."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / 'docs/planning/plan-data.json'

def write(path, text):
    p = ROOT / path
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(text.rstrip() + '\n')

def lines(values):
    return '\n'.join('- ' + v for v in values)

def main():
    data = json.loads(DATA.read_text())
    usecases = data['use_cases']
    tasks = [t for e in data['epics'] for t in e['tasks']]
    for e in data['epics']:
        body = [f"# {e['id']} -- {e['title']}", '', 'fidelity: executable',
                'Contract maturity: detailed draft; prerequisite and stage revalidation required. No task is execution-certified.', '',
                'Acceptance: ' + ' '.join(e['exit_criteria']), '', e['intent'], '',
                'All checkboxes describe future implementation. The complete inventory is intentional; later-stage tasks may not dispatch before their prerequisites and external gates.', '']
        for t in e['tasks']:
            deps = ', '.join(t['deps']) or 'none'
            outcome = ('verifies: [' + ', '.join(t['use_cases']) + ']') if t['kind'] == 'engineering' else ('delivers: [' + t['objective'] + ']')
            acc = f"  acc: [{t['acceptance'][0]}]" if t['kind'] == 'engineering' else '  lane: agent'
            body += [f"- [ ] {t['id']} {t['title']}  Owner: {t['lane']}  Est: {t['estimate_minutes']}m  {outcome}{acc}",
                     f"  - Stage: {t['stage']}; Wave: {t['wave']}; deps: [{deps}]; kind: {'human' if t['kind']=='human' else 'agent'}.",
                     '  - Scope: ' + ', '.join('`' + p + '`' for p in t['owned_paths']) + '.',
                     '  - Acceptance: ' + ' '.join(t['acceptance']),
                     f"  - Contract: [docs/tasks/{t['id']}.md](../tasks/{t['id']}.md)."]
            if t.get('external_gate'):
                body += ['  - External gate: ' + t['external_gate']]
            body += ['']
            text = f"""# {t['id']}: {t['title']}

Status: PLANNED. Detailed draft, not execution-certified. Stage {t['stage']}; wave {t['wave']}; owning lane {t['lane']}; estimate {t['estimate_minutes']} minutes (planning hypothesis).

## Objective

{t['objective']}

## Dependencies and readiness

{deps}

Read the actual outputs of every dependency before starting. Reconcile this contract against the frozen contract version and current code; report material mismatch instead of inventing behavior. The listed verification commands are future prescriptions, not claims that executables already exist.

External gate: {t.get('external_gate') or 'None beyond normal task authorization and prerequisites.'}

## Scope

{lines(t['owned_paths'])}

Only these owned paths may change. Repository-qualified upstream paths require a separate upstream task claim and that repository's permissions/review rules; they are not permission to edit a neighboring checkout. Shared module files, migration ordering, aggregate APIs, root workflow wiring and executable entrypoints remain integrator-owned unless explicitly listed and exclusively delegated.

## Implementation instructions

{chr(10).join(str(i + 1) + '. ' + v for i, v in enumerate(t['instructions']))}

## Acceptance criteria

{lines(t['acceptance'])}

Use cases: {', '.join(t['use_cases'])}.

## Negative verification

{t['negative_test']}

For behavior changes, show the check detects the predicted failure using a disposable isolated test mutation, then restore and report the passing check. Never damage a shared checkout or live environment. A design-only task uses a rejected schema/document fixture instead of pretending it changes runtime behavior.

## Verification

```sh
{chr(10).join(t['verification'])}
```

Run the relevant formatter/linter gates after implementation. Required database/browser/provider suites must report absence explicitly. Provider fixtures never satisfy real-provider gates. Root release tasks distinguish integrated, provider-qualified, deployed and rehearsed evidence; no local task alone claims a production release. Heavy multi-package checks require the shared build lease and current load check documented in the execution guide.

## Risks and handoff

{lines(t.get('risks') or ['Resolve contract or dependency mismatch before dispatch; do not expand ownership silently.'])}

Report changed paths, commands actually executed, genuine negative evidence, remaining limitations and the next integration gate. No secrets, raw diagnostic payloads or private source context in public artifacts.

## Pointers

- [Vision](../VISION.md)
- [RFC 0001](../rfc/rfc-0001.md)
- [Cross-lane contracts](../planning/contracts.md)
- [Execution guide](../planning/execution.md)
- [Epic](../plans/{e['id']}.md)

Certification: NOT_RUN. Required intended-tier execution and review have not occurred.
"""
            write('docs/tasks/' + t['id'] + '.md', text)
        write('docs/plans/' + e['id'] + '.md', '\n'.join(body))
    manifest=[]
    for u in usecases:
        v=dict(u)
        v['wiring_status']='PLANNED'
        v['coverage']={'interfaces_found':[], 'has_tests':False}
        manifest.append(v)
    write('docs/usecases-manifest.json', json.dumps(manifest, indent=2))
    write('.claude/scratch/usecases-manifest.json', json.dumps(manifest, indent=2))
    rows=['# Use cases', '', 'All use cases are PLANNED. Interface paths are proposed contracts, not implemented endpoints. The canonical machine-readable manifest is `docs/usecases-manifest.json`.', '', '| ID | Domain | Outcome | Tasks |', '|---|---|---|---|']
    for u in usecases:
        ids=[t['id'] for t in tasks if u['id'] in t['use_cases']]
        rows.append('| '+u['id']+' | '+u['domain']+' | '+u['name'].replace('|','/')+' | '+', '.join(ids)+' |')
    write('docs/use-cases.md', '\n'.join(rows))
    waves=['# Dependency and ownership waves', '', 'Waves are a capacity ceiling, not authorization to dispatch blocked work. Each listed task has its own owning lane and isolated workspace. Skip externally blocked tasks; do not skip their dependencies. An actual runtime with fewer slots runs a compatible subset. A lane has at most one active writer.', '']
    for w in data['waves']:
        waves += [f"## Wave {w['id']}: {len(w['tasks'])} task slots", '', '| Task | Lane | Stage | External gate |', '|---|---|---|---|']
        for tid in w['tasks']:
            t=next(t for t in tasks if t['id']==tid)
            waves.append(f"| [{tid}](../tasks/{tid}.md) | {t['lane']} | {t['stage']} | {t.get('external_gate') or 'None'} |")
        waves += ['']
    write('docs/planning/waves.md','\n'.join(waves))
    print(json.dumps({'epics':len(data['epics']),'tasks':len(tasks),'use_cases':len(usecases),'waves':len(data['waves']),'peak_slots':max(len(w['tasks']) for w in data['waves'])}))

if __name__ == '__main__':
    main()
