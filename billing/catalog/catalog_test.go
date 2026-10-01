package catalog

import (
	"errors"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
)

var (
	testInstall = "018f0000-0000-7000-8000-000000000001"
	testApp     = "018f0000-0000-7000-8000-000000000002"
	testEnv     = "018f0000-0000-7000-8000-000000000003"
	testWork    = "018f0000-0000-7000-8000-000000000004"
	otherWork   = "018f0000-0000-7000-8000-000000000005"
)

func testScope() Scope {
	return Scope{InstallationID: testInstall, ApplicationID: testApp, EnvironmentID: testEnv,
		WorkspaceID: testWork, Provider: "stripe", ProviderAccountID: "acct_test", AccountMode: provider.AccountTest}
}
func testPlan(scope Scope) Plan {
	scope.WorkspaceID = "" // catalog versions are shared across workspaces
	return Plan{Key: "pro-v1", Revision: "2026-01", ProductFamily: "application", Model: ModelFlat,
		Scope: scope, Currency: "USD", MinorUnit: 2, EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Features: []string{"projects", "exports"}, Prices: []Price{
			{Key: "pro-month", ProviderPriceKey: "price_month", AmountMinor: 1200, Interval: IntervalMonth},
			{Key: "pro-year", ProviderPriceKey: "price_year", AmountMinor: 12000, Interval: IntervalYear},
		}}
}
func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := New(Config{Revision: "catalog-7", Plans: []Plan{testPlan(testScope())}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testProjection(scope Scope, state, price string, observed, start, end time.Time) *billingstore.SubscriptionProjection {
	return &billingstore.SubscriptionProjection{Binding: provider.Binding{InstallationID: scope.InstallationID,
		EnvironmentID: scope.EnvironmentID, WorkspaceID: scope.WorkspaceID, Provider: scope.Provider,
		AccountID: scope.ProviderAccountID, AccountMode: scope.AccountMode}, State: state, PriceKey: price,
		ObservedAt: observed, PeriodStart: start, PeriodEnd: end}
}

func TestT5_4_ResolvesVersionedMonthlyAndYearlyPrices(t *testing.T) {
	c := testCatalog(t)
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		key      string
		amount   int64
		interval Interval
	}{
		{"pro-month", 1200, IntervalMonth}, {"pro-year", 12000, IntervalYear},
	} {
		t.Run(string(tc.interval), func(t *testing.T) {
			plan, price, err := c.ResolveSelection(tc.key, now)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Currency != "USD" || plan.MinorUnit != 2 || price.AmountMinor != tc.amount || price.Interval != tc.interval {
				t.Fatalf("resolved wrong terms: plan=%+v price=%+v", plan, price)
			}
		})
	}
	if _, _, err := c.ResolveSelection("deleted-price", now); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("unknown selection accepted: %v", err)
	}
}

func TestT5_4_AllowsOnlyFreshScopedConfirmedPriceWithinHalfOpenPeriod(t *testing.T) {
	c := testCatalog(t)
	scope := testScope()
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	p := testProjection(scope, "confirmed", "price_month", now.Add(-time.Minute), start, end)
	decision := c.Evaluate(scope, p, "projects", now)
	if decision.Outcome != OutcomeAllowed || decision.Reason != ReasonEntitled || decision.CatalogRevision != "catalog-7" || decision.AmountMinor != 1200 || decision.Interval != IntervalMonth {
		t.Fatalf("expected current entitlement: %+v", decision)
	}
	if decision.AuthorizeBusinessAction(PermissionDenied).Reason != ReasonPermissionDenied || decision.AuthorizeBusinessAction(PermissionDenied).Outcome != OutcomeDenied {
		t.Fatal("billing entitlement bypassed missing tenant permission")
	}
	if decision.AuthorizeBusinessAction(PermissionGranted).Outcome != OutcomeAllowed {
		t.Fatal("separately permitted action denied")
	}
	p.PeriodEnd = now
	p.ObservedAt = now
	if got := c.Evaluate(scope, p, "projects", now); got.Outcome != OutcomeDenied || got.Reason != ReasonOutsidePeriod {
		t.Fatalf("period end must be exclusive: %+v", got)
	}
}

