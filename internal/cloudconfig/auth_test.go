package cloudconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validProfileDocument = `version: 1
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
`

const validOIDCDocument = `version: 1
environment: production
aws:
  accountId: "000000000000"
  region: us-east-1
  credentialRole: deploy
  oidc:
    tokenFileEnv: env://ACTIONS_ID_TOKEN_FILE
    audience: sts.amazonaws.com
roles:
  bootstrap: arn:aws:iam::000000000000:role/amos-bootstrap-production
  preview: arn:aws:iam::000000000000:role/amos-preview-production
  deploy: arn:aws:iam::000000000000:role/amos-deploy-production
  host: arn:aws:iam::000000000000:role/amos-host-production
  recovery: arn:aws:iam::000000000000:role/amos-recovery-production
cloudflare:
  zoneId: 0123456789abcdef0123456789abcdef
  tokenEnv: env://AMOS_CF_PRODUCTION_DNS_TOKEN
  permissions: [dns:edit, zone:read]
pulumiState:
  encryptionMode: kms
  kmsKeyArn: arn:aws:kms:us-east-1:000000000000:key/01234567-89ab-cdef-0123-456789abcdef
  recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-production
`

func TestAuthentication(t *testing.T) {
	t.Run("accepts profile and OIDC identities", func(t *testing.T) {
		profile, err := DecodeAuthentication(strings.NewReader(validProfileDocument))
		if err != nil || profile.AWS.Profile != "amos-staging" {
			t.Fatalf("profile document rejected: profile=%q err=%v", profile.AWS.Profile, err)
		}
		federated, err := DecodeAuthentication(strings.NewReader(validOIDCDocument))
		if err != nil || federated.AWS.OIDC == nil {
			t.Fatalf("OIDC document rejected: oidc=%v err=%v", federated.AWS.OIDC != nil, err)
		}
	})

	t.Run("loads SDK config only through selected source", func(t *testing.T) {
		doc, err := DecodeAuthentication(strings.NewReader(validOIDCDocument))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := doc.loadAWSConfig(context.Background(), func(key string) (string, bool) {
			if key == "ACTIONS_ID_TOKEN_FILE" {
				return "/tmp/synthetic-oidc-token", true
			}
			return "", false
		})
		if err != nil || cfg.Region != "us-east-1" || cfg.Credentials == nil {
			t.Fatalf("AWS SDK OIDC config not resolved: region=%q credentials=%t err=%v", cfg.Region, cfg.Credentials != nil, err)
		}
		_, err = doc.loadAWSConfig(context.Background(), func(string) (string, bool) { return "synthetic-static-value", true })
		if err == nil {
			t.Fatal("static AWS credential environment values were accepted")
		}
	})

	t.Run("loads a selected SSO profile without resolving live credentials", func(t *testing.T) {
		for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
			t.Setenv(key, "")
		}
		dir := t.TempDir()
		configFile := filepath.Join(dir, "config")
		credentialsFile := filepath.Join(dir, "credentials")
		config := "[sso-session amos]\nsso_start_url = https://example.invalid/start\nsso_region = us-east-1\nsso_registration_scopes = sso:account:access\n\n[profile amos-staging]\nsso_session = amos\nsso_account_id = 000000000000\nsso_role_name = ReadOnly\nregion = us-east-1\n"
		if err := os.WriteFile(configFile, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(credentialsFile, nil, 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("AWS_CONFIG_FILE", configFile)
		t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsFile)
		doc, err := DecodeAuthentication(strings.NewReader(validProfileDocument))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := doc.LoadAWSConfig(context.Background())
		if err != nil || cfg.Region != "us-east-1" || cfg.Credentials == nil {
			t.Fatalf("selected SSO profile was not loaded: region=%q credentials=%t err=%v", cfg.Region, cfg.Credentials != nil, err)
		}
	})

	t.Run("rejects static keys in the selected shared profile", func(t *testing.T) {
		for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
			t.Setenv(key, "")
		}
		dir := t.TempDir()
		configFile := filepath.Join(dir, "config")
		credentialsFile := filepath.Join(dir, "credentials")
		if err := os.WriteFile(configFile, []byte("[profile amos-staging]\nregion = us-east-1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(credentialsFile, []byte("[amos-staging]\naws_access_key_id = synthetic-key\naws_secret_access_key = synthetic-secret\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("AWS_CONFIG_FILE", configFile)
		t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsFile)
		doc, err := DecodeAuthentication(strings.NewReader(validProfileDocument))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := doc.LoadAWSConfig(context.Background()); err == nil {
			t.Fatal("static credentials in shared profile were accepted")
		}
	})

	tests := []struct {
		name string
		edit func(string) string
	}{
		{"runtime secret field", func(s string) string {
			return strings.Replace(s, "pulumiState:\n", "databasePassword: literal-app-secret\npulumiState:\n", 1)
		}},
		{"static AWS key field", func(s string) string {
			return strings.Replace(s, "  profile: amos-staging\n", "  profile: amos-staging\n  secretAccessKey: literal-static-key\n", 1)
		}},
		{"unknown field", func(s string) string {
			return strings.Replace(s, "  region: us-east-1\n", "  region: us-east-1\n  mystery: true\n", 1)
		}},
		{"cross environment role", func(s string) string {
			return strings.Replace(s, "role/amos-deploy-staging", "role/amos-deploy-production", 1)
		}},
		{"overscoped Cloudflare token", func(s string) string {
			return strings.Replace(s, "permissions: [dns:edit, zone:read]", "permissions: [account:admin, dns:edit, zone:read]", 1)
		}},
		{"cross environment Cloudflare token", func(s string) string {
			return strings.Replace(s, "env://AMOS_CF_STAGING_DNS_TOKEN", "env://AMOS_CF_PRODUCTION_DNS_TOKEN", 1)
		}},
		{"production passphrase", func(s string) string {
			s = strings.Replace(s, "environment: staging", "environment: production", 1)
			return strings.Replace(s, "encryptionMode: kms\n  kmsKeyArn: arn:aws:kms:us-east-1:000000000000:key/01234567-89ab-cdef-0123-456789abcdef\n  recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-staging\n", "encryptionMode: local-passphrase\n  passphraseEnv: env://PULUMI_CONFIG_PASSPHRASE\n", 1)
		}},
		{"staging passphrase", func(s string) string {
			return strings.Replace(s, "encryptionMode: kms\n  kmsKeyArn: arn:aws:kms:us-east-1:000000000000:key/01234567-89ab-cdef-0123-456789abcdef\n  recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-staging\n", "encryptionMode: local-passphrase\n  passphraseEnv: env://PULUMI_CONFIG_PASSPHRASE\n", 1)
		}},
		{"deploy role reused for recovery", func(s string) string {
			return strings.Replace(s, "recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-staging", "recoveryRoleArn: arn:aws:iam::000000000000:role/amos-deploy-staging", 1)
		}},
		{"cross environment recovery role", func(s string) string {
			return strings.Replace(s, "recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-staging", "recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-production", 1)
		}},
	}
	for _, tc := range tests {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if _, err := DecodeAuthentication(strings.NewReader(tc.edit(validProfileDocument))); err == nil {
				t.Fatalf("accepted %s", tc.name)
			}
		})
	}

	t.Run("allows local passphrase only outside production", func(t *testing.T) {
		local := strings.Replace(validProfileDocument, "environment: staging", "environment: development", 1)
		local = strings.ReplaceAll(local, "-staging", "-development")
		local = strings.Replace(local, "AMOS_CF_STAGING_DNS_TOKEN", "AMOS_CF_DEVELOPMENT_DNS_TOKEN", 1)
		local = strings.Replace(local, "encryptionMode: kms\n  kmsKeyArn: arn:aws:kms:us-east-1:000000000000:key/01234567-89ab-cdef-0123-456789abcdef\n  recoveryRoleArn: arn:aws:iam::000000000000:role/amos-recovery-staging\n", "encryptionMode: local-passphrase\n  passphraseEnv: env://PULUMI_CONFIG_PASSPHRASE\n", 1)
		if _, err := DecodeAuthentication(strings.NewReader(local)); err != nil {
			t.Fatalf("nonproduction passphrase rejected: %v", err)
		}
	})

	t.Run("bounds document size and rejects multiple documents", func(t *testing.T) {
		if _, err := DecodeAuthentication(strings.NewReader(strings.Repeat("x", maxAuthDocumentBytes+1))); err == nil {
			t.Fatal("oversized document accepted")
		}
		if _, err := DecodeAuthentication(strings.NewReader(validProfileDocument + "---\nversion: 1\n")); err == nil {
			t.Fatal("multiple documents accepted")
		}
	})
}
