# Makefile for bridge-claude-code.
#
# Zero third-party tooling: just the Go toolchain, gofmt, go vet, go test.
# `make cross` reproduces the release matrix locally (pure Go, CGO disabled).

BINARIES   := scrub-claude scrub-setup
DISTDIR    := dist
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X main.version=$(VERSION)
GOFLAGS    := -trimpath

# Release targets: os/arch pairs. Matches .github/workflows/release.yml.
PLATFORMS  := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build install test race vet fmt fmt-check tidy cross clean help

all: fmt-check vet test build ## Format-check, vet, test, then build

build: ## Build both binaries for the host into ./dist
	@mkdir -p $(DISTDIR)
	@for b in $(BINARIES); do \
		echo "build $$b"; \
		CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DISTDIR)/$$b ./cmd/$$b; \
	done

install: ## go install both binaries into GOBIN/GOPATH/bin
	@for b in $(BINARIES); do \
		echo "install $$b"; \
		CGO_ENABLED=0 go install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/$$b; \
	done

test: ## Run unit tests
	go test ./...

race: ## Run unit tests with the race detector (needs a C toolchain)
	CGO_ENABLED=1 go test -race ./...

vet: ## go vet
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

fmt-check: ## Fail if any Go file is not gofmt-clean
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

tidy: ## Verify go.mod stays dependency-free
	go mod tidy
	@git diff --exit-code go.mod go.sum 2>/dev/null || (echo "go.mod/go.sum changed — did a dependency sneak in?"; exit 1)

cross: ## Cross-compile every release target into ./dist
	@mkdir -p $(DISTDIR)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		for b in $(BINARIES); do \
			out=$(DISTDIR)/$${b}_$(VERSION)_$${os}_$${arch}$$ext; \
			echo "cross $$out"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $$out ./cmd/$$b; \
		done; \
	done

clean: ## Remove build output
	rm -rf $(DISTDIR)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
