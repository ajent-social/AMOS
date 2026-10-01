# Workspace switch UI component: 2026-10-01

Source batch `f941a53`, copied from reviewed `b7ac8e7`, adds the replaceable
workspace switch handler and its unit and future browser checks.

## Obtained evidence

- Independent headless review cleared stale-hint remediation and unauthorized-response findings.
- Fresh isolated `go test -race ./ui/workspaceswitch -count=1`, scoped vet and pinned lint passed; lint reported zero issues.
- Genuine old-error-guard mutations disclosed foreign DTO fields and failed their regressions; byte-identical restoration and positive checks passed.
- Pinned Prettier 3.9.9, public artifact and diff checks passed.
- Stale selection errors discard their DTO, clear the hint cookie and query current choices without a hint. Only a successful validated current result renders; subsequent errors disclose no workspace fields.

## Remaining gates

The browser spec remains NOT_RUN because the composed organization fixture is
absent. It allows either authorization denial or non-enumerating not-found
responses. Real organization membership/selection adapters and native host
composition remain separate. T6.4 stays in progress; this is standalone UI
component acceptance, not full task, organization, SaaS or deployment qualification.
