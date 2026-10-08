# Native email verification writer

`NewWithWriter` keeps the shared root and pool-free delivery writers. Its copied
configuration requires the full installation/application/environment realm;
legacy construction retains the prior zero-environment behavior. The additional
public field requires named Config literals for source compatibility. Existing
repository callers use named fields; the legacy construction test covers them.

Issue, confirmation and registration delivery use the same ordered attempt.
Discovery reads under G do not authorize; P/C/H/W rows are acquired separately
before native state checks and staging. The bounded workspace/owner-trigger
inventory includes affected memberships and persons. Confirmation requires the
active personal workspace owned by the pending person. Pending creation remains
the login/store/personal compound operation, not a nested email transaction.

QueueExistingChallengeWriter requires the exact full realm, acquired person,
contact and challenge, original purpose/digest/expiry and rate budget. Its sealed
material ID is the challenge ID; its job key is `email-verification:` followed by
the request ID. Protected material and email intent use the existing D writers.
No mail is dispatched here. A participant failure is already terminal; its
caller returns the matching rollback outcome without another Finish.

Final comparison uses plain reads through the same retained transaction after
constraint drain and the original F sample. The original expiry remains strict.
Positive acknowledgements require the private permit and committed one-use
release. Unknown contacts and rate caps use the existing generic acknowledgement
only after a no-write rollback; dependency or completion failures do not.

Required-service source exercises actual native issue/confirmation with a fresh
reviewer-owned TLS fixture and synthetic contacts/material key. It does not
qualify a real mail provider or all lock/unique/constraint wait schedules.
Complete native composition, full selected writer schedules and product gates
remain required before host admission.
