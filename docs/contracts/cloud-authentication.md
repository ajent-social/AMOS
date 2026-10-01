# Cloud authentication references

Status: proposed implementation contract for review. This file does not authorize AWS or Cloudflare account access, spending, deployment, or production use.

## Document boundary

`internal/cloudconfig` accepts one bounded YAML or JSON authentication document. The decoder recognizes only the typed fields in `AuthenticationDocument`, rejects unknown fields and trailing documents, and limits input to 64 KiB. The document stores identity references and role ARNs; it has no fields for resolved AWS keys, Cloudflare token values, database passwords, session keys, webhook secrets, or other application runtime secrets. Secret material is supplied to consuming processes through environment references and is never copied into this document or its diagnostics.

AWS identity selects exactly one source and one role through `credentialRole`:

- A named shared AWS profile for local owner use. The profile may use AWS IAM Identity Center (SSO) and its normal local token cache. The AWS SDK v2 loads that source profile and assumes only the selected `bootstrap` or `preview` role ARN from the document.
- A CI OIDC source plus an `env://NAME` reference whose value is the runner's short-lived OIDC token file path. It may select only the `preview` or `deploy` role ARN from the document. The AWS SDK v2 `stscreds` web-identity provider reads that file when credentials are retrieved. The OIDC audience is a trust-policy condition and must match the CI issuer configuration. No ad hoc CI role ARN, token, or resolved credential value is serialized.

The loader fails closed when static `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, or `AWS_SESSION_TOKEN` environment values are present, or when the selected shared profile or its source profile contains static keys. Production documents require CI OIDC; local profile/SSO is limited to nonproduction use. Configuration fields that could carry static key values are unknown and rejected. Missing profile, OIDC token file, or valid SDK credentials are errors; there is no anonymous fallback. AWS account ID and region are explicit and every configured IAM role ARN must name that account and carry the configured environment suffix. The loader cannot select host or recovery as its routine credential role; those are attached or invoked by their respective controlled paths. Actual AWS identity, OIDC trust, and role permissions still require separate provider qualification.

Cloudflare DNS receives a per-environment API token through a secret environment input such as `env://AMOS_CF_STAGING_DNS_TOKEN`. The token is scoped to exactly the configured Cloudflare zone and grants `Zone:DNS:Edit` plus `Zone:Read` for zone verification. Account-wide administration, zone writes outside the selected zone, global API keys, and proxy-setting permission are not part of this contract. The value is never logged or persisted. Production and nonproduction use separately issued tokens and secret-store entries.

## Role separation and permissions

Role ARNs are unique and environment-specific. AWS trust and permission policies must also bind the installation/environment tags, target account, region, and resource names; naming convention validation alone is not an IAM security boundary.

| Identity | Allowed purpose and permissions | Explicitly excluded |
| --- | --- | --- |
| Bootstrap | Owner-operated initial setup of deployment roles, CI OIDC trust, state bucket, KMS key policy, and permission boundaries. Requires an owner-controlled, short-lived local/SSO session and separate review. | Routine CI apply, app runtime, use as preview/deploy/recovery identity, unrestricted IAM administration after bootstrap. |
| Preview | Read provider inventory/state and calculate a proposed change for this environment. State read/decrypt may be granted only as required by the protected preview runner. | Resource mutation, IAM/trust mutation, Cloudflare token, runtime application secret access, another environment. |
| Deploy | Mutate only the reviewed profile resources in its environment and region; pass only the designated host/task role; write that environment's protected Pulumi state and encrypt with its configured KMS key. | Bootstrap/IAM policy authoring, role creation, recovery key administration, cross-environment state or resources, runtime app credentials. |
| Host instance | Minimum execution needs: retrieve its own image, write its own logs, and read only explicitly assigned runtime secret references. Any data access is limited to the running AMOS installation. | Pulumi state, deploy APIs, Cloudflare DNS, OIDC administration, KMS key-policy changes, other installations' secret values. |
| Recovery | Independently controlled break-glass path to read/version-recover encrypted state and invoke approved database/storage recovery procedures. Its KMS decrypt grant is separate from deploy and requires owner-controlled MFA/approval and audit. | Routine preview/deploy, editing KMS policy, application runtime, Cloudflare DNS, unattended CI use. |

For shared AWS accounts, role trust conditions must require the matching environment principal/session tag and resource policies must carry the same environment tag. Where environments use separate accounts, each document's account ID and role ARNs must resolve only within that account. CI OIDC trust binds exact repository/workflow/branch or protected environment claims and a fixed audience; pull request code from untrusted forks receives no AWS, Cloudflare, or state credentials. The deploy role cannot assume bootstrap, host, or recovery roles.

## Pulumi state and recovery

Production state uses Pulumi's KMS secrets provider backed by a customer-managed KMS key in the selected AWS account and region. The KMS key has rotation and deletion protection, audited use, and an explicit key policy. Deploy can use the key only for its own stack's state encryption/decryption; it cannot administer the key policy or schedule deletion. The independently named recovery role is explicitly configured, can decrypt the same state after an owner-approved recovery event, and is not the deploy role. State resides in an owner-controlled private, versioned, encrypted S3 backend with access logging and retention controls; exact bucket and key references are assigned by the integration/deployment task.

`local-passphrase` mode is allowed only when the document environment is `development`. The passphrase comes from a local secret environment reference, is not emitted to generated files, and must not be copied into CI artifacts or deployed state. Staging and production require KMS mode. Local passphrase mode is not a production fallback when KMS is unavailable.

## Example shape

The values below are intentionally synthetic and nonfunctional:

```yaml
version: 1
environment: staging
aws:
  accountId: "000000000000"
  region: us-east-1
  credentialRole: preview
  profile: amos-staging
roles:
  bootstrap: arn:aws:iam::000000000000:role/amos-bootstrap-staging
  preview: arn:aws:iam::000000000000:role/amos-preview-staging
  deploy: arn:aws:iam::000000000000:role/amos-deploy-staging
  host: arn:aws:iam::000000000000:role/amos-host-staging
  recovery: arn:aws:iam::000000000000:role/amos-recovery-staging
cloudflare:
  zoneId: 0123456789abcdef0123456789abcdef
  tokenEnv: env://AMOS_CF_STAGING_DNS_TOKEN
  permissions: [dns:edit, zone:read]
pulumiState:
  encryptionMode: kms
  kmsKeyArn: arn:aws:kms:us-east-1:000000000000:key/01234567-89ab-cdef-0123-456789abcdef
  recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-staging
```

For production CI, replace `profile` with `credentialRole: deploy` and `oidc: { tokenFileEnv: env://ACTIONS_ID_TOKEN_FILE, audience: sts.amazonaws.com }`; the SDK assumes the document's environment-bound deploy role.

Any added application runtime secret such as `databasePassword`, `sessionKey`, or `webhookSecret`, any resolved token/key field, a static AWS credential field, unknown field, cross-environment role ARN, broad Cloudflare permission, duplicate role ARN, oversized document, or second YAML document is rejected. This strict configuration boundary is not a guarantee that downstream IAM, secret-store, Cloudflare, or Pulumi resources are correctly provisioned. No live AWS or Cloudflare validation is claimed.
