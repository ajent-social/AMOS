#!/usr/bin/env python3
"""Reject likely credentials and private locators in files intended for publication.

By default the checker inspects tracked files and non-ignored untracked files
reported by Git. Pass ``--paths`` to inspect only selected files or directories;
this is useful for review fixtures and pre-publication selections.
"""

from __future__ import annotations

import argparse
import os
from dataclasses import dataclass
from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
IPV4_OCTET = r"(?:25[0-5]|2[0-4][0-9]|1?[0-9]{1,2})"
PRIVATE_IPV4 = (
    rf"127\.(?!0\.0\.1(?:$|[^0-9.])){IPV4_OCTET}\.{IPV4_OCTET}\.{IPV4_OCTET}"
    rf"|10\.{IPV4_OCTET}\.{IPV4_OCTET}\.{IPV4_OCTET}"
    rf"|192\.168\.{IPV4_OCTET}\.{IPV4_OCTET}"
    rf"|169\.254\.{IPV4_OCTET}\.{IPV4_OCTET}"
    rf"|172\.(?:1[6-9]|2[0-9]|3[01])\.{IPV4_OCTET}\.{IPV4_OCTET}"
)

PRIVATE_PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    (
        "private-home-path",
        re.compile(r"(?:/|\\)(?:Users|home)[/\\][A-Za-z0-9._-]+(?:[/\\]|(?=$|[\"']))", re.IGNORECASE),
    ),
    (
        "private-system-path",
        re.compile(r"/(?:private/(?:var|tmp)|Volumes)/[^/\s\"'<>]+", re.IGNORECASE),
    ),
    (
        "private-network-address",
        re.compile(
            rf"(?ix)(?<![0-9.])(?:{PRIVATE_IPV4}"
            r"|(?:(?:fc[0-9a-f]{2}|fd[0-9a-f]{2})|fe80):"
            r"(?:[0-9a-f]{1,4}:){1,6}[0-9a-f]{1,4}"
            r"|(?:(?:fc[0-9a-f]{2}|fd[0-9a-f]{2})|fe80):"
            r"(?:[0-9a-f]{1,4}:)?:(?:[0-9a-f]{1,4}:?){1,6}"
            r")(?![0-9a-f:.])"
        ),
    ),
    (
        "private-hostname",
        re.compile(r"\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.(?:local|internal|lan|home\.arpa)\b", re.IGNORECASE),
    ),
)

