# Frequently used commands. `make check` is the gate everything must pass.

GO  ?= go
BIN := mtga-draft-assistant
PKG := ./cmd/$(BIN)

# Pinned, and run through `go run`, so the linter and vulnerability scanner a
# contributor runs are byte-for-byte the ones CI runs (decision 2).
GOLANGCI_LINT := $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOVULNCHECK   := $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.8.0

# The stripped release binary may not grow past this without a decision
# (decision 10). Raise it in the PR that spends it, and say what bought it.
SIZE_BUDGET_MIB := 28

RELEASE_FLAGS := -trimpath -ldflags="-s -w"

.DEFAULT_GOAL := help
.PHONY: help build release run test race lint fmt fmt-check vet vuln cross size smoke check clean

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

build: ## Debug build into bin/
	$(GO) build -o bin/$(BIN) $(PKG)

release: ## Stripped release build into bin/
	$(GO) build $(RELEASE_FLAGS) -o bin/$(BIN) $(PKG)

run: ## Run the app (make run ARGS="-version")
	$(GO) run $(PKG) $(ARGS)

test: ## Run the test suite
	$(GO) test ./...

race: ## Run the test suite under the race detector
	$(GO) test -race ./...

lint: ## golangci-lint, with the config in .golangci.yml
	$(GOLANGCI_LINT) run ./...

fmt: ## Format the code
	$(GOLANGCI_LINT) fmt ./...

fmt-check: ## Verify formatting without changing files
	@out="$$($(GOLANGCI_LINT) fmt --diff ./...)"; \
		if [ -n "$$out" ]; then echo "$$out"; echo "run 'make fmt'" >&2; exit 1; fi

vet: ## go vet
	$(GO) vet ./...

vuln: ## Report known vulnerabilities in code the binary actually calls
	$(GOVULNCHECK) ./...

cross: ## Check that every other target still compiles: Windows and Linux are cgo-free
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -o /dev/null $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 $(GO) build -o /dev/null $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build -o /dev/null $(PKG)

size: release ## Fail if the release binary is over its size budget
	scripts/check-size.sh bin/$(BIN) $(SIZE_BUDGET_MIB)

smoke: release ## Launch the real window, screenshot it, and check it idles (Linux, needs X or xvfb-run)
	scripts/smoke.sh bin/$(BIN) bin/first-paint.png

check: fmt-check vet lint race cross size ## Everything CI runs on every platform — run before pushing
	@echo "All checks passed."

clean: ## Remove build output
	rm -rf bin
