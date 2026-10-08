#!/usr/bin/env python3
"""Fail closed on current release qualification; retain explicit historical parser checks."""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

from typing import Any


ROOT = Path(__file__).resolve().parents[1]
MATRIX_PATH = ROOT / "docs/releases/compatibility.md"
START = "<!-- release-matrix-data:start -->"
END = "<!-- release-matrix-data:end -->"
STATE_FIELDS = {
    "implemented",
    "tested",
    "qualified",
    "deployed",
    "rehearsed",
}
CLAIMS = {"not_claimed", "supported", "unsupported"}
REQUIRED_AWS_PROFILES = {"aws_managed", "aws_vm"}
PROFILE_REQUIRED_EVIDENCE = {
    "aws_managed": ["behavior", "cloud", "recovery"],
    "aws_vm": ["behavior", "cloud", "recovery"],
    "cloudflare_routing": ["behavior", "cloud", "network"],
}
PROFILE_NAMES = {
    "aws_managed": "Managed containers with managed PostgreSQL",
    "aws_vm": "One VM hosting the application and PostgreSQL",
    "cloudflare_routing": "Public DNS and same-domain routing",
}
SCOPE_SOURCES = [
    "docs/planning/requirements.md",
    "docs/usecases-manifest.json",
    "docs/planning/plan-data.json",
    "docs/planning/contracts.md",
    "docs/tasks/<task-id>.md",
]
MATRIX_FIELDS = {
    "schema_version", "contract_version", "candidate_id", "release_head", "release_scope",
    "scope_sources", "capabilities", "stories", "profiles",
}
CAPABILITY_FIELDS = {
    "id", "name", "planned_refs", "task_ids", "contract_refs", "story_ids",
    "interfaces", "required_evidence", "claim", "states", "evidence",
}
STORY_FIELDS = {
    "id", "name", "source_status", "task_ids", "contract_refs", "interfaces",
    "required_evidence", "claim", "states", "evidence",
}
PROFILE_FIELDS = {"id", "name", "claim", "states", "required_evidence", "evidence"}


class MatrixError(Exception):
    pass


def parse_matrix_text(text: str) -> dict[str, Any]:
    if text.count(START) != 1 or text.count(END) != 1:
        raise MatrixError("matrix must have exactly one delimited JSON data block")
    block = text.split(START, 1)[1].split(END, 1)[0]
    match = re.search(r"```json\s*\n(.*?)\n```", block, re.DOTALL)
    if not match:
        raise MatrixError("matrix JSON data block is missing")
    try:
        data = json.loads(match.group(1))
    except json.JSONDecodeError as exc:
        raise MatrixError(f"matrix JSON is invalid at line {exc.lineno}: {exc.msg}") from exc
    if not isinstance(data, dict):
        raise MatrixError("matrix root must be a JSON object")
    return data


def read_matrix(path: Path = MATRIX_PATH) -> dict[str, Any]:
    return parse_matrix_text(path.read_text(encoding="utf-8"))


def parse_requirement_inventory() -> dict[str, dict[str, str]]:
    path = ROOT / "docs/planning/requirements.md"
    requirements: dict[str, dict[str, str]] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
        if len(cells) < 3 or not re.fullmatch(r"R\d{2}", cells[0]):
            continue
        requirement_id = cells[0]
        if requirement_id in requirements:
            raise MatrixError(f"duplicate canonical requirement {requirement_id}")
        requirements[requirement_id] = {"title": cells[1], "planned_refs": cells[2]}
    if not requirements:
        raise MatrixError("canonical requirements inventory is empty")
    return requirements


def parse_plan() -> tuple[dict[str, dict[str, Any]], dict[str, set[str]]]:
    plan = json.loads((ROOT / "docs/planning/plan-data.json").read_text(encoding="utf-8"))
    tasks: dict[str, dict[str, Any]] = {}
    for epic in plan.get("epics", []):
        for task in epic.get("tasks", []):
            task_id = task.get("id")
            if not isinstance(task_id, str) or task_id in tasks:
                raise MatrixError(f"invalid or duplicate task ID in plan data: {task_id!r}")
            tasks[task_id] = {**task, "epic_id": epic.get("id")}
    use_case_tasks: dict[str, set[str]] = {}
    for task_id, task in tasks.items():
        for use_case_id in task.get("use_cases", []):
            use_case_tasks.setdefault(use_case_id, set()).add(task_id)
    return tasks, use_case_tasks


