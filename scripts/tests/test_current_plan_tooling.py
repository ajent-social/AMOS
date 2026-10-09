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


class NativeProjectionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.current = PLAN.load_current()

    def stage(self, files=None):
        temp = tempfile.TemporaryDirectory(prefix='amos-native-plan-')
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        for name, content in (files or PLAN.native_projection_files(self.current)).items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        return root

    def test_actual_inventory_idempotent_and_authored_bytes(self):
        files = PLAN.native_projection_files(self.current)
        root = self.stage(files)
        self.assertEqual(files, PLAN.native_projection_files(self.current, root))
        PLAN.check_native_projection(root, self.current)
        for name, digest in PLAN.PRODUCT_TEXT_SHA256.items():
            template = PLAN.product_template(files['docs/plans/' + name], name)
            self.assertEqual(hashlib.sha256(template.encode()).hexdigest(), digest)
        self.assertIn(b'59/257', files['docs/plan.md'])
        self.assertIn(b'1598 product and delivery nodes', files['docs/plan.md'])
        self.assertIn(b'node completion is not product completion', files['docs/plan.md'])
        self.assertEqual(len(self.current[0]['required_task_ids']), 1598)
        self.assertEqual(len(files), 31)
        self.assertTrue(all(len(content) < 1_000_000 for content in files.values()))

    def test_all_stage_mappings_and_original_milestones(self):
        seen = set()
        for tid, task in self.current[1].items():
            stage = PLAN.display_stage(task, tid in self.current[2])
            self.assertIn(stage, PLAN.DISPLAY_STAGES)
            seen.add(stage)
        self.assertEqual(seen, PLAN.DISPLAY_STAGES)
        self.assertEqual(PLAN.display_stage({'stage': 'author'}), 'implement')
        self.assertEqual(PLAN.display_stage({'stage': 'landed'}), 'verify-landed')
        self.assertEqual(PLAN.display_stage({'stage': 'accept'}), 'verify-landed')
        with self.assertRaisesRegex(PLAN.PlanError, 'unsupported native display stage'):
            PLAN.display_stage({'stage': 'future-stage'})
        with self.assertRaisesRegex(PLAN.PlanError, 'unsupported product milestone'):
            PLAN.display_stage({'stage': 'S99'}, True)

    def test_registry_display_only_and_narrative_ignored(self):
        current = copy.deepcopy(self.current)
        product = next(iter(current[2]))
        lifecycle = next(t for t in current[1] if t not in current[2])
        for tid, ref, complete in ((product, PLAN.REGISTRY_REFS[0], 'ACCEPTED'),
                                   (lifecycle, PLAN.REGISTRY_REFS[1], 'COMPLETE')):
            records = current[4][ref]
            if tid == lifecycle:
                records = records['tasks']
            for status, marker in ((complete, 'x'), ('IN_PROGRESS', '~'), ('BLOCKED', '-'), ('PLANNED', ' ')):
                records[tid] = {'status': status}
                self.assertEqual(PLAN.reported_status(current, tid), (marker, status, ref))
            records.pop(tid)
            self.assertEqual(PLAN.reported_status(current, tid), (' ', 'UNRECORDED', ref))
            records[tid] = {'status': 'SUCCESS'}
            with self.assertRaisesRegex(PLAN.PlanError, 'unsupported reported registry status'):
                PLAN.reported_status(current, tid)
        source = copy.deepcopy(self.current[0])
        for row in source['tasks']:
            row['status'] = 'COMPLETE'
        mutated = (source, *self.current[1:])
        self.assertEqual(PLAN.native_projection_files(mutated), PLAN.native_projection_files(self.current))

    def test_freshness_rejects_metadata_content_status_and_duplicates(self):
        root = self.stage()
        path = root / 'docs/plans/E1.md'
        original = path.read_bytes()
        for before, after in ((b'Stage: implement', b'Stage: S0'),
                              (b'product-milestone: S0', b'product-milestone: S5'),
                              (b'- [x] T1.1', b'- [ ] T1.1'),
                              (b'Module compiles', b'Module fails')):
            path.write_bytes(original.replace(before, after, 1))
            with self.assertRaises(PLAN.PlanError):
                PLAN.check_native_projection(root, self.current)
        path.write_bytes(original)
        extra = root / 'docs/plans/duplicate.md'
        extra.write_bytes(original)
        with self.assertRaisesRegex(PLAN.PlanError, 'file set differs'):
            PLAN.check_native_projection(root, self.current)
        extra.unlink()
        path.unlink()
        with self.assertRaisesRegex(PLAN.PlanError, 'file set differs'):
            PLAN.check_native_projection(root, self.current)

    def test_product_authored_tampering_rejected_as_template(self):
        root = self.stage()
        path = root / 'docs/plans/E1.md'
        path.write_bytes(path.read_bytes().replace(b'Module compiles', b'Module fails', 1))
        with self.assertRaisesRegex(PLAN.PlanError, 'authored product text changed'):
            PLAN.native_projection_files(self.current, root)

    def test_native_untrusted_text_cannot_inject_tasks_or_markup(self):
        tid = next(t for t in self.current[1] if t not in self.current[2])
        for field in ('title', 'acceptance'):
            for text in ('text] Stage: review', 'text\n- [x] FORGED.1 task',
                         '<script>bad</script>', '`fence`', 'text\rhidden'):
                current = copy.deepcopy(self.current)
                current[1][tid][field] = text
                with self.assertRaisesRegex(PLAN.PlanError, 'unsupported native Markdown text'):
                    PLAN.native_projection_files(current)

    def test_all_canonical_product_fields_bound_before_native_render(self):
        original = self.current[0]['tasks'][0]['native_product_task']
        for field in original:
            with self.subTest(field=field):
                current = copy.deepcopy(self.current)
                product = current[0]['tasks'][0]['native_product_task']
                # Exercise even fields not shown by the consumer, not just the
                # title/stage/deps/acceptance normalized by validate_source.
                value = product[field]
                if isinstance(value, list):
                    product[field] = value + ['Changed authored value']
                elif isinstance(value, int):
                    product[field] = value + 1
                else:
                    product[field] = 'Changed authored value'
                with self.assertRaisesRegex(PLAN.PlanError, 'canonical authored product definitions changed'):
                    PLAN.native_projection_files(current)
        current = copy.deepcopy(self.current)
        product = current[0]['tasks'][0]['native_product_task']
        product['owned_paths'] = ['changed/scope']
        product['external_gate'] = 'New required external approval'
        tasks, products = PLAN.validate_source(current[0])
        current = (current[0], tasks, products, *current[3:])
        with self.assertRaisesRegex(PLAN.PlanError, 'canonical authored product definitions changed'):
            PLAN.native_projection_files(current)

    def test_canonical_product_drift_cannot_render_stale_template(self):
        for field, value in (('title', 'Changed title'), ('acceptance', 'Changed acceptance'),
                             ('deps', ['T1.2']), ('stage', 'S5')):
            current = copy.deepcopy(self.current)
            current[1]['T1.1'][field] = value
            with self.assertRaisesRegex(PLAN.PlanError, 'canonical product fields differ'):
                PLAN.native_projection_files(current)

    def test_registry_change_makes_tracked_projection_stale(self):
        root = self.stage()
        current = copy.deepcopy(self.current)
        current[4][PLAN.REGISTRY_REFS[0]]['T1.1']['status'] = 'IN_PROGRESS'
        with self.assertRaisesRegex(PLAN.PlanError, 'stale or altered'):
            PLAN.check_native_projection(root, current)

    def test_native_renderer_exclusive_and_missing_consumer_visible(self):
        temp = tempfile.TemporaryDirectory(prefix='amos-native-cli-')
        self.addCleanup(temp.cleanup)
        root = Path(temp.name) / 'new'
        command = [sys.executable, str(ROOT / 'scripts/render-plan.py'), '--native-markdown', '--output-dir', str(root)]
        first = subprocess.run(command, capture_output=True, text=True)
        self.assertEqual(first.returncode, 0, first.stderr)
        PLAN.check_native_projection(root, self.current)
        second = subprocess.run(command, capture_output=True, text=True)
        self.assertEqual(second.returncode, 2)
        with self.assertRaisesRegex(PLAN.PlanError, 'required consumer module missing'):
            PLAN.check_consumer(root / 'missing-consumer', root, self.current)


