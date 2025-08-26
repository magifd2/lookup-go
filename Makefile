# Makefile for lookup-go

# Default shell
SHELL = /bin/bash

# --- Configuration ---

# The name of the binary
BINARY_NAME = lookup-go
# The directory for all build artifacts
BIN_DIR = ./bin

# Get the version from the latest git tag. e.g., v1.2.3
VERSION ?= $(shell git describe --tags --always --abbrev=0 2>/dev/null || git rev-parse --short HEAD)
# Get the commit hash
COMMIT_HASH = $(shell git rev-parse --short HEAD)
# Get the build date
BUILD_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go linker flags to inject version information
LDFLAGS = -ldflags="-s -w -X 'main.version=$(VERSION) (build: $(COMMIT_HASH), date: $(BUILD_DATE))'"

# Platforms for cross-compilation
PLATFORMS ?= darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
# Get the current platform string, e.g., "darwin_amd64"
CURRENT_PLATFORM = $(shell go env GOOS)_$(shell go env GOARCH)


# --- Main Targets ---

.PHONY: all
all: cross-build macos-universal

# Build for the current host platform
.PHONY: build
build:
	@echo -e "\033[34m>> Building for current platform ($(CURRENT_PLATFORM))...\033[0m"
	@mkdir -p $(BIN_DIR)/$(CURRENT_PLATFORM)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(CURRENT_PLATFORM)/$(BINARY_NAME) main.go

# Run all tests
.PHONY: test
test:
	@echo -e "\033[34m>> Running tests...\033[0m"
	go test -v ./...

# Cross-compile for all target platforms
.PHONY: cross-build
cross-build: test check-gox
	@echo -e "\033[34m>> Cross-compiling for all platforms...\033[0m"
	@gox -osarch="$(PLATFORMS)" -output="$(BIN_DIR)/{{.OS}}_{{.Arch}}/$(BINARY_NAME)" $(LDFLAGS)
	@echo -e "\033[32m✓ Cross-compilation complete.\033[0m"

# Create a macOS universal binary from the amd64 and arm64 builds
.PHONY: macos-universal
macos-universal:
	@echo -e "\033[34m>> Creating macOS Universal Binary...\033[0m"
	@if [ ! -f "$(BIN_DIR)/darwin_amd64/$(BINARY_NAME)" ] || [ ! -f "$(BIN_DIR)/darwin_arm64/$(BINARY_NAME)" ]; then \
		echo -e "\033[31mError: Missing darwin_amd64 or darwin_arm64 builds. Run 'make cross-build' first.\033[0m"; \
		exit 1; \
	fi
	@mkdir -p "$(BIN_DIR)/darwin_universal"
	@lipo -create -output "$(BIN_DIR)/darwin_universal/$(BINARY_NAME)" \
		"$(BIN_DIR)/darwin_amd64/$(BINARY_NAME)" \
		"$(BIN_DIR)/darwin_arm64/$(BINARY_NAME)"
	@echo -e "\033[32m✓ Universal binary created.\033[0m"

# Create final release archives in the ./bin directory
.PHONY: package
package: clean cross-build macos-universal
	@echo -e "\033[34m>> Packaging release archives into $(BIN_DIR)...";
	@cd $(BIN_DIR) && for platform_dir in *; do \
		if [ -d "$$platform_dir" ]; then \
			archive_name="$(BINARY_NAME)_$(VERSION)_$$platform_dir"; \
			binary_path="$$platform_dir/$(BINARY_NAME)"; \
			if [[ "$$platform_dir" == *"windows"* ]]; then \
				mv "$$binary_path" "$$platform_dir/$(BINARY_NAME).exe"; \
				zip -j "$$archive_name.zip" "$$platform_dir/$(BINARY_NAME).exe" > /dev/null; \
			else \
				tar -czf "$$archive_name.tar.gz" -C "$$platform_dir" $(BINARY_NAME) > /dev/null; \
			fi; \
			
			echo -e "  \033[32m✓ Created archive:\033[0m $$archive_name"; \
		fi; \
	done
	@echo -e "\033[32m✓ Packaging complete.\033[0m"


# --- Utility Targets ---

# Clean up all build artifacts
.PHONY: clean
clean:
	@echo -e "\033[34m>> Cleaning up...\033[0m"
	@rm -rf $(BIN_DIR)

# Check for gox dependency
.PHONY: check-gox
check-gox:
	@if ! command -v gox &> /dev/null; then \
		echo -e "\033[31mError: gox is not installed. Please run: go install github.com/mitchellh/gox@latest\033[0m"; \
		exit 1; \
	fi

# Show help message
.PHONY: help
help:
	@echo -e "Usage: make <target>"
	@echo -e ""
	@echo -e "Targets:"
	@echo -e "  all              Build for all target platforms (cross-compile)."
	@echo -e "  build            Build for the current host platform into ./bin/{os}_{arch}/"
	@echo -e "  test             Run all tests."
	@echo -e "  cross-build      Cross-compile for all target platforms into ./bin/"
	@echo -e "  macos-universal  Create a macOS universal binary in ./bin/darwin_universal/"
	@echo -e "  package          Create final release archives in the ./bin/ directory."
	@echo -e "  clean            Remove all build artifacts from ./bin/"
	@echo -e "  help             Show this help message."