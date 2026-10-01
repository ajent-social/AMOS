-- Magic links may bind the requesting browser without storing its raw cookie.
ALTER TABLE identity_challenges ADD COLUMN browser_binding_digest bytea;
ALTER TABLE identity_challenges ADD CONSTRAINT identity_magic_browser_binding CHECK (
 browser_binding_digest IS NULL OR (purpose='email_magic_link' AND octet_length(browser_binding_digest)=32)
);

ALTER TABLE identity_auth_limits DROP CONSTRAINT identity_auth_limits_operation_check;
ALTER TABLE identity_auth_limits ADD CONSTRAINT identity_auth_limits_operation_check CHECK (operation IN ('signup','signin','verification','recovery','password_change','magic_link'));
