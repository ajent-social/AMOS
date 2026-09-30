#!/usr/bin/env python3
"""Validate AMOS schemas and their accepted/rejected contract fixtures."""
from __future__ import annotations

import json
import sys
from importlib.metadata import version
from pathlib import Path

from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "tests" / "contracts"


def read_json(path: Path):
    with path.open(encoding="utf-8") as stream:
        return json.load(stream, object_pairs_hook=no_duplicate_keys)


def no_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON object key: {key}")
        result[key] = value
    return result


def main() -> int:
    dependency_version = version("jsonschema")
    if dependency_version != "4.26.0":
        raise RuntimeError(f"jsonschema 4.26.0 is required by scripts/requirements-contracts.txt, found {dependency_version}")
    schemas = {
        "policy": read_json(ROOT / "api" / "policy.schema.json"),
        "config": read_json(ROOT / "config" / "schema.json"),
    }
    validators = {}
    for name, schema in schemas.items():
        Draft202012Validator.check_schema(schema)
        validators[name] = Draft202012Validator(schema, format_checker=Draft202012Validator.FORMAT_CHECKER)

    expectations = {
        "policy-allowed.json": ("policy", True, None),
        "policy-read.json": ("policy", True, None),
        "policy-missing-permission.json": ("policy", False, "permissions"),
        "policy-unknown-field.json": ("policy", False, "mystery"),
        "config-development.json": ("config", True, None),
        "config-production.json": ("config", True, None),
        "config-literal-secret.json": ("config", False, "/database/urlRef"),
        "config-unknown-field.json": ("config", False, "debugSecrets"),
        "config-missing-credentials.json": ("config", False, "credentialRef"),
        "config-missing-proxy-mode.json": ("config", False, "proxyMode"),
        "config-production-disabled.json": ("config", False, "state"),
        "config-unsupported-profile.json": ("config", False, "cloudflare"),
    }
    if set(expectations) != {path.name for path in FIXTURES.glob("*.json")}:
        raise ValueError("contract fixture inventory differs from the checker manifest")

    errors = []
    for filename, (schema_name, expected_valid, expected_diagnostic) in expectations.items():
        instance = read_json(FIXTURES / filename)
        found = sorted(validators[schema_name].iter_errors(instance), key=lambda error: list(map(str, error.path)))
        actual_valid = not found
        details = "; ".join(f"/{'/'.join(map(str, error.path))}: {error.message}" for error in found)
        if actual_valid != expected_valid or (expected_diagnostic and expected_diagnostic not in details):
            errors.append(f"{filename}: expected valid={expected_valid}, got valid={actual_valid}; {details or 'no validation errors'}")
        else:
            print(f"PASS {filename}: {'accepted' if actual_valid else 'rejected'} as expected")

    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"Validated {len(schemas)} Draft 2020-12 schemas and {len(expectations)} contract fixtures with jsonschema {dependency_version}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
