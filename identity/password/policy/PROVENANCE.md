# Local password policy provenance

Version: `amos-common-100k-20261001`. This is a bounded common-password snapshot,
not a claim to cover all compromised credentials or qualify security maturity.

Public source: [SecLists Xato top 100,000 common passwords](https://github.com/danielmiessler/SecLists/blob/master/Passwords/Common-Credentials/xato-net-10-million-passwords-100000.txt),
retrieved 2026-10-01. Source Git blob SHA-1:
`fa9a7c2e9f930c99b125aa96b7d87b3b9fcbe333`; exact source bytes SHA-256:
`1472aafa2561df5e3293aee252aee3ca660c12b399a283cf808bb01b39be388b`.
Repository [MIT license](https://github.com/danielmiessler/SecLists/blob/master/LICENSE)
notice is preserved in the root third-party inventory.

Conversion: decode UTF-8, split lines, NFC-normalize each phrase, retain phrases
with 15–128 Unicode code points and at most 512 UTF-8 bytes (the hasher's accepted
bounds), SHA-256 UTF-8, deduplicate and sort lowercase hexadecimal digests, append
one LF per row. The 100,000-row snapshot yields 72 eligible distinct phrases;
shorter entries are already rejected by the hasher's minimum length. This
filtering does not claim broader breached-password coverage. The committed digest
asset has SHA-256
`a0802be3bba37cde6697251c2a92cf2a9e66a4f4a5575a626d70b6a642db6493`.

The adapter validates the exact asset checksum before becoming ready and performs
local membership checks only. Installation owners can add reviewed normalized
phrase digests with an explicit exact-byte checksum. Supplemental inputs are
bounded and malformed inputs fail startup. No password is transmitted to a
remote lookup, logged or added to the public source tree. Updating the snapshot,
adding application-specific terms and obtaining broader compromised-credential
coverage remain explicit owner policy work.