func TestT5_4_MissingStaleAndWrongScopeProjectionUnavailable(t *testing.T) {
	c := testCatalog(t)
	scope := testScope()
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	if got := c.Evaluate(scope, nil, "projects", now); got.Outcome != OutcomeUnavailable || got.Reason != ReasonNoProjection {
		t.Fatalf("missing projection: %+v", got)
	}
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	stale := testProjection(scope, "confirmed", "price_month", now.Add(-5*time.Minute), start, end)
	if got := c.Evaluate(scope, stale, "projects", now); got.Outcome != OutcomeUnavailable || got.Reason != ReasonStaleProjection {
		t.Fatalf("stale projection: %+v", got)
	}
	wrong := scope
	wrong.WorkspaceID = otherWork
	foreign := testProjection(wrong, "confirmed", "price_month", now, start, end)
	if got := c.Evaluate(scope, foreign, "projects", now); got.Outcome != OutcomeUnavailable || got.Reason != ReasonScopeMismatch {
		t.Fatalf("foreign workspace projection: %+v", got)
	}
	wrongApp := scope
	wrongApp.ApplicationID = "018f0000-0000-7000-8000-000000000006"
	if got := c.Evaluate(wrongApp, testProjection(scope, "confirmed", "price_month", now, start, end), "projects", now); got.Outcome != OutcomeUnavailable || got.Reason != ReasonScopeMismatch {
		t.Fatalf("foreign application context: %+v", got)
	}
}

func TestT5_4_RejectsUnknownPriceAndUnconfirmedState(t *testing.T) {
	c := testCatalog(t)
	scope := testScope()
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	wrongPrice := testProjection(scope, "confirmed", "obsolete_or_other_app_price", now, start, end)
	if got := c.Evaluate(scope, wrongPrice, "projects", now); got.Outcome != OutcomeUnavailable || got.Reason != ReasonUnknownPrice {
		t.Fatalf("unauthorized price mutation accepted: %+v", got)
	}
	for _, state := range []string{"unknown", "pending", "incomplete", "failed"} {
		p := testProjection(scope, state, "price_month", now, start, end)
		if got := c.Evaluate(scope, p, "projects", now); got.Outcome == OutcomeAllowed {
			t.Fatalf("%s projection granted access", state)
		}
	}
	pastDue := testProjection(scope, "past_due", "price_month", now, start, end)
	if got := c.Evaluate(scope, pastDue, "projects", now); got.Outcome != OutcomeDenied || got.Reason != ReasonDelinquent {
		t.Fatalf("past-due policy: %+v", got)
	}
}

func TestT5_4_EssentialAccountRecoveryAndBillingRemainAvailable(t *testing.T) {
	for _, path := range []EssentialPath{PathAccount, PathRecovery, PathBillingRestoration} {
		if got := EvaluateEssentialPath(path); got.Outcome != OutcomeAllowed || got.Reason != ReasonEssentialPath {
			t.Fatalf("essential path %q denied: %+v", path, got)
		}
	}
	if got := EvaluateEssentialPath("business"); got.Outcome != OutcomeDenied {
		t.Fatalf("unknown path allowed: %+v", got)
	}
}

func TestT5_4_RejectsIncompleteOrMutatedCatalog(t *testing.T) {
	plan := testPlan(testScope())
	plan.Prices = plan.Prices[:1]
	if _, err := New(Config{Revision: "catalog-8", Plans: []Plan{plan}}); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("single-period catalog accepted: %v", err)
	}
	plan = testPlan(testScope())
	plan.Prices[0].AmountMinor = -1
	if _, err := New(Config{Revision: "catalog-8", Plans: []Plan{plan}}); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("mutated unauthorized price accepted: %v", err)
	}
}
