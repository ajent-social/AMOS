# ADR045: declare upstream diagnostic infrastructure separately

Status: proposed; adopted after independent design/schema review and landing.

Adopt [upstream configurationv1](../contracts/upstream-service.md) as amendment
v1.31 for T14.13. The initial supported declaration profile uses separate upstream
and declared consumer accounts, distinct role purposes/namespaces, private
normalized quarantine, finite intake/storage/privacy budgets and default-off
collection. It selects no real account, resources, credentials or deployment.
Same-account hosting and noncommercial partitions remain unsupported profiles.

The validator rejects role/account/purpose contradictions, consumer operator
reuse, public/raw storage and unknown trust/credential fields. Configuration
proves requested declarations only: authoritative inventory completeness and
actual IAM/state/KMS/bucket/network/operator enforcement need later qualification.
Different ARN strings or a caller approval flag do not establish effective
separation. No resources, intake, sender or operator login are created here.