def consumer_suite(consumer):
    # Explicit integration entry point keeps the ordinary source-only suite
    # dependency-free. This suite never substitutes a parser or skips absence.
    class ActualConsumerTests(NativeProjectionTests):
        def test_actual_parser_discovery_router_and_registry(self):
            parsed = PLAN.check_consumer(consumer, ROOT, self.current)
            self.assertEqual(len(parsed['raw']), 1598)
            self.assertEqual(sum(r['status'] == 'complete' for r in parsed['raw']
                                 if r['sourceId'] in self.current[2]), 59)
            self.assertEqual(sum(r['status'] == 'complete' for r in parsed['raw']),
                             sum(PLAN.reported_status(self.current, tid)[0] == 'x'
                                 for tid in self.current[1]))
            self.assertEqual({r['displayLane'] for r in parsed['raw']},
                             {'preflight', 'implement', 'verify', 'review', 'land'})

        def test_actual_consumer_rejects_duplicate_missing_and_unsupported(self):
            root = self.stage()
            extra = root / 'docs/plans/duplicate.md'
            extra.write_bytes((root / 'docs/plans/E1.md').read_bytes())
            with self.assertRaises(PLAN.PlanError):
                PLAN.check_consumer(consumer, root, self.current)
            extra.unlink()
            delivery = root / 'docs/plans/delivery-01.md'
            original = delivery.read_bytes()
            delivery.write_bytes(original.replace(b'Stage: preflight', b'Stage: unsupported', 1))
            with self.assertRaisesRegex(PLAN.PlanError, 'unsupported display stage'):
                PLAN.check_consumer(consumer, root, self.current)
            delivery.write_bytes(original)
            hidden = root / 'docs/planning/hidden.md'
            hidden.parent.mkdir(parents=True)
            delivery.rename(hidden)
            with self.assertRaises(PLAN.PlanError):
                PLAN.check_consumer(consumer, root, self.current)

        def test_actual_consumer_reported_status_and_acceptance_corruption(self):
            root = self.stage()
            path = root / 'docs/plans/E1.md'
            original = path.read_bytes()
            for before, after in ((b'- [x] T1.1', b'- [ ] T1.1'),
                                  (b'Acceptance: [Module compiles', b'Acceptance: [Module fails')):
                path.write_bytes(original.replace(before, after, 1))
                with self.assertRaises(PLAN.PlanError):
                    PLAN.check_consumer(consumer, root, self.current)
            path.write_bytes(original)
            PLAN.check_consumer(consumer, root, self.current)

    return unittest.defaultTestLoader.loadTestsFromTestCase(ActualConsumerTests)


if __name__ == '__main__':
    if len(sys.argv) == 3 and sys.argv[1] == '--consumer-root':
        result = unittest.TextTestRunner(verbosity=2).run(consumer_suite(Path(sys.argv[2])))
        sys.exit(not result.wasSuccessful())
    unittest.main()
