# AMOS release capability matrix

This matrix tracks the complete confirmed requirement and user-story inventories. Its capability rows come from `docs/planning/requirements.md`; story rows come from `docs/usecases-manifest.json` and must match all task associations in `docs/planning/plan-data.json`. Every linked public task contract is listed so contract coverage is checked across the selected scope instead of inferred from accepted-task counts.

Every requirement and story has separate `implemented`, `tested`, `qualified`, `deployed`, and `rehearsed` states. Current requirement/story implementation is incomplete, tests are not release-reviewed, and qualification/deployment/recovery rehearsal are not established. Existing component acceptance notes in `docs/planning/execution-state.json` are deliberately not used as release credentials. No provider, API, browser, client, AWS, DNS, deployment, or recovery evidence is recorded here.

The two confirmed AWS shapes remain separate target profiles: `aws_managed` and `aws_vm`. Both profiles must remain present in this inventory and are currently unqualified. `cloudflare_routing` separately records the public DNS and same-domain routing gate. Each installation selects one AWS profile; the matrix does not qualify either profile or authorize infrastructure work.

Evidence classes are derived from linked public task contracts and story interfaces. Every selected row requires all listed evidence classes. Each evidence record must hash a public `docs/evidence/` artifact and a separate review artifact, bind evidence and review to the same exact Git head, and declare its execution tier. Provider and cloud evidence must be live; fixtures and schema checks cannot satisfy those gates. Cloud records name the exact deployment profile. A narrative `ACCEPTED` label alone cannot satisfy these checks.

The strict check intentionally fails today: no release candidate or scope is declared, the confirmed product capabilities and stories remain incomplete, and deployment profiles are unqualified. Run `python3 scripts/check-release-evidence.py --self-test` for synthetic parser and rejection checks only; it makes no release claim. Run `python3 scripts/check-release-evidence.py --strict` (the default mode) for promotion. Exit 0 means the declared scope has the required evidence, exit 1 means the matrix is structurally valid but release blockers remain, and exit 2 means the matrix or source inventory is invalid. The current repository is expected to exit 1; exit 2 is a checker/data failure.

## Evidence record format

Each entry's `evidence` array accepts only records with `class`, `path`, `sha256`, `review_path`, `review_sha256`, `artifact_revision`, `reviewed_head`, `review_status`, and `execution`; `profile_id` is required for cloud evidence. Both paths must be distinct public artifacts under `docs/evidence/`. The hashes must match. `review_status` must be `independent_pass`, and `reviewed_head` must equal the 40-character `artifact_revision` and the release head for selected entries. Provider/cloud evidence must say `execution: live`; cloud evidence must name one selected profile.

## Machine-readable matrix

