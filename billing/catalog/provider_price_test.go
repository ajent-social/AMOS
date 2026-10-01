package catalog

import (
	"errors"
	"testing"
	"time"
)

func TestResolveProviderPricePreservesIdentityAndPrivateCatalog(t *testing.T) {
	c := testCatalog(t)
	plan, price, err := c.ResolveProviderPrice("price_month")
	if err != nil || price.Key != "pro-month" || price.ProviderPriceKey != "price_month" || plan.Key != "pro-v1" {
		t.Fatal("provider price lookup changed reviewed price identity")
	}
	plan.Features[0] = "mutated"
	plan.Prices[0].ProviderPriceKey = "mutated"
	again, _, err := c.ResolveProviderPrice("price_month")
	if err != nil || again.Features[0] != "projects" || again.Prices[0].ProviderPriceKey != "price_month" {
		t.Fatal("returned plan changed the private catalog")
	}
	for _, key := range []string{"", "pro-month", "price_foreign"} {
		if _, _, err := c.ResolveProviderPrice(key); !errors.Is(err, ErrInvalidCatalog) {
			t.Fatal("unknown provider price accepted")
		}
	}
	var absent *Catalog
	if _, _, err := absent.ResolveProviderPrice("price_month"); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatal("nil catalog accepted")
	}
	withdrawn := testPlan(testScope())
	withdrawn.WithdrawnAt = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	historical, err := New(Config{Revision: "historical-1", Plans: []Plan{withdrawn}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := historical.ResolveProviderPrice("price_month"); err != nil {
		t.Fatal("withdrawn historical price disappeared from reconciliation")
	}
	if _, _, err := historical.ResolveSelection("pro-month", withdrawn.WithdrawnAt); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatal("historical lookup granted new checkout authority")
	}
}
