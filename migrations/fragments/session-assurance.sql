-- Optional durable assurance metadata. Base session lifetime remains independent.
ALTER TABLE identity_sessions ADD COLUMN assurance_level text NOT NULL DEFAULT 'aal1';
ALTER TABLE identity_sessions ADD COLUMN assurance_expires_at timestamptz;
ALTER TABLE identity_sessions ADD CONSTRAINT identity_session_assurance CHECK (
 (assurance_level='aal1' AND assurance_expires_at IS NULL) OR
 (assurance_level IN ('aal2','aal3') AND assurance_expires_at IS NOT NULL AND
  assurance_expires_at>authenticated_at AND assurance_expires_at<=expires_at AND
  assurance_expires_at<=authenticated_at+interval '15 minutes'));
ALTER TABLE identity_sessions DROP CONSTRAINT identity_sessions_authentication_method_check;
ALTER TABLE identity_sessions ADD CONSTRAINT identity_sessions_authentication_method_check CHECK (
 authentication_method IN ('email_password','email_magic_link','google','github','apple','passkey','enterprise_oidc','password+totp'));
