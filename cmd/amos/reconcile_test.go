package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/provider"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/google/uuid"
)

func billingFixture(t *testing.T) []byte {
	t.Helper()
	id := func() string {
		value, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		return value.String()
	}
	cfg := billingServiceConfig{ReturnHosts: []string{"example.invalid"}, Catalog: catalog.Config{Revision: "catalog-v1", Plans: []catalog.Plan{{Key: "pro-v1", Revision: "plan-v1", ProductFamily: "app", Model: catalog.ModelFlat, Scope: catalog.Scope{InstallationID: id(), ApplicationID: id(), EnvironmentID: id(), Provider: "stripe", ProviderAccountID: "acct_test", AccountMode: provider.AccountTest}, Currency: "USD", MinorUnit: 2, EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Features: []string{"reports"}, Prices: []catalog.Price{{Key: "monthly", ProviderPriceKey: "price_monthly", AmountMinor: 1000, Interval: catalog.IntervalMonth}, {Key: "annual", ProviderPriceKey: "price_annual", AmountMinor: 10000, Interval: catalog.IntervalYear}}}}}}
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestBillingOwnerConfigRejectsAmbiguousAndUnsupportedInput(t *testing.T) {
	valid := string(billingFixture(t))
	cases := map[string]string{
		"valid":                valid,
		"duplicate alias":      strings.Replace(valid, `"catalog":`, `"Catalog":{},"catalog":`, 1),
		"unknown":              strings.Replace(valid, `"return_hosts":`, `"secret":true,"return_hosts":`, 1),
		"trailing":             valid + `{}`,
		"unsupported provider": strings.Replace(valid, `"Provider":"stripe"`, `"Provider":"unknown"`, 1),
		"empty plans":          `{"catalog":{"Revision":"v1","Plans":[]},"return_hosts":["example.invalid"]}`,
		"oversize":             valid + strings.Repeat(" ", 1<<20),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "billing.json")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := readBillingConfig(path)
			if (err == nil) != (name == "valid") {
				t.Fatalf("configuration acceptance mismatch: %v", err)
			}
		})
	}
}

func TestBillingCommandRequiresExplicitConfigAndMigrationMode(t *testing.T) {
	for _, args := range [][]string{{"billing-reconcile"}, {"billing-reconcile", "--config", "missing", "--once", "--migrate-only"}} {
		if got := run(args); got != 2 {
			t.Fatalf("invalid arguments returned %d", got)
		}
	}
}

func TestBillingMigrationKeepsFoundationAndAppendsReconciliation(t *testing.T) {
	registry, err := billingMigrationRegistry()
	if err != nil {
		t.Fatal(err)
	}
	entries := registry.Migrations()
	if len(entries) != 17 || entries[16].ID() != "000017.operation.invocations" || entries[14].Sequence != 15 || entries[14].Namespace != "billing_reconcile" || entries[15].Sequence != 16 || entries[15].Name != "federation_flows" {
		t.Fatal("reference migration allocation changed")
	}
}

// Empty scoped account data exercises actual migration and scheduler composition
// without provider reads. This qualifies local wiring only.
func TestBillingServiceMigratesAndRunsBoundedEmptyScope(t *testing.T) {
	_, schema := testkit.NewPostgres(t)
	configured, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(configured)
	if err != nil {
		t.Fatal("test database configuration invalid")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	t.Setenv("AMOS_MIGRATION_DATABASE_URL", parsed.String())
	t.Setenv("AMOS_BILLING_DATABASE_URL", parsed.String())
	t.Setenv("AMOS_STRIPE_API_KEY", "sk_test_fixture_key")
	var cfg billingServiceConfig
	if err := json.Unmarshal(billingFixture(t), &cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := serveBilling(ctx, cfg, false, true); err != nil {
		t.Fatal("local migration composition failed:", err)
	}
	if err := serveBilling(ctx, cfg, true, false); err != nil {
		t.Fatal("bounded empty scoped service failed:", err)
	}
}
