.PHONY: all build test lint clean setup-hooks install-tools check fmt vet
.PHONY: release release-dry-run changelog version vulncheck ci apidiff apidiff-baseline

# Project info
PROJECT_NAME := llms
MODULE := github.com/nocturnium/llm-go-sdk/v2
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build flags
LDFLAGS := -s -w \
	-X $(MODULE).Version=$(VERSION) \
	-X $(MODULE).Commit=$(COMMIT) \
	-X $(MODULE).Date=$(DATE)

# Default target
all: check build

# Build the CLI
build:
	@echo "Building llms-cli ($(VERSION))..."
	GOWORK=off go build -v -trimpath -ldflags "$(LDFLAGS)" -o llms-cli ./cmd

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-linux-amd64 ./cmd
	GOOS=linux GOARCH=arm64 GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-linux-arm64 ./cmd
	GOOS=darwin GOARCH=amd64 GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-darwin-amd64 ./cmd
	GOOS=darwin GOARCH=arm64 GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-darwin-arm64 ./cmd
	GOOS=windows GOARCH=amd64 GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-windows-amd64.exe ./cmd

# Run tests
test:
	@echo "Running tests..."
	GOWORK=off go test -race -coverprofile=coverage.out ./...

# Run tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	GOWORK=off go test -v -race -coverprofile=coverage.out ./...

# Run short tests only
test-short:
	@echo "Running short tests..."
	GOWORK=off go test -short -race ./...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	GOWORK=off go test -v -tags=integration -race ./...

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	GOWORK=off go test -bench=. -benchmem -run=^$$ ./...

# Run linters
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Run 'make install-tools' first."; \
		exit 1; \
	fi

# Run go vet
vet:
	@echo "Running go vet..."
	GOWORK=off go vet ./...

# Check for vulnerabilities
vulncheck:
	@echo "Running vulnerability check..."
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not installed. Run 'make install-tools' first."; \
		exit 1; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	gofmt -w .
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w -local github.com/nocturnium/llm-go-sdk/v2 .; \
	fi

# Check formatting
fmt-check:
	@echo "Checking formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "The following files are not properly formatted:"; \
		gofmt -l .; \
		exit 1; \
	fi

# Run all checks (used by CI)
check: fmt-check vet lint test
	@echo "All checks passed!"

# Run CI checks (comprehensive)
ci: fmt-check vet lint vulncheck apidiff test
	@echo "All CI checks passed!"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f llms-cli
	rm -f coverage.out
	rm -rf dist/
	GOWORK=off go clean ./...

