#!/usr/bin/env python3
"""Bounded current-source semantics and projection freshness; no execution certification."""
import argparse
import hashlib
import json
import re
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
    require(len(retained) == INVENTORY_COUNT and len(set(retained)) == INVENTORY_COUNT,
            'retained task inventory count differs from adopted full plan')
    require(hashlib.sha256('\n'.join(sorted(retained)).encode()).hexdigest() == INVENTORY_SHA256,
            'retained task IDs differ from adopted full plan')
    rows = source.get('tasks')
    require(isinstance(rows, list) and len(rows) == INVENTORY_COUNT, 'missing or extra current tasks')
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
    args = parser.parse_args()
    try:
        current = load_current(args.data)
        if args.projection_dir:
            check_projection(args.projection_dir, projection_files(current))
        print(f'PASS: {len(current[1])} current tasks; bounded source semantics' +
              (' and projection freshness' if args.projection_dir else '') +
              ' only. No execution, receipt authenticity, or release qualified.')
        return 0
    except (OSError, ValueError, TypeError, RecursionError) as exc:
        print('ERROR: current plan validation failed: ' + str(exc), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
