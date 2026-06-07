.PHONY: all build test lint clean setup-hooks install-tools check fmt vet
.PHONY: release release-dry-run changelog version vulncheck ci

# Project info
PROJECT_NAME := llms
MODULE := github.com/nocturnium/llm-go-sdk
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
	go build -v -trimpath -ldflags "$(LDFLAGS)" -o llms-cli ./cmd

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-linux-amd64 ./cmd
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-linux-arm64 ./cmd
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-darwin-amd64 ./cmd
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-darwin-arm64 ./cmd
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/llms-cli-windows-amd64.exe ./cmd

# Run tests
test:
	@echo "Running tests..."
	go test -race -coverprofile=coverage.out ./...

# Run tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	go test -v -race -coverprofile=coverage.out ./...

# Run short tests only
test-short:
	@echo "Running short tests..."
	go test -short -race ./...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	go test -v -tags=integration -race ./...

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem -run=^$$ ./...

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
	go vet ./...

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
		goimports -w -local github.com/nocturnium/llm-go-sdk .; \
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
ci: fmt-check vet lint vulncheck test
	@echo "All CI checks passed!"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f llms-cli
	rm -f coverage.out
	rm -rf dist/
	go clean ./...

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
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install github.com/goreleaser/goreleaser/v2@latest
	go install github.com/orhun/git-cliff/git-cliff@latest || echo "git-cliff install failed (optional)"
	@echo "Tools installed successfully"

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	go mod tidy

# Verify dependencies
verify:
	@echo "Verifying dependencies..."
	go mod verify

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download

# Generate code
generate:
	@echo "Generating code..."
	go generate ./...

# Run a quick sanity check
sanity:
	@echo "Running sanity check..."
	go build -v ./...
	go test -short ./...
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
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

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
