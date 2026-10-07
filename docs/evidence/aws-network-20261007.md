# AWS VM network and host identity source evidence

The bounded T8.4 Pulumi component was independently reviewed and merged through
[PR25](https://github.com/ajent-social/AMOS/pull/25). Reviewed head
`9336ab38b63b48784c2e639b937a44dafe1c3991` against base
`ba3565c416f912525743edd6c86c2d85dccb91ff` landed at
`64ba2c8494fdef39da72a53df2bfcb495f79ec1d`. All four source/test files and both
module files match the reviewed bytes. Original author commits remain reachable;
rebasing preserved their source bytes and all 36 pre-existing dependency versions.

The source composes a single-AZ public network and an explicitly scoped host role.
It permits HTTP/HTTPS ingress, rejects public database and SSH ingress, and creates
no NAT or load balancer. IAM inputs scope image, secret, backup, encryption and log
access. Later backup integration must preserve the documented object-prefix KMS
context and disable S3 Bucket Keys for this profile.

## Obtained local verification

At the reviewed head, these bounded checks passed:

- `go test -p=1 ./internal/infra/aws -run 'Test(Network|Identity)' -count=1`
- `go test -race -p=1 ./internal/infra/aws -count=1`
- `go vet -p=1 ./internal/infra/aws`
- `golangci-lint run --timeout=5m ./internal/infra/aws` using verified v2.13.2:
  zero issues.

A separate Astra reviewer inspected all six changed files, reran scoped tests,
confirmed effective module versions, and temporarily admitted public PostgreSQL
in the validation path. The negative test failed with the expected acceptance
error; exact source restoration and the scoped rerun passed. No blocking findings
remained at the reviewed head. The source author did not approve their own change.

Publication scanning reported 16 matches, manually classified as synthetic
IAM/CIDR/bucket fixtures and source expressions. Automated scanner success is not
claimed. Whitespace checks passed. GitHub reported no check entries, no branch
rules and an unprotected main at immediate head/base/check readback. Guarded rebase
merge matched the reviewed head; no protection or status was bypassed or fabricated.
Local verification is not hosted CI success.

Fresh `go test -p=1 ./internal/infra/aws -count=1` passed on the landed SHA
after fetched-main reachability and all six byte comparisons. T8.4 is accepted
at this source/mock component boundary only.

## Qualification boundary

Pulumi mocks prove source construction and asserted inputs only. No AWS API,
preview, provider qualification, spending, deployment, production role behavior,
backup operation or recovery qualification occurred. Those gates remain separate.
