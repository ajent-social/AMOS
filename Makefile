.PHONY: check fmt lint test test-integration test-api test-browser check-plan check-contracts check-public
fmt:
	./scripts/check-go.sh fmt
lint:
	./scripts/check-go.sh lint
test:
	./scripts/check-go.sh unit
test-integration:
	./scripts/check-go.sh integration
test-api:
	./scripts/test-api.sh unit
test-browser:
	./scripts/test-browser.sh fixtures
check-plan:
	python3 scripts/check-plan.py
check-contracts:
	python3 scripts/check-contract-schemas.py
check-public:
	python3 scripts/check-public-artifacts.py

# Complete required local gates; provider qualification remains separate.
check: fmt lint test test-integration test-api test-browser check-plan check-contracts check-public