def parse_use_cases() -> dict[str, dict[str, Any]]:
    rows = json.loads((ROOT / "docs/usecases-manifest.json").read_text(encoding="utf-8"))
    result: dict[str, dict[str, Any]] = {}
    for row in rows:
        use_case_id = row.get("id")
        if not isinstance(use_case_id, str) or use_case_id in result:
            raise MatrixError(f"invalid or duplicate use-case ID: {use_case_id!r}")
        result[use_case_id] = row
    return result


def expand_planned_task_refs(refs: str, tasks: dict[str, dict[str, Any]]) -> set[str]:
    selected: set[str] = set()
    for match in re.finditer(r"\bE(\d+)(?:\s*-\s*E?(\d+))?\b", refs):
        first = int(match.group(1))
        last = int(match.group(2) or first)
        epics = {f"E{number}" for number in range(first, last + 1)}
        selected.update(task_id for task_id, task in tasks.items() if task.get("epic_id") in epics)
    pattern = r"\bT(\d+)\.(\d+)(?:\s*-\s*T?(\d+)\.(\d+))?\b"
    for match in re.finditer(pattern, refs):
        epic, first = int(match.group(1)), int(match.group(2))
        end_epic, last = int(match.group(3) or epic), int(match.group(4) or first)
        if epic != end_epic:
            raise MatrixError(f"unsupported cross-epic task range in requirements: {match.group(0)}")
        selected.update(f"T{epic}.{number}" for number in range(first, last + 1))
    return selected & tasks.keys()


def evidence_classes(tasks: list[dict[str, Any]], interface_types: set[str], scope_text: str = "") -> list[str]:
    classes = {"implementation", "behavior"}
    normalized = {item.casefold() for item in interface_types}
    if normalized & {"web", "ui", "browser"}:
        classes.add("browser")
    if normalized & {"api", "rest", "http", "openapi"}:
        classes.add("api")
    if normalized & {"mcp", "oauth", "cli", "go", "sdk"}:
        classes.add("client")
    text = (scope_text + " " + " ".join(
        str(value)
        for task in tasks
        for key in ("title", "objective", "external_gate")
        if (value := task.get(key))
    )).casefold()
    provider_patterns = (
        r"\baws\b", r"\bcloudflare\b", r"\bstripe\b", r"\bses\b",
        r"\bpayment provider\b", r"\bidentity[- ]provider\b",
        r"\bemail delivery\b", r"\bfederated login\b",
    )
    if any(re.search(pattern, text) for pattern in provider_patterns):
        classes.add("provider")
    cloud_patterns = (
        r"\baws\b", r"\bcloudflare\b", r"\bcloud deployment\b", r"\bcloud infrastructure\b",
        r"\bdns\b", r"\bsame-domain routing\b", r"\bpulumi\b",
    )
    if any(re.search(pattern, text) for pattern in cloud_patterns):
        classes.add("cloud")
    return sorted(classes)


