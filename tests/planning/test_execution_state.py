"""Execution evidence must survive regeneration of the complete plan."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class ExecutionStateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        spec = importlib.util.spec_from_file_location('render', ROOT / 'scripts/render-plan.py')
        self.renderer = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.renderer)
        self.renderer.ROOT = self.root
        self.renderer.DATA = ROOT / 'docs/planning/plan-data.json'
        (self.root / 'docs/planning').mkdir(parents=True)

    def render(self, state):
        (self.root / 'docs/planning/execution-state.json').write_text(json.dumps(state))
        self.renderer.main()

    def test_accepted_evidence_survives_two_renders(self):
        state = {'T1.1': {'status': 'ACCEPTED', 'certification': 'REVIEWED',
                         'evidence': ['Real database isolation and failure checks passed.']}}
        self.render(state)
        task = self.root / 'docs/tasks/T1.1.md'
        first = task.read_text()
        self.render(state)
        self.assertEqual(first, task.read_text())
        self.assertIn('Certification: REVIEWED', first)
        self.assertIn('Real database isolation', first)
        self.assertIn('- [x] T1.1 ', (self.root / 'docs/plans/E1.md').read_text())

    def test_in_progress_is_not_checked_off(self):
        self.render({'T1.1': {'status': 'IN_PROGRESS', 'certification': 'NOT_RUN'}})
        self.assertIn('- [ ] T1.1 ', (self.root / 'docs/plans/E1.md').read_text())
        self.assertIn('Status: IN_PROGRESS', (self.root / 'docs/tasks/T1.1.md').read_text())

    def test_checker_rejects_false_acceptance(self):
        self.render({'T1.1': {'status': 'ACCEPTED', 'certification': 'NOT_RUN'}})
        script = (ROOT / 'scripts/check-plan.py').read_text()
        scripts = self.root / 'scripts'
        scripts.mkdir()
        (scripts / 'check-plan.py').write_text(script)
        data = json.loads(self.renderer.DATA.read_text())
        (self.root / 'docs/planning/plan-data.json').write_text(json.dumps(data))
        result = subprocess.run([sys.executable, str(scripts / 'check-plan.py')],
                                capture_output=True, text=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('acceptance requires evidence', result.stdout)
        self.assertIn('acceptance requires reviewed certification', result.stdout)


if __name__ == '__main__':
    unittest.main()
