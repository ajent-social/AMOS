package migrations_test

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/ajent-social/amos/billing/reconcile"
	reference "github.com/ajent-social/amos/examples/reference/migrations"
	"github.com/ajent-social/amos/identity/federation"
	"github.com/ajent-social/amos/identity/mfa"
	"github.com/ajent-social/amos/migrations"
)

// These IDs and effective SQL hashes were banked from preceding source before adding
// sequence 17. They must not be regenerated to accommodate an old-fragment edit.
func TestOperationMigrationPreservesExactPrefix(t *testing.T) {
	fragments := []migrations.Fragment{reference.Fragment()}
	for _, f := range []struct {
		sequence uint64
		make     func(uint64) (migrations.Fragment, error)
	}{
		{9, migrations.BillingWebhookIngress}, {10, migrations.RuntimeBinding}, {11, migrations.MagicBrowserBinding},
		{12, migrations.SessionAssurance}, {13, mfa.Fragment}, {14, migrations.MFAProtection},
		{15, reconcile.Schema}, {16, federation.Fragment}, {17, migrations.OperationInvocations},
	} {
		fragment, err := f.make(f.sequence)
		if err != nil {
			t.Fatal("load reference fragment")
		}
		fragments = append(fragments, fragment)
	}
	r, err := migrations.Core(fragments...)
	if err != nil {
		t.Fatal(err)
	}
	got := r.Migrations()
	if len(got) != 17 || got[16].ID() != "000017.operation.invocations" {
		t.Fatal("invalid operation allocation")
	}
	expected := []struct{ id, hash string }{
		{"000001.identity.foundation", "5ef343242cb0da6e9e0fd331d6f06e4fbd7dc570fec0a804bd49252891adfe22"},
		{"000002.jobs.foundation", "d4c12a509472263e8b333c40a8e2b5a470d967b31e096c9c6d023e7716fb441e"},
		{"000003.workspace.foundation", "e75e8265e08b9ea444ea8908810916e5cfcca650c0b3e9c9f0d80df97834c011"},
		{"000004.audit.foundation", "ea52025c5c507dcf79571164ef81aa99e74b3381864774318d109f2b1c9c998c"},
		{"000005.billing.foundation", "20bc53858ea737591190a30cbbc91e72803bb14147bc778b6b6f192dd4cf6d69"},
		{"000006.identity.authentication_limits", "23d3f6fe55f272ef3f82e96650fbef96e825a44098ae4b56cb896138e869457f"},
		{"000007.delivery.protected_email_material", "d04565d55f1766187f84883fe462025868235f2b3cd65aaf40019dd0050bb848"},
		{"000008.reference.todos", "6549d46304b72a5d59562a56adf3d68b640716a6ffc2fce52e5b18740f8c6565"},
		{"000009.billing.verified_webhook_ingress", "32d8395a3d4f1ffa11cbbfe42dadb137576ed193d4b3100dae29358a0602d3c1"},
		{"000010.runtime.deployment_binding", "5c61d6eb3dd136b860697ca4e9141b1939820f5fee431ae472790e9878985122"},
		{"000011.identity.magic_browser_binding", "56b75f37f3110b96bcd494714a456bf3cc480d2457b580f8186c7d348392208f"},
		{"000012.identity.session_assurance", "9c34157823ae2847c1ec1df2c45ad7aa2523545b1938342b82c331fae09be7c9"},
		{"000013.identity_mfa.totp_factors", "8d62cbde41b607cafafd8eff2f0242098081245fad006d439db40fd837159620"},
		{"000014.identity.mfa_protection", "4f8efab3fbf0fc5dd5bc58d8568db808d63d34a8e4e59f5b3c34e5285af617da"},
		{"000015.billing_reconcile.reconciliation_work", "74b0ee9e0250de947d84b6ad0418f8a0aab17f2e858d9866ba4ada5e45528f93"},
		{"000016.identity.federation_flows", "877708094d2610359d7bf1fe0946184ebe2b8320a8270744f3ac40531d60c387"},
	}
	for i, want := range expected {
		if got[i].ID() != want.id || fmt.Sprintf("%x", sha256.Sum256([]byte(got[i].SQL))) != want.hash {
			t.Fatalf("immutable migration changed at sequence %d", i+1)
		}
	}
}

func TestOperationMigrationRejectsZeroSequence(t *testing.T) {
	if _, err := migrations.OperationInvocations(0); err == nil {
		t.Fatal("zero sequence accepted")
	}
}