def expected_inventory() -> dict[str, Any]:
    requirements = parse_requirement_inventory()
    tasks, use_case_tasks = parse_plan()
    plan = json.loads((ROOT / "docs/planning/plan-data.json").read_text(encoding="utf-8"))
    use_cases = parse_use_cases()
    plan_use_cases = set(use_case_tasks)
    if plan_use_cases != set(use_cases):
        missing = sorted(set(use_cases) - plan_use_cases)
        unknown = sorted(plan_use_cases - set(use_cases))
        raise MatrixError(f"plan/use-case inventory mismatch; unreferenced={missing}; unknown={unknown}")

    story_rows: dict[str, dict[str, Any]] = {}
    for use_case_id, use_case in sorted(use_cases.items()):
        task_ids = sorted(use_case_tasks[use_case_id])
        contract_refs = [f"docs/tasks/{task_id}.md" for task_id in task_ids]
        missing_contracts = [ref for ref in contract_refs if not (ROOT / ref).is_file()]
        if missing_contracts:
            raise MatrixError(f"missing public task contracts for {use_case_id}: {missing_contracts}")
        interface_types = {item.get("type", "") for item in use_case.get("interfaces", [])}
        story_rows[use_case_id] = {
            "id": use_case_id,
            "name": use_case["name"],
            "source_status": use_case.get("status", ""),
            "task_ids": task_ids,
            "contract_refs": contract_refs,
            "interfaces": sorted(interface_types),
            "required_evidence": evidence_classes([tasks[task_id] for task_id in task_ids], interface_types, use_case["name"]),
        }

    capability_rows: dict[str, dict[str, Any]] = {}
    for requirement_id, requirement in sorted(requirements.items()):
        task_ids = sorted(expand_planned_task_refs(requirement["planned_refs"], tasks))
        if not task_ids:
            raise MatrixError(f"requirement {requirement_id} has no resolvable planned task contracts")
        contract_refs = [f"docs/tasks/{task_id}.md" for task_id in task_ids]
        missing_contracts = [ref for ref in contract_refs if not (ROOT / ref).is_file()]
        if missing_contracts:
            raise MatrixError(f"missing public task contracts for {requirement_id}: {missing_contracts}")
        story_ids = sorted({use_case_id for task_id in task_ids for use_case_id in tasks[task_id].get("use_cases", [])})
        interfaces = {interface for use_case_id in story_ids for interface in story_rows[use_case_id]["interfaces"]}
        capability_rows[requirement_id] = {
            "id": requirement_id,
            "name": requirement["title"],
            "planned_refs": requirement["planned_refs"],
            "task_ids": task_ids,
            "contract_refs": contract_refs,
            "story_ids": story_ids,
            "interfaces": sorted(interfaces),
            "required_evidence": evidence_classes([tasks[task_id] for task_id in task_ids], interfaces, requirement["title"]),
        }
    return {
        "contract_version": plan.get("contract", ""),
        "capabilities": capability_rows,
        "stories": story_rows,
        "tasks": tasks,
    }


def _unique_index(rows: Any, label: str, errors: list[str]) -> dict[str, dict[str, Any]]:
    if not isinstance(rows, list):
        errors.append(f"{label} must be an array")
        return {}
    index: dict[str, dict[str, Any]] = {}
    for row in rows:
        if not isinstance(row, dict) or not isinstance(row.get("id"), str):
            errors.append(f"{label} contains an entry without a string ID")
            continue
        item_id = row["id"]
        if item_id in index:
            errors.append(f"duplicate {label} ID: {item_id}")
        index[item_id] = row
    return index


