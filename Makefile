# DNS Monitor Makefile
# Build, test, and package the DNS monitoring tool

# Variables
BINARY_NAME := dns-monitor
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GO_VERSION := $(shell go version | cut -d' ' -f3)

# Go build flags
LDFLAGS := -s -w \
	-X 'main.Version=$(VERSION)' \
	-X 'main.BuildTime=$(BUILD_TIME)' \
	-X 'main.GitCommit=$(GIT_COMMIT)' \
	-X 'main.GoVersion=$(GO_VERSION)'

# Directories
BUILD_DIR := build
DIST_DIR := dist
CMD_DIR := cmd/dns-monitoring

# Go commands
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := gofmt
GOLINT := golangci-lint

# Platforms
PLATFORMS := linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[1;33m
NC := \033[0m # No Color

.PHONY: all build clean test lint fmt deps help

# Default target
all: test build

# Help target
help:
	@echo "$(GREEN)DNS Monitor Makefile$(NC)"
	@echo ""
	@echo "$(YELLOW)Usage:$(NC)"
	@echo "  make [target]"
	@echo ""
	@echo "$(YELLOW)Targets:$(NC)"
	@echo "  $(GREEN)all$(NC)          - Run tests and build binary"
	@echo "  $(GREEN)build$(NC)        - Build binary for current platform"
	@echo "  $(GREEN)build-all$(NC)    - Build binaries for all platforms"
	@echo "  $(GREEN)clean$(NC)        - Remove build artifacts"
	@echo "  $(GREEN)test$(NC)         - Run tests"
	@echo "  $(GREEN)test-cover$(NC)   - Run tests with coverage"
	@echo "  $(GREEN)bench$(NC)        - Run benchmarks"
	@echo "  $(GREEN)lint$(NC)         - Run linters"
	@echo "  $(GREEN)fmt$(NC)          - Format code"
	@echo "  $(GREEN)deps$(NC)         - Download dependencies"
	@echo "  $(GREEN)update-deps$(NC)  - Update dependencies"
	@echo "  $(GREEN)install$(NC)      - Install binary to /usr/local/bin"
	@echo "  $(GREEN)uninstall$(NC)    - Remove installed binary"
	@echo "  $(GREEN)docker$(NC)       - Build Docker image"
	@echo "  $(GREEN)docker-push$(NC)  - Push Docker image"
	@echo "  $(GREEN)rpm$(NC)          - Build RPM package"
	@echo "  $(GREEN)deb$(NC)          - Build DEB package"
	@echo "  $(GREEN)package-all$(NC)  - Build all packages"
	@echo "  $(GREEN)release$(NC)      - Create release artifacts"
	@echo "  $(GREEN)run$(NC)          - Run with default config"
	@echo "  $(GREEN)run-dev$(NC)      - Run with minimal config (development)"
	@echo ""
	@echo "$(YELLOW)Variables:$(NC)"
	@echo "  VERSION      - Set version (default: git tag or 'dev')"
	@echo "  PLATFORMS    - Target platforms for cross-compilation"
	@echo ""
	@echo "$(YELLOW)Examples:$(NC)"
	@echo "  make build VERSION=v1.0.0"
	@echo "  make docker-push VERSION=latest"
	@echo "  make rpm VERSION=1.0.0"

# Build binary for current platform
build:
	@echo "$(YELLOW)Building $(BINARY_NAME) $(VERSION)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GOBUILD) -v -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY_NAME)$(NC)"

# Build for all platforms
build-all:
	@echo "$(YELLOW)Building for all platforms...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$$(echo $$platform | cut -d'/' -f1); \
		GOARCH=$$(echo $$platform | cut -d'/' -f2); \
		output_name=$(BUILD_DIR)/$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		if [ "$$GOOS" = "windows" ]; then \
			output_name="$$output_name.exe"; \
		fi; \
		echo "$(YELLOW)Building for $$GOOS/$$GOARCH...$(NC)"; \
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH $(GOBUILD) \
			-ldflags="$(LDFLAGS)" -o $$output_name $(CMD_DIR) || exit 1; \
	done
	@echo "$(GREEN)All platforms built successfully$(NC)"

# Clean build artifacts
clean:
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	@$(GOCLEAN)
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	@echo "$(GREEN)Clean complete$(NC)"

# Run tests
test:
	@echo "$(YELLOW)Running tests...$(NC)"
	@$(GOTEST) -v -race ./...
	@echo "$(GREEN)Tests passed$(NC)"

# Run tests with coverage
test-cover:
	@echo "$(YELLOW)Running tests with coverage...$(NC)"
	@$(GOTEST) -v -race -coverprofile=coverage.txt -covermode=atomic ./...
	@$(GOCMD) tool cover -html=coverage.txt -o coverage.html
	@echo "$(GREEN)Coverage report: coverage.html$(NC)"

# Run benchmarks
bench:
	@echo "$(YELLOW)Running benchmarks...$(NC)"
	@$(GOTEST) -bench=. -benchmem -benchtime=10s ./...

# Run linters
lint:
	@echo "$(YELLOW)Running linters...$(NC)"
	@if command -v $(GOLINT) >/dev/null 2>&1; then \
		$(GOLINT) run ./...; \
	else \
		echo "$(RED)golangci-lint not installed. Install with:$(NC)"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin"; \
		exit 1; \
	fi
	@echo "$(GREEN)Lint complete$(NC)"

# Format code
fmt:
	@echo "$(YELLOW)Formatting code...$(NC)"
	@$(GOFMT) -s -w .
	@$(GOMOD) tidy
	@echo "$(GREEN)Format complete$(NC)"

# Download dependencies
deps:
	@echo "$(YELLOW)Downloading dependencies...$(NC)"
	@$(GOMOD) download
	@$(GOMOD) verify
	@echo "$(GREEN)Dependencies downloaded$(NC)"

# Update dependencies
update-deps:
	@echo "$(YELLOW)Updating dependencies...$(NC)"
	@$(GOGET) -u ./...
	@$(GOMOD) tidy
	@echo "$(GREEN)Dependencies updated$(NC)"

# Install binary
install: build
	@echo "$(YELLOW)Installing $(BINARY_NAME)...$(NC)"
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@sudo chmod +x /usr/local/bin/$(BINARY_NAME)
	@echo "$(GREEN)Installed to /usr/local/bin/$(BINARY_NAME)$(NC)"

# Uninstall binary
uninstall:
	@echo "$(YELLOW)Uninstalling $(BINARY_NAME)...$(NC)"
	@sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "$(GREEN)Uninstalled$(NC)"

# Build Docker image
docker:
	@echo "$(YELLOW)Building Docker image...$(NC)"
	@docker build -t $(BINARY_NAME):$(VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-f Dockerfile .
	@echo "$(GREEN)Docker image built: $(BINARY_NAME):$(VERSION)$(NC)"

# Build multi-arch Docker image
docker-multiarch:
	@echo "$(YELLOW)Building multi-arch Docker image...$(NC)"
	@docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 \
		-t $(BINARY_NAME):$(VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-f Dockerfile.multiarch .
	@echo "$(GREEN)Multi-arch Docker image built$(NC)"

# Push Docker image
docker-push: docker
	@echo "$(YELLOW)Pushing Docker image...$(NC)"
	@docker tag $(BINARY_NAME):$(VERSION) ghcr.io/$$(git config user.name)/$(BINARY_NAME):$(VERSION)
	@docker push ghcr.io/$$(git config user.name)/$(BINARY_NAME):$(VERSION)
	@echo "$(GREEN)Docker image pushed$(NC)"

# Build RPM package
rpm:
	@echo "$(YELLOW)Building RPM package...$(NC)"
	@bash packaging/rpm/build-rpm.sh $(VERSION) $(GIT_COMMIT) 1
	@echo "$(GREEN)RPM package built$(NC)"

# Build DEB package
deb:
	@echo "$(YELLOW)Building DEB package...$(NC)"
	@bash packaging/deb/build-deb.sh $(VERSION) $(GIT_COMMIT) 1
	@echo "$(GREEN)DEB package built$(NC)"

# Build all packages
package-all: rpm deb
	@echo "$(GREEN)All packages built$(NC)"

# Create release artifacts
release: clean build-all package-all
	@echo "$(YELLOW)Creating release artifacts...$(NC)"
	@mkdir -p $(DIST_DIR)/binaries $(DIST_DIR)/packages
	
	# Copy binaries
	@cp -r $(BUILD_DIR)/* $(DIST_DIR)/binaries/
	
	# Create archives for each platform
	@for platform in $(PLATFORMS); do \
		GOOS=$$(echo $$platform | cut -d'/' -f1); \
		GOARCH=$$(echo $$platform | cut -d'/' -f2); \
		binary_name=$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		if [ "$$GOOS" = "windows" ]; then \
			binary_name="$$binary_name.exe"; \
		fi; \
		archive_name=$(DIST_DIR)/$(BINARY_NAME)-$(VERSION)-$$GOOS-$$GOARCH; \
		if [ "$$GOOS" = "windows" ]; then \
			cd $(BUILD_DIR) && zip -q ../$$archive_name.zip $$binary_name && cd ..; \
		else \
			tar -czf $$archive_name.tar.gz -C $(BUILD_DIR) $$binary_name; \
		fi; \
	done
	
	# Generate checksums
	@cd $(DIST_DIR) && sha256sum *.tar.gz *.zip > checksums.sha256 2>/dev/null || true
	@cd $(DIST_DIR) && md5sum *.tar.gz *.zip > checksums.md5 2>/dev/null || true
	
	@echo "$(GREEN)Release artifacts created in $(DIST_DIR)$(NC)"

# Run the application
run: build
	@echo "$(YELLOW)Running $(BINARY_NAME)...$(NC)"
	@$(BUILD_DIR)/$(BINARY_NAME) monitor -c config.yaml

# Run with minimal config (development)
run-dev: build
	@echo "$(YELLOW)Running $(BINARY_NAME) (development mode)...$(NC)"
	@$(BUILD_DIR)/$(BINARY_NAME) monitor -c config-minimal.yaml -v

# Run with CLI output
run-cli: build
	@echo "$(YELLOW)Running $(BINARY_NAME) with CLI output...$(NC)"
	@$(BUILD_DIR)/$(BINARY_NAME) monitor -c config.yaml --output cli --color

# Check version
version: build
	@$(BUILD_DIR)/$(BINARY_NAME) version

# Validate configuration
validate-config: build
	@echo "$(YELLOW)Validating configuration...$(NC)"
	@$(BUILD_DIR)/$(BINARY_NAME) validate -c config.yaml
	@echo "$(GREEN)Configuration valid$(NC)"

.DEFAULT_GOAL := help