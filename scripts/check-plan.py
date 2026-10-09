#!/usr/bin/env python3
"""Bounded current-source semantics and projection freshness; no execution certification."""
import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
SOURCE_REF = 'docs/planning/wazi-source.json'
REGISTRY_REFS = ('docs/planning/execution-state.json', 'docs/planning/sdlc-stage-state.json')
# Retention boundary for the adopted full inventory, independent of mutable input.
INVENTORY_COUNT = 1501
INVENTORY_SHA256 = 'a1444086050732da078fc9df94dbccc1ea7bfd06994568ff46df48a81e796bfb'
MAX_BYTES = 16 * 1024 * 1024


class PlanError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise PlanError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON object key')
        result[key] = value
    return result


def read_json(path):
    with path.open('rb') as stream:
        raw = stream.read(MAX_BYTES + 1)
    require(len(raw) <= MAX_BYTES, 'input exceeds bounded size')
    value = json.loads(raw, object_pairs_hook=unique_object,
                       parse_constant=lambda _: (_ for _ in ()).throw(PlanError('non-finite JSON number')))
    require(isinstance(value, dict), 'input root must be an object')
    return value, 'sha256:' + hashlib.sha256(raw).hexdigest()


def string(value):
    return isinstance(value, str) and bool(value.strip())


def string_list(value):
    return isinstance(value, list) and all(string(item) for item in value)


def validate_source(source):
    require(source.get('schema') == 'amos-local-sdlc-plan-v1' and
            source.get('contract') == 'amos-wazi-authored-plan/1',
            'unsupported current source; retired baseline is not a current plan')
    retained = source.get('required_task_ids')
    require(string_list(retained), 'missing retained task inventory')
    require(len(retained) >= INVENTORY_COUNT and len(set(retained)) == len(retained),
            'retained task inventory count differs from adopted full plan')
    require(hashlib.sha256('\n'.join(sorted(retained[:INVENTORY_COUNT])).encode()).hexdigest() == INVENTORY_SHA256,
            'retained task IDs differ from adopted full plan')
    rows = source.get('tasks')
    require(isinstance(rows, list) and len(rows) == len(retained), 'missing or extra current tasks')
    tasks = {}
    products = set()
    for row in rows:
        require(isinstance(row, dict), 'task must be an object')
        tid = row.get('id')
        require(string(tid) and tid in retained and tid not in tasks, 'unknown or duplicate task ID')
        native = row
        if 'native_product_task' in row:
            products.add(tid)
            native = row['native_product_task']
            require(isinstance(native, dict) and native.get('id') == tid, 'product identity mismatch')
            require(not {'title', 'stage', 'deps', 'acceptance'} & row.keys(),
                    'duplicate authored product fields')
            require(string_list(native.get('acceptance')) and native['acceptance'], 'missing product acceptance')
            acceptance = '\n'.join(native['acceptance'])
        else:
            acceptance = row.get('acceptance')
        require(string(native.get('title')) and string(native.get('stage')) and string(acceptance),
                'task lacks title, stage or acceptance')
        deps = native.get('deps')
        require(string_list(deps) and len(deps) == len(set(deps)), 'invalid or repeated dependencies')
        # These are authored definitions, never qualified execution snapshots.
        for item in (row, native):
            require('authoredStatus' not in item or item['authoredStatus'] == 'pending',
                    'authored status promotion is forbidden')
        tasks[tid] = {'id': tid, 'title': native['title'], 'stage': native['stage'],
                      'acceptance': acceptance, 'deps': deps}
    require(set(tasks) == set(retained), 'missing retained task IDs')
    require(products == {tid for tid in retained if re.fullmatch(r'T[0-9]+\.[0-9]+', tid)},
            'retained product task representation changed')
    # Kahn traversal avoids recursion limits on long valid or malicious graphs.
    remaining = {tid: len(task['deps']) for tid, task in tasks.items()}
    followers = {tid: [] for tid in tasks}
    for tid, task in tasks.items():
        for dep in task['deps']:
            require(dep in tasks, 'dangling dependency')
            followers[dep].append(tid)
    ready = [tid for tid, count in remaining.items() if count == 0]
    visited = 0
    while ready:
        tid = ready.pop()
        visited += 1
        for child in followers[tid]:
            remaining[child] -= 1
            if remaining[child] == 0:
                ready.append(child)
    require(visited == len(tasks), 'dependency cycle')
    require(source.get('terminal_task') in tasks, 'missing terminal task')
    return tasks, products


def load_current(source_path=None, root=ROOT):
    source, source_digest = read_json(source_path or root / SOURCE_REF)
    tasks, products = validate_source(source)
    registries = {}
    digests = {}
    for ref in REGISTRY_REFS:
        registry, digest = read_json(root / ref)
        entries = registry
        if ref.endswith('sdlc-stage-state.json'):
            require(registry.get('schema') == 'amos-local-sdlc-stage-receipts-v1', 'unsupported receipt journal')
            entries = registry.get('tasks')
        require(isinstance(entries, dict), 'registry tasks must be an object')
        for tid, record in entries.items():
            require(tid in tasks and isinstance(record, dict), 'registry references unknown task or invalid record')
            require(string(record.get('status')), 'registry record lacks status')
            if ref.endswith('execution-state.json'):
                require(tid in products, 'product registry references a lifecycle task')
                require(record['status'] in {'PLANNED', 'IN_PROGRESS', 'BLOCKED', 'ACCEPTED'}, 'invalid product registry status')
                if record['status'] == 'ACCEPTED':
                    require(record.get('certification') == 'REVIEWED' and
                            string_list(record.get('evidence')) and record['evidence'],
                            'accepted registry record lacks reviewed evidence')
            else:
                require(record.get('task_id') == tid, 'receipt task identity mismatch')
        registries[ref] = registry
        digests[ref] = digest
    return source, tasks, products, source_digest, registries, digests


def projection_files(current):
    source, tasks, products, digest, registries, registry_digests = current
    projected = []
    for tid, task in sorted(tasks.items()):
        dependencies = []
        for dep in task['deps']:
            dependency = {'taskId': 'amos:task:' + dep,
                          'predicate': 'domain-accepted' if dep in products else 'execution-complete'}
            if dep in products:
                dependency['requirementId'] = 'amos:acceptance:' + dep
            dependencies.append(dependency)
        projected.append({'id': 'amos:task:' + tid, 'canonicalId': tid,
                          'title': task['title'], 'stage': task['stage'],
                          'acceptance': task['acceptance'], 'authoredStatus': 'pending',
                          'dependencies': dependencies})
    payload = {'schema': 'amos-current-plan-projection/1', 'planId': 'amos:plan',
               'source': {'ref': SOURCE_REF, 'digest': digest},
               'registryDigests': registry_digests, 'tasks': projected,
               'authoredSource': source, 'registryRecords': registries,
               'narrativeStatusIsQualifiedEvidence': False,
               'evidence': [], 'evaluations': []}
    # Preserve complete source and registry records without claiming to authenticate them.
    encoded = json.dumps(payload, ensure_ascii=True, sort_keys=True, indent=2) + '\n'
    lines = ['# Current plan projection', '',
             'Read-only derived inventory; no task execution or release is certified.',
             'All authored statuses are pending. Registry records remain separate, unqualified inputs.',
             'Full source, acceptance text and unchanged registry records are in current-plan.json.', '',
             f'Source digest: `{digest}`.', f'Tasks: {len(tasks)}.', '',
             '| Task | Stage | Title | Authored status | Dependencies |',
             '|---|---|---|---|---|']
    def cell(value):
        # Escape authored text so it cannot inject Markdown/HTML into the index.
        import html
        return html.escape(value).replace('|', '&#124;').replace('\n', ' ').replace('\r', ' ').replace('`', '&#96;').replace('[', '&#91;').replace(']', '&#93;')
    for task in projected:
        lines.append('| ' + ' | '.join(cell(value) for value in (
            task['canonicalId'], task['stage'], task['title'], 'pending',
            ', '.join(dep['taskId'] for dep in task['dependencies']) or 'none')) + ' |')
    return {'current-plan.json': encoded.encode(), 'current-plan.md': ('\n'.join(lines) + '\n').encode()}



# Authored product text retention, with checkbox markers normalized. These are
# preservation pins, not evidence of execution or approval. Updating authored
# templates requires a separately reviewed pin update.
PRODUCT_TEXT_SHA256 = {'E1.md': 'fe3ca78149c2cb356e21cb869d9b64c11871379630bdafc30acd2801914fbc1c',
 'E10.md': 'beb465835c454a1d2d8a290181f2c86614a3bd3a763e20d2a49b2179b85e06fe',
 'E11.md': 'de7eb8e1e96de9af256b58057cbdff2263373d93371b5244e96a8f7bc3ba9d64',
 'E12.md': 'b16dbab2acbf6541e53d9170cf95ffe375c60353581f63672fa95254fe816dc5',
 'E13.md': 'ea3380b932197064ed42e0f345ce6318ce79f607600ee6ffe14e852c01a75e44',
 'E14.md': '6dbc44ffa943ced9c94a885296a804fa0e8bff9ec137f5fecca6f7a21dcd4cc9',
 'E15.md': '63f76ae5642543b3677600d8de1694ca6e5a132acd928a641868d5f9d8203739',
 'E16.md': 'd5622e67bd7ecd917772e7043aec2449b5d93f94d74762f150873770963c1fed',
 'E2.md': '51f46499296bf16fe23a83a4aeb6f0185675f91e45b114078917921964b77e20',
 'E3.md': 'fbd09935ad9b091a450823c16df8c8bfad7036861112f52a48ff150fad2b8a9d',
 'E4.md': 'c23afce8ba60a641a08ca7dd000c64d8196225bc702f575490f3c57f227c5f07',
 'E5.md': '4a20f2c23a46bd9c9b9d5c9bf0d1dc81cac5a4539b078c17d42c5528ea1711b5',
 'E6.md': '444b82b58b6375258be354be99eba133165a8a8cf48892c743fc4711a2e38871',
 'E7.md': 'b555c4e48fbe0a9d1c0ed48d74ddd2c41bed305843aadf15c095be98c182c752',
 'E8.md': '7aaca5184adbf69ccd64dc3ee040e39eeed9aa8ba4ff072f2fc7a1fd97d47691',
 'E9.md': '45618f33024cca2ee2d9d03ea54517b36e5ae1c4f53bd4485a947301bba193e9'}
DISPLAY_STAGES = {'preflight', 'implement', 'verify', 'review', 'merge', 'verify-landed'}
DISPLAY_ALIASES = {'author': 'implement', 'landed': 'verify-landed', 'accept': 'verify-landed'}
NOTICE_START = '<!-- current-native-projection:start -->'
NOTICE_END = '<!-- current-native-projection:end -->'
PRODUCT_METADATA = re.compile(r'^  - Stage: implement; canonical-id: [^\n]*\n', re.MULTILINE)
PRODUCT_ROW = re.compile(r'^- \[[ xX~-]\] (T[0-9]+\.[0-9]+)\b[^\n]*\n', re.MULTILINE)


def strip_notice(text):
    pattern = re.escape(NOTICE_START) + r'.*?' + re.escape(NOTICE_END) + r'\n\n'
    return re.sub(pattern, '', text, flags=re.DOTALL)


def product_template(raw, name):
    text = strip_notice(raw.decode('utf-8'))
    text = PRODUCT_METADATA.sub('', text)
    text = text.replace('  - product-milestone:', '  - Stage:')
    text = re.sub(r'(?m)^  - Acceptance: \[(.*)\]$', r'  - Acceptance: \1', text)
    text = re.sub(r'(?m)^- \[[ xX~-]\]', '- [ ]', text)
    require(hashlib.sha256(text.encode()).hexdigest() == PRODUCT_TEXT_SHA256[name],
            'authored product text changed: ' + name)
    return text


