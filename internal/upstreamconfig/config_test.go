package upstreamconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestUpstreamConfiguration(t *testing.T) {
	valid := fixture(t, "valid.json")
	c, err := Decode(valid)
	if err != nil {
		t.Fatal(err)
	}
	if c.Collection != DefaultCollection() || c.Collection.Mode != "disabled" {
		t.Fatal("default policy mismatch or enabled collection")
	}
	if Validate(Config{}) == nil {
		t.Fatal("empty defaults selected identities")
	}
	for _, name := range []string{"rejected-consumer-operator.json", "rejected-public-raw-store.json", "profile-rejected-shared-account.json"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(fixture(t, name)); !errors.Is(err, ErrInvalid) {
				t.Fatal("isolation rejection fixture accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"unequal roles same consumer account", func(c *Config) {
			c.Consumers[0].AccountID = c.Upstream.AccountID
			c.Consumers[0].DeploymentARN = "arn:aws:iam::" + c.Upstream.AccountID + ":role/amos-consumer/deploy/another"
		}},
		{"consumer role namespace", func(c *Config) {
			c.Identities.Operator.ARN = "arn:aws:iam::" + c.Upstream.AccountID + ":role/amos-consumer/deploy/example"
		}},
		{"operator role misdeclared purpose", func(c *Config) { c.Identities.Operator.Purpose = "deploy" }},
		{"intake assumes operator", func(c *Config) { c.Identities.Intake = c.Identities.Operator }},
		{"cross account storage", func(c *Config) { c.Storage.AccountID = c.Consumers[0].AccountID }},
		{"cross account key", func(c *Config) {
			c.Storage.KMSKeyARN = strings.Replace(c.Storage.KMSKeyARN, c.Upstream.AccountID, c.Consumers[0].AccountID, 1)
		}},
		{"different key region", func(c *Config) {
			c.Storage.KMSKeyARN = strings.Replace(c.Storage.KMSKeyARN, "us-east-1", "us-west-2", 1)
		}},
		{"consumer storage namespace", func(c *Config) { c.Storage.Namespace = "consumer-data" }},
		{"public storage", func(c *Config) { c.Storage.Access = "public" }},
		{"raw payload storage", func(c *Config) { c.Storage.RawPayloadPolicy = "persist" }},
		{"retention exceeds privacy budget", func(c *Config) { c.Storage.RetentionHours = 169 }},
		{"unbounded storage", func(c *Config) { c.Storage.MaxBytes = 0 }},
		{"oversized payload", func(c *Config) { c.Collection.MaxPayloadBytes = 8193 }},
		{"compressed input excess", func(c *Config) { c.Collection.MaxCompressedBytes = 8193 }},
		{"infinite request timeout", func(c *Config) { c.Collection.TimeoutSeconds = 0 }},
		{"anonymous rate excess", func(c *Config) { c.Collection.Anonymous.Global24H = 17 }},
		{"sender budget excess", func(c *Config) { c.Collection.Enrolled.PerPrincipal24H = 2 }},
		{"global budget inconsistent", func(c *Config) { c.Collection.DailyReports = 1 }},
		{"disabled channel inconsistent", func(c *Config) { c.Collection.Anonymous.PerPrincipal24H = 0 }},
		{"consumer abuse credential", func(c *Config) { c.Abuse.KeyReference.AccountID = c.Consumers[0].AccountID }},
		{"arbitrary secret reference", func(c *Config) { c.Abuse.KeyReference.Name = "consumer/deployment-password" }},
		{"weak anonymous source key", func(c *Config) { c.Abuse.SourceKeyMode = "raw-ip" }},
		{"clock fallback", func(c *Config) { c.Abuse.ClockPolicy = "reset-on-restart" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Decode(valid)
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&c)
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(raw); !errors.Is(err, ErrInvalid) {
				t.Fatal("bad declared policy accepted")
			}
		})
	}
	c, err = Decode(valid)
	if err != nil {
		t.Fatal(err)
	}
	c.Profile = "aws_vm"
	c.Collection.Mode = "enabled"
	c.Collection.Anonymous = SenderBudget{0, 0, 1}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(raw); err != nil {
		t.Fatal("explicit requested profile rejected", err)
	}
}
func TestUpstreamConfigurationExactKeys(t *testing.T) {
	valid := fixture(t, "valid.json")
	for _, tc := range []struct{ name, key, value, alias string }{
		{"root", "schema", Schema, "Schema"},
		{"upstream scope", "identity_namespace", "amos-upstream-operators", "Identity_Namespace"},
		{"consumer scope", "deployment_arn", "arn:aws:iam::000000000002:role/amos-consumer/deploy/example", "Deployment_ARN"},
		{"role", "purpose", "operator", "Purpose"},
		{"storage", "access", "operator-only", "Access"},
		{"collection", "mode", "disabled", "Mode"},
		{"secret reference", "provider", "aws_secrets_manager", "Provider"},
		{"abuse policy", "source_key_mode", "daily-hmac", "Source_Key_Mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			needle := []byte(`"` + tc.key + `": "` + tc.value + `"`)
			if bytes.Count(valid, needle) != 1 {
				t.Fatal("ambiguous alias fixture")
			}
			for _, replace := range []string{`"` + tc.alias + `": "` + tc.value + `"`, `"` + tc.alias + `":"CANARY_PRIVATE", ` + string(needle)} {
				bad := bytes.Replace(valid, needle, []byte(replace), 1)
				if _, err := Decode(bad); !errors.Is(err, ErrInvalid) {
					t.Fatal("case alias/canary overwrite accepted")
				}
			}
		})
	}
	for _, bad := range [][]byte{
		bytes.Replace(valid, []byte(`"global_24h": 8`), []byte(`"Global_24H": 8`), 1),
		bytes.Replace(valid, []byte(`"global_24h": 8`), []byte(`"Global_24H":"CANARY_PRIVATE", "global_24h": 8`), 1),
		bytes.Replace(valid, []byte(`"operator": {`), []byte(`"Operator": {`), 1),
	} {
		if _, err := Decode(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("nested budget or identity-set alias accepted")
		}
	}
	for _, prefix := range []string{`"password":"synthetic-canary",`, `"raw_diagnostics":{},`, `"Schema":"CANARY_PRIVATE",`, `"schema":"duplicate",`, `"\u0073chema":"duplicate",`, `"isolated":true,`, `"\u0000metadata":"canary",`} {
		bad := append([]byte("{"+prefix), valid[1:]...)
		if _, err := Decode(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("unknown, duplicate or trust-flag input accepted")
		}
	}
	for _, bad := range [][]byte{nil, []byte("null"), append(bytes.Clone(valid), valid...), bytes.Repeat([]byte(" "), 32769), {0xff}, bytes.Replace(valid, []byte(`"retention_hours": 168`), []byte(`"retention_hours": 1e2`), 1)} {
		_, err := Decode(bad)
		if !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid encoding accepted")
		}
		if err.Error() != ErrInvalid.Error() {
			t.Fatal("diagnostics reflected input")
		}
	}
}
func TestUpstreamConfigurationSchemaClosed(t *testing.T) {
	raw, err := os.ReadFile("../../infra/upstream/config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["type"] == "object" && v["additionalProperties"] != false {
				t.Fatal("open object in schema")
			}
			if v["type"] == "array" && v["maxItems"] == nil {
				t.Fatal("unbounded array in schema")
			}
			if v["type"] == "integer" && (v["minimum"] == nil || v["maximum"] == nil) {
				t.Fatal("unbounded budget")
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(schema)
}
