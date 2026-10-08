"""Retired commands must not overwrite current projections from stale inputs."""
import pathlib
import subprocess
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]

class RetiredPlanToolsTests(unittest.TestCase):
    def test_current_master_blocks_stale_tools_without_writes(self):
        for name in ("render-plan.py", "check-plan.py", "check-release-evidence.py"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = pathlib.Path(tmp)
                (root / "scripts").mkdir()
                (root / "docs/planning").mkdir(parents=True)
                script = root / "scripts" / name
                script.write_bytes((ROOT / "scripts" / name).read_bytes())
                master = root / "docs/planning/wazi-source.json"
                master.write_text('{"canonical_edit":"must not be overwritten"}')
                baseline = root / "docs/planning/plan-data.json"
                baseline.write_text("INVALID STALE BASELINE")
                before = {str(p.relative_to(root)): p.read_bytes()
                          for p in root.rglob("*") if p.is_file()}
                result = subprocess.run([sys.executable, str(script)], capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b"Retired baseline command", result.stderr)
                after = {str(p.relative_to(root)): p.read_bytes()
                         for p in root.rglob("*") if p.is_file()}
                self.assertEqual(before, after)

if __name__ == "__main__":
    unittest.main()