def reported_status(current, tid):
    ref = REGISTRY_REFS[0] if tid in current[2] else REGISTRY_REFS[1]
    records = current[4][ref]
    if tid not in current[2]:
        records = records['tasks']
    record = records.get(tid)
    status = record['status'] if record else 'UNRECORDED'
    allowed = {'UNRECORDED', 'PLANNED', 'IN_PROGRESS', 'BLOCKED'}
    allowed.add('ACCEPTED' if tid in current[2] else 'COMPLETE')
    require(status in allowed, 'unsupported reported registry status: ' + tid)
    marker = {'ACCEPTED': 'x', 'COMPLETE': 'x', 'IN_PROGRESS': '~', 'BLOCKED': '-'}.get(status, ' ')
    return marker, status, ref


def display_stage(task, product=False):
    if product:
        require(re.fullmatch(r'S[0-5]', task['stage']), 'unsupported product milestone')
        return 'implement'
    stage = DISPLAY_ALIASES.get(task['stage'], task['stage'])
    require(stage in DISPLAY_STAGES, 'unsupported native display stage: ' + task['stage'])
    return stage


def native_notice(current):
    accepted = sum(record['status'] == 'ACCEPTED' for record in current[4][REGISTRY_REFS[0]].values())
    return '\n'.join([
        NOTICE_START,
        '## Current read-only display projection', '',
        f'Product acceptance: **{accepted}/{len(current[2])}**. Combined inventory: '
        f'**{len(current[1])} product and delivery nodes**; node completion is not product completion.',
        'This current view supersedes historical inventory/tooling statements below; the original 1,501 IDs remain retained.',
        'Checkboxes report registry status only; they grant no execution, acceptance or release authority.',
        'The consumer may additionally display dependency-blocked status. Consult the registries and claims before dispatch.',
        'Product `S0`-`S5` milestones remain `product-milestone` metadata; their display `Stage: implement` is a work bucket, not advancement.',
        'Delivery aliases: `author` -> `implement`; `landed` and `accept` -> `verify-landed`. Original labels remain `authored-stage`.',
        'Unrecorded tasks remain unchecked. Checked product rows report ACCEPTED; checked delivery rows report COMPLETE.',
        'These generated fields do not modify canonical authored statuses or authenticate registry receipts.',
        f'Canonical source: `{SOURCE_REF}`; `{current[3]}`.',
        *[f'Registry: `{ref}`; `{digest}`.' for ref, digest in current[5].items()],
        'See [projection contract](../planning/portable-plan-export.md) for regeneration and limits.',
        NOTICE_END, '', ''])


