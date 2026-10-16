# Initial local restricted-hold source

T14.7 remains IN_PROGRESS. This implements only the v1.36 local decision core;
private-channel verification, external intake and actual downstream publication
enforcement remain unavailable/unqualified. No delivery, storage or public-release
capability is implemented or implied.

Design PR141 landed `f3401d6f150c36eaf717eafae121263503a979c6` before source dispatch,
with full-tree equality to independently reviewed6796b10. Fresh bounded plan,
in-memory projection and changed-document scan checks passed after landing.

The initial core accepts only finite reporter/analysis/signal observations. All
unreviewed inputs, including reported non-security findings, stay held. A routine
reporter label cannot downgrade an authentication signal. Invalid/unknown/failed
analysis cannot grant public automation or make private handling available.
Decision state is private; its zero value holds. Closed JSON contains only finite
observations and never echoes input. The component has no sender, channel callback,
raw diagnostic field, file writer, network client, command or publisher.

Source07173af received independent approval. A subsequent test-only change at
319dda4 replaces unsupported JSON decoding into private state with a direct
immutability check; production bytes are unchanged. Fresh scoped Go1.27.1 race
checks passed29 named tests/subtests, zero failures/skips. Vet, pinned lint and
changed-source public-artifact scan passed; lint has zero issues. These are local
Linux checks, not hosted CI, a whole-repository run or deployed routing acceptance.

In an isolated compiled mutation, a routine reporter label granted public
automation even with an authentication signal. The required regression produced
RED with public=true, then passed targeted race checks after exact restoration.
Restored production SHA256 for internal/vulnerabilityroute/route.go is
`b58a49e93e6fd20df87de07413e310bd2d851ff1e9d89ea6618bdcdde8c1d75f`.
Tests also cover all15 label/analysis combinations, all9 signals, duplicate/unknown/
oversized input, canonical output bounds, unknown internal reasons, zero values,
no input retention and concurrent decisions.

Independent final review approved703d8c46d2dfc62a0eeef63771b48e48229bbffc.
PR142 guarded-rebase merged83199575ff11a313f6960be8e8058d1be75bfc2d; its complete
tree equals that reviewed head. Fresh isolated landed race checks passed29 named
checks with zero failures/skips and clean checkout.
No task acceptance follows from this local core; the verified private-channel
prerequisite and downstream wiring are separate. Retained plan semantics cover
all1,598 tasks; generated projections are in-memory checks only.

Requested batch ceiling10; actual member1, T14.7 partial local source with accepted
T14.1/T15.1 dependencies. Root implements the bounded component and a separate
inherited-session reviewer supplies independent assessment. No model/effort
change was requested; cost/token totals are unavailable. The original claim stays
owned until this reviewed source handoff lands, then is released without accepting
the full task. [Follow-up143](https://github.com/ajent-social/AMOS/issues/143) tracks
private handling and actual downstream integration. Existing LAN preview, persistent data and private configuration are unchanged.
