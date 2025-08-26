# Makefile for lookup-go

# Go a new version of the binary. It's recommended to run `make clean` before creating a new release.

# Default shell
SHELL = /bin/bash

# --- Configuration ---

# The name of the binary
BINARY_NAME = lookup-go

# Get the version from the latest git tag. e.g., v1.2.3
# If no tags are available, it uses the short commit hash.
VERSION ?= $(shell git describe --tags --always --abbrev=0 2>/dev/null || git rev-parse --short HEAD)
# Get the commit hash
COMMIT_HASH = $(shell git rev-parse --short HEAD)
# Get the build date
BUILD_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go linker flags to inject version information into the binary.
# This uses the -X flag to set the value of the 'version' variable in the main package.
LDFLAGS = -ldflags="-s -w -X main.version=$(VERSION) (build: $(COMMIT_HASH), date: $(BUILD_DATE))"

# The platforms to build for. Format: "os/arch"
# You can add or remove platforms here.
PLATFORMS ?= darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

# Release directory
RELEASE_DIR = ./release

# --- Main Targets ---

.PHONY: all
all: build

.PHONY: build
build:
	@echo "\033[34m>> Building $(BINARY_NAME) for current OS/Arch...\033[0m"
	go build $(LDFLAGS) -o $(BINARY_NAME) main.go

# Requires 'gox' to be installed (go install github.com/mitchellh/gox@latest)
.PHONY: release
release: check-gox clean
	@echo "\033[34m>> Starting release build for version $(VERSION)...\033[0m"
	@gox -osarch="$(PLATFORMS)" -output="$(RELEASE_DIR)/$(BINARY_NAME)_$(VERSION)_{{.OS}}_{{.Arch}}/$(BINARY_NAME)" $(LDFLAGS)
	@echo "\033[32m✓ Cross-compilation complete.\033[0m"

# Creates a macOS universal binary.
# This target must be run after 'release' as it depends on the amd64 and arm64 binaries.
.PHONY: macos-universal
macos-universal:
	@echo "\033[34m>> Creating macOS Universal Binary...\033[0m"
	@if [ ! -f "$(RELEASE_DIR)/$(BINARY_NAME)_$(VERSION)_darwin_amd64/$(BINARY_NAME)" ] || [ ! -f "$(RELEASE_DIR)/$(BINARY_NAME)_$(VERSION)_darwin_arm64/$(BINARY_NAME)" ]; then \
		echo "\033[31mError: Missing darwin_amd64 or darwin_arm64 builds. Run 'make release' first.\033[0m"; \
		exit 1; \
	fi
	@mkdir -p "$(RELEASE_DIR)/$(BINARY_NAME)_$(VERSION)_darwin_universal"
	@lipo -create -output "$(RELEASE_DIR)/$(BINARY_NAME)_$(VERSION)_darwin_universal/$(BINARY_NAME)" \
		"$(RELEASE_DIR)/$(BINARY_NAME)_$(VERSION)_darwin_amd64/$(BINARY_NAME)" \
		"$(RELEASE_DIR)/$(BINARY_NAME)_$(VERSION)_darwin_arm64/$(BINARY_NAME)"
	@echo "\033[32m✓ Universal binary created.\033[0m"

# Creates archives (.tar.gz for Unix, .zip for Windows) for all release builds.
.PHONY: package
package: release macos-universal
	@echo "\033[34m>> Packaging release archives...\033[0m"
	@cd $(RELEASE_DIR) && for dir in *; do \
		if [ -d "$$dir" ]; then \
			base_name=$${dir};
			if [[ "$$dir" == *"windows"* ]]; then \
				mv "$$dir/$(BINARY_NAME)" "$$dir/$(BINARY_NAME).exe"; \
				zip -j "$$base_name.zip" "$$dir/$(BINARY_NAME).exe" > /dev/null; \
			else \
				tar -czf "$$base_name.tar.gz" -C "$$dir" $(BINARY_NAME) > /dev/null; \
			fi; \
			rm -r "$$dir"; \
			echo "  \033[32m✓ Created archive:\033[0m $$base_name archive"; \
		fi; \
	done
	@echo "\033[32m✓ Packaging complete.\033[0m"


# --- Utility Targets ---

.PHONY: clean
clean:
	@echo "\033[34m>> Cleaning up...\033[0m"
	@rm -f $(BINARY_NAME)
	@rm -rf $(RELEASE_DIR)

# Checks if gox is installed.
.PHONY: check-gox
check-gox:
	@if ! command -v gox &> /dev/null; then \
		echo "\033[31mError: gox is not installed. Please run: go install github.com/mitchellh/gox@latest"; \
		exit 1; \
	fi

.PHONY: help
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@echo "  all              Alias for build."
	@echo "  build            Build the binary for the current OS/architecture."
	@echo "  release          Build binaries for all target platforms (requires gox)."
	@echo "  macos-universal  Create a macOS universal binary."
	@echo "  package          Create release archives for all platforms."
	@echo "  clean            Remove all build artifacts."
	@echo "  help             Show this help message."
