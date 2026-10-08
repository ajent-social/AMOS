"""Actual-current-inventory regressions; synthetic mutations never edit repository inputs."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('current_plan', ROOT / 'scripts/check-plan.py')
PLAN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PLAN)


class CurrentPlanTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.current = PLAN.load_current()
        cls.source = cls.current[0]

    def mutation(self, change, message):
        source = copy.deepcopy(self.source)
        change(source)
        with self.assertRaisesRegex(PLAN.PlanError, message):
            PLAN.validate_source(source)

    def test_actual_full_inventory_and_records_preserved(self):
        files = PLAN.projection_files(self.current)
        projected = json.loads(files['current-plan.json'])
        self.assertEqual(len(projected['tasks']), len(self.source['tasks']))
        self.assertEqual(projected['authoredSource'], self.source)
        self.assertEqual(projected['registryRecords'], self.current[4])
        self.assertEqual(projected['registryDigests'], self.current[5])
        self.assertEqual({t['canonicalId'] for t in projected['tasks']}, set(self.source['required_task_ids']))
        self.assertEqual({t['authoredStatus'] for t in projected['tasks']}, {'pending'})
        self.assertFalse(projected['narrativeStatusIsQualifiedEvidence'])
        self.assertEqual(projected['evidence'], [])
        self.assertEqual(projected['evaluations'], [])
        self.assertEqual(files, PLAN.projection_files(self.current))

    def test_additive_lifecycle_extension_preserves_retention(self):
        source = copy.deepcopy(self.source)
        source['tasks'].append({'id': 'NEW-LIFECYCLE', 'title': 'New review',
                                'stage': 'review', 'deps': [source['terminal_task']],
                                'acceptance': 'Independent exact-head review'})
        source['required_task_ids'].append('NEW-LIFECYCLE')
        source['terminal_task'] = 'NEW-LIFECYCLE'
        tasks, _ = PLAN.validate_source(source)
        self.assertEqual(len(tasks), len(self.source['tasks']) + 1)
        source['required_task_ids'][0] = 'REPLACED-ORIGINAL'
        with self.assertRaisesRegex(PLAN.PlanError, 'differ from adopted'):
            PLAN.validate_source(source)

    def test_retired_baseline_rejected(self):
        baseline = json.loads((ROOT / 'docs/planning/plan-data.json').read_text())
        with self.assertRaisesRegex(PLAN.PlanError, 'retired baseline'):
            PLAN.validate_source(baseline)

    def test_missing_task_rejected(self):
        self.mutation(lambda s: s['tasks'].pop(), 'missing or extra')

    def test_missing_task_and_retention_entry_rejected(self):
        def change(s):
            tid = s['tasks'].pop(0)['id']
            s['required_task_ids'].remove(tid)
        self.mutation(change, 'inventory count|differ from adopted')

    def test_same_count_inventory_substitution_rejected(self):
        def change(s):
            tid = s['tasks'][0]['id']
            s['tasks'][0]['id'] = 'FORGED'
            s['required_task_ids'][s['required_task_ids'].index(tid)] = 'FORGED'
        self.mutation(change, 'differ from adopted')

    def test_duplicate_task_rejected(self):
        self.mutation(lambda s: s['tasks'].__setitem__(-1, s['tasks'][0]), 'duplicate task')

    def test_dangling_dependency_rejected(self):
        self.mutation(lambda s: s['tasks'][-1]['deps'].append('MISSING'), 'dangling dependency')

    def test_duplicate_dependency_rejected(self):
        self.mutation(lambda s: s['tasks'][-1]['deps'].append(s['tasks'][-1]['deps'][0]), 'repeated dependencies')

    def test_cycle_rejected(self):
        self.mutation(lambda s: s['tasks'][0]['native_product_task']['deps'].append(s['tasks'][0]['id']), 'cycle')

    def test_missing_acceptance_rejected(self):
        self.mutation(lambda s: s['tasks'][-1].pop('acceptance'), 'acceptance')
        self.mutation(lambda s: s['tasks'][0]['native_product_task'].__setitem__('acceptance', []), 'acceptance')

    def test_duplicate_authored_fields_rejected(self):
        self.mutation(lambda s: s['tasks'][0].__setitem__('title', 'Conflict'), 'duplicate authored')

    def test_product_cannot_be_reclassified_as_lifecycle(self):
        def change(s):
            product = s['tasks'][0]['native_product_task']
            s['tasks'][0] = {**product, 'acceptance': '\n'.join(product['acceptance'])}
        self.mutation(change, 'product task representation changed')

    def test_product_identity_rejected(self):
        self.mutation(lambda s: s['tasks'][0]['native_product_task'].__setitem__('id', 'FORGED'), 'identity mismatch')

    def test_authored_promotion_rejected(self):
        self.mutation(lambda s: s['tasks'][-1].__setitem__('authoredStatus', 'complete'), 'promotion')
        self.mutation(lambda s: s['tasks'][0]['native_product_task'].__setitem__('authoredStatus', 'accepted'), 'promotion')

    def test_narrative_completion_cannot_promote(self):
        source = copy.deepcopy(self.source)
        for task in source['tasks']:
            task['status'] = 'COMPLETE'
        tasks, products = PLAN.validate_source(source)
        current = (source, tasks, products, self.current[3], self.current[4], self.current[5])
        projected = json.loads(PLAN.projection_files(current)['current-plan.json'])
        self.assertEqual({t['authoredStatus'] for t in projected['tasks']}, {'pending'})
        self.assertEqual(projected['evidence'], [])

    def test_dependency_predicates_and_acceptance(self):
        projected = json.loads(PLAN.projection_files(self.current)['current-plan.json'])
        found = set()
        for task in projected['tasks']:
            native = self.current[1][task['canonicalId']]
            self.assertEqual(task['acceptance'], native['acceptance'])
            for dep in task['dependencies']:
                product = dep['taskId'].removeprefix('amos:task:') in self.current[2]
                expected = 'domain-accepted' if product else 'execution-complete'
                self.assertEqual(dep['predicate'], expected)
                self.assertEqual('requirementId' in dep, product)
                found.add(expected)
        self.assertEqual(found, {'domain-accepted', 'execution-complete'})

    def test_unknown_stages_preserved(self):
        source = copy.deepcopy(self.source)
        source['tasks'][-1]['stage'] = 'future-stage'
        tasks, _ = PLAN.validate_source(source)
        self.assertEqual(tasks[source['tasks'][-1]['id']]['stage'], 'future-stage')


class CLITests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='amos-current-tooling-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'scripts').mkdir()
        (self.root / 'docs/planning').mkdir(parents=True)
        for name in ('check-plan.py', 'render-plan.py', 'check-release-evidence.py'):
            (self.root / 'scripts' / name).write_bytes((ROOT / 'scripts' / name).read_bytes())
        self.input_paths = (PLAN.SOURCE_REF, *PLAN.REGISTRY_REFS, 'docs/planning/plan-data.json')
        for ref in self.input_paths:
            (self.root / ref).write_bytes((ROOT / ref).read_bytes())
        self.before = self.hashes()

    def hashes(self):
        return {ref: hashlib.sha256((self.root / ref).read_bytes()).hexdigest()
                for ref in self.input_paths}

    def cli(self, script, *args, status=0):
        result = subprocess.run([sys.executable, str(self.root / 'scripts' / script), *map(str, args)],
                                cwd=self.root, capture_output=True, text=True, timeout=20)
        self.assertEqual(result.returncode, status, result.stdout + result.stderr)
        return result.stdout + result.stderr

    def edit(self, ref, change):
        path = self.root / ref
        data = json.loads(path.read_text())
        change(data)
        path.write_text(json.dumps(data))

    def test_safe_default_and_check_do_not_write(self):
        paths_before = set(self.root.rglob('*'))
        self.cli('render-plan.py', status=2)
        self.cli('render-plan.py', '--check')
        self.cli('check-plan.py')
        self.assertEqual(self.before, self.hashes())
        self.assertFalse((self.root / 'current-plan.json').exists())
        self.assertEqual(paths_before, set(self.root.rglob('*')))

    def test_render_check_no_overwrite_and_restoration(self):
        output = self.root / 'output'
        self.cli('render-plan.py', '--output-dir', output)
        self.cli('render-plan.py', '--check', '--output-dir', output)
        self.cli('check-plan.py', '--projection-dir', output)
        self.cli('render-plan.py', '--output-dir', output, status=2)
        path = output / 'current-plan.json'
        original = path.read_bytes()
        projection = json.loads(original)
        projection['tasks'][0]['authoredStatus'] = 'complete'
        path.write_text(json.dumps(projection))
        self.assertIn('stale or altered', self.cli('check-plan.py', '--projection-dir', output, status=2))
        path.write_bytes(original)
        self.cli('check-plan.py', '--projection-dir', output)
        self.assertEqual(self.before, self.hashes())

    def test_symlink_output_rejected(self):
        alias = self.root / 'alias'
        alias.symlink_to(self.root, target_is_directory=True)
        self.cli('render-plan.py', '--output-dir', alias, status=2)
        self.cli('render-plan.py', '--check', '--output-dir', alias, status=2)

    def test_stale_source_and_registry_detected(self):
        output = self.root / 'output'
        self.cli('render-plan.py', '--output-dir', output)
        source = self.root / PLAN.SOURCE_REF
        original = source.read_bytes()
        source.write_bytes(original + b'\n')
        self.cli('check-plan.py', '--projection-dir', output, status=2)
        source.write_bytes(original)
        self.cli('check-plan.py', '--projection-dir', output)
        registry = self.root / PLAN.REGISTRY_REFS[0]
        registry.write_bytes(registry.read_bytes() + b'\n')
        self.cli('check-plan.py', '--projection-dir', output, status=2)

    def test_stale_baseline_cli_rejected(self):
        baseline = self.root / 'docs/planning/plan-data.json'
        self.cli('check-plan.py', '--data', baseline, status=2)
        self.cli('render-plan.py', '--data', baseline, '--output-dir', self.root / 'output', status=2)
        self.assertFalse((self.root / 'output').exists())

    def test_missing_source_never_falls_back(self):
        (self.root / PLAN.SOURCE_REF).unlink()
        self.cli('check-plan.py', status=2)
        self.cli('render-plan.py', '--check', status=2)
        self.cli('check-release-evidence.py', status=2)

    def test_foreign_registry_and_missing_journal_rejected(self):
        self.edit(PLAN.REGISTRY_REFS[0], lambda d: d.__setitem__('FORGED', {'status': 'ACCEPTED'}))
        self.cli('check-plan.py', status=2)
        (self.root / PLAN.REGISTRY_REFS[0]).write_bytes((ROOT / PLAN.REGISTRY_REFS[0]).read_bytes())
        (self.root / PLAN.REGISTRY_REFS[1]).unlink()
        self.cli('check-plan.py', status=2)

    def test_duplicate_json_keys_rejected(self):
        path = self.root / PLAN.SOURCE_REF
        path.write_text('{"schema":"old","schema":"new"}')
        self.assertIn('duplicate JSON', self.cli('check-plan.py', status=2))

    def test_malformed_inputs_fail_without_traceback(self):
        for value in ([], None, {'schema': 'amos-local-sdlc-plan-v1', 'contract': 'amos-wazi-authored-plan/1', 'required_task_ids': [None]}):
            (self.root / PLAN.SOURCE_REF).write_text(json.dumps(value))
            self.assertNotIn('Traceback', self.cli('check-plan.py', status=2))

    def test_current_release_never_qualifies_historical_matrix(self):
        for args in ((), ('--strict',), ('--structure-only',), ('--self-test',)):
            self.assertIn('not implemented', self.cli('check-release-evidence.py', *args, status=1))
        self.assertIn('historical baseline cannot qualify', self.cli('check-release-evidence.py', '--historical-product', status=1))
        self.assertEqual(self.before, self.hashes())


if __name__ == '__main__':
    unittest.main()