# Install git hooks
setup-hooks:
	@echo "Installing git hooks..."
	@if [ -d .git ]; then \
		cp scripts/hooks/* .git/hooks/; \
		chmod +x .git/hooks/*; \
		echo "Git hooks installed successfully"; \
	else \
		echo "Not a git repository"; \
		exit 1; \
	fi

# Install development tools
install-tools:
	@echo "Installing development tools..."
	GOWORK=off go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	GOWORK=off go install golang.org/x/tools/cmd/goimports@latest
	GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@latest
	GOWORK=off go install honnef.co/go/tools/cmd/staticcheck@latest
	GOWORK=off go install golang.org/x/exp/cmd/apidiff@latest
	GOWORK=off go install github.com/goreleaser/goreleaser/v2@latest
	GOWORK=off go install github.com/orhun/git-cliff/git-cliff@latest || echo "git-cliff install failed (optional)"
	@echo "Tools installed successfully"

# Tidy dependencies
# Path to the committed public-API baseline (apidiff export data).
API_BASELINE := api/v2.txt

# apidiff: fail if the exported API surface has changed since the committed
# baseline. Guards the single-version v2 contract — every exported change must
# be deliberate and re-baselined via `make apidiff-baseline`.
apidiff:
	@echo "Checking public API against $(API_BASELINE)..."
	@if ! command -v apidiff >/dev/null 2>&1; then \
		echo "apidiff not installed. Run 'make install-tools' first."; \
		exit 1; \
	fi
	@if [ ! -f "$(API_BASELINE)" ]; then \
		echo "Baseline $(API_BASELINE) missing. Run 'make apidiff-baseline'."; \
		exit 1; \
	fi
	@out="$$(GOWORK=off apidiff -m $(API_BASELINE) $(MODULE) 2>&1 | grep -v '^Ignoring internal package' || true)"; \
	if [ -n "$$out" ]; then \
		echo "Public API changed since baseline:"; \
		echo "$$out"; \
		echo "If intentional, re-baseline with 'make apidiff-baseline' and review the diff."; \
		exit 1; \
	fi
	@echo "Public API matches baseline."

# Regenerate the committed API baseline. Run this (and review) whenever an
# exported-surface change is intentional.
apidiff-baseline:
	@echo "Writing API baseline to $(API_BASELINE)..."
	@mkdir -p $(dir $(API_BASELINE))
	GOWORK=off apidiff -m -w $(API_BASELINE) $(MODULE)
	@echo "Baseline written. Review and commit $(API_BASELINE)."

tidy:
	@echo "Tidying dependencies..."
	GOWORK=off go mod tidy

# Verify dependencies
verify:
	@echo "Verifying dependencies..."
	GOWORK=off go mod verify

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	GOWORK=off go mod download

# Generate code
generate:
	@echo "Generating code..."
	GOWORK=off go generate ./...

# Run a quick sanity check
sanity:
	@echo "Running sanity check..."
	GOWORK=off go build -v ./...
	GOWORK=off go test -short ./...
	@echo "Sanity check passed!"

# Show version info
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(DATE)"

# Generate changelog
changelog:
	@echo "Generating changelog..."
	@if command -v git-cliff >/dev/null 2>&1; then \
		git-cliff -o CHANGELOG.md; \
	else \
		echo "git-cliff not installed. Run 'make install-tools' first."; \
		exit 1; \
	fi

# Create a new release (dry run)
release-dry-run:
	@echo "Running release dry-run..."
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean --skip=publish; \
	else \
		echo "goreleaser not installed. Run 'make install-tools' first."; \
		exit 1; \
	fi

# Create a new release (requires GITHUB_TOKEN)
release:
	@echo "Creating release..."
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --clean; \
	else \
		echo "goreleaser not installed. Run 'make install-tools' first."; \
		exit 1; \
	fi

# Show coverage report in browser
coverage:
	@echo "Generating coverage report..."
	GOWORK=off go test -race -coverprofile=coverage.out ./...
	GOWORK=off go tool cover -html=coverage.out

# Show help
help:
	@echo "LLMs Go SDK - Makefile Help"
	@echo ""
	@echo "Build targets:"
	@echo "  all             - Run checks and build (default)"
	@echo "  build           - Build the CLI"
	@echo "  build-all       - Build for all platforms"
	@echo ""
	@echo "Test targets:"
	@echo "  test            - Run tests with coverage"
	@echo "  test-verbose    - Run tests with verbose output"
	@echo "  test-short      - Run short tests only"
	@echo "  test-integration - Run integration tests"
	@echo "  bench           - Run benchmarks"
	@echo "  coverage        - Generate and view coverage report"
	@echo ""
	@echo "Quality targets:"
	@echo "  lint            - Run golangci-lint"
	@echo "  vet             - Run go vet"
	@echo "  vulncheck       - Run vulnerability check"
	@echo "  apidiff         - Check public API against committed baseline"
	@echo "  apidiff-baseline - Regenerate the public API baseline"
	@echo "  fmt             - Format code"
	@echo "  fmt-check       - Check code formatting"
	@echo "  check           - Run all checks"
	@echo "  ci              - Run comprehensive CI checks"
	@echo ""
	@echo "Release targets:"
	@echo "  release         - Create a release"
	@echo "  release-dry-run - Test release process"
	@echo "  changelog       - Generate changelog"
	@echo "  version         - Show version info"
	@echo ""
	@echo "Setup targets:"
	@echo "  setup-hooks     - Install git hooks"
	@echo "  install-tools   - Install development tools"
	@echo "  deps            - Download dependencies"
	@echo "  tidy            - Tidy dependencies"
	@echo "  verify          - Verify dependencies"
	@echo ""
	@echo "Other targets:"
	@echo "  clean           - Clean build artifacts"
	@echo "  generate        - Run go generate"
	@echo "  sanity          - Quick sanity check"
	@echo "  help            - Show this help"
