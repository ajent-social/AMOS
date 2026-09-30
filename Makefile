.PHONY: test test-integration vet

test:
	go test ./internal/testkit

test-integration:
	go test -tags=integration ./internal/testkit

vet:
	go vet ./internal/testkit