<!-- release-matrix-data:start -->
```json
{
  "schema_version": 1,
  "candidate_id": null,
  "release_head": null,
  "release_scope": {
    "capabilities": [],
    "stories": [],
    "profiles": []
  },
  "scope_sources": [
    "docs/planning/requirements.md",
    "docs/usecases-manifest.json",
    "docs/planning/plan-data.json",
    "docs/planning/contracts.md",
    "docs/tasks/<task-id>.md"
  ],
  "capabilities": [
    {
      "id": "R01",
      "name": "Complete new paid SaaS foundation",
      "planned_refs": "E1-E6; T2.4-T2.7, T16.2-T16.4",
      "task_ids": [
        "T1.1",
        "T1.10",
        "T1.11",
        "T1.12",
        "T1.13",
        "T1.2",
        "T1.3",
        "T1.4",
        "T1.5",
        "T1.6",
        "T1.7",
        "T1.8",
        "T1.9",
        "T16.2",
        "T16.3",
        "T16.4",
        "T2.1",
        "T2.2",
        "T2.3",
        "T2.4",
        "T2.5",
        "T2.6",
        "T2.7",
        "T2.8",
        "T2.9",
        "T3.1",
        "T3.10",
        "T3.11",
        "T3.12",
        "T3.13",
        "T3.14",
        "T3.15",
        "T3.16",
        "T3.17",
        "T3.18",
        "T3.19",
        "T3.2",
        "T3.20",
        "T3.21",
        "T3.22",
        "T3.23",
        "T3.3",
        "T3.4",
        "T3.5",
        "T3.6",
        "T3.7",
        "T3.8",
        "T3.9",
        "T4.1",
        "T4.10",
        "T4.11",
        "T4.12",
        "T4.13",
        "T4.14",
        "T4.2",
        "T4.3",
        "T4.4",
        "T4.5",
        "T4.6",
        "T4.7",
        "T4.8",
        "T4.9",
        "T5.1",
        "T5.10",
        "T5.11",
        "T5.12",
        "T5.13",
        "T5.14",
        "T5.15",
        "T5.16",
        "T5.17",
        "T5.18",
        "T5.19",
        "T5.2",
        "T5.20",
        "T5.21",
        "T5.22",
        "T5.3",
        "T5.4",
        "T5.5",
        "T5.6",
        "T5.7",
        "T5.8",
        "T5.9",
        "T6.1",
        "T6.10",
        "T6.11",
        "T6.12",
        "T6.13",
        "T6.14",
        "T6.15",
        "T6.16",
        "T6.17",
        "T6.18",
        "T6.2",
        "T6.3",
        "T6.4",
        "T6.5",
        "T6.6",
        "T6.7",
        "T6.8",
        "T6.9"
      ],
      "contract_refs": [
        "docs/tasks/T1.1.md",
        "docs/tasks/T1.10.md",
        "docs/tasks/T1.11.md",
        "docs/tasks/T1.12.md",
        "docs/tasks/T1.13.md",
        "docs/tasks/T1.2.md",
        "docs/tasks/T1.3.md",
        "docs/tasks/T1.4.md",
        "docs/tasks/T1.5.md",
        "docs/tasks/T1.6.md",
        "docs/tasks/T1.7.md",
        "docs/tasks/T1.8.md",
        "docs/tasks/T1.9.md",
        "docs/tasks/T16.2.md",
        "docs/tasks/T16.3.md",
        "docs/tasks/T16.4.md",
        "docs/tasks/T2.1.md",
        "docs/tasks/T2.2.md",
        "docs/tasks/T2.3.md",
        "docs/tasks/T2.4.md",
        "docs/tasks/T2.5.md",
        "docs/tasks/T2.6.md",
        "docs/tasks/T2.7.md",
        "docs/tasks/T2.8.md",
        "docs/tasks/T2.9.md",
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.11.md",
        "docs/tasks/T3.12.md",
        "docs/tasks/T3.13.md",
        "docs/tasks/T3.14.md",
        "docs/tasks/T3.15.md",
        "docs/tasks/T3.16.md",
        "docs/tasks/T3.17.md",
        "docs/tasks/T3.18.md",
        "docs/tasks/T3.19.md",
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.20.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.22.md",
        "docs/tasks/T3.23.md",
        "docs/tasks/T3.3.md",
        "docs/tasks/T3.4.md",
        "docs/tasks/T3.5.md",
        "docs/tasks/T3.6.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T3.8.md",
        "docs/tasks/T3.9.md",
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.10.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T4.12.md",
        "docs/tasks/T4.13.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.3.md",
        "docs/tasks/T4.4.md",
        "docs/tasks/T4.5.md",
        "docs/tasks/T4.6.md",
        "docs/tasks/T4.7.md",
        "docs/tasks/T4.8.md",
        "docs/tasks/T4.9.md",
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.10.md",
        "docs/tasks/T5.11.md",
        "docs/tasks/T5.12.md",
        "docs/tasks/T5.13.md",
        "docs/tasks/T5.14.md",
        "docs/tasks/T5.15.md",
        "docs/tasks/T5.16.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T5.18.md",
        "docs/tasks/T5.19.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.20.md",
        "docs/tasks/T5.21.md",
        "docs/tasks/T5.22.md",
        "docs/tasks/T5.3.md",
        "docs/tasks/T5.4.md",
        "docs/tasks/T5.5.md",
        "docs/tasks/T5.6.md",
        "docs/tasks/T5.7.md",
        "docs/tasks/T5.8.md",
        "docs/tasks/T5.9.md",
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.10.md",
        "docs/tasks/T6.11.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.13.md",
        "docs/tasks/T6.14.md",
        "docs/tasks/T6.15.md",
        "docs/tasks/T6.16.md",
        "docs/tasks/T6.17.md",
        "docs/tasks/T6.18.md",
        "docs/tasks/T6.2.md",
        "docs/tasks/T6.3.md",
        "docs/tasks/T6.4.md",
        "docs/tasks/T6.5.md",
        "docs/tasks/T6.6.md",
        "docs/tasks/T6.7.md",
        "docs/tasks/T6.8.md",
        "docs/tasks/T6.9.md"
      ],
      "story_ids": [
        "UC-001",
        "UC-002",
        "UC-003",
        "UC-004",
        "UC-005",
        "UC-006",
        "UC-007",
        "UC-008",
        "UC-009",
        "UC-010",
        "UC-020",
        "UC-021",
        "UC-022",
        "UC-023",
        "UC-024",
        "UC-025",
        "UC-026",
        "UC-027",
        "UC-028",
        "UC-029",
        "UC-030",
        "UC-031",
        "UC-032",
        "UC-033",
        "UC-034",
        "UC-035",
        "UC-036",
        "UC-037",
        "UC-038",
        "UC-039",
        "UC-040",
        "UC-041",
        "UC-042",
        "UC-043",
        "UC-044",
        "UC-045",
        "UC-046",
        "UC-047",
        "UC-048",
        "UC-049",
        "UC-120",
        "UC-121",
        "UC-123"
      ],
      "interfaces": [
        "api",
        "cli",
        "developer",
        "docs",
        "mcp",
        "public-contract",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R02",
      "name": "Owner-hosted installations and accounts",
      "planned_refs": "T7.1, T8.2, T8.23, T16.4",
      "task_ids": [
        "T16.4",
        "T7.1",
        "T8.2",
        "T8.23"
      ],
      "contract_refs": [
        "docs/tasks/T16.4.md",
        "docs/tasks/T7.1.md",
        "docs/tasks/T8.2.md",
        "docs/tasks/T8.23.md"
      ],
      "story_ids": [
        "UC-050",
        "UC-053",
        "UC-055",
        "UC-058",
        "UC-059",
        "UC-062",
        "UC-064",
        "UC-121"
      ],
      "interfaces": [
        "ci",
        "cli",
        "config",
        "web"
      ],
      "required_evidence": [
        "behavior",
        "browser",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R03",
      "name": "`amos init` prepares app, workflows and IaC",
      "planned_refs": "T7.1-T7.14, T9.1-T9.3, T9.8",
      "task_ids": [
        "T7.1",
        "T7.10",
        "T7.11",
        "T7.12",
        "T7.13",
        "T7.14",
        "T7.2",
        "T7.3",
        "T7.4",
        "T7.5",
        "T7.6",
        "T7.7",
        "T7.8",
        "T7.9",
        "T9.1",
        "T9.2",
        "T9.3",
        "T9.8"
      ],
      "contract_refs": [
        "docs/tasks/T7.1.md",
        "docs/tasks/T7.10.md",
        "docs/tasks/T7.11.md",
        "docs/tasks/T7.12.md",
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.14.md",
        "docs/tasks/T7.2.md",
        "docs/tasks/T7.3.md",
        "docs/tasks/T7.4.md",
        "docs/tasks/T7.5.md",
        "docs/tasks/T7.6.md",
        "docs/tasks/T7.7.md",
        "docs/tasks/T7.8.md",
        "docs/tasks/T7.9.md",
        "docs/tasks/T9.1.md",
        "docs/tasks/T9.2.md",
        "docs/tasks/T9.3.md",
        "docs/tasks/T9.8.md"
      ],
      "story_ids": [
        "UC-050",
        "UC-051",
        "UC-052",
        "UC-053",
        "UC-054",
        "UC-055",
        "UC-056",
        "UC-057",
        "UC-062",
        "UC-063",
        "UC-064",
        "UC-065",
        "UC-066",
        "UC-068",
        "UC-070",
        "UC-077"
      ],
      "interfaces": [
        "artifact",
        "ci",
        "cli",
        "config",
        "http",
        "iac"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R04",
      "name": "`amos deploy` makes the reviewed app live",
      "planned_refs": "T7.9-T7.11, T8.15/T8.22, T9.14, T16.4",
      "task_ids": [
        "T16.4",
        "T7.10",
        "T7.11",
        "T7.9",
        "T8.15",
        "T8.22",
        "T9.14"
      ],
      "contract_refs": [
        "docs/tasks/T16.4.md",
        "docs/tasks/T7.10.md",
        "docs/tasks/T7.11.md",
        "docs/tasks/T7.9.md",
        "docs/tasks/T8.15.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T9.14.md"
      ],
      "story_ids": [
        "UC-055",
        "UC-056",
        "UC-059",
        "UC-060",
        "UC-062",
        "UC-064",
        "UC-065",
        "UC-066",
        "UC-121"
      ],
      "interfaces": [
        "artifact",
        "ci",
        "cli",
        "config",
        "iac",
        "web"
      ],
      "required_evidence": [
        "behavior",
        "browser",
        "client",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R05",
      "name": "One public domain; shared versus business routes",
      "planned_refs": "T2.2, T12.1, T12.3-T12.9, T16.8",
      "task_ids": [
        "T12.1",
        "T12.3",
        "T12.4",
        "T12.5",
        "T12.6",
        "T12.7",
        "T12.8",
        "T12.9",
        "T16.8",
        "T2.2"
      ],
      "contract_refs": [
        "docs/tasks/T12.1.md",
        "docs/tasks/T12.3.md",
        "docs/tasks/T12.4.md",
        "docs/tasks/T12.5.md",
        "docs/tasks/T12.6.md",
        "docs/tasks/T12.7.md",
        "docs/tasks/T12.8.md",
        "docs/tasks/T12.9.md",
        "docs/tasks/T16.8.md",
        "docs/tasks/T2.2.md"
      ],
      "story_ids": [
        "UC-002",
        "UC-004",
        "UC-081",
        "UC-088",
        "UC-089",
        "UC-090",
        "UC-091",
        "UC-093",
        "UC-095",
        "UC-112",
        "UC-113",
        "UC-124"
      ],
      "interfaces": [
        "Browser",
        "CLI",
        "Document",
        "Go",
        "HTTP",
        "MCP",
        "Network",
        "OpenAPI",
        "REST",
        "cli",
        "docs",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R06",
      "name": "Integrated default and arbitrary-stack private service",
      "planned_refs": "T2.3-T2.6, T7.8, T12.4-T12.11",
      "task_ids": [
        "T12.10",
        "T12.11",
        "T12.4",
        "T12.5",
        "T12.6",
        "T12.7",
        "T12.8",
        "T12.9",
        "T2.3",
        "T2.4",
        "T2.5",
        "T2.6",
        "T7.8"
      ],
      "contract_refs": [
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.11.md",
        "docs/tasks/T12.4.md",
        "docs/tasks/T12.5.md",
        "docs/tasks/T12.6.md",
        "docs/tasks/T12.7.md",
        "docs/tasks/T12.8.md",
        "docs/tasks/T12.9.md",
        "docs/tasks/T2.3.md",
        "docs/tasks/T2.4.md",
        "docs/tasks/T2.5.md",
        "docs/tasks/T2.6.md",
        "docs/tasks/T7.8.md"
      ],
      "story_ids": [
        "UC-003",
        "UC-007",
        "UC-010",
        "UC-054",
        "UC-081",
        "UC-082",
        "UC-084",
        "UC-086",
        "UC-090",
        "UC-091",
        "UC-092",
        "UC-093",
        "UC-095",
        "UC-112",
        "UC-113",
        "UC-120",
        "UC-123"
      ],
      "interfaces": [
        "Browser",
        "Document",
        "HTTP",
        "MCP",
        "Network",
        "OAuth",
        "OpenAPI",
        "REST",
        "UI",
        "api",
        "cli",
        "config",
        "docs",
        "http",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R07",
      "name": "Go SSR, HTMX, plain JavaScript/CSS default",
      "planned_refs": "T6.1-T6.5, T2.6, T6.16",
      "task_ids": [
        "T2.6",
        "T6.1",
        "T6.16",
        "T6.2",
        "T6.3",
        "T6.4",
        "T6.5"
      ],
      "contract_refs": [
        "docs/tasks/T2.6.md",
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.16.md",
        "docs/tasks/T6.2.md",
        "docs/tasks/T6.3.md",
        "docs/tasks/T6.4.md",
        "docs/tasks/T6.5.md"
      ],
      "story_ids": [
        "UC-007",
        "UC-020",
        "UC-021",
        "UC-022",
        "UC-033",
        "UC-038",
        "UC-040",
        "UC-041",
        "UC-045",
        "UC-046",
        "UC-047",
        "UC-048",
        "UC-049",
        "UC-123"
      ],
      "interfaces": [
        "api",
        "developer",
        "docs",
        "mcp",
        "public-contract",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R08",
      "name": "AWS first with Cloudflare required",
      "planned_refs": "E8; T8.8/T8.21, T16.4",
      "task_ids": [
        "T16.4",
        "T8.1",
        "T8.10",
        "T8.11",
        "T8.12",
        "T8.13",
        "T8.14",
        "T8.15",
        "T8.16",
        "T8.17",
        "T8.18",
        "T8.19",
        "T8.2",
        "T8.20",
        "T8.21",
        "T8.22",
        "T8.23",
        "T8.24",
        "T8.25",
        "T8.26",
        "T8.27",
        "T8.28",
        "T8.3",
        "T8.4",
        "T8.5",
        "T8.6",
        "T8.7",
        "T8.8",
        "T8.9"
      ],
      "contract_refs": [
        "docs/tasks/T16.4.md",
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.10.md",
        "docs/tasks/T8.11.md",
        "docs/tasks/T8.12.md",
        "docs/tasks/T8.13.md",
        "docs/tasks/T8.14.md",
        "docs/tasks/T8.15.md",
        "docs/tasks/T8.16.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.18.md",
        "docs/tasks/T8.19.md",
        "docs/tasks/T8.2.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.21.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.23.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T8.25.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T8.27.md",
        "docs/tasks/T8.28.md",
        "docs/tasks/T8.3.md",
        "docs/tasks/T8.4.md",
        "docs/tasks/T8.5.md",
        "docs/tasks/T8.6.md",
        "docs/tasks/T8.7.md",
        "docs/tasks/T8.8.md",
        "docs/tasks/T8.9.md"
      ],
      "story_ids": [
        "UC-050",
        "UC-051",
        "UC-054",
        "UC-055",
        "UC-056",
        "UC-058",
        "UC-059",
        "UC-060",
        "UC-061",
        "UC-062",
        "UC-063",
        "UC-064",
        "UC-066",
        "UC-072",
        "UC-073",
        "UC-074",
        "UC-075",
        "UC-076",
        "UC-077",
        "UC-078",
        "UC-121"
      ],
      "interfaces": [
        "artifact",
        "ci",
        "cli",
        "config",
        "dns",
        "document",
        "http",
        "iac",
        "telemetry",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R09",
      "name": "Both managed and small-VM profiles selected per install",
      "planned_refs": "T8.1, T8.4-T8.7, T8.19-T8.26",
      "task_ids": [
        "T8.1",
        "T8.19",
        "T8.20",
        "T8.21",
        "T8.22",
        "T8.23",
        "T8.24",
        "T8.25",
        "T8.26",
        "T8.4",
        "T8.5",
        "T8.6",
        "T8.7"
      ],
      "contract_refs": [
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.19.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.21.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.23.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T8.25.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T8.4.md",
        "docs/tasks/T8.5.md",
        "docs/tasks/T8.6.md",
        "docs/tasks/T8.7.md"
      ],
      "story_ids": [
        "UC-050",
        "UC-054",
        "UC-055",
        "UC-056",
        "UC-058",
        "UC-059",
        "UC-060",
        "UC-061",
        "UC-062",
        "UC-063",
        "UC-064",
        "UC-066",
        "UC-072",
        "UC-075",
        "UC-076",
        "UC-077",
        "UC-078"
      ],
      "interfaces": [
        "artifact",
        "ci",
        "cli",
        "config",
        "dns",
        "document",
        "http",
        "iac",
        "telemetry"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R10",
      "name": "Complete web app and APIs/MCP first; other user clients later",
      "planned_refs": "E6, E11-E12, T16.8; explicit non-goals in master plan",
      "task_ids": [
        "T11.1",
        "T11.10",
        "T11.11",
        "T11.12",
        "T11.13",
        "T11.14",
        "T11.2",
        "T11.3",
        "T11.4",
        "T11.5",
        "T11.6",
        "T11.7",
        "T11.8",
        "T11.9",
        "T12.1",
        "T12.10",
        "T12.11",
        "T12.12",
        "T12.2",
        "T12.3",
        "T12.4",
        "T12.5",
        "T12.6",
        "T12.7",
        "T12.8",
        "T12.9",
        "T16.8",
        "T6.1",
        "T6.10",
        "T6.11",
        "T6.12",
        "T6.13",
        "T6.14",
        "T6.15",
        "T6.16",
        "T6.17",
        "T6.18",
        "T6.2",
        "T6.3",
        "T6.4",
        "T6.5",
        "T6.6",
        "T6.7",
        "T6.8",
        "T6.9"
      ],
      "contract_refs": [
        "docs/tasks/T11.1.md",
        "docs/tasks/T11.10.md",
        "docs/tasks/T11.11.md",
        "docs/tasks/T11.12.md",
        "docs/tasks/T11.13.md",
        "docs/tasks/T11.14.md",
        "docs/tasks/T11.2.md",
        "docs/tasks/T11.3.md",
        "docs/tasks/T11.4.md",
        "docs/tasks/T11.5.md",
        "docs/tasks/T11.6.md",
        "docs/tasks/T11.7.md",
        "docs/tasks/T11.8.md",
        "docs/tasks/T11.9.md",
        "docs/tasks/T12.1.md",
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.11.md",
        "docs/tasks/T12.12.md",
        "docs/tasks/T12.2.md",
        "docs/tasks/T12.3.md",
        "docs/tasks/T12.4.md",
        "docs/tasks/T12.5.md",
        "docs/tasks/T12.6.md",
        "docs/tasks/T12.7.md",
        "docs/tasks/T12.8.md",
        "docs/tasks/T12.9.md",
        "docs/tasks/T16.8.md",
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.10.md",
        "docs/tasks/T6.11.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.13.md",
        "docs/tasks/T6.14.md",
        "docs/tasks/T6.15.md",
        "docs/tasks/T6.16.md",
        "docs/tasks/T6.17.md",
        "docs/tasks/T6.18.md",
        "docs/tasks/T6.2.md",
        "docs/tasks/T6.3.md",
        "docs/tasks/T6.4.md",
        "docs/tasks/T6.5.md",
        "docs/tasks/T6.6.md",
        "docs/tasks/T6.7.md",
        "docs/tasks/T6.8.md",
        "docs/tasks/T6.9.md"
      ],
      "story_ids": [
        "UC-020",
        "UC-021",
        "UC-022",
        "UC-023",
        "UC-024",
        "UC-025",
        "UC-026",
        "UC-027",
        "UC-028",
        "UC-029",
        "UC-030",
        "UC-031",
        "UC-032",
        "UC-033",
        "UC-034",
        "UC-035",
        "UC-036",
        "UC-037",
        "UC-038",
        "UC-039",
        "UC-040",
        "UC-041",
        "UC-042",
        "UC-043",
        "UC-044",
        "UC-045",
        "UC-046",
        "UC-047",
        "UC-048",
        "UC-049",
        "UC-080",
        "UC-081",
        "UC-082",
        "UC-083",
        "UC-084",
        "UC-085",
        "UC-086",
        "UC-087",
        "UC-088",
        "UC-089",
        "UC-090",
        "UC-091",
        "UC-092",
        "UC-093",
        "UC-094",
        "UC-095",
        "UC-112",
        "UC-113",
        "UC-124"
      ],
      "interfaces": [
        "Browser",
        "CLI",
        "Document",
        "Go",
        "HTTP",
        "MCP",
        "Network",
        "OAuth",
        "OpenAPI",
        "REST",
        "UI",
        "api",
        "developer",
        "docs",
        "mcp",
        "public-contract",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R11",
      "name": "Personal accounts and organization membership",
      "planned_refs": "T3.2-T3.5, T4.1-T4.14, T6.4/T6.10",
      "task_ids": [
        "T3.2",
        "T3.3",
        "T3.4",
        "T3.5",
        "T4.1",
        "T4.10",
        "T4.11",
        "T4.12",
        "T4.13",
        "T4.14",
        "T4.2",
        "T4.3",
        "T4.4",
        "T4.5",
        "T4.6",
        "T4.7",
        "T4.8",
        "T4.9",
        "T6.10",
        "T6.4"
      ],
      "contract_refs": [
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.3.md",
        "docs/tasks/T3.4.md",
        "docs/tasks/T3.5.md",
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.10.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T4.12.md",
        "docs/tasks/T4.13.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.3.md",
        "docs/tasks/T4.4.md",
        "docs/tasks/T4.5.md",
        "docs/tasks/T4.6.md",
        "docs/tasks/T4.7.md",
        "docs/tasks/T4.8.md",
        "docs/tasks/T4.9.md",
        "docs/tasks/T6.10.md",
        "docs/tasks/T6.4.md"
      ],
      "story_ids": [
        "UC-020",
        "UC-021",
        "UC-028",
        "UC-029",
        "UC-030",
        "UC-031",
        "UC-032",
        "UC-033",
        "UC-034",
        "UC-035",
        "UC-036",
        "UC-037",
        "UC-038",
        "UC-039",
        "UC-046"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R12",
      "name": "Separate personal and organization subscriptions",
      "planned_refs": "T5.1-T5.4, T5.17, T16.7",
      "task_ids": [
        "T16.7",
        "T5.1",
        "T5.17",
        "T5.2",
        "T5.3",
        "T5.4"
      ],
      "contract_refs": [
        "docs/tasks/T16.7.md",
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.3.md",
        "docs/tasks/T5.4.md"
      ],
      "story_ids": [
        "UC-031",
        "UC-037",
        "UC-040",
        "UC-041",
        "UC-042",
        "UC-043",
        "UC-044",
        "UC-045",
        "UC-120",
        "UC-124"
      ],
      "interfaces": [
        "api",
        "docs",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R13",
      "name": "Flat monthly/yearly, seat and usage pricing",
      "planned_refs": "T5.4, T5.13-T5.20, T6.12, T16.7",
      "task_ids": [
        "T16.7",
        "T5.13",
        "T5.14",
        "T5.15",
        "T5.16",
        "T5.17",
        "T5.18",
        "T5.19",
        "T5.20",
        "T5.4",
        "T6.12"
      ],
      "contract_refs": [
        "docs/tasks/T16.7.md",
        "docs/tasks/T5.13.md",
        "docs/tasks/T5.14.md",
        "docs/tasks/T5.15.md",
        "docs/tasks/T5.16.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T5.18.md",
        "docs/tasks/T5.19.md",
        "docs/tasks/T5.20.md",
        "docs/tasks/T5.4.md",
        "docs/tasks/T6.12.md"
      ],
      "story_ids": [
        "UC-031",
        "UC-035",
        "UC-037",
        "UC-040",
        "UC-041",
        "UC-042",
        "UC-043",
        "UC-044",
        "UC-045",
        "UC-046",
        "UC-120",
        "UC-124"
      ],
      "interfaces": [
        "api",
        "docs",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R14",
      "name": "Stripe first, future-provider adapter",
      "planned_refs": "T5.2/T5.5/T5.7/T5.18; provider gaps explicit",
      "task_ids": [
        "T5.18",
        "T5.2",
        "T5.5",
        "T5.7"
      ],
      "contract_refs": [
        "docs/tasks/T5.18.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.5.md",
        "docs/tasks/T5.7.md"
      ],
      "story_ids": [
        "UC-040",
        "UC-041",
        "UC-042",
        "UC-043",
        "UC-044",
        "UC-045"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R15",
      "name": "Email/password and email magic-link sign-in",
      "planned_refs": "T3.4-T3.9, T6.3/T6.7, T3.21",
      "task_ids": [
        "T3.21",
        "T3.4",
        "T3.5",
        "T3.6",
        "T3.7",
        "T3.8",
        "T3.9",
        "T6.3",
        "T6.7"
      ],
      "contract_refs": [
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.4.md",
        "docs/tasks/T3.5.md",
        "docs/tasks/T3.6.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T3.8.md",
        "docs/tasks/T3.9.md",
        "docs/tasks/T6.3.md",
        "docs/tasks/T6.7.md"
      ],
      "story_ids": [
        "UC-020",
        "UC-021",
        "UC-022",
        "UC-023",
        "UC-024",
        "UC-025",
        "UC-026",
        "UC-027",
        "UC-028",
        "UC-029",
        "UC-030",
        "UC-046"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R16",
      "name": "Google, GitHub and Apple sign-in",
      "planned_refs": "T3.10-T3.13/T3.22, T6.7, T3.21",
      "task_ids": [
        "T3.10",
        "T3.11",
        "T3.12",
        "T3.13",
        "T3.21",
        "T3.22",
        "T6.7"
      ],
      "contract_refs": [
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.11.md",
        "docs/tasks/T3.12.md",
        "docs/tasks/T3.13.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.22.md",
        "docs/tasks/T6.7.md"
      ],
      "story_ids": [
        "UC-023",
        "UC-024",
        "UC-025",
        "UC-026",
        "UC-027",
        "UC-028",
        "UC-029",
        "UC-046"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R17",
      "name": "Passkeys",
      "planned_refs": "T3.14/T3.18, T6.8, T3.21",
      "task_ids": [
        "T3.14",
        "T3.18",
        "T3.21",
        "T6.8"
      ],
      "contract_refs": [
        "docs/tasks/T3.14.md",
        "docs/tasks/T3.18.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T6.8.md"
      ],
      "story_ids": [
        "UC-023",
        "UC-024",
        "UC-025",
        "UC-026",
        "UC-027",
        "UC-028",
        "UC-029",
        "UC-046"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R18",
      "name": "Per-organization OIDC enterprise SSO",
      "planned_refs": "T4.9-T4.10, T6.11, T16.6",
      "task_ids": [
        "T16.6",
        "T4.10",
        "T4.9",
        "T6.11"
      ],
      "contract_refs": [
        "docs/tasks/T16.6.md",
        "docs/tasks/T4.10.md",
        "docs/tasks/T4.9.md",
        "docs/tasks/T6.11.md"
      ],
      "story_ids": [
        "UC-028",
        "UC-039",
        "UC-046",
        "UC-120",
        "UC-124"
      ],
      "interfaces": [
        "api",
        "docs",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R19",
      "name": "MFA with organization enforcement",
      "planned_refs": "T3.15-T3.17, T4.11, T6.9/T6.11",
      "task_ids": [
        "T3.15",
        "T3.16",
        "T3.17",
        "T4.11",
        "T6.11",
        "T6.9"
      ],
      "contract_refs": [
        "docs/tasks/T3.15.md",
        "docs/tasks/T3.16.md",
        "docs/tasks/T3.17.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T6.11.md",
        "docs/tasks/T6.9.md"
      ],
      "story_ids": [
        "UC-028",
        "UC-029",
        "UC-032",
        "UC-037",
        "UC-039",
        "UC-046"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R20",
      "name": "Profiles, invitations, roles and account administration",
      "planned_refs": "E3-E4, T6.6/T6.10/T6.13, T16.6",
      "task_ids": [
        "T16.6",
        "T3.1",
        "T3.10",
        "T3.11",
        "T3.12",
        "T3.13",
        "T3.14",
        "T3.15",
        "T3.16",
        "T3.17",
        "T3.18",
        "T3.19",
        "T3.2",
        "T3.20",
        "T3.21",
        "T3.22",
        "T3.23",
        "T3.3",
        "T3.4",
        "T3.5",
        "T3.6",
        "T3.7",
        "T3.8",
        "T3.9",
        "T4.1",
        "T4.10",
        "T4.11",
        "T4.12",
        "T4.13",
        "T4.14",
        "T4.2",
        "T4.3",
        "T4.4",
        "T4.5",
        "T4.6",
        "T4.7",
        "T4.8",
        "T4.9",
        "T6.10",
        "T6.13",
        "T6.6"
      ],
      "contract_refs": [
        "docs/tasks/T16.6.md",
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.11.md",
        "docs/tasks/T3.12.md",
        "docs/tasks/T3.13.md",
        "docs/tasks/T3.14.md",
        "docs/tasks/T3.15.md",
        "docs/tasks/T3.16.md",
        "docs/tasks/T3.17.md",
        "docs/tasks/T3.18.md",
        "docs/tasks/T3.19.md",
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.20.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.22.md",
        "docs/tasks/T3.23.md",
        "docs/tasks/T3.3.md",
        "docs/tasks/T3.4.md",
        "docs/tasks/T3.5.md",
        "docs/tasks/T3.6.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T3.8.md",
        "docs/tasks/T3.9.md",
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.10.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T4.12.md",
        "docs/tasks/T4.13.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.3.md",
        "docs/tasks/T4.4.md",
        "docs/tasks/T4.5.md",
        "docs/tasks/T4.6.md",
        "docs/tasks/T4.7.md",
        "docs/tasks/T4.8.md",
        "docs/tasks/T4.9.md",
        "docs/tasks/T6.10.md",
        "docs/tasks/T6.13.md",
        "docs/tasks/T6.6.md"
      ],
      "story_ids": [
        "UC-020",
        "UC-021",
        "UC-022",
        "UC-023",
        "UC-024",
        "UC-025",
        "UC-026",
        "UC-027",
        "UC-028",
        "UC-029",
        "UC-030",
        "UC-031",
        "UC-032",
        "UC-033",
        "UC-034",
        "UC-035",
        "UC-036",
        "UC-037",
        "UC-038",
        "UC-039",
        "UC-046",
        "UC-120",
        "UC-124"
      ],
      "interfaces": [
        "api",
        "docs",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R21",
      "name": "Customer-created API keys and MCP OAuth",
      "planned_refs": "T11.5-T11.12, T11.14",
      "task_ids": [
        "T11.10",
        "T11.11",
        "T11.12",
        "T11.14",
        "T11.5",
        "T11.6",
        "T11.7",
        "T11.8",
        "T11.9"
      ],
      "contract_refs": [
        "docs/tasks/T11.10.md",
        "docs/tasks/T11.11.md",
        "docs/tasks/T11.12.md",
        "docs/tasks/T11.14.md",
        "docs/tasks/T11.5.md",
        "docs/tasks/T11.6.md",
        "docs/tasks/T11.7.md",
        "docs/tasks/T11.8.md",
        "docs/tasks/T11.9.md"
      ],
      "story_ids": [
        "UC-081",
        "UC-082",
        "UC-083",
        "UC-084",
        "UC-085",
        "UC-086",
        "UC-087",
        "UC-092",
        "UC-113"
      ],
      "interfaces": [
        "Browser",
        "MCP",
        "OAuth",
        "REST",
        "UI"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R22",
      "name": "Full applicable agent access under application controls",
      "planned_refs": "T2.8, T11.13, T12.10, T15.2, T16.8",
      "task_ids": [
        "T11.13",
        "T12.10",
        "T15.2",
        "T16.8",
        "T2.8"
      ],
      "contract_refs": [
        "docs/tasks/T11.13.md",
        "docs/tasks/T12.10.md",
        "docs/tasks/T15.2.md",
        "docs/tasks/T16.8.md",
        "docs/tasks/T2.8.md"
      ],
      "story_ids": [
        "UC-004",
        "UC-080",
        "UC-081",
        "UC-083",
        "UC-085",
        "UC-086",
        "UC-087",
        "UC-091",
        "UC-092",
        "UC-113",
        "UC-124"
      ],
      "interfaces": [
        "Browser",
        "HTTP",
        "MCP",
        "Network",
        "OAuth",
        "REST",
        "UI",
        "cli",
        "docs"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R23",
      "name": "Independent agents/external runtimes; no mandatory governance/runtime",
      "planned_refs": "ADR 001, T11.1/T11.14, T13.2; no customer approval-inbox epic",
      "task_ids": [
        "T11.1",
        "T11.14",
        "T13.2"
      ],
      "contract_refs": [
        "docs/tasks/T11.1.md",
        "docs/tasks/T11.14.md",
        "docs/tasks/T13.2.md"
      ],
      "story_ids": [
        "UC-080",
        "UC-084",
        "UC-085",
        "UC-087",
        "UC-096",
        "UC-098",
        "UC-103"
      ],
      "interfaces": [
        "CLI",
        "Config",
        "HTTP",
        "MCP",
        "OAuth",
        "REST",
        "Runner",
        "UI"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R24",
      "name": "OpenAPI first; generated Go scaffolding and MCP",
      "planned_refs": "T2.1/T2.3, T11.2-T11.4, T12.2/T12.12",
      "task_ids": [
        "T11.2",
        "T11.3",
        "T11.4",
        "T12.12",
        "T12.2",
        "T2.1",
        "T2.3"
      ],
      "contract_refs": [
        "docs/tasks/T11.2.md",
        "docs/tasks/T11.3.md",
        "docs/tasks/T11.4.md",
        "docs/tasks/T12.12.md",
        "docs/tasks/T12.2.md",
        "docs/tasks/T2.1.md",
        "docs/tasks/T2.3.md"
      ],
      "story_ids": [
        "UC-003",
        "UC-010",
        "UC-080",
        "UC-081",
        "UC-087",
        "UC-088",
        "UC-094",
        "UC-095"
      ],
      "interfaces": [
        "CLI",
        "MCP",
        "OpenAPI",
        "REST",
        "cli"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R25",
      "name": "All shared UI replaceable",
      "planned_refs": "T6.14-T6.17, T12.10-T12.11, T16.9",
      "task_ids": [
        "T12.10",
        "T12.11",
        "T16.9",
        "T6.14",
        "T6.15",
        "T6.16",
        "T6.17"
      ],
      "contract_refs": [
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.11.md",
        "docs/tasks/T16.9.md",
        "docs/tasks/T6.14.md",
        "docs/tasks/T6.15.md",
        "docs/tasks/T6.16.md",
        "docs/tasks/T6.17.md"
      ],
      "story_ids": [
        "UC-046",
        "UC-047",
        "UC-048",
        "UC-049",
        "UC-081",
        "UC-082",
        "UC-084",
        "UC-086",
        "UC-092",
        "UC-113",
        "UC-124"
      ],
      "interfaces": [
        "Browser",
        "MCP",
        "OAuth",
        "REST",
        "UI",
        "api",
        "developer",
        "docs",
        "public-contract",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R26",
      "name": "Hybrid versioned core, customizable UI, managed deployment files",
      "planned_refs": "T9.8-T9.12, T6.17, T12.12",
      "task_ids": [
        "T12.12",
        "T6.17",
        "T9.10",
        "T9.11",
        "T9.12",
        "T9.8",
        "T9.9"
      ],
      "contract_refs": [
        "docs/tasks/T12.12.md",
        "docs/tasks/T6.17.md",
        "docs/tasks/T9.10.md",
        "docs/tasks/T9.11.md",
        "docs/tasks/T9.12.md",
        "docs/tasks/T9.8.md",
        "docs/tasks/T9.9.md"
      ],
      "story_ids": [
        "UC-047",
        "UC-048",
        "UC-050",
        "UC-068",
        "UC-070",
        "UC-071",
        "UC-077",
        "UC-088",
        "UC-094",
        "UC-095"
      ],
      "interfaces": [
        "CLI",
        "MCP",
        "OpenAPI",
        "artifact",
        "cli",
        "config",
        "developer",
        "public-contract"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R27",
      "name": "Automated upgrade PRs",
      "planned_refs": "T9.13/T9.16, T14.11, T16.9",
      "task_ids": [
        "T14.11",
        "T16.9",
        "T9.13",
        "T9.16"
      ],
      "contract_refs": [
        "docs/tasks/T14.11.md",
        "docs/tasks/T16.9.md",
        "docs/tasks/T9.13.md",
        "docs/tasks/T9.16.md"
      ],
      "story_ids": [
        "UC-068",
        "UC-069",
        "UC-070",
        "UC-071",
        "UC-094",
        "UC-109",
        "UC-110",
        "UC-124"
      ],
      "interfaces": [
        "CLI",
        "Process",
        "Repository",
        "artifact",
        "ci",
        "cli",
        "config",
        "docs",
        "github"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R28",
      "name": "Deployment, monitoring, backup and recovery tooling",
      "planned_refs": "E7-E10; T16.4-T16.5",
      "task_ids": [
        "T10.1",
        "T10.10",
        "T10.11",
        "T10.12",
        "T10.13",
        "T10.14",
        "T10.15",
        "T10.16",
        "T10.17",
        "T10.2",
        "T10.3",
        "T10.4",
        "T10.5",
        "T10.6",
        "T10.7",
        "T10.8",
        "T10.9",
        "T16.4",
        "T16.5",
        "T7.1",
        "T7.10",
        "T7.11",
        "T7.12",
        "T7.13",
        "T7.14",
        "T7.15",
        "T7.16",
        "T7.2",
        "T7.3",
        "T7.4",
        "T7.5",
        "T7.6",
        "T7.7",
        "T7.8",
        "T7.9",
        "T8.1",
        "T8.10",
        "T8.11",
        "T8.12",
        "T8.13",
        "T8.14",
        "T8.15",
        "T8.16",
        "T8.17",
        "T8.18",
        "T8.19",
        "T8.2",
        "T8.20",
        "T8.21",
        "T8.22",
        "T8.23",
        "T8.24",
        "T8.25",
        "T8.26",
        "T8.27",
        "T8.28",
        "T8.3",
        "T8.4",
        "T8.5",
        "T8.6",
        "T8.7",
        "T8.8",
        "T8.9",
        "T9.1",
        "T9.10",
        "T9.11",
        "T9.12",
        "T9.13",
        "T9.14",
        "T9.15",
        "T9.16",
        "T9.17",
        "T9.2",
        "T9.3",
        "T9.4",
        "T9.5",
        "T9.6",
        "T9.7",
        "T9.8",
        "T9.9"
      ],
      "contract_refs": [
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.10.md",
        "docs/tasks/T10.11.md",
        "docs/tasks/T10.12.md",
        "docs/tasks/T10.13.md",
        "docs/tasks/T10.14.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T10.16.md",
        "docs/tasks/T10.17.md",
        "docs/tasks/T10.2.md",
        "docs/tasks/T10.3.md",
        "docs/tasks/T10.4.md",
        "docs/tasks/T10.5.md",
        "docs/tasks/T10.6.md",
        "docs/tasks/T10.7.md",
        "docs/tasks/T10.8.md",
        "docs/tasks/T10.9.md",
        "docs/tasks/T16.4.md",
        "docs/tasks/T16.5.md",
        "docs/tasks/T7.1.md",
        "docs/tasks/T7.10.md",
        "docs/tasks/T7.11.md",
        "docs/tasks/T7.12.md",
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.14.md",
        "docs/tasks/T7.15.md",
        "docs/tasks/T7.16.md",
        "docs/tasks/T7.2.md",
        "docs/tasks/T7.3.md",
        "docs/tasks/T7.4.md",
        "docs/tasks/T7.5.md",
        "docs/tasks/T7.6.md",
        "docs/tasks/T7.7.md",
        "docs/tasks/T7.8.md",
        "docs/tasks/T7.9.md",
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.10.md",
        "docs/tasks/T8.11.md",
        "docs/tasks/T8.12.md",
        "docs/tasks/T8.13.md",
        "docs/tasks/T8.14.md",
        "docs/tasks/T8.15.md",
        "docs/tasks/T8.16.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.18.md",
        "docs/tasks/T8.19.md",
        "docs/tasks/T8.2.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.21.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.23.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T8.25.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T8.27.md",
        "docs/tasks/T8.28.md",
        "docs/tasks/T8.3.md",
        "docs/tasks/T8.4.md",
        "docs/tasks/T8.5.md",
        "docs/tasks/T8.6.md",
        "docs/tasks/T8.7.md",
        "docs/tasks/T8.8.md",
        "docs/tasks/T8.9.md",
        "docs/tasks/T9.1.md",
        "docs/tasks/T9.10.md",
        "docs/tasks/T9.11.md",
        "docs/tasks/T9.12.md",
        "docs/tasks/T9.13.md",
        "docs/tasks/T9.14.md",
        "docs/tasks/T9.15.md",
        "docs/tasks/T9.16.md",
        "docs/tasks/T9.17.md",
        "docs/tasks/T9.2.md",
        "docs/tasks/T9.3.md",
        "docs/tasks/T9.4.md",
        "docs/tasks/T9.5.md",
        "docs/tasks/T9.6.md",
        "docs/tasks/T9.7.md",
        "docs/tasks/T9.8.md",
        "docs/tasks/T9.9.md"
      ],
      "story_ids": [
        "UC-050",
        "UC-051",
        "UC-052",
        "UC-053",
        "UC-054",
        "UC-055",
        "UC-056",
        "UC-057",
        "UC-058",
        "UC-059",
        "UC-060",
        "UC-061",
        "UC-062",
        "UC-063",
        "UC-064",
        "UC-065",
        "UC-066",
        "UC-067",
        "UC-068",
        "UC-069",
        "UC-070",
        "UC-071",
        "UC-072",
        "UC-073",
        "UC-074",
        "UC-075",
        "UC-076",
        "UC-077",
        "UC-078",
        "UC-079",
        "UC-121",
        "UC-122"
      ],
      "interfaces": [
        "artifact",
        "ci",
        "cli",
        "config",
        "dns",
        "document",
        "github",
        "http",
        "iac",
        "telemetry",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R29",
      "name": "Autonomous diagnosis, fixes, releases and eligible deployment",
      "planned_refs": "E13, T15.7-T15.14, T16.10",
      "task_ids": [
        "T13.1",
        "T13.10",
        "T13.11",
        "T13.12",
        "T13.13",
        "T13.2",
        "T13.3",
        "T13.4",
        "T13.5",
        "T13.6",
        "T13.7",
        "T13.8",
        "T13.9",
        "T15.10",
        "T15.11",
        "T15.12",
        "T15.13",
        "T15.14",
        "T15.7",
        "T15.8",
        "T15.9",
        "T16.10"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.10.md",
        "docs/tasks/T13.11.md",
        "docs/tasks/T13.12.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.2.md",
        "docs/tasks/T13.3.md",
        "docs/tasks/T13.4.md",
        "docs/tasks/T13.5.md",
        "docs/tasks/T13.6.md",
        "docs/tasks/T13.7.md",
        "docs/tasks/T13.8.md",
        "docs/tasks/T13.9.md",
        "docs/tasks/T15.10.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.12.md",
        "docs/tasks/T15.13.md",
        "docs/tasks/T15.14.md",
        "docs/tasks/T15.7.md",
        "docs/tasks/T15.8.md",
        "docs/tasks/T15.9.md",
        "docs/tasks/T16.10.md"
      ],
      "story_ids": [
        "UC-096",
        "UC-097",
        "UC-098",
        "UC-099",
        "UC-100",
        "UC-101",
        "UC-102",
        "UC-103",
        "UC-105",
        "UC-106",
        "UC-108",
        "UC-114",
        "UC-115",
        "UC-116",
        "UC-117",
        "UC-118",
        "UC-119",
        "UC-125"
      ],
      "interfaces": [
        "Artifact",
        "CI",
        "CLI",
        "Config",
        "Event",
        "HTTP",
        "Policy",
        "Process",
        "Release",
        "Repository",
        "Runner",
        "cli"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R30",
      "name": "Shared fixes upstream; business and custom UI fixes private",
      "planned_refs": "T13.7, E14, T16.10-T16.11",
      "task_ids": [
        "T13.7",
        "T14.1",
        "T14.10",
        "T14.11",
        "T14.12",
        "T14.13",
        "T14.14",
        "T14.2",
        "T14.3",
        "T14.4",
        "T14.5",
        "T14.6",
        "T14.7",
        "T14.8",
        "T14.9",
        "T16.10",
        "T16.11"
      ],
      "contract_refs": [
        "docs/tasks/T13.7.md",
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.10.md",
        "docs/tasks/T14.11.md",
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.13.md",
        "docs/tasks/T14.14.md",
        "docs/tasks/T14.2.md",
        "docs/tasks/T14.3.md",
        "docs/tasks/T14.4.md",
        "docs/tasks/T14.5.md",
        "docs/tasks/T14.6.md",
        "docs/tasks/T14.7.md",
        "docs/tasks/T14.8.md",
        "docs/tasks/T14.9.md",
        "docs/tasks/T16.10.md",
        "docs/tasks/T16.11.md"
      ],
      "story_ids": [
        "UC-094",
        "UC-098",
        "UC-102",
        "UC-103",
        "UC-104",
        "UC-105",
        "UC-106",
        "UC-107",
        "UC-108",
        "UC-109",
        "UC-110",
        "UC-111",
        "UC-112",
        "UC-114",
        "UC-115",
        "UC-116",
        "UC-117",
        "UC-125"
      ],
      "interfaces": [
        "Artifact",
        "CI",
        "CLI",
        "Document",
        "HTTP",
        "Policy",
        "Process",
        "Repository",
        "Runner",
        "cli"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R31",
      "name": "Default sanitized upstream diagnostics, no raw/private data",
      "planned_refs": "T14.1-T14.7, T15.4/T15.11, T16.11",
      "task_ids": [
        "T14.1",
        "T14.2",
        "T14.3",
        "T14.4",
        "T14.5",
        "T14.6",
        "T14.7",
        "T15.11",
        "T15.4",
        "T16.11"
      ],
      "contract_refs": [
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.2.md",
        "docs/tasks/T14.3.md",
        "docs/tasks/T14.4.md",
        "docs/tasks/T14.5.md",
        "docs/tasks/T14.6.md",
        "docs/tasks/T14.7.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.4.md",
        "docs/tasks/T16.11.md"
      ],
      "story_ids": [
        "UC-082",
        "UC-085",
        "UC-098",
        "UC-099",
        "UC-103",
        "UC-104",
        "UC-105",
        "UC-106",
        "UC-107",
        "UC-108",
        "UC-109",
        "UC-111",
        "UC-112",
        "UC-114",
        "UC-117",
        "UC-125"
      ],
      "interfaces": [
        "Artifact",
        "CLI",
        "Document",
        "HTTP",
        "OAuth",
        "Policy",
        "Process",
        "REST",
        "Runner",
        "UI",
        "cli"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R32",
      "name": "Owner-controlled application agents; separate upstream maintenance",
      "planned_refs": "T13.1-T13.6, T14.8/T14.13-T14.14",
      "task_ids": [
        "T13.1",
        "T13.2",
        "T13.3",
        "T13.4",
        "T13.5",
        "T13.6",
        "T14.13",
        "T14.14",
        "T14.8"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.2.md",
        "docs/tasks/T13.3.md",
        "docs/tasks/T13.4.md",
        "docs/tasks/T13.5.md",
        "docs/tasks/T13.6.md",
        "docs/tasks/T14.13.md",
        "docs/tasks/T14.14.md",
        "docs/tasks/T14.8.md"
      ],
      "story_ids": [
        "UC-096",
        "UC-097",
        "UC-098",
        "UC-099",
        "UC-100",
        "UC-101",
        "UC-102",
        "UC-103",
        "UC-105",
        "UC-108",
        "UC-114",
        "UC-116",
        "UC-117"
      ],
      "interfaces": [
        "CI",
        "CLI",
        "Config",
        "Event",
        "HTTP",
        "Policy",
        "Release",
        "Repository",
        "Runner"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R33",
      "name": "Full-scope plan, staged delivery and inexpensive parallel workers",
      "planned_refs": "ADR 007, E1-E16, per-task contracts and execution/wave guide",
      "task_ids": [
        "T1.1",
        "T1.10",
        "T1.11",
        "T1.12",
        "T1.13",
        "T1.2",
        "T1.3",
        "T1.4",
        "T1.5",
        "T1.6",
        "T1.7",
        "T1.8",
        "T1.9",
        "T10.1",
        "T10.10",
        "T10.11",
        "T10.12",
        "T10.13",
        "T10.14",
        "T10.15",
        "T10.16",
        "T10.17",
        "T10.2",
        "T10.3",
        "T10.4",
        "T10.5",
        "T10.6",
        "T10.7",
        "T10.8",
        "T10.9",
        "T11.1",
        "T11.10",
        "T11.11",
        "T11.12",
        "T11.13",
        "T11.14",
        "T11.2",
        "T11.3",
        "T11.4",
        "T11.5",
        "T11.6",
        "T11.7",
        "T11.8",
        "T11.9",
        "T12.1",
        "T12.10",
        "T12.11",
        "T12.12",
        "T12.2",
        "T12.3",
        "T12.4",
        "T12.5",
        "T12.6",
        "T12.7",
        "T12.8",
        "T12.9",
        "T13.1",
        "T13.10",
        "T13.11",
        "T13.12",
        "T13.13",
        "T13.2",
        "T13.3",
        "T13.4",
        "T13.5",
        "T13.6",
        "T13.7",
        "T13.8",
        "T13.9",
        "T14.1",
        "T14.10",
        "T14.11",
        "T14.12",
        "T14.13",
        "T14.14",
        "T14.2",
        "T14.3",
        "T14.4",
        "T14.5",
        "T14.6",
        "T14.7",
        "T14.8",
        "T14.9",
        "T15.1",
        "T15.10",
        "T15.11",
        "T15.12",
        "T15.13",
        "T15.14",
        "T15.15",
        "T15.2",
        "T15.3",
        "T15.4",
        "T15.5",
        "T15.6",
        "T15.7",
        "T15.8",
        "T15.9",
        "T16.1",
        "T16.10",
        "T16.11",
        "T16.12",
        "T16.2",
        "T16.3",
        "T16.4",
        "T16.5",
        "T16.6",
        "T16.7",
        "T16.8",
        "T16.9",
        "T2.1",
        "T2.2",
        "T2.3",
        "T2.4",
        "T2.5",
        "T2.6",
        "T2.7",
        "T2.8",
        "T2.9",
        "T3.1",
        "T3.10",
        "T3.11",
        "T3.12",
        "T3.13",
        "T3.14",
        "T3.15",
        "T3.16",
        "T3.17",
        "T3.18",
        "T3.19",
        "T3.2",
        "T3.20",
        "T3.21",
        "T3.22",
        "T3.23",
        "T3.3",
        "T3.4",
        "T3.5",
        "T3.6",
        "T3.7",
        "T3.8",
        "T3.9",
        "T4.1",
        "T4.10",
        "T4.11",
        "T4.12",
        "T4.13",
        "T4.14",
        "T4.2",
        "T4.3",
        "T4.4",
        "T4.5",
        "T4.6",
        "T4.7",
        "T4.8",
        "T4.9",
        "T5.1",
        "T5.10",
        "T5.11",
        "T5.12",
        "T5.13",
        "T5.14",
        "T5.15",
        "T5.16",
        "T5.17",
        "T5.18",
        "T5.19",
        "T5.2",
        "T5.20",
        "T5.21",
        "T5.22",
        "T5.3",
        "T5.4",
        "T5.5",
        "T5.6",
        "T5.7",
        "T5.8",
        "T5.9",
        "T6.1",
        "T6.10",
        "T6.11",
        "T6.12",
        "T6.13",
        "T6.14",
        "T6.15",
        "T6.16",
        "T6.17",
        "T6.18",
        "T6.2",
        "T6.3",
        "T6.4",
        "T6.5",
        "T6.6",
        "T6.7",
        "T6.8",
        "T6.9",
        "T7.1",
        "T7.10",
        "T7.11",
        "T7.12",
        "T7.13",
        "T7.14",
        "T7.15",
        "T7.16",
        "T7.2",
        "T7.3",
        "T7.4",
        "T7.5",
        "T7.6",
        "T7.7",
        "T7.8",
        "T7.9",
        "T8.1",
        "T8.10",
        "T8.11",
        "T8.12",
        "T8.13",
        "T8.14",
        "T8.15",
        "T8.16",
        "T8.17",
        "T8.18",
        "T8.19",
        "T8.2",
        "T8.20",
        "T8.21",
        "T8.22",
        "T8.23",
        "T8.24",
        "T8.25",
        "T8.26",
        "T8.27",
        "T8.28",
        "T8.3",
        "T8.4",
        "T8.5",
        "T8.6",
        "T8.7",
        "T8.8",
        "T8.9",
        "T9.1",
        "T9.10",
        "T9.11",
        "T9.12",
        "T9.13",
        "T9.14",
        "T9.15",
        "T9.16",
        "T9.17",
        "T9.2",
        "T9.3",
        "T9.4",
        "T9.5",
        "T9.6",
        "T9.7",
        "T9.8",
        "T9.9"
      ],
      "contract_refs": [
        "docs/tasks/T1.1.md",
        "docs/tasks/T1.10.md",
        "docs/tasks/T1.11.md",
        "docs/tasks/T1.12.md",
        "docs/tasks/T1.13.md",
        "docs/tasks/T1.2.md",
        "docs/tasks/T1.3.md",
        "docs/tasks/T1.4.md",
        "docs/tasks/T1.5.md",
        "docs/tasks/T1.6.md",
        "docs/tasks/T1.7.md",
        "docs/tasks/T1.8.md",
        "docs/tasks/T1.9.md",
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.10.md",
        "docs/tasks/T10.11.md",
        "docs/tasks/T10.12.md",
        "docs/tasks/T10.13.md",
        "docs/tasks/T10.14.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T10.16.md",
        "docs/tasks/T10.17.md",
        "docs/tasks/T10.2.md",
        "docs/tasks/T10.3.md",
        "docs/tasks/T10.4.md",
        "docs/tasks/T10.5.md",
        "docs/tasks/T10.6.md",
        "docs/tasks/T10.7.md",
        "docs/tasks/T10.8.md",
        "docs/tasks/T10.9.md",
        "docs/tasks/T11.1.md",
        "docs/tasks/T11.10.md",
        "docs/tasks/T11.11.md",
        "docs/tasks/T11.12.md",
        "docs/tasks/T11.13.md",
        "docs/tasks/T11.14.md",
        "docs/tasks/T11.2.md",
        "docs/tasks/T11.3.md",
        "docs/tasks/T11.4.md",
        "docs/tasks/T11.5.md",
        "docs/tasks/T11.6.md",
        "docs/tasks/T11.7.md",
        "docs/tasks/T11.8.md",
        "docs/tasks/T11.9.md",
        "docs/tasks/T12.1.md",
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.11.md",
        "docs/tasks/T12.12.md",
        "docs/tasks/T12.2.md",
        "docs/tasks/T12.3.md",
        "docs/tasks/T12.4.md",
        "docs/tasks/T12.5.md",
        "docs/tasks/T12.6.md",
        "docs/tasks/T12.7.md",
        "docs/tasks/T12.8.md",
        "docs/tasks/T12.9.md",
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.10.md",
        "docs/tasks/T13.11.md",
        "docs/tasks/T13.12.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.2.md",
        "docs/tasks/T13.3.md",
        "docs/tasks/T13.4.md",
        "docs/tasks/T13.5.md",
        "docs/tasks/T13.6.md",
        "docs/tasks/T13.7.md",
        "docs/tasks/T13.8.md",
        "docs/tasks/T13.9.md",
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.10.md",
        "docs/tasks/T14.11.md",
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.13.md",
        "docs/tasks/T14.14.md",
        "docs/tasks/T14.2.md",
        "docs/tasks/T14.3.md",
        "docs/tasks/T14.4.md",
        "docs/tasks/T14.5.md",
        "docs/tasks/T14.6.md",
        "docs/tasks/T14.7.md",
        "docs/tasks/T14.8.md",
        "docs/tasks/T14.9.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.10.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.12.md",
        "docs/tasks/T15.13.md",
        "docs/tasks/T15.14.md",
        "docs/tasks/T15.15.md",
        "docs/tasks/T15.2.md",
        "docs/tasks/T15.3.md",
        "docs/tasks/T15.4.md",
        "docs/tasks/T15.5.md",
        "docs/tasks/T15.6.md",
        "docs/tasks/T15.7.md",
        "docs/tasks/T15.8.md",
        "docs/tasks/T15.9.md",
        "docs/tasks/T16.1.md",
        "docs/tasks/T16.10.md",
        "docs/tasks/T16.11.md",
        "docs/tasks/T16.12.md",
        "docs/tasks/T16.2.md",
        "docs/tasks/T16.3.md",
        "docs/tasks/T16.4.md",
        "docs/tasks/T16.5.md",
        "docs/tasks/T16.6.md",
        "docs/tasks/T16.7.md",
        "docs/tasks/T16.8.md",
        "docs/tasks/T16.9.md",
        "docs/tasks/T2.1.md",
        "docs/tasks/T2.2.md",
        "docs/tasks/T2.3.md",
        "docs/tasks/T2.4.md",
        "docs/tasks/T2.5.md",
        "docs/tasks/T2.6.md",
        "docs/tasks/T2.7.md",
        "docs/tasks/T2.8.md",
        "docs/tasks/T2.9.md",
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.11.md",
        "docs/tasks/T3.12.md",
        "docs/tasks/T3.13.md",
        "docs/tasks/T3.14.md",
        "docs/tasks/T3.15.md",
        "docs/tasks/T3.16.md",
        "docs/tasks/T3.17.md",
        "docs/tasks/T3.18.md",
        "docs/tasks/T3.19.md",
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.20.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.22.md",
        "docs/tasks/T3.23.md",
        "docs/tasks/T3.3.md",
        "docs/tasks/T3.4.md",
        "docs/tasks/T3.5.md",
        "docs/tasks/T3.6.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T3.8.md",
        "docs/tasks/T3.9.md",
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.10.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T4.12.md",
        "docs/tasks/T4.13.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.3.md",
        "docs/tasks/T4.4.md",
        "docs/tasks/T4.5.md",
        "docs/tasks/T4.6.md",
        "docs/tasks/T4.7.md",
        "docs/tasks/T4.8.md",
        "docs/tasks/T4.9.md",
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.10.md",
        "docs/tasks/T5.11.md",
        "docs/tasks/T5.12.md",
        "docs/tasks/T5.13.md",
        "docs/tasks/T5.14.md",
        "docs/tasks/T5.15.md",
        "docs/tasks/T5.16.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T5.18.md",
        "docs/tasks/T5.19.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.20.md",
        "docs/tasks/T5.21.md",
        "docs/tasks/T5.22.md",
        "docs/tasks/T5.3.md",
        "docs/tasks/T5.4.md",
        "docs/tasks/T5.5.md",
        "docs/tasks/T5.6.md",
        "docs/tasks/T5.7.md",
        "docs/tasks/T5.8.md",
        "docs/tasks/T5.9.md",
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.10.md",
        "docs/tasks/T6.11.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.13.md",
        "docs/tasks/T6.14.md",
        "docs/tasks/T6.15.md",
        "docs/tasks/T6.16.md",
        "docs/tasks/T6.17.md",
        "docs/tasks/T6.18.md",
        "docs/tasks/T6.2.md",
        "docs/tasks/T6.3.md",
        "docs/tasks/T6.4.md",
        "docs/tasks/T6.5.md",
        "docs/tasks/T6.6.md",
        "docs/tasks/T6.7.md",
        "docs/tasks/T6.8.md",
        "docs/tasks/T6.9.md",
        "docs/tasks/T7.1.md",
        "docs/tasks/T7.10.md",
        "docs/tasks/T7.11.md",
        "docs/tasks/T7.12.md",
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.14.md",
        "docs/tasks/T7.15.md",
        "docs/tasks/T7.16.md",
        "docs/tasks/T7.2.md",
        "docs/tasks/T7.3.md",
        "docs/tasks/T7.4.md",
        "docs/tasks/T7.5.md",
        "docs/tasks/T7.6.md",
        "docs/tasks/T7.7.md",
        "docs/tasks/T7.8.md",
        "docs/tasks/T7.9.md",
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.10.md",
        "docs/tasks/T8.11.md",
        "docs/tasks/T8.12.md",
        "docs/tasks/T8.13.md",
        "docs/tasks/T8.14.md",
        "docs/tasks/T8.15.md",
        "docs/tasks/T8.16.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.18.md",
        "docs/tasks/T8.19.md",
        "docs/tasks/T8.2.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.21.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.23.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T8.25.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T8.27.md",
        "docs/tasks/T8.28.md",
        "docs/tasks/T8.3.md",
        "docs/tasks/T8.4.md",
        "docs/tasks/T8.5.md",
        "docs/tasks/T8.6.md",
        "docs/tasks/T8.7.md",
        "docs/tasks/T8.8.md",
        "docs/tasks/T8.9.md",
        "docs/tasks/T9.1.md",
        "docs/tasks/T9.10.md",
        "docs/tasks/T9.11.md",
        "docs/tasks/T9.12.md",
        "docs/tasks/T9.13.md",
        "docs/tasks/T9.14.md",
        "docs/tasks/T9.15.md",
        "docs/tasks/T9.16.md",
        "docs/tasks/T9.17.md",
        "docs/tasks/T9.2.md",
        "docs/tasks/T9.3.md",
        "docs/tasks/T9.4.md",
        "docs/tasks/T9.5.md",
        "docs/tasks/T9.6.md",
        "docs/tasks/T9.7.md",
        "docs/tasks/T9.8.md",
        "docs/tasks/T9.9.md"
      ],
      "story_ids": [
        "UC-001",
        "UC-002",
        "UC-003",
        "UC-004",
        "UC-005",
        "UC-006",
        "UC-007",
        "UC-008",
        "UC-009",
        "UC-010",
        "UC-020",
        "UC-021",
        "UC-022",
        "UC-023",
        "UC-024",
        "UC-025",
        "UC-026",
        "UC-027",
        "UC-028",
        "UC-029",
        "UC-030",
        "UC-031",
        "UC-032",
        "UC-033",
        "UC-034",
        "UC-035",
        "UC-036",
        "UC-037",
        "UC-038",
        "UC-039",
        "UC-040",
        "UC-041",
        "UC-042",
        "UC-043",
        "UC-044",
        "UC-045",
        "UC-046",
        "UC-047",
        "UC-048",
        "UC-049",
        "UC-050",
        "UC-051",
        "UC-052",
        "UC-053",
        "UC-054",
        "UC-055",
        "UC-056",
        "UC-057",
        "UC-058",
        "UC-059",
        "UC-060",
        "UC-061",
        "UC-062",
        "UC-063",
        "UC-064",
        "UC-065",
        "UC-066",
        "UC-067",
        "UC-068",
        "UC-069",
        "UC-070",
        "UC-071",
        "UC-072",
        "UC-073",
        "UC-074",
        "UC-075",
        "UC-076",
        "UC-077",
        "UC-078",
        "UC-079",
        "UC-080",
        "UC-081",
        "UC-082",
        "UC-083",
        "UC-084",
        "UC-085",
        "UC-086",
        "UC-087",
        "UC-088",
        "UC-089",
        "UC-090",
        "UC-091",
        "UC-092",
        "UC-093",
        "UC-094",
        "UC-095",
        "UC-096",
        "UC-097",
        "UC-098",
        "UC-099",
        "UC-100",
        "UC-101",
        "UC-102",
        "UC-103",
        "UC-104",
        "UC-105",
        "UC-106",
        "UC-107",
        "UC-108",
        "UC-109",
        "UC-110",
        "UC-111",
        "UC-112",
        "UC-113",
        "UC-114",
        "UC-115",
        "UC-116",
        "UC-117",
        "UC-118",
        "UC-119",
        "UC-120",
        "UC-121",
        "UC-122",
        "UC-123",
        "UC-124",
        "UC-125"
      ],
      "interfaces": [
        "Artifact",
        "Browser",
        "CI",
        "CLI",
        "Config",
        "Document",
        "Event",
        "Go",
        "HTTP",
        "MCP",
        "Network",
        "OAuth",
        "OpenAPI",
        "Policy",
        "Process",
        "REST",
        "Release",
        "Repository",
        "Runner",
        "UI",
        "api",
        "artifact",
        "ci",
        "cli",
        "config",
        "developer",
        "dns",
        "docs",
        "document",
        "github",
        "http",
        "iac",
        "mcp",
        "public-contract",
        "telemetry",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "R34",
      "name": "Public open-source information boundary",
      "planned_refs": "T1.4/T1.6/T1.7, T14.1/T14.7, publication checks",
      "task_ids": [
        "T1.4",
        "T1.6",
        "T1.7",
        "T14.1",
        "T14.7"
      ],
      "contract_refs": [
        "docs/tasks/T1.4.md",
        "docs/tasks/T1.6.md",
        "docs/tasks/T1.7.md",
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.7.md"
      ],
      "story_ids": [
        "UC-005",
        "UC-009",
        "UC-104",
        "UC-105",
        "UC-107",
        "UC-111",
        "UC-112",
        "UC-117"
      ],
      "interfaces": [
        "CLI",
        "Document",
        "HTTP",
        "Process",
        "Runner",
        "docs"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    }
  ],
  "stories": [
    {
      "id": "UC-001",
      "name": "Initialize a developer-owned application",
      "source_status": "PLANNED",
      "task_ids": [
        "T2.7"
      ],
      "contract_refs": [
        "docs/tasks/T2.7.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-002",
      "name": "Compose a working local application",
      "source_status": "PLANNED",
      "task_ids": [
        "T1.1",
        "T1.10",
        "T1.11",
        "T1.5",
        "T2.2"
      ],
      "contract_refs": [
        "docs/tasks/T1.1.md",
        "docs/tasks/T1.10.md",
        "docs/tasks/T1.11.md",
        "docs/tasks/T1.5.md",
        "docs/tasks/T2.2.md"
      ],
      "interfaces": [
        "cli",
        "web"
      ],
      "required_evidence": [
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-003",
      "name": "Declare a business operation once",
      "source_status": "PLANNED",
      "task_ids": [
        "T1.2",
        "T2.1",
        "T2.3"
      ],
      "contract_refs": [
        "docs/tasks/T1.2.md",
        "docs/tasks/T2.1.md",
        "docs/tasks/T2.3.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-004",
      "name": "Verify route and policy registration",
      "source_status": "PLANNED",
      "task_ids": [
        "T1.12",
        "T1.2",
        "T1.5",
        "T2.2",
        "T2.8",
        "T2.9"
      ],
      "contract_refs": [
        "docs/tasks/T1.12.md",
        "docs/tasks/T1.2.md",
        "docs/tasks/T1.5.md",
        "docs/tasks/T2.2.md",
        "docs/tasks/T2.8.md",
        "docs/tasks/T2.9.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-005",
      "name": "Understand supported dependencies",
      "source_status": "PLANNED",
      "task_ids": [
        "T1.2",
        "T1.4",
        "T1.6"
      ],
      "contract_refs": [
        "docs/tasks/T1.2.md",
        "docs/tasks/T1.4.md",
        "docs/tasks/T1.6.md"
      ],
      "interfaces": [
        "docs"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-006",
      "name": "Apply compatible schema migrations",
      "source_status": "PLANNED",
      "task_ids": [
        "T1.3"
      ],
      "contract_refs": [
        "docs/tasks/T1.3.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-007",
      "name": "Exercise a reference paid feature",
      "source_status": "PLANNED",
      "task_ids": [
        "T2.4",
        "T2.5",
        "T2.6"
      ],
      "contract_refs": [
        "docs/tasks/T2.4.md",
        "docs/tasks/T2.5.md",
        "docs/tasks/T2.6.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-008",
      "name": "Resume development from public documentation",
      "source_status": "PLANNED",
      "task_ids": [
        "T1.1",
        "T1.13",
        "T1.9",
        "T2.7"
      ],
      "contract_refs": [
        "docs/tasks/T1.1.md",
        "docs/tasks/T1.13.md",
        "docs/tasks/T1.9.md",
        "docs/tasks/T2.7.md"
      ],
      "interfaces": [
        "docs"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-009",
      "name": "Submit a compatible contribution",
      "source_status": "PLANNED",
      "task_ids": [
        "T1.4",
        "T1.6",
        "T1.7",
        "T1.8",
        "T1.9"
      ],
      "contract_refs": [
        "docs/tasks/T1.4.md",
        "docs/tasks/T1.6.md",
        "docs/tasks/T1.7.md",
        "docs/tasks/T1.8.md",
        "docs/tasks/T1.9.md"
      ],
      "interfaces": [
        "docs"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-010",
      "name": "Regenerate without losing custom work",
      "source_status": "PLANNED",
      "task_ids": [
        "T2.1",
        "T2.3"
      ],
      "contract_refs": [
        "docs/tasks/T2.1.md",
        "docs/tasks/T2.3.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-020",
      "name": "Register and verify an account",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.2",
        "T3.4",
        "T3.5",
        "T3.6",
        "T3.7",
        "T4.3",
        "T6.3"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.4.md",
        "docs/tasks/T3.5.md",
        "docs/tasks/T3.6.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T4.3.md",
        "docs/tasks/T6.3.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-021",
      "name": "Sign in with email and password",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.3",
        "T3.4",
        "T3.5",
        "T3.7",
        "T6.3"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.3.md",
        "docs/tasks/T3.4.md",
        "docs/tasks/T3.5.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T6.3.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-022",
      "name": "Recover or change password",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.6",
        "T3.7",
        "T3.8",
        "T6.3"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.6.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T3.8.md",
        "docs/tasks/T6.3.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-023",
      "name": "Sign in by email magic link",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.21",
        "T3.9",
        "T6.7"
      ],
      "contract_refs": [
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.9.md",
        "docs/tasks/T6.7.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-024",
      "name": "Sign in with Google",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.10",
        "T3.11",
        "T3.21",
        "T3.22",
        "T6.7"
      ],
      "contract_refs": [
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.11.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.22.md",
        "docs/tasks/T6.7.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-025",
      "name": "Sign in with GitHub",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.10",
        "T3.12",
        "T3.21",
        "T3.22",
        "T6.7"
      ],
      "contract_refs": [
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.12.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.22.md",
        "docs/tasks/T6.7.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-026",
      "name": "Sign in with Apple",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.10",
        "T3.13",
        "T3.21",
        "T3.22",
        "T6.7"
      ],
      "contract_refs": [
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.13.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.22.md",
        "docs/tasks/T6.7.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-027",
      "name": "Enroll and use passkeys",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.14",
        "T3.21",
        "T3.7",
        "T6.8"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.14.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T3.7.md",
        "docs/tasks/T6.8.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-028",
      "name": "Enroll, satisfy and recover MFA",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.15",
        "T3.16",
        "T3.17",
        "T3.21",
        "T4.10",
        "T4.11",
        "T6.11",
        "T6.9"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.15.md",
        "docs/tasks/T3.16.md",
        "docs/tasks/T3.17.md",
        "docs/tasks/T3.21.md",
        "docs/tasks/T4.10.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T6.11.md",
        "docs/tasks/T6.9.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-029",
      "name": "Manage profile, email and sign-in methods",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.10",
        "T3.16",
        "T3.17",
        "T3.18",
        "T3.19",
        "T3.2",
        "T3.6",
        "T3.8",
        "T6.6",
        "T6.8",
        "T6.9"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.10.md",
        "docs/tasks/T3.16.md",
        "docs/tasks/T3.17.md",
        "docs/tasks/T3.18.md",
        "docs/tasks/T3.19.md",
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.6.md",
        "docs/tasks/T3.8.md",
        "docs/tasks/T6.6.md",
        "docs/tasks/T6.8.md",
        "docs/tasks/T6.9.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-030",
      "name": "Inspect and revoke sessions",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.19",
        "T3.23",
        "T3.3",
        "T3.8",
        "T6.6"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.19.md",
        "docs/tasks/T3.23.md",
        "docs/tasks/T3.3.md",
        "docs/tasks/T3.8.md",
        "docs/tasks/T6.6.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-031",
      "name": "Export or delete personal account",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.2",
        "T3.20",
        "T5.17",
        "T6.13"
      ],
      "contract_refs": [
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.20.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T6.13.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-032",
      "name": "Disable account and invalidate authority",
      "source_status": "PLANNED",
      "task_ids": [
        "T3.1",
        "T3.17",
        "T3.2",
        "T3.20",
        "T3.23",
        "T3.3",
        "T6.13"
      ],
      "contract_refs": [
        "docs/tasks/T3.1.md",
        "docs/tasks/T3.17.md",
        "docs/tasks/T3.2.md",
        "docs/tasks/T3.20.md",
        "docs/tasks/T3.23.md",
        "docs/tasks/T3.3.md",
        "docs/tasks/T6.13.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-033",
      "name": "Use personal workspace and switch organizations",
      "source_status": "PLANNED",
      "task_ids": [
        "T4.1",
        "T4.2",
        "T4.3",
        "T4.4",
        "T6.4"
      ],
      "contract_refs": [
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.3.md",
        "docs/tasks/T4.4.md",
        "docs/tasks/T6.4.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-034",
      "name": "Create an organization",
      "source_status": "PLANNED",
      "task_ids": [
        "T4.1",
        "T4.2",
        "T4.5",
        "T6.10"
      ],
      "contract_refs": [
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.5.md",
        "docs/tasks/T6.10.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-035",
      "name": "Invite and join organization",
      "source_status": "PLANNED",
      "task_ids": [
        "T4.1",
        "T4.2",
        "T4.6",
        "T4.7",
        "T5.14",
        "T6.10"
      ],
      "contract_refs": [
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.6.md",
        "docs/tasks/T4.7.md",
        "docs/tasks/T5.14.md",
        "docs/tasks/T6.10.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-036",
      "name": "Manage roles, leave or remove members",
      "source_status": "PLANNED",
      "task_ids": [
        "T4.1",
        "T4.12",
        "T4.14",
        "T4.2",
        "T4.7",
        "T4.8",
        "T6.10"
      ],
      "contract_refs": [
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.12.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.2.md",
        "docs/tasks/T4.7.md",
        "docs/tasks/T4.8.md",
        "docs/tasks/T6.10.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-037",
      "name": "Transfer, suspend, export or delete organization",
      "source_status": "PLANNED",
      "task_ids": [
        "T4.1",
        "T4.11",
        "T4.12",
        "T4.13",
        "T4.14",
        "T4.5",
        "T5.17",
        "T6.10",
        "T6.13"
      ],
      "contract_refs": [
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T4.12.md",
        "docs/tasks/T4.13.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.5.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T6.10.md",
        "docs/tasks/T6.13.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-038",
      "name": "Access only authorized tenant resources",
      "source_status": "PLANNED",
      "task_ids": [
        "T4.1",
        "T4.13",
        "T4.14",
        "T4.4",
        "T4.8",
        "T6.4"
      ],
      "contract_refs": [
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.13.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.4.md",
        "docs/tasks/T4.8.md",
        "docs/tasks/T6.4.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-039",
      "name": "Configure and enforce organization OIDC SSO",
      "source_status": "PLANNED",
      "task_ids": [
        "T4.1",
        "T4.10",
        "T4.11",
        "T4.14",
        "T4.9",
        "T6.11"
      ],
      "contract_refs": [
        "docs/tasks/T4.1.md",
        "docs/tasks/T4.10.md",
        "docs/tasks/T4.11.md",
        "docs/tasks/T4.14.md",
        "docs/tasks/T4.9.md",
        "docs/tasks/T6.11.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-040",
      "name": "Buy independent personal subscription",
      "source_status": "PLANNED",
      "task_ids": [
        "T5.1",
        "T5.10",
        "T5.2",
        "T5.21",
        "T5.3",
        "T5.4",
        "T5.5",
        "T5.6",
        "T5.7",
        "T5.8",
        "T5.9",
        "T6.5"
      ],
      "contract_refs": [
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.10.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.21.md",
        "docs/tasks/T5.3.md",
        "docs/tasks/T5.4.md",
        "docs/tasks/T5.5.md",
        "docs/tasks/T5.6.md",
        "docs/tasks/T5.7.md",
        "docs/tasks/T5.8.md",
        "docs/tasks/T5.9.md",
        "docs/tasks/T6.5.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-041",
      "name": "Buy independent organization subscription",
      "source_status": "PLANNED",
      "task_ids": [
        "T5.1",
        "T5.10",
        "T5.2",
        "T5.3",
        "T5.4",
        "T5.5",
        "T5.6",
        "T5.7",
        "T5.8",
        "T5.9",
        "T6.5"
      ],
      "contract_refs": [
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.10.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.3.md",
        "docs/tasks/T5.4.md",
        "docs/tasks/T5.5.md",
        "docs/tasks/T5.6.md",
        "docs/tasks/T5.7.md",
        "docs/tasks/T5.8.md",
        "docs/tasks/T5.9.md",
        "docs/tasks/T6.5.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-042",
      "name": "Manage subscription, invoices and payment actions",
      "source_status": "PLANNED",
      "task_ids": [
        "T5.1",
        "T5.11",
        "T5.12",
        "T5.17",
        "T5.2",
        "T5.22",
        "T5.3",
        "T6.12",
        "T6.18"
      ],
      "contract_refs": [
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.11.md",
        "docs/tasks/T5.12.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.22.md",
        "docs/tasks/T5.3.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.18.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-043",
      "name": "Pay and reconcile organization seats",
      "source_status": "PLANNED",
      "task_ids": [
        "T5.1",
        "T5.13",
        "T5.14",
        "T5.2",
        "T5.20",
        "T5.21",
        "T5.3",
        "T6.12",
        "T6.18"
      ],
      "contract_refs": [
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.13.md",
        "docs/tasks/T5.14.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.20.md",
        "docs/tasks/T5.21.md",
        "docs/tasks/T5.3.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.18.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-044",
      "name": "Record and inspect usage-based billing",
      "source_status": "PLANNED",
      "task_ids": [
        "T5.1",
        "T5.15",
        "T5.16",
        "T5.17",
        "T5.18",
        "T5.19",
        "T5.2",
        "T5.20",
        "T5.21",
        "T6.12",
        "T6.18"
      ],
      "contract_refs": [
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.15.md",
        "docs/tasks/T5.16.md",
        "docs/tasks/T5.17.md",
        "docs/tasks/T5.18.md",
        "docs/tasks/T5.19.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.20.md",
        "docs/tasks/T5.21.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.18.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-045",
      "name": "Understand denied or unavailable paid access",
      "source_status": "PLANNED",
      "task_ids": [
        "T5.1",
        "T5.10",
        "T5.12",
        "T5.19",
        "T5.2",
        "T5.21",
        "T5.4",
        "T5.7",
        "T5.8",
        "T5.9",
        "T6.12",
        "T6.18",
        "T6.5"
      ],
      "contract_refs": [
        "docs/tasks/T5.1.md",
        "docs/tasks/T5.10.md",
        "docs/tasks/T5.12.md",
        "docs/tasks/T5.19.md",
        "docs/tasks/T5.2.md",
        "docs/tasks/T5.21.md",
        "docs/tasks/T5.4.md",
        "docs/tasks/T5.7.md",
        "docs/tasks/T5.8.md",
        "docs/tasks/T5.9.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.18.md",
        "docs/tasks/T6.5.md"
      ],
      "interfaces": [
        "api",
        "mcp",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-046",
      "name": "Complete lifecycle through default shared pages",
      "source_status": "PLANNED",
      "task_ids": [
        "T6.1",
        "T6.10",
        "T6.11",
        "T6.12",
        "T6.13",
        "T6.15",
        "T6.16",
        "T6.18",
        "T6.2",
        "T6.3",
        "T6.4",
        "T6.5",
        "T6.6",
        "T6.7",
        "T6.8",
        "T6.9"
      ],
      "contract_refs": [
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.10.md",
        "docs/tasks/T6.11.md",
        "docs/tasks/T6.12.md",
        "docs/tasks/T6.13.md",
        "docs/tasks/T6.15.md",
        "docs/tasks/T6.16.md",
        "docs/tasks/T6.18.md",
        "docs/tasks/T6.2.md",
        "docs/tasks/T6.3.md",
        "docs/tasks/T6.4.md",
        "docs/tasks/T6.5.md",
        "docs/tasks/T6.6.md",
        "docs/tasks/T6.7.md",
        "docs/tasks/T6.8.md",
        "docs/tasks/T6.9.md"
      ],
      "interfaces": [
        "api",
        "web"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-047",
      "name": "Theme or replace an individual shared page",
      "source_status": "PLANNED",
      "task_ids": [
        "T6.1",
        "T6.14",
        "T6.17",
        "T6.2"
      ],
      "contract_refs": [
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.14.md",
        "docs/tasks/T6.17.md",
        "docs/tasks/T6.2.md"
      ],
      "interfaces": [
        "developer",
        "public-contract"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-048",
      "name": "Replace the whole shared user interface",
      "source_status": "PLANNED",
      "task_ids": [
        "T6.1",
        "T6.15",
        "T6.17"
      ],
      "contract_refs": [
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.15.md",
        "docs/tasks/T6.17.md"
      ],
      "interfaces": [
        "developer",
        "public-contract"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-049",
      "name": "Use lifecycle with keyboard and small screen",
      "source_status": "PLANNED",
      "task_ids": [
        "T6.1",
        "T6.14",
        "T6.16",
        "T6.2"
      ],
      "contract_refs": [
        "docs/tasks/T6.1.md",
        "docs/tasks/T6.14.md",
        "docs/tasks/T6.16.md",
        "docs/tasks/T6.2.md"
      ],
      "interfaces": [
        "public-contract",
        "web"
      ],
      "required_evidence": [
        "behavior",
        "browser",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-050",
      "name": "Initialize a named application",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.1",
        "T7.13",
        "T7.14",
        "T7.16",
        "T7.2",
        "T8.23",
        "T9.3",
        "T9.8"
      ],
      "contract_refs": [
        "docs/tasks/T7.1.md",
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.14.md",
        "docs/tasks/T7.16.md",
        "docs/tasks/T7.2.md",
        "docs/tasks/T8.23.md",
        "docs/tasks/T9.3.md",
        "docs/tasks/T9.8.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-051",
      "name": "Diagnose local prerequisites",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.13",
        "T7.15",
        "T7.3",
        "T8.28"
      ],
      "contract_refs": [
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.15.md",
        "docs/tasks/T7.3.md",
        "docs/tasks/T8.28.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-052",
      "name": "Run the local reference application",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.2",
        "T7.13",
        "T7.14",
        "T7.4",
        "T7.5",
        "T7.6",
        "T9.3"
      ],
      "contract_refs": [
        "docs/tasks/T10.2.md",
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.14.md",
        "docs/tasks/T7.4.md",
        "docs/tasks/T7.5.md",
        "docs/tasks/T7.6.md",
        "docs/tasks/T9.3.md"
      ],
      "interfaces": [
        "cli",
        "http"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-053",
      "name": "Resume an interrupted local setup",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.1",
        "T7.12",
        "T7.13",
        "T7.2",
        "T7.7"
      ],
      "contract_refs": [
        "docs/tasks/T7.1.md",
        "docs/tasks/T7.12.md",
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.2.md",
        "docs/tasks/T7.7.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-054",
      "name": "Choose an integrated or separate business service",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.15",
        "T7.8",
        "T8.10",
        "T8.21"
      ],
      "contract_refs": [
        "docs/tasks/T7.15.md",
        "docs/tasks/T7.8.md",
        "docs/tasks/T8.10.md",
        "docs/tasks/T8.21.md"
      ],
      "interfaces": [
        "config",
        "http"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-055",
      "name": "Inspect deployment inputs before mutation",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.10",
        "T7.14",
        "T7.15",
        "T7.9",
        "T8.15",
        "T8.22",
        "T8.23",
        "T8.26",
        "T9.14"
      ],
      "contract_refs": [
        "docs/tasks/T7.10.md",
        "docs/tasks/T7.14.md",
        "docs/tasks/T7.15.md",
        "docs/tasks/T7.9.md",
        "docs/tasks/T8.15.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.23.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T9.14.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-056",
      "name": "Deploy an approved immutable application release",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.11",
        "T7.12",
        "T7.15",
        "T7.16",
        "T8.13",
        "T8.22",
        "T8.25",
        "T8.26",
        "T9.5"
      ],
      "contract_refs": [
        "docs/tasks/T7.11.md",
        "docs/tasks/T7.12.md",
        "docs/tasks/T7.15.md",
        "docs/tasks/T7.16.md",
        "docs/tasks/T8.13.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.25.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T9.5.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-057",
      "name": "Inspect, stop, and clean local resources",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.13",
        "T7.5",
        "T7.7"
      ],
      "contract_refs": [
        "docs/tasks/T7.13.md",
        "docs/tasks/T7.5.md",
        "docs/tasks/T7.7.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-058",
      "name": "Select a costed AWS hosting profile",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.16",
        "T8.1",
        "T8.14",
        "T8.19",
        "T8.23"
      ],
      "contract_refs": [
        "docs/tasks/T10.16.md",
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.14.md",
        "docs/tasks/T8.19.md",
        "docs/tasks/T8.23.md"
      ],
      "interfaces": [
        "config"
      ],
      "required_evidence": [
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-059",
      "name": "Bootstrap owner-controlled infrastructure state",
      "source_status": "PLANNED",
      "task_ids": [
        "T8.11",
        "T8.12",
        "T8.17",
        "T8.2",
        "T8.22",
        "T8.3",
        "T9.14"
      ],
      "contract_refs": [
        "docs/tasks/T8.11.md",
        "docs/tasks/T8.12.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.2.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.3.md",
        "docs/tasks/T9.14.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-060",
      "name": "Provision an isolated AWS application host",
      "source_status": "PLANNED",
      "task_ids": [
        "T8.1",
        "T8.11",
        "T8.12",
        "T8.17",
        "T8.18",
        "T8.19",
        "T8.20",
        "T8.22",
        "T8.26",
        "T8.27",
        "T8.28",
        "T8.4",
        "T8.5",
        "T8.6"
      ],
      "contract_refs": [
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.11.md",
        "docs/tasks/T8.12.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.18.md",
        "docs/tasks/T8.19.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T8.27.md",
        "docs/tasks/T8.28.md",
        "docs/tasks/T8.4.md",
        "docs/tasks/T8.5.md",
        "docs/tasks/T8.6.md"
      ],
      "interfaces": [
        "iac"
      ],
      "required_evidence": [
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-061",
      "name": "Connect a required Cloudflare domain",
      "source_status": "PLANNED",
      "task_ids": [
        "T8.10",
        "T8.11",
        "T8.12",
        "T8.16",
        "T8.17",
        "T8.18",
        "T8.21",
        "T8.26",
        "T8.27",
        "T8.8",
        "T8.9"
      ],
      "contract_refs": [
        "docs/tasks/T8.10.md",
        "docs/tasks/T8.11.md",
        "docs/tasks/T8.12.md",
        "docs/tasks/T8.16.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.18.md",
        "docs/tasks/T8.21.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T8.27.md",
        "docs/tasks/T8.8.md",
        "docs/tasks/T8.9.md"
      ],
      "interfaces": [
        "config",
        "dns"
      ],
      "required_evidence": [
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-062",
      "name": "Keep cloud credentials out of application artifacts",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.13",
        "T7.9",
        "T8.19",
        "T8.2",
        "T8.27",
        "T8.28",
        "T8.4",
        "T8.6",
        "T9.14"
      ],
      "contract_refs": [
        "docs/tasks/T10.13.md",
        "docs/tasks/T7.9.md",
        "docs/tasks/T8.19.md",
        "docs/tasks/T8.2.md",
        "docs/tasks/T8.27.md",
        "docs/tasks/T8.28.md",
        "docs/tasks/T8.4.md",
        "docs/tasks/T8.6.md",
        "docs/tasks/T9.14.md"
      ],
      "interfaces": [
        "ci",
        "config"
      ],
      "required_evidence": [
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-063",
      "name": "Persist and recover PostgreSQL in either profile",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.10",
        "T10.16",
        "T10.7",
        "T7.4",
        "T8.1",
        "T8.12",
        "T8.17",
        "T8.20",
        "T8.24",
        "T8.25",
        "T8.26",
        "T8.5",
        "T8.7"
      ],
      "contract_refs": [
        "docs/tasks/T10.10.md",
        "docs/tasks/T10.16.md",
        "docs/tasks/T10.7.md",
        "docs/tasks/T7.4.md",
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.12.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T8.25.md",
        "docs/tasks/T8.26.md",
        "docs/tasks/T8.5.md",
        "docs/tasks/T8.7.md"
      ],
      "interfaces": [
        "cli",
        "iac"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-064",
      "name": "Protect cost and retained resources during changes",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.10",
        "T8.14",
        "T8.15",
        "T8.17",
        "T8.18",
        "T8.23",
        "T8.24"
      ],
      "contract_refs": [
        "docs/tasks/T7.10.md",
        "docs/tasks/T8.14.md",
        "docs/tasks/T8.15.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.18.md",
        "docs/tasks/T8.23.md",
        "docs/tasks/T8.24.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-065",
      "name": "Validate public contributions without cloud privilege",
      "source_status": "PLANNED",
      "task_ids": [
        "T9.1",
        "T9.14",
        "T9.17",
        "T9.2",
        "T9.3"
      ],
      "contract_refs": [
        "docs/tasks/T9.1.md",
        "docs/tasks/T9.14.md",
        "docs/tasks/T9.17.md",
        "docs/tasks/T9.2.md",
        "docs/tasks/T9.3.md"
      ],
      "interfaces": [
        "ci"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-066",
      "name": "Build and verify immutable release artifacts",
      "source_status": "PLANNED",
      "task_ids": [
        "T8.11",
        "T8.13",
        "T8.22",
        "T9.1",
        "T9.15",
        "T9.17",
        "T9.4",
        "T9.5",
        "T9.6"
      ],
      "contract_refs": [
        "docs/tasks/T8.11.md",
        "docs/tasks/T8.13.md",
        "docs/tasks/T8.22.md",
        "docs/tasks/T9.1.md",
        "docs/tasks/T9.15.md",
        "docs/tasks/T9.17.md",
        "docs/tasks/T9.4.md",
        "docs/tasks/T9.5.md",
        "docs/tasks/T9.6.md"
      ],
      "interfaces": [
        "artifact",
        "ci"
      ],
      "required_evidence": [
        "behavior",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-067",
      "name": "Install a versioned CLI safely",
      "source_status": "PLANNED",
      "task_ids": [
        "T9.6",
        "T9.7"
      ],
      "contract_refs": [
        "docs/tasks/T9.6.md",
        "docs/tasks/T9.7.md"
      ],
      "interfaces": [
        "artifact",
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-068",
      "name": "Upgrade generated files without losing custom code",
      "source_status": "PLANNED",
      "task_ids": [
        "T9.10",
        "T9.11",
        "T9.12",
        "T9.15",
        "T9.16",
        "T9.17",
        "T9.8"
      ],
      "contract_refs": [
        "docs/tasks/T9.10.md",
        "docs/tasks/T9.11.md",
        "docs/tasks/T9.12.md",
        "docs/tasks/T9.15.md",
        "docs/tasks/T9.16.md",
        "docs/tasks/T9.17.md",
        "docs/tasks/T9.8.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-069",
      "name": "Receive and review an upgrade pull request",
      "source_status": "PLANNED",
      "task_ids": [
        "T9.13",
        "T9.16"
      ],
      "contract_refs": [
        "docs/tasks/T9.13.md",
        "docs/tasks/T9.16.md"
      ],
      "interfaces": [
        "ci",
        "github"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-070",
      "name": "Preserve overrides and package extension contracts",
      "source_status": "PLANNED",
      "task_ids": [
        "T7.14",
        "T9.10",
        "T9.16",
        "T9.8",
        "T9.9"
      ],
      "contract_refs": [
        "docs/tasks/T7.14.md",
        "docs/tasks/T9.10.md",
        "docs/tasks/T9.16.md",
        "docs/tasks/T9.8.md",
        "docs/tasks/T9.9.md"
      ],
      "interfaces": [
        "artifact",
        "config"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-071",
      "name": "Reject incompatible or untrusted upgrades",
      "source_status": "PLANNED",
      "task_ids": [
        "T9.10",
        "T9.12",
        "T9.15",
        "T9.16",
        "T9.9"
      ],
      "contract_refs": [
        "docs/tasks/T9.10.md",
        "docs/tasks/T9.12.md",
        "docs/tasks/T9.15.md",
        "docs/tasks/T9.16.md",
        "docs/tasks/T9.9.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-072",
      "name": "Observe application health without exposing internals",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.1",
        "T10.17",
        "T10.2",
        "T8.10",
        "T8.21"
      ],
      "contract_refs": [
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.17.md",
        "docs/tasks/T10.2.md",
        "docs/tasks/T8.10.md",
        "docs/tasks/T8.21.md"
      ],
      "interfaces": [
        "http"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-073",
      "name": "Trace requests and business-side failures",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.1",
        "T10.14",
        "T10.17",
        "T10.3",
        "T10.4",
        "T10.5",
        "T8.16"
      ],
      "contract_refs": [
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.14.md",
        "docs/tasks/T10.17.md",
        "docs/tasks/T10.3.md",
        "docs/tasks/T10.4.md",
        "docs/tasks/T10.5.md",
        "docs/tasks/T8.16.md"
      ],
      "interfaces": [
        "telemetry"
      ],
      "required_evidence": [
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-074",
      "name": "Detect failed background processing",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.1",
        "T10.15",
        "T10.4",
        "T10.6",
        "T10.9",
        "T8.28"
      ],
      "contract_refs": [
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T10.4.md",
        "docs/tasks/T10.6.md",
        "docs/tasks/T10.9.md",
        "docs/tasks/T8.28.md"
      ],
      "interfaces": [
        "telemetry"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-075",
      "name": "Create encrypted application backups",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.1",
        "T10.15",
        "T10.17",
        "T10.7",
        "T10.8",
        "T10.9",
        "T8.20",
        "T8.7"
      ],
      "contract_refs": [
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T10.17.md",
        "docs/tasks/T10.7.md",
        "docs/tasks/T10.8.md",
        "docs/tasks/T10.9.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.7.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-076",
      "name": "Restore into an isolated target",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.1",
        "T10.10",
        "T10.11",
        "T10.15",
        "T10.17",
        "T8.17",
        "T8.20",
        "T8.24",
        "T8.25",
        "T8.26"
      ],
      "contract_refs": [
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.10.md",
        "docs/tasks/T10.11.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T10.17.md",
        "docs/tasks/T8.17.md",
        "docs/tasks/T8.20.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T8.25.md",
        "docs/tasks/T8.26.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-077",
      "name": "Recover an unsuccessful release",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.12",
        "T10.15",
        "T7.12",
        "T8.13",
        "T8.24",
        "T9.12"
      ],
      "contract_refs": [
        "docs/tasks/T10.12.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T7.12.md",
        "docs/tasks/T8.13.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T9.12.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-078",
      "name": "Operate within declared availability and cost limits",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.1",
        "T10.14",
        "T10.15",
        "T10.16",
        "T10.6",
        "T10.8",
        "T10.9",
        "T8.1",
        "T8.24",
        "T8.25"
      ],
      "contract_refs": [
        "docs/tasks/T10.1.md",
        "docs/tasks/T10.14.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T10.16.md",
        "docs/tasks/T10.6.md",
        "docs/tasks/T10.8.md",
        "docs/tasks/T10.9.md",
        "docs/tasks/T8.1.md",
        "docs/tasks/T8.24.md",
        "docs/tasks/T8.25.md"
      ],
      "interfaces": [
        "document",
        "telemetry"
      ],
      "required_evidence": [
        "behavior",
        "cloud",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-079",
      "name": "Rotate secrets and export diagnostic evidence",
      "source_status": "PLANNED",
      "task_ids": [
        "T10.13",
        "T10.14",
        "T10.15",
        "T10.17",
        "T10.3"
      ],
      "contract_refs": [
        "docs/tasks/T10.13.md",
        "docs/tasks/T10.14.md",
        "docs/tasks/T10.15.md",
        "docs/tasks/T10.17.md",
        "docs/tasks/T10.3.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-080",
      "name": "Discover only eligible MCP tools",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.1",
        "T11.13",
        "T11.2",
        "T11.3",
        "T11.4"
      ],
      "contract_refs": [
        "docs/tasks/T11.1.md",
        "docs/tasks/T11.13.md",
        "docs/tasks/T11.2.md",
        "docs/tasks/T11.3.md",
        "docs/tasks/T11.4.md"
      ],
      "interfaces": [
        "MCP"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-081",
      "name": "Invoke domain behavior with REST/MCP parity",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.11",
        "T11.12",
        "T11.13",
        "T11.2",
        "T11.4",
        "T11.6",
        "T12.10",
        "T12.3",
        "T12.9",
        "T15.2"
      ],
      "contract_refs": [
        "docs/tasks/T11.11.md",
        "docs/tasks/T11.12.md",
        "docs/tasks/T11.13.md",
        "docs/tasks/T11.2.md",
        "docs/tasks/T11.4.md",
        "docs/tasks/T11.6.md",
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.3.md",
        "docs/tasks/T12.9.md",
        "docs/tasks/T15.2.md"
      ],
      "interfaces": [
        "MCP",
        "REST"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-082",
      "name": "Create and manage bounded API keys",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.5",
        "T11.7",
        "T12.11",
        "T15.4"
      ],
      "contract_refs": [
        "docs/tasks/T11.5.md",
        "docs/tasks/T11.7.md",
        "docs/tasks/T12.11.md",
        "docs/tasks/T15.4.md"
      ],
      "interfaces": [
        "REST",
        "UI"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-083",
      "name": "Reject stale or overpowered keys",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.13",
        "T11.5",
        "T11.6",
        "T15.2"
      ],
      "contract_refs": [
        "docs/tasks/T11.13.md",
        "docs/tasks/T11.5.md",
        "docs/tasks/T11.6.md",
        "docs/tasks/T15.2.md"
      ],
      "interfaces": [
        "REST"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-084",
      "name": "Authorize public MCP clients",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.1",
        "T11.10",
        "T11.14",
        "T11.8",
        "T11.9",
        "T12.11",
        "T15.3"
      ],
      "contract_refs": [
        "docs/tasks/T11.1.md",
        "docs/tasks/T11.10.md",
        "docs/tasks/T11.14.md",
        "docs/tasks/T11.8.md",
        "docs/tasks/T11.9.md",
        "docs/tasks/T12.11.md",
        "docs/tasks/T15.3.md"
      ],
      "interfaces": [
        "OAuth",
        "UI"
      ],
      "required_evidence": [
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-085",
      "name": "Refresh or revoke MCP access",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.10",
        "T11.11",
        "T11.14",
        "T11.8",
        "T15.2",
        "T15.4"
      ],
      "contract_refs": [
        "docs/tasks/T11.10.md",
        "docs/tasks/T11.11.md",
        "docs/tasks/T11.14.md",
        "docs/tasks/T11.8.md",
        "docs/tasks/T15.2.md",
        "docs/tasks/T15.4.md"
      ],
      "interfaces": [
        "OAuth",
        "REST"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-086",
      "name": "Resume required human proof",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.10",
        "T11.12",
        "T11.13",
        "T12.10",
        "T12.11"
      ],
      "contract_refs": [
        "docs/tasks/T11.10.md",
        "docs/tasks/T11.12.md",
        "docs/tasks/T11.13.md",
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.11.md"
      ],
      "interfaces": [
        "MCP",
        "REST",
        "UI"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-087",
      "name": "Use independently qualified MCP clients",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.1",
        "T11.13",
        "T11.14",
        "T11.4",
        "T11.9"
      ],
      "contract_refs": [
        "docs/tasks/T11.1.md",
        "docs/tasks/T11.13.md",
        "docs/tasks/T11.14.md",
        "docs/tasks/T11.4.md",
        "docs/tasks/T11.9.md"
      ],
      "interfaces": [
        "MCP"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-088",
      "name": "Generate transport from business contract",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.3",
        "T12.1",
        "T12.12",
        "T12.2"
      ],
      "contract_refs": [
        "docs/tasks/T11.3.md",
        "docs/tasks/T12.1.md",
        "docs/tasks/T12.12.md",
        "docs/tasks/T12.2.md"
      ],
      "interfaces": [
        "CLI",
        "OpenAPI"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-089",
      "name": "Compose integrated Go business handlers",
      "source_status": "PLANNED",
      "task_ids": [
        "T12.1",
        "T12.3"
      ],
      "contract_refs": [
        "docs/tasks/T12.1.md",
        "docs/tasks/T12.3.md"
      ],
      "interfaces": [
        "Go",
        "REST"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-090",
      "name": "Proxy to privately reachable service",
      "source_status": "PLANNED",
      "task_ids": [
        "T12.1",
        "T12.4",
        "T12.5",
        "T12.6",
        "T12.7",
        "T12.8"
      ],
      "contract_refs": [
        "docs/tasks/T12.1.md",
        "docs/tasks/T12.4.md",
        "docs/tasks/T12.5.md",
        "docs/tasks/T12.6.md",
        "docs/tasks/T12.7.md",
        "docs/tasks/T12.8.md"
      ],
      "interfaces": [
        "HTTP"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-091",
      "name": "Reject proxy boundary bypass",
      "source_status": "PLANNED",
      "task_ids": [
        "T12.4",
        "T12.5",
        "T12.6",
        "T12.8",
        "T15.1",
        "T15.2",
        "T15.3",
        "T15.5"
      ],
      "contract_refs": [
        "docs/tasks/T12.4.md",
        "docs/tasks/T12.5.md",
        "docs/tasks/T12.6.md",
        "docs/tasks/T12.8.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.2.md",
        "docs/tasks/T15.3.md",
        "docs/tasks/T15.5.md"
      ],
      "interfaces": [
        "HTTP",
        "Network"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-092",
      "name": "Publish fully replaced account UI",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.12",
        "T12.10",
        "T12.11"
      ],
      "contract_refs": [
        "docs/tasks/T11.12.md",
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.11.md"
      ],
      "interfaces": [
        "Browser",
        "REST"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-093",
      "name": "Preserve side effects across retries",
      "source_status": "PLANNED",
      "task_ids": [
        "T12.3",
        "T12.7",
        "T12.9"
      ],
      "contract_refs": [
        "docs/tasks/T12.3.md",
        "docs/tasks/T12.7.md",
        "docs/tasks/T12.9.md"
      ],
      "interfaces": [
        "MCP",
        "REST"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-094",
      "name": "Upgrade generated contracts safely",
      "source_status": "PLANNED",
      "task_ids": [
        "T12.12",
        "T12.2",
        "T14.11"
      ],
      "contract_refs": [
        "docs/tasks/T12.12.md",
        "docs/tasks/T12.2.md",
        "docs/tasks/T14.11.md"
      ],
      "interfaces": [
        "CLI"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-095",
      "name": "Qualify unsupported operation shapes",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.2",
        "T11.3",
        "T12.12",
        "T12.2",
        "T12.7",
        "T15.5"
      ],
      "contract_refs": [
        "docs/tasks/T11.2.md",
        "docs/tasks/T11.3.md",
        "docs/tasks/T12.12.md",
        "docs/tasks/T12.2.md",
        "docs/tasks/T12.7.md",
        "docs/tasks/T15.5.md"
      ],
      "interfaces": [
        "MCP",
        "OpenAPI"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-096",
      "name": "Configure owner-hosted maintenance",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.1",
        "T13.12",
        "T13.13",
        "T13.2",
        "T13.6"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.12.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.2.md",
        "docs/tasks/T13.6.md"
      ],
      "interfaces": [
        "CLI",
        "Config"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-097",
      "name": "Create bounded diagnosis from observations",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.1",
        "T13.13",
        "T13.3",
        "T13.4"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.3.md",
        "docs/tasks/T13.4.md"
      ],
      "interfaces": [
        "Event"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-098",
      "name": "Run isolated private coding job",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.1",
        "T13.13",
        "T13.2",
        "T13.5",
        "T13.6",
        "T13.7",
        "T15.11",
        "T15.7"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.2.md",
        "docs/tasks/T13.5.md",
        "docs/tasks/T13.6.md",
        "docs/tasks/T13.7.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.7.md"
      ],
      "interfaces": [
        "Runner"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-099",
      "name": "Verify independently of proposed patch",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.1",
        "T13.13",
        "T13.8",
        "T13.9",
        "T15.11",
        "T15.8"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.8.md",
        "docs/tasks/T13.9.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.8.md"
      ],
      "interfaces": [
        "Runner"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-100",
      "name": "Promote eligible repair automatically",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.1",
        "T13.10",
        "T13.11",
        "T13.13",
        "T13.4",
        "T13.9",
        "T15.13",
        "T15.9"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.10.md",
        "docs/tasks/T13.11.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.4.md",
        "docs/tasks/T13.9.md",
        "docs/tasks/T15.13.md",
        "docs/tasks/T15.9.md"
      ],
      "interfaces": [
        "Release"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-101",
      "name": "Pause, recover and resume safely",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.10",
        "T13.11",
        "T13.12",
        "T13.13",
        "T13.4",
        "T13.6",
        "T15.12",
        "T15.13",
        "T15.14"
      ],
      "contract_refs": [
        "docs/tasks/T13.10.md",
        "docs/tasks/T13.11.md",
        "docs/tasks/T13.12.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.4.md",
        "docs/tasks/T13.6.md",
        "docs/tasks/T15.12.md",
        "docs/tasks/T15.13.md",
        "docs/tasks/T15.14.md"
      ],
      "interfaces": [
        "CLI"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-102",
      "name": "Retain private fixes and evidence",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.13",
        "T13.5",
        "T13.7"
      ],
      "contract_refs": [
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.5.md",
        "docs/tasks/T13.7.md"
      ],
      "interfaces": [
        "Repository"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-103",
      "name": "Continue service during maintenance outage",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.12",
        "T13.13",
        "T13.2",
        "T13.3",
        "T14.3",
        "T15.14"
      ],
      "contract_refs": [
        "docs/tasks/T13.12.md",
        "docs/tasks/T13.13.md",
        "docs/tasks/T13.2.md",
        "docs/tasks/T13.3.md",
        "docs/tasks/T14.3.md",
        "docs/tasks/T15.14.md"
      ],
      "interfaces": [
        "HTTP"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-104",
      "name": "Inspect and disable sanitized reporting",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.1",
        "T14.12",
        "T14.2",
        "T14.3",
        "T15.4"
      ],
      "contract_refs": [
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.2.md",
        "docs/tasks/T14.3.md",
        "docs/tasks/T15.4.md"
      ],
      "interfaces": [
        "CLI"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-105",
      "name": "Receive bounded sanitized diagnostics",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.1",
        "T14.12",
        "T14.13",
        "T14.14",
        "T14.2",
        "T14.4",
        "T14.5",
        "T15.11",
        "T15.5"
      ],
      "contract_refs": [
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.13.md",
        "docs/tasks/T14.14.md",
        "docs/tasks/T14.2.md",
        "docs/tasks/T14.4.md",
        "docs/tasks/T14.5.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.5.md"
      ],
      "interfaces": [
        "HTTP"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-106",
      "name": "Create public synthetic reproduction",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.12",
        "T14.6",
        "T14.9",
        "T15.11"
      ],
      "contract_refs": [
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.6.md",
        "docs/tasks/T14.9.md",
        "docs/tasks/T15.11.md"
      ],
      "interfaces": [
        "Artifact"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-107",
      "name": "Route suspected vulnerabilities privately",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.12",
        "T14.4",
        "T14.5",
        "T14.7"
      ],
      "contract_refs": [
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.4.md",
        "docs/tasks/T14.5.md",
        "docs/tasks/T14.7.md"
      ],
      "interfaces": [
        "Process"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-108",
      "name": "Repair shared AMOS code independently",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.12",
        "T14.6",
        "T14.8",
        "T14.9",
        "T15.10"
      ],
      "contract_refs": [
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.6.md",
        "docs/tasks/T14.8.md",
        "docs/tasks/T14.9.md",
        "docs/tasks/T15.10.md"
      ],
      "interfaces": [
        "Runner"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-109",
      "name": "Contribute demonstrated AMSL mechanism gaps",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.10",
        "T14.11",
        "T14.12",
        "T14.6"
      ],
      "contract_refs": [
        "docs/tasks/T14.10.md",
        "docs/tasks/T14.11.md",
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.6.md"
      ],
      "interfaces": [
        "Process"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-110",
      "name": "Adopt upstream repair through upgrade PR",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.11",
        "T14.12"
      ],
      "contract_refs": [
        "docs/tasks/T14.11.md",
        "docs/tasks/T14.12.md"
      ],
      "interfaces": [
        "Repository"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-111",
      "name": "Delete or expire reporting data",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.1",
        "T14.12",
        "T14.3",
        "T14.4",
        "T14.5"
      ],
      "contract_refs": [
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.12.md",
        "docs/tasks/T14.3.md",
        "docs/tasks/T14.4.md",
        "docs/tasks/T14.5.md"
      ],
      "interfaces": [
        "CLI",
        "HTTP"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-112",
      "name": "Review application and maintenance threat boundaries",
      "source_status": "PLANNED",
      "task_ids": [
        "T12.4",
        "T14.7",
        "T15.1",
        "T15.15",
        "T15.3",
        "T15.4"
      ],
      "contract_refs": [
        "docs/tasks/T12.4.md",
        "docs/tasks/T14.7.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.15.md",
        "docs/tasks/T15.3.md",
        "docs/tasks/T15.4.md"
      ],
      "interfaces": [
        "Document"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-113",
      "name": "Deny tenant and authority confusion",
      "source_status": "PLANNED",
      "task_ids": [
        "T11.11",
        "T11.6",
        "T11.8",
        "T12.10",
        "T12.6",
        "T15.1",
        "T15.15",
        "T15.2",
        "T15.3"
      ],
      "contract_refs": [
        "docs/tasks/T11.11.md",
        "docs/tasks/T11.6.md",
        "docs/tasks/T11.8.md",
        "docs/tasks/T12.10.md",
        "docs/tasks/T12.6.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.15.md",
        "docs/tasks/T15.2.md",
        "docs/tasks/T15.3.md"
      ],
      "interfaces": [
        "Browser",
        "MCP",
        "REST"
      ],
      "required_evidence": [
        "api",
        "behavior",
        "browser",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-114",
      "name": "Enforce protected change policy",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.1",
        "T13.7",
        "T13.8",
        "T13.9",
        "T14.8",
        "T14.9",
        "T15.1",
        "T15.11",
        "T15.12",
        "T15.15",
        "T15.6",
        "T15.7",
        "T15.8",
        "T15.9"
      ],
      "contract_refs": [
        "docs/tasks/T13.1.md",
        "docs/tasks/T13.7.md",
        "docs/tasks/T13.8.md",
        "docs/tasks/T13.9.md",
        "docs/tasks/T14.8.md",
        "docs/tasks/T14.9.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.12.md",
        "docs/tasks/T15.15.md",
        "docs/tasks/T15.6.md",
        "docs/tasks/T15.7.md",
        "docs/tasks/T15.8.md",
        "docs/tasks/T15.9.md"
      ],
      "interfaces": [
        "Policy"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-115",
      "name": "Verify reproducible release provenance",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.10",
        "T13.9",
        "T14.9",
        "T15.10",
        "T15.12",
        "T15.15",
        "T15.6",
        "T15.9"
      ],
      "contract_refs": [
        "docs/tasks/T13.10.md",
        "docs/tasks/T13.9.md",
        "docs/tasks/T14.9.md",
        "docs/tasks/T15.10.md",
        "docs/tasks/T15.12.md",
        "docs/tasks/T15.15.md",
        "docs/tasks/T15.6.md",
        "docs/tasks/T15.9.md"
      ],
      "interfaces": [
        "Artifact"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-116",
      "name": "Treat third-party proposals as untrusted",
      "source_status": "PLANNED",
      "task_ids": [
        "T14.8",
        "T15.10",
        "T15.15",
        "T15.6",
        "T15.7"
      ],
      "contract_refs": [
        "docs/tasks/T14.8.md",
        "docs/tasks/T15.10.md",
        "docs/tasks/T15.15.md",
        "docs/tasks/T15.6.md",
        "docs/tasks/T15.7.md"
      ],
      "interfaces": [
        "CI"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-117",
      "name": "Withstand injected instructions and exfiltration",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.5",
        "T13.8",
        "T14.1",
        "T14.2",
        "T14.4",
        "T14.6",
        "T15.1",
        "T15.10",
        "T15.11",
        "T15.15",
        "T15.4",
        "T15.5",
        "T15.7",
        "T15.8"
      ],
      "contract_refs": [
        "docs/tasks/T13.5.md",
        "docs/tasks/T13.8.md",
        "docs/tasks/T14.1.md",
        "docs/tasks/T14.2.md",
        "docs/tasks/T14.4.md",
        "docs/tasks/T14.6.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.10.md",
        "docs/tasks/T15.11.md",
        "docs/tasks/T15.15.md",
        "docs/tasks/T15.4.md",
        "docs/tasks/T15.5.md",
        "docs/tasks/T15.7.md",
        "docs/tasks/T15.8.md"
      ],
      "interfaces": [
        "Runner"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-118",
      "name": "Recover compatible data and code state",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.11",
        "T15.1",
        "T15.12",
        "T15.13",
        "T15.15"
      ],
      "contract_refs": [
        "docs/tasks/T13.11.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.12.md",
        "docs/tasks/T15.13.md",
        "docs/tasks/T15.15.md"
      ],
      "interfaces": [
        "CLI",
        "Release"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-119",
      "name": "Pause fleet and qualify recovery authority",
      "source_status": "PLANNED",
      "task_ids": [
        "T13.12",
        "T15.1",
        "T15.12",
        "T15.14",
        "T15.15"
      ],
      "contract_refs": [
        "docs/tasks/T13.12.md",
        "docs/tasks/T15.1.md",
        "docs/tasks/T15.12.md",
        "docs/tasks/T15.14.md",
        "docs/tasks/T15.15.md"
      ],
      "interfaces": [
        "CLI",
        "Process"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-120",
      "name": "Verify a complete paid user journey",
      "source_status": "PLANNED",
      "task_ids": [
        "T16.2",
        "T16.3",
        "T16.6",
        "T16.7",
        "T2.5"
      ],
      "contract_refs": [
        "docs/tasks/T16.2.md",
        "docs/tasks/T16.3.md",
        "docs/tasks/T16.6.md",
        "docs/tasks/T16.7.md",
        "docs/tasks/T2.5.md"
      ],
      "interfaces": [
        "web"
      ],
      "required_evidence": [
        "behavior",
        "browser",
        "implementation",
        "provider"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-121",
      "name": "Deploy and observe a qualified release",
      "source_status": "PLANNED",
      "task_ids": [
        "T16.4"
      ],
      "contract_refs": [
        "docs/tasks/T16.4.md"
      ],
      "interfaces": [
        "cli",
        "web"
      ],
      "required_evidence": [
        "behavior",
        "browser",
        "client",
        "cloud",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-122",
      "name": "Recover a released application",
      "source_status": "PLANNED",
      "task_ids": [
        "T16.5"
      ],
      "contract_refs": [
        "docs/tasks/T16.5.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-123",
      "name": "Rehearse the reference application",
      "source_status": "PLANNED",
      "task_ids": [
        "T16.2",
        "T2.6",
        "T2.7"
      ],
      "contract_refs": [
        "docs/tasks/T16.2.md",
        "docs/tasks/T2.6.md",
        "docs/tasks/T2.7.md"
      ],
      "interfaces": [
        "docs"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-124",
      "name": "Adopt a release with accurate support claims",
      "source_status": "PLANNED",
      "task_ids": [
        "T16.1",
        "T16.12",
        "T16.6",
        "T16.7",
        "T16.8",
        "T16.9"
      ],
      "contract_refs": [
        "docs/tasks/T16.1.md",
        "docs/tasks/T16.12.md",
        "docs/tasks/T16.6.md",
        "docs/tasks/T16.7.md",
        "docs/tasks/T16.8.md",
        "docs/tasks/T16.9.md"
      ],
      "interfaces": [
        "docs"
      ],
      "required_evidence": [
        "behavior",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    },
    {
      "id": "UC-125",
      "name": "Verify autonomous private and upstream repair",
      "source_status": "PLANNED",
      "task_ids": [
        "T16.10",
        "T16.11"
      ],
      "contract_refs": [
        "docs/tasks/T16.10.md",
        "docs/tasks/T16.11.md"
      ],
      "interfaces": [
        "cli"
      ],
      "required_evidence": [
        "behavior",
        "client",
        "implementation"
      ],
      "claim": "not_claimed",
      "states": {
        "implemented": "incomplete",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "evidence": []
    }
  ],
  "profiles": [
    {
      "id": "aws_managed",
      "name": "Managed containers with managed PostgreSQL",
      "claim": "not_claimed",
      "states": {
        "implemented": "not_established",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "required_evidence": [
        "behavior",
        "cloud",
        "recovery"
      ],
      "evidence": []
    },
    {
      "id": "aws_vm",
      "name": "One VM hosting the application and PostgreSQL",
      "claim": "not_claimed",
      "states": {
        "implemented": "not_established",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "required_evidence": [
        "behavior",
        "cloud",
        "recovery"
      ],
      "evidence": []
    },
    {
      "id": "cloudflare_routing",
      "name": "Public DNS and same-domain routing",
      "claim": "not_claimed",
      "states": {
        "implemented": "not_established",
        "tested": "not_release_reviewed",
        "qualified": "not_qualified",
        "deployed": "not_deployed",
        "rehearsed": "not_rehearsed"
      },
      "required_evidence": [
        "behavior",
        "cloud",
        "network"
      ],
      "evidence": []
    }
  ],
  "contract_version": "amos-contract-draft-1"
}
```
<!-- release-matrix-data:end -->