def validate_structure(matrix: dict[str, Any], inventory: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if set(matrix) != MATRIX_FIELDS:
        errors.append("matrix has unknown or missing root fields")
    if matrix.get("schema_version") != 1:
        errors.append("matrix schema_version must be 1")
    if matrix.get("contract_version") != inventory["contract_version"]:
        errors.append("matrix contract_version is stale or unsupported")
    if matrix.get("scope_sources") != SCOPE_SOURCES:
        errors.append("matrix scope_sources do not match the approved public inventories")
    capabilities = _unique_index(matrix.get("capabilities"), "capability", errors)
    stories = _unique_index(matrix.get("stories"), "story", errors)
    for label, actual, expected in (
        ("capability", capabilities, inventory["capabilities"]),
        ("story", stories, inventory["stories"]),
    ):
        for missing in sorted(expected.keys() - actual.keys()):
            errors.append(f"missing {label} {missing}")
        for unknown in sorted(actual.keys() - expected.keys()):
            errors.append(f"unknown {label} {unknown}")
        for item_id in sorted(actual.keys() & expected.keys()):
            row = actual[item_id]
            expected_row = expected[item_id]
            expected_fields = CAPABILITY_FIELDS if label == "capability" else STORY_FIELDS
            if set(row) != expected_fields:
                errors.append(f"{label} {item_id} has unknown or missing fields")
            for field in ("name", "task_ids", "contract_refs", "interfaces", "required_evidence"):
                if row.get(field) != expected_row.get(field):
                    errors.append(f"{label} {item_id} has stale or incorrect {field}")
            if label == "capability":
                for field in ("planned_refs", "story_ids"):
                    if row.get(field) != expected_row.get(field):
                        errors.append(f"capability {item_id} has stale or incorrect {field}")
            elif row.get("source_status") != expected_row.get("source_status"):
                errors.append(f"story {item_id} has stale source_status")
            if not isinstance(row.get("claim"), str) or row["claim"] not in CLAIMS:
                errors.append(f"{label} {item_id} has unknown release claim")
            states = row.get("states")
            if not isinstance(states, dict) or set(states) != STATE_FIELDS:
                errors.append(f"{label} {item_id} must state all five release stages")
            elif not all(isinstance(value, str) and value for value in states.values()):
                errors.append(f"{label} {item_id} has an empty release stage")
            if not isinstance(row.get("evidence"), list):
                errors.append(f"{label} {item_id} evidence must be an array")

    covered_stories = {
        story_id
        for row in inventory["capabilities"].values()
        for story_id in row["story_ids"]
    }
    expected_stories = set(inventory["stories"])
    if covered_stories != expected_stories:
        errors.append("canonical capability task contracts do not cover the complete user-story inventory")

    profiles = _unique_index(matrix.get("profiles"), "profile", errors)
    required_profiles = REQUIRED_AWS_PROFILES | {"cloudflare_routing"}
    for missing in sorted(required_profiles - profiles.keys()):
        errors.append(f"missing deployment profile {missing}")
    for unknown in sorted(profiles.keys() - required_profiles):
        errors.append(f"unknown deployment profile {unknown}")
    for profile_id, row in profiles.items():
        if set(row) != PROFILE_FIELDS:
            errors.append(f"profile {profile_id} has unknown or missing fields")
        if row.get("name") != PROFILE_NAMES.get(profile_id):
            errors.append(f"profile {profile_id} has stale or unsupported description")
        if not isinstance(row.get("claim"), str) or row["claim"] not in CLAIMS:
            errors.append(f"profile {profile_id} has unknown release claim")
        if not isinstance(row.get("states"), dict) or set(row["states"]) != STATE_FIELDS:
            errors.append(f"profile {profile_id} must state all five release stages")
        if row.get("required_evidence") != PROFILE_REQUIRED_EVIDENCE.get(profile_id):
            errors.append(f"profile {profile_id} has stale mandatory evidence requirements")
        if not isinstance(row.get("evidence"), list):
            errors.append(f"profile {profile_id} evidence must be an array")

    scope = matrix.get("release_scope")
    if not isinstance(scope, dict):
        errors.append("release_scope must be an object")
    else:
        expected_scope_keys = {"capabilities", "stories", "profiles"}
        if set(scope) != expected_scope_keys:
            errors.append("release_scope must contain exactly capabilities, stories, and profiles")
        for key in expected_scope_keys & scope.keys():
            value = scope[key]
            if not isinstance(value, list) or any(not isinstance(item, str) for item in value):
                errors.append(f"release_scope.{key} must be an array of string IDs")
            elif len(value) != len(set(value)):
                errors.append(f"release_scope.{key} must be a duplicate-free array")
            elif set(value) - set((capabilities if key == "capabilities" else stories if key == "stories" else profiles)):
                errors.append(f"release_scope.{key} contains unknown IDs")
    return errors


def validate_evidence(
    row: dict[str, Any],
    label: str,
    release_head: str | None,
    selected_profiles: set[str] | None = None,
) -> list[str]:
    errors: list[str] = []
    required = set(row.get("required_evidence", []))
    evidence = row.get("evidence", [])
    seen: set[str] = set()
    for record in evidence:
        if not isinstance(record, dict):
            errors.append(f"{label} evidence record must be an object")
            continue
        allowed_fields = {
            "class", "path", "sha256", "review_path", "review_sha256",
            "artifact_revision", "reviewed_head", "review_status", "execution", "profile_id",
        }
        if set(record) - allowed_fields:
            errors.append(f"{label} evidence has unknown fields")
        required_fields = allowed_fields - {"profile_id"}
        if required_fields - set(record):
            errors.append(f"{label} evidence is missing required fields")
        kind = record.get("class")
        if not isinstance(kind, str) or kind not in required:
            errors.append(f"{label} contains unsupported evidence class {kind!r}")
            continue
        if kind in seen:
            errors.append(f"{label} duplicates evidence class {kind}")
        seen.add(kind)
        source = record.get("path")
        if not isinstance(source, str) or not source.startswith("docs/evidence/") or ".." in Path(source).parts:
            errors.append(f"{label} {kind} evidence must reference a public docs/evidence artifact")
            continue
        path = ROOT / source
        if not path.is_file():
            errors.append(f"{label} {kind} evidence artifact is missing")
            continue
        evidence_root = (ROOT / "docs/evidence").resolve()
        try:
            path.resolve().relative_to(evidence_root)
        except ValueError:
            errors.append(f"{label} {kind} evidence artifact resolves outside docs/evidence")
            continue
        review_source = record.get("review_path")
        if not isinstance(review_source, str) or not review_source.startswith("docs/evidence/") or ".." in Path(review_source).parts:
            errors.append(f"{label} {kind} evidence must reference a separate public review artifact")
            continue
        review_path = ROOT / review_source
        if not review_path.is_file() or review_source == source:
            errors.append(f"{label} {kind} independent review artifact is missing or not separate")
            continue
        try:
            review_path.resolve().relative_to(evidence_root)
        except ValueError:
            errors.append(f"{label} {kind} review artifact resolves outside docs/evidence")
            continue
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if record.get("sha256") != digest:
            errors.append(f"{label} {kind} evidence artifact hash does not match")
        review_digest = hashlib.sha256(review_path.read_bytes()).hexdigest()
        if record.get("review_sha256") != review_digest:
            errors.append(f"{label} {kind} review artifact hash does not match")
        artifact_revision = record.get("artifact_revision", "")
        reviewed_head = record.get("reviewed_head", "")
        if not isinstance(artifact_revision, str) or not re.fullmatch(r"[0-9a-f]{40}", artifact_revision) or reviewed_head != artifact_revision:
            errors.append(f"{label} {kind} evidence is not bound to one exact reviewed head")
        if release_head and artifact_revision != release_head:
            errors.append(f"{label} {kind} evidence is for a different release head")
        if record.get("review_status") != "independent_pass":
            errors.append(f"{label} {kind} evidence lacks independent passing review")
        if not isinstance(record.get("execution"), str) or record["execution"] not in {"local", "live"}:
            errors.append(f"{label} {kind} evidence has an unsupported execution tier")
        if kind in {"provider", "cloud"} and record.get("execution") != "live":
            errors.append(f"{label} {kind} evidence must come from a live service/profile")
        if kind == "cloud" and not record.get("profile_id"):
            errors.append(f"{label} cloud evidence must name its exact deployment profile")
        if kind == "cloud" and record.get("profile_id") not in (REQUIRED_AWS_PROFILES | {"cloudflare_routing"}):
            errors.append(f"{label} cloud evidence names an unknown deployment profile")
        if kind == "cloud" and selected_profiles is not None and record.get("profile_id") not in selected_profiles:
            errors.append(f"{label} cloud evidence names a profile outside this release scope")
        if kind != "cloud" and "profile_id" in record:
            errors.append(f"{label} {kind} evidence cannot declare a deployment profile")
    return errors


def validate_release_states(row: dict[str, Any], label: str) -> list[str]:
    errors: list[str] = []
    states = row.get("states", {})
    required = set(row.get("required_evidence", []))
    for stage in ("implemented", "tested"):
        if states.get(stage) != "complete":
            errors.append(f"{label} has incomplete {stage} state")
    if required & {"api", "browser", "client", "provider", "cloud", "network"}:
        if states.get("qualified") != "complete":
            errors.append(f"{label} has incomplete qualification state")
    elif not isinstance(states.get("qualified"), str) or states["qualified"] not in {"complete", "not_required"}:
        errors.append(f"{label} has incomplete qualification state")
    if "cloud" in required or "network" in required:
        if states.get("deployed") != "complete":
            errors.append(f"{label} has incomplete deployment state")
    elif not isinstance(states.get("deployed"), str) or states["deployed"] not in {"complete", "not_required"}:
        errors.append(f"{label} has incomplete deployment state")
    if "recovery" in required:
        if states.get("rehearsed") != "complete":
            errors.append(f"{label} has incomplete recovery-rehearsal state")
    elif not isinstance(states.get("rehearsed"), str) or states["rehearsed"] not in {"complete", "not_required"}:
        errors.append(f"{label} has incomplete recovery-rehearsal state")
    return errors


def validate_release(matrix: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    candidate = matrix.get("candidate_id")
    if not isinstance(candidate, str) or not candidate.strip():
        errors.append("release candidate ID is not declared")
    scope = matrix.get("release_scope", {})
    selected = {
        "capabilities": set(scope.get("capabilities", [])),
        "stories": set(scope.get("stories", [])),
        "profiles": set(scope.get("profiles", [])),
    }
    if not any(selected.values()):
        errors.append("release scope is empty")
        errors.append(
            f"full confirmed scope remains incomplete: {len(matrix.get('capabilities', []))} capabilities, "
            f"{len(matrix.get('stories', []))} stories, and {len(matrix.get('profiles', []))} profiles are inventoried"
        )
    release_head = matrix.get("release_head")
    if selected["capabilities"] or selected["stories"] or selected["profiles"]:
        if not isinstance(release_head, str) or not re.fullmatch(r"[0-9a-f]{40}", release_head):
            errors.append("release head must be an exact 40-character Git revision")
        else:
            current_head = subprocess.run(
                ["git", "rev-parse", "HEAD"], cwd=ROOT, capture_output=True, text=True, check=False
            )
            if current_head.returncode != 0 or current_head.stdout.strip() != release_head:
                errors.append("release head does not match the checked-out source revision")
            status = subprocess.run(
                ["git", "status", "--porcelain", "--untracked-files=all"], cwd=ROOT,
                capture_output=True, text=True, check=False,
            )
            if status.returncode != 0 or status.stdout.strip():
                errors.append("release checkout has uncommitted or untracked files")
    for kind, rows in (("capability", matrix.get("capabilities", [])), ("story", matrix.get("stories", [])), ("profile", matrix.get("profiles", []))):
        scope_key = {"capability": "capabilities", "story": "stories", "profile": "profiles"}[kind]
        chosen = selected.get(scope_key, set())
        for row in rows:
            if not isinstance(row, dict):
                continue
            item_id = row.get("id", "<unknown>")
            label = f"{kind} {item_id}"
            claim = row.get("claim")
            if claim == "unsupported":
                errors.append(f"{label} is unsupported and cannot be advertised")
            elif claim == "supported" and item_id not in chosen:
                errors.append(f"{label} is marked supported but omitted from release scope")
            elif item_id in chosen and claim != "supported":
                errors.append(f"{label} is selected without a supported claim")
            errors.extend(validate_evidence(
                row,
                label,
                release_head if item_id in chosen and isinstance(release_head, str) else None,
                selected["profiles"] if item_id in chosen else None,
            ))
            if item_id in chosen:
                errors.extend(validate_release_states(row, label))
                missing = set(row.get("required_evidence", [])) - {record.get("class") for record in row.get("evidence", []) if isinstance(record, dict)}
                if missing:
                    errors.append(f"{label} is missing mandatory evidence: {', '.join(sorted(missing))}")

    cloud_selected = any(
        item_id in selected["capabilities"] and "cloud" in row.get("required_evidence", [])
        for item_id, row in ((row.get("id"), row) for row in matrix.get("capabilities", []) if isinstance(row, dict))
    ) or any(
        item_id in selected["stories"] and "cloud" in row.get("required_evidence", [])
        for item_id, row in ((row.get("id"), row) for row in matrix.get("stories", []) if isinstance(row, dict))
    )
    if cloud_selected and not selected["profiles"]:
        errors.append("cloud capability selected without a separately selected deployment profile")
    if cloud_selected and not selected["profiles"] & REQUIRED_AWS_PROFILES:
        errors.append("cloud capability selected without aws_managed or aws_vm profile qualification")
    selected_story_ids = selected["stories"]
    for capability in matrix.get("capabilities", []):
        if not isinstance(capability, dict) or capability.get("id") not in selected["capabilities"]:
            continue
        missing_stories = set(capability.get("story_ids", [])) - selected_story_ids
        if missing_stories:
            errors.append(f"capability {capability['id']} omits linked stories from release scope")
    covered_by_selected_capabilities = {
        story_id
        for row in matrix.get("capabilities", [])
        if isinstance(row, dict) and row.get("id") in selected["capabilities"]
        for story_id in row.get("story_ids", [])
    }
    if selected_story_ids - covered_by_selected_capabilities:
        errors.append("release scope contains stories without their advertised capability")
    profiles = {row.get("id"): row for row in matrix.get("profiles", []) if isinstance(row, dict)}
    for profile_id in selected["profiles"]:
        row = profiles.get(profile_id)
        if row is None:
            errors.append(f"selected deployment profile {profile_id} is missing")
            continue
        if row.get("claim") != "supported":
            errors.append(f"deployment profile {profile_id} is selected without a supported claim")
        errors.extend(validate_release_states(row, f"deployment profile {profile_id}"))
        errors.extend(validate_evidence(row, f"deployment profile {profile_id}", release_head if isinstance(release_head, str) else None, {profile_id}))
        missing = set(row.get("required_evidence", [])) - {record.get("class") for record in row.get("evidence", []) if isinstance(record, dict)}
        if missing:
            errors.append(f"deployment profile {profile_id} is missing mandatory evidence: {', '.join(sorted(missing))}")
    return errors


def self_test(matrix: dict[str, Any], inventory: dict[str, Any]) -> None:
    synthetic = json.loads(json.dumps(matrix))
    errors = validate_structure(synthetic, inventory)
    if errors:
        raise MatrixError("current matrix is structurally invalid: " + "; ".join(errors[:5]))
    source = MATRIX_PATH.read_text(encoding="utf-8")
    start = source.index(START) + len(START)
    end = source.index(END)
    def with_matrix(data: dict[str, Any]) -> str:
        return source[:start] + "\n```json\n" + json.dumps(data, indent=2) + "\n```\n" + source[end:]

    def run_structure_fixture(path: Path, expected_status: int) -> str:
        result = subprocess.run(
            [sys.executable, str(Path(__file__).resolve()), "--historical-product", "--input", str(path), "--structure-only"],
            cwd=ROOT,
            capture_output=True,
            text=True,
            check=False,
        )
        if result.returncode != expected_status:
            raise MatrixError(f"synthetic parser fixture returned {result.returncode}, expected {expected_status}")
        return result.stdout + result.stderr

    with tempfile.TemporaryDirectory(prefix="amos-release-matrix-", dir=ROOT) as temp_dir:
        fixture_dir = Path(temp_dir)
        valid_path = fixture_dir / "valid.md"
        missing_path = fixture_dir / "missing-capability.md"
        valid_path.write_text(with_matrix(synthetic), encoding="utf-8")
        run_structure_fixture(valid_path, 0)
        broken = json.loads(json.dumps(synthetic))
        broken["capabilities"].pop()
        missing_path.write_text(with_matrix(broken), encoding="utf-8")
        negative_output = run_structure_fixture(missing_path, 2)
        if "missing capability" not in negative_output:
            raise MatrixError("negative CLI fixture did not report the omitted capability")
        valid_path.write_text(with_matrix(synthetic), encoding="utf-8")
        run_structure_fixture(valid_path, 0)

    for mutate, expected in (
        (lambda data: data["capabilities"].append(json.loads(json.dumps(data["capabilities"][0]))), "duplicate capability"),
        (lambda data: data["capabilities"].append({**json.loads(json.dumps(data["capabilities"][0])), "id": "R99"}), "unknown capability"),
    ):
        broken = json.loads(json.dumps(synthetic))
        mutate(broken)
        if not any(expected in error for error in validate_structure(broken, inventory)):
            raise MatrixError(f"negative fixture did not detect a {expected}")

    advertised = json.loads(json.dumps(synthetic))
    cap = advertised["capabilities"][0]
    cap["claim"] = "unsupported"
    advertised["release_scope"]["capabilities"] = [cap["id"]]
    if not any("unsupported" in error or "not supported" in error for error in validate_release(advertised)):
        raise MatrixError("negative fixture did not block an unsupported advertised capability")

    live_missing = json.loads(json.dumps(synthetic))
    profile = next(row for row in live_missing["profiles"] if row["id"] == "aws_managed")
    profile["claim"] = "supported"
    profile["states"] = {field: "complete" for field in STATE_FIELDS}
    live_missing["candidate_id"] = "synthetic-parser-only"
    live_missing["release_head"] = "a" * 40
    live_missing["release_scope"]["profiles"] = ["aws_managed"]
    live_errors = validate_release(live_missing)
    if not any("cloud evidence must come from a live" in error or "missing mandatory evidence" in error for error in live_errors):
        raise MatrixError("negative fixture did not block missing live-profile evidence")

    fake_cloud = json.loads(json.dumps(live_missing["profiles"][0]))
    evidence_path = ROOT / "docs/evidence/inflight-integration-20261001.md"
    review_path = ROOT / "docs/evidence/luna-takeover-20261002.md"
    fake_cloud["evidence"] = [{
        "class": "cloud",
        "path": evidence_path.relative_to(ROOT).as_posix(),
        "sha256": hashlib.sha256(evidence_path.read_bytes()).hexdigest(),
        "review_path": review_path.relative_to(ROOT).as_posix(),
        "review_sha256": hashlib.sha256(review_path.read_bytes()).hexdigest(),
        "artifact_revision": "a" * 40,
        "reviewed_head": "a" * 40,
        "review_status": "independent_pass",
        "execution": "local",
        "profile_id": "aws_managed",
    }]
    if not any("must come from a live" in error for error in validate_evidence(fake_cloud, "synthetic AWS profile", "a" * 40, {"aws_managed"})):
        raise MatrixError("negative fixture did not reject local-only cloud evidence")

    unreviewed = json.loads(json.dumps(synthetic))
    story = next(row for row in unreviewed["stories"] if row["required_evidence"])
    evidence_path = ROOT / "docs/evidence/inflight-integration-20261001.md"
    review_path = ROOT / "docs/evidence/luna-takeover-20261002.md"
    story["evidence"] = [{
        "class": story["required_evidence"][0],
        "path": evidence_path.relative_to(ROOT).as_posix(),
        "sha256": hashlib.sha256(evidence_path.read_bytes()).hexdigest(),
        "review_path": review_path.relative_to(ROOT).as_posix(),
        "review_sha256": hashlib.sha256(review_path.read_bytes()).hexdigest(),
        "artifact_revision": "a" * 40,
        "reviewed_head": "a" * 40,
        "review_status": "pending",
        "execution": "local",
    }]
    if not any("lacks independent passing review" in error for error in validate_evidence(story, "synthetic story", None)):
        raise MatrixError("negative fixture did not reject unreviewed evidence")
    if validate_structure(synthetic, inventory):
        raise MatrixError("restored synthetic parser fixture did not pass structure validation")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--historical-product", action="store_true", help="explicit retired-baseline parser checks only; never release qualification")
    parser.add_argument("--input", type=Path, default=MATRIX_PATH, help=argparse.SUPPRESS)
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--strict", action="store_const", const="strict", dest="mode", help="report current release qualification blockers (the default)")
    modes.add_argument("--structure-only", action="store_const", const="structure", dest="mode", help=argparse.SUPPRESS)
    parser.set_defaults(mode="strict")
    parser.add_argument("--self-test", action="store_true", help="run synthetic parser and rejection checks only")
    args = parser.parse_args()
    try:
        if not args.historical_product:
            spec = importlib.util.spec_from_file_location(
                "amos_current_plan", Path(__file__).with_name("check-plan.py"))
            checker = importlib.util.module_from_spec(spec)
            sys.dont_write_bytecode = True
            spec.loader.exec_module(checker)
            current = checker.load_current()
            print(
                f"BLOCKED: current source contains {len(current[1])} tasks; current release "
                "coverage and receipt qualification are not implemented. The historical "
                "matrix cannot qualify this plan, including in structure-only/self-test mode.",
                file=sys.stderr,
            )
            return 1
        if args.mode == "strict" and not args.self_test:
            print("BLOCKED: historical baseline cannot qualify a current release; "
                  "only explicit --structure-only or --self-test checks are supported.", file=sys.stderr)
            return 1
        matrix = read_matrix(args.input)
        inventory = expected_inventory()
        structural_errors = validate_structure(matrix, inventory)
        if args.self_test:
            self_test(matrix, inventory)
            print("PASS: historical-only synthetic parser and rejection checks; no current plan or release evidence was qualified")
            return 0
        if structural_errors:
            for error in structural_errors:
                print(f"ERROR: {error}", file=sys.stderr)
            return 2
        if args.mode == "structure":
            print("PASS: historical-only matrix structure; no current plan or release evidence was qualified")
            return 0
        raise MatrixError("unsupported historical mode; no release qualified")
    except (OSError, ValueError, MatrixError, TypeError, KeyError, RecursionError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
