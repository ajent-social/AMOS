ALTER TABLE identity_auth_limits DROP CONSTRAINT identity_auth_limits_operation_check;
ALTER TABLE identity_auth_limits ADD CONSTRAINT identity_auth_limits_operation_check CHECK (
 operation IN ('signup','signin','verification','recovery','password_change','magic_link','mfa'));
