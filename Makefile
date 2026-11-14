GOLANGCI_LINT_VERSION := v2.15.0
GOVULNCHECK_VERSION := v1.9.0

BIN := bin
GOLANGCI_LINT := $(BIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK := $(BIN)/govulncheck-$(GOVULNCHECK_VERSION)

.PHONY: check lint fmt test vuln

check: lint test vuln

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

fmt: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) fmt ./...

test:
	go test -race -count=1 ./...

vuln: $(GOVULNCHECK)
	$(GOVULNCHECK) ./...

$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/$(BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(BIN)/golangci-lint $@

$(GOVULNCHECK):
	GOBIN=$(CURDIR)/$(BIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	mv $(BIN)/govulncheck $@
