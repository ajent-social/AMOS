package catalog

// ResolveProviderPrice looks up a configured provider price without granting
// checkout or entitlement authority. Historical withdrawn prices remain known
// for reconciliation; Evaluate and ResolveSelection retain their own policy.
// The returned plan is copied so callers cannot mutate the live catalog.
func (c *Catalog) ResolveProviderPrice(key string) (Plan, Price, error) {
	if c == nil || !validIdentifier(key) {
		return Plan{}, Price{}, ErrInvalidCatalog
	}
	entry, ok := c.providerPrices[key]
	if !ok {
		return Plan{}, Price{}, ErrInvalidCatalog
	}
	return clonePlan(entry.plan), entry.price, nil
}