CREDENTIAL_PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    ("cloud-access-key", re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b")),
    ("provider-token", re.compile(r"\b(?:gh[pousr]_|github_pat_|xox[baprs]-|sk-proj-)[A-Za-z0-9_-]{16,}\b")),
    ("private-key-material", re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----")),
)

ASSIGNMENT = re.compile(
    r"(?<![a-z0-9])(?:password|passwd|passphrase|secret(?:[_-]?(?:access[_-]?)?key)?|api[_-]?key|"
    r"access[_-]?token|auth[_-]?token|client[_-]?secret|private[_-]?key|"
    r"credential|(?:database|db)[_-]?url)(?![a-z0-9])\s*[:=]\s*"
    r"(?P<value>\"[^\"\r\n]*\"|'[^'\r\n]*'|[^\s,;#]+)",
    re.IGNORECASE,
)
SAFE_ASSIGNMENT_VALUES = re.compile(
    r"(?ix)^(?:"
    r"(?:string|str|bytes|bool|int|true|false|none|null|undefined)"
    r"|(?:example|test|dummy|placeholder|redacted|changeme|replace[-_ ]?me)"
    r"|your[-_ ]?(?:api[-_ ]?key|secret|password)"
    r"|<[^>]+>"
    r"|\$\{?[A-Z_][A-Z0-9_]*\}?"
    r"|(?:os\.)?(?:getenv\(.+\)|environ(?:\[[^]]+\])?)"
    r"|(?:settings?|config|vault|secretmanager)(?:[._\[(].*)?"
    r")$"
)


@dataclass(frozen=True)
class Finding:
    path: str
    line: int
    category: str


def _display_path(root: Path, path: Path) -> str:
    try:
        return path.relative_to(root).as_posix()
    except ValueError:
        return "<outside-root>"


def _within_root(root: Path, path: Path) -> bool:
    try:
        path.relative_to(root)
        return True
    except ValueError:
        return False


def _is_temporary_path(root: Path, path: Path) -> bool:
    parts = path.relative_to(root).parts
    return (
        bool(parts) and parts[0] in {"tmp", ".worktrees"}
        or "__pycache__" in parts
        or parts[:2] == (".claude", "scratch")
    )


def _explicit_files(root: Path, selected: list[str]) -> tuple[list[Path], list[Finding]]:
    files: list[Path] = []
    errors: list[Finding] = []
    for name in selected:
        candidate = Path(name)
        if not candidate.is_absolute():
            candidate = root / candidate
        try:
            candidate = Path(os.path.abspath(candidate))
        except (OSError, RuntimeError):
            errors.append(Finding("<selected-input>", 0, "unreadable-input"))
            continue
        if not _within_root(root, candidate) or ".git" in candidate.relative_to(root).parts:
            errors.append(Finding("<selected-input>", 0, "disallowed-input-path"))
            continue
        if _is_temporary_path(root, candidate):
            errors.append(Finding("<selected-input>", 0, "ignored-temporary-input"))
            continue
        if not candidate.is_symlink():
            try:
                resolved_parent = candidate.parent.resolve(strict=True)
            except (OSError, RuntimeError):
                errors.append(Finding("<selected-input>", 0, "unreadable-input"))
                continue
            if not _within_root(root, resolved_parent):
                errors.append(Finding("<selected-input>", 0, "disallowed-input-path"))
                continue
        if candidate.is_symlink():
            files.append(candidate)
        elif candidate.is_dir():
            for current, dirs, names in os.walk(candidate, followlinks=False):
                current_path = Path(current)
                dirs[:] = [
                    directory
                    for directory in dirs
                    if directory != ".git" and not _is_temporary_path(root, current_path / directory)
                ]
                for filename in names:
                    path = Path(current) / filename
                    if ".git" not in path.relative_to(root).parts:
                        files.append(path)
        elif candidate.is_file():
            files.append(candidate)
        else:
            errors.append(Finding("<selected-input>", 0, "unreadable-input"))
    return sorted(set(files)), errors


def _git_files(root: Path) -> tuple[list[Path], list[Finding]]:
    try:
        result = subprocess.run(
            ["git", "-C", str(root), "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
            check=True,
            capture_output=True,
        )
    except (OSError, subprocess.CalledProcessError):
        return [], [Finding("<repository>", 0, "git-file-list-failed")]
    paths = [
        path
        for path in (root / os.fsdecode(item) for item in result.stdout.split(b"\0") if item)
        if path.exists() or path.is_symlink()
    ]
    return paths, []


def _content(path: Path) -> tuple[str | None, bool]:
    try:
        if path.is_symlink():
            return os.readlink(path), False
        raw = path.read_bytes()
    except (OSError, RuntimeError):
        return None, True
    if b"\0" in raw:
        return None, False
    try:
        return raw.decode("utf-8"), False
    except UnicodeDecodeError:
        return None, True


def scan_public_artifacts(root: Path, paths: list[str] | None = None) -> list[Finding]:
    """Return sanitized findings; matched content is never included."""
    root = root.resolve()
    candidates, findings = _explicit_files(root, paths) if paths else _git_files(root)

    for path in candidates:
        display_path = _display_path(root, path)
        content, unreadable = _content(path)
        if unreadable:
            findings.append(Finding(display_path, 0, "unreadable-artifact"))
            continue
        if content is None:
            continue
        for line_number, line in enumerate(content.splitlines(), 1):
            for category, pattern in PRIVATE_PATTERNS + CREDENTIAL_PATTERNS:
                if pattern.search(line):
                    findings.append(Finding(display_path, line_number, category))
            for match in ASSIGNMENT.finditer(line):
                value = match.group("value").strip("\"'")
                if value and not SAFE_ASSIGNMENT_VALUES.fullmatch(value):
                    findings.append(Finding(display_path, line_number, "credential-assignment"))

    return sorted(set(findings), key=lambda finding: (finding.path, finding.line, finding.category))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT, help="repository root (defaults to this checkout)")
    parser.add_argument("--paths", nargs="+", help="scan only these files or directories under --root")
    args = parser.parse_args(argv)

    findings = scan_public_artifacts(args.root, args.paths)
    for finding in findings:
        where = f"{finding.path}:{finding.line}" if finding.line else finding.path
        print(f"{where}: {finding.category}", file=sys.stderr)
    if findings:
        print(f"public artifact check failed: {len(findings)} finding(s)", file=sys.stderr)
        return 1
    print("public artifact check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
