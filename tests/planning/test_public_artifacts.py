from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CHECKER = ROOT / "scripts" / "check-public-artifacts.py"


def run_checker(root: Path, paths: list[str] | None = None) -> subprocess.CompletedProcess[str]:
    command = [sys.executable, str(CHECKER), "--root", str(root)]
    if paths:
        command.extend(["--paths", *paths])
    return subprocess.run(command, capture_output=True, text=True, check=False)


class PublicArtifactCheckerTests(unittest.TestCase):
    def test_rejects_synthetic_secrets_and_private_locators_without_echoing_values(self) -> None:
        fixtures = {
            "credential.txt": "api_" + 'key = "' + ("Synthetic" * 4) + '"\n',
            "short-password.txt": "pass" + 'word = "bad"\n',
            "home-path.txt": 'config = "' + "/" + "Users" + "/fixture/private/settings" + '"\n',
            "windows-home-path.txt": (
                'config = "C:' + "\\" + "Users" + "\\fixture\\private\\settings" + '"\n'
            ),
            "private-ip.txt": "host = \"" + ".".join(("192", "168", "44", "19")) + "\"\n",
            "loopback-ip.txt": "host = \"" + ".".join(("127", "0", "0", "2")) + "\"\n",
            "private-ipv6.txt": "host = \"" + ":".join(("fd00", "", "1")) + "\"\n",
            "private-host.txt": 'server = "build-agent' + ".local" + '"\n',
            "private-key.txt": "-----BEGIN " + "OPENSSH PRIVATE KEY" + "-----\n",
            "env-secret.txt": "AWS_SECRET_ACCESS_" + "KEY=" + ("q" * 32) + "\n",
        }
        expected_categories = {
            "credential.txt": "credential-assignment",
            "short-password.txt": "credential-assignment",
            "home-path.txt": "private-home-path",
            "windows-home-path.txt": "private-home-path",
            "private-ip.txt": "private-network-address",
            "loopback-ip.txt": "private-network-address",
            "private-ipv6.txt": "private-network-address",
            "private-host.txt": "private-hostname",
            "private-key.txt": "private-key-material",
            "env-secret.txt": "credential-assignment",
        }

        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            for name, contents in fixtures.items():
                (root / name).write_text(contents, encoding="utf-8")

            for name, category in expected_categories.items():
                with self.subTest(fixture=name):
                    result = run_checker(root, [name])
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(category, result.stderr)
                    self.assertNotIn("Synthetic" * 4, result.stderr)
                    private_address = ".".join(("192", "168", "44", "19"))
                    loopback_address = ".".join(("127", "0", "0", "2"))
                    private_ipv6 = ":".join(("fd00", "", "1"))
                    private_host = "build-agent" + ".local"
                    private_path = "/" + "Users" + "/fixture"
                    self.assertNotIn(private_address, result.stderr)
                    self.assertNotIn(loopback_address, result.stderr)
                    self.assertNotIn(private_ipv6, result.stderr)
                    self.assertNotIn(private_host, result.stderr)
                    self.assertNotIn(private_path, result.stderr)

    def test_allows_clean_artifact_and_documented_placeholders(self) -> None:
        content = "\n".join(
            (
                'api_key = os.getenv("PUBLIC_API_KEY")',
                'password: "<configured-at-deploy-time>"',
                "example address: 203.0.113.12",
                "example host: service.example.test",
                'development bind addresses: "localhost", "'
                + ".".join(("127", "0", "0", "1"))
                + '", "::'
                + "1"
                + '"',
                r"generic scanner regexes: /Users/|/home/ and \bAKIA[A-Z0-9]{16}\b",
                "pattern examples use symbolic placeholders only",
            )
        )
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            (root / "README.md").write_text(content, encoding="utf-8")
            result = run_checker(root, ["README.md"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("passed", result.stdout)

    def test_scans_tracked_and_nonignored_untracked_repository_artifacts(self) -> None:
        result = run_checker(ROOT)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_refuses_explicit_git_metadata_path(self) -> None:
        result = run_checker(ROOT, [".git"])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("disallowed-input-path", result.stderr)
        self.assertNotIn("remote", result.stderr)

    def test_does_not_scan_explicit_ignored_temporary_path(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            (root / "tmp").mkdir()
            sample_value = "tmp-only" * 4
            assignment = "api_" + "key = "
            (root / "tmp" / "fixture.txt").write_text(
                assignment + '"' + sample_value + '"\n', encoding="utf-8"
            )
            result = run_checker(root, ["tmp"])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ignored-temporary-input", result.stderr)
        self.assertNotIn(sample_value, result.stderr)
        self.assertNotIn("credential-assignment", result.stderr)

    def test_scans_symlink_target_text_without_following_it(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            target = "/" + "Users" + "/fixture/private/settings"
            os.symlink(target, root / "private-link")
            result = run_checker(root, ["private-link"])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("private-home-path", result.stderr)
        self.assertNotIn(target, result.stderr)


if __name__ == "__main__":
    unittest.main()