def native_projection_files(current, root=ROOT):
    """Native scanner view; preserve all product prose and never write inputs."""
    tasks, products = current[1:3]
    notice = native_notice(current)
    files = {}
    seen = set()
    for name in PRODUCT_TEXT_SHA256:
        template = product_template((root / 'docs/plans' / name).read_bytes(), name)
        matches = list(PRODUCT_ROW.finditer(template))
        for index, match in enumerate(matches):
            tid = match[1]
            require(tid in products, 'unknown native product ID')
            task = tasks[tid]
            end = matches[index + 1].start() if index + 1 < len(matches) else len(template)
            body = template[match.end():end]
            deps = ', '.join(task['deps']) or 'none'
            acceptance = ' '.join(task['acceptance'].splitlines())
            require(match[0].startswith(f'- [ ] {tid} {task["title"]}  ') and
                    f'  - Stage: {task["stage"]};' in body and
                    f'deps: [{deps}];' in body and
                    f'  - Acceptance: {acceptance}\n' in body,
                    'canonical product fields differ from preserved template: ' + tid)
        def replace_row(match):
            tid = match[1]
            require(tid in products and tid not in seen, 'unknown or duplicate native product ID')
            seen.add(tid)
            display_stage(tasks[tid], product=True)
            marker, status, ref = reported_status(current, tid)
            return (match[0].replace('- [ ]', f'- [{marker}]', 1) +
                    f'  - Stage: implement; canonical-id: {tid}; status-source: {ref}; '
                    f'reported-status: {status}; authority: [reported-display-only]\n')
        rendered = PRODUCT_ROW.sub(replace_row, template)
        rendered = rendered.replace('  - Stage: S', '  - product-milestone: S')
        rendered = re.sub(r'(?m)^  - Acceptance: (.*)$', r'  - Acceptance: [\1]', rendered)
        heading, rest = rendered.split('\n', 1)
        files['docs/plans/' + name] = (heading + '\n\n' + notice + rest.lstrip('\n')).encode()
    require(seen == products, 'missing native product rows')
    lifecycle = sorted(set(tasks) - products)
    for start in range(0, len(lifecycle), 100):
        lines = [f'# Delivery inventory {start // 100 + 1:02d}', '', notice.rstrip(), '']
        for tid in lifecycle[start:start + 100]:
            task = tasks[tid]
            marker, status, ref = reported_status(current, tid)
            lines.extend([
                f'- [{marker}] {tid} [{task["title"]}]',
                f'  Stage: {display_stage(task)}',
                f'  canonical-id: {tid}',
                f'  authored-stage: {task["stage"]}',
                '  deps: [' + ', '.join(task['deps']) + ']',
                f'  status-source: {ref}',
                f'  reported-status: {status}',
                '  authority: reported-display-only',
                f'  Acceptance: [{task["acceptance"]}]', ''])
        files[f'docs/plans/delivery-{start // 100 + 1:02d}.md'] = ('\n'.join(lines) + '\n').encode()
    master = strip_notice((root / 'docs/plan.md').read_text())
    heading, rest = master.split('\n', 1)
    master_notice = notice.replace('../planning/portable-plan-export.md', 'planning/portable-plan-export.md')
    files['docs/plan.md'] = (heading + '\n\n' + master_notice + rest.lstrip('\n')).encode()
    require(len(files) - 1 <= 256 and all(len(v) <= 1_000_000 for v in files.values()),
            'native discovery file/count limit exceeded')
    return files


def check_native_projection(root, current, template_root=ROOT):
    files = native_projection_files(current, template_root)
    plan_dir = root / 'docs/plans'
    require(plan_dir.is_dir() and not plan_dir.is_symlink(), 'native plans directory missing or symlinked')
    discovered = {str(p.relative_to(root)) for p in plan_dir.iterdir()
                  if p.suffix.lower() == '.md'}
    discovered.update(str(p.relative_to(root)) for p in (root / 'plan.md', root / 'docs/plan.md') if p.exists())
    require(discovered == set(files), 'native discovery file set differs (missing or extra plans)')
    check_projection(root, files)



def check_consumer(consumer_root, root, current):
    """Execute the separately supplied real consumer; no parser substitute."""
    consumer_root = consumer_root.resolve()
    for name in ('scripts/plans.mjs', 'src/plan-parser.mjs', 'src/demo.mjs'):
        require((consumer_root / name).is_file(), 'required consumer module missing: ' + name)
    program = r"""
import fs from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
const [consumer, root] = process.argv.slice(1);
const {scanPlans} = await import(pathToFileURL(path.join(consumer, 'scripts/plans.mjs')));
const {parsePlan} = await import(pathToFileURL(path.join(consumer, 'src/plan-parser.mjs')));
const {laneFor} = await import(pathToFileURL(path.join(consumer, 'src/demo.mjs')));
const scan = await scanPlans({root});
const plans = scan.projects.flatMap(project => project.plans);
const raw = [];
for (const plan of plans) {
  const text = await fs.readFile(path.join(root, plan.path), 'utf8');
  raw.push(...parsePlan(text, {path: plan.path}).tasks.map(t => ({...t, displayLane: laneFor(t)})));
}
console.log(JSON.stringify({raw, scanned: plans.flatMap(p => p.tasks),
  warnings: [...scan.warnings, ...plans.flatMap(p => p.warnings)]}));
"""
    result = subprocess.run(['node', '--input-type=module', '-e', program,
                             str(consumer_root), str(root.resolve())],
                            capture_output=True, text=True, timeout=30)
    require(result.returncode == 0, 'actual consumer execution failed: ' + result.stderr[:1000])
    parsed = json.loads(result.stdout)
    require(not parsed['warnings'], 'actual consumer discovery/dependency warnings')
    tasks, products = current[1:3]
    for label in ('raw', 'scanned'):
        rows = parsed[label]
        require(len(rows) == len(tasks) and {r['sourceId'] for r in rows} == set(tasks),
                'actual consumer missing or duplicate IDs')
    for row in parsed['raw']:
        tid = row['sourceId']
        task = tasks[tid]
        require(row['canonicalId'] == tid, 'consumer canonical identity mismatch: ' + tid)
        expected_title = task['title'] if tid in products else '[' + task['title'] + ']'
        require(row['title'] == expected_title, 'consumer authored title mismatch: ' + tid)
        require(row['dependencies'] == task['deps'], 'consumer dependencies mismatch: ' + tid)
        acceptance = ' '.join(task['acceptance'].splitlines()) if tid in products else task['acceptance']
        require(row['metadata'].get('acceptance') == acceptance, 'consumer acceptance mismatch: ' + tid)
        require(row['stage'] == display_stage(task, tid in products) and row['displayLane'] != 'other',
                'consumer unsupported display stage: ' + tid)
        field = 'product-milestone' if tid in products else 'authored-stage'
        require(row['metadata'].get(field) == task['stage'], 'consumer authored stage mismatch: ' + tid)
        marker, status, ref = reported_status(current, tid)
        expected = {'x': 'complete', '~': 'active', '-': 'blocked', ' ': 'pending'}[marker]
        require(row['status'] == expected and row['metadata'].get('reported-status') == status and
                row['metadata'].get('status-source') == ref and
                row['metadata'].get('authority') == 'reported-display-only',
                'consumer reported status mismatch: ' + tid)
    complete = {r['sourceId'] for r in parsed['scanned'] if r['status'] == 'complete'}
    expected_complete = {tid for tid in tasks if reported_status(current, tid)[0] == 'x'}
    require(complete == expected_complete, 'consumer completion differs from registry report')
    return parsed


def check_projection(directory, files):
    require(directory.is_dir() and not directory.is_symlink(), 'projection directory missing or symlinked')
    for name, expected in files.items():
        path = directory / name
        require(path.is_file() and not path.is_symlink(), 'projection file missing or symlinked')
        with path.open('rb') as stream:
            actual = stream.read(len(expected) + 1)
        require(actual == expected, 'stale or altered projection: ' + name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data', type=Path, help='current-source candidate; retired baseline rejected')
    parser.add_argument('--projection-dir', type=Path, help='also compare both derived files byte-for-byte')
    parser.add_argument('--native-root', type=Path, help='check discoverable Markdown against current source and registries')
    parser.add_argument('--consumer-root', type=Path, help='execute actual external parser, scanner and stage router; requires --native-root')
    args = parser.parse_args()
    if args.consumer_root and not args.native_root:
        parser.error('--consumer-root requires --native-root')
    try:
        current = load_current(args.data)
        if args.native_root:
            check_native_projection(args.native_root, current)
        if args.consumer_root:
            check_consumer(args.consumer_root, args.native_root, current)
        if args.projection_dir:
            check_projection(args.projection_dir, projection_files(current))
        print(f'PASS: {len(current[1])} current tasks; bounded source semantics' +
              (' and projection freshness' if args.projection_dir else '') +
              (' and native Markdown freshness' if args.native_root else '') +
              (' and actual consumer compatibility' if args.consumer_root else '') +
              ' only. No execution, receipt authenticity, or release qualified.')
        return 0
    except (OSError, ValueError, TypeError, RecursionError, subprocess.SubprocessError) as exc:
        print('ERROR: current plan validation failed: ' + str(exc), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
