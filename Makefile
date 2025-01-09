EXECUTABLE=senhub-agent
WINDOWS=$(EXECUTABLE)_windows_amd64.exe
LINUX_AMD64=$(EXECUTABLE)_linux_amd64
LINUX_ARM64=$(EXECUTABLE)_linux_arm64
DARWIN=$(EXECUTABLE)_darwin_amd64
VERSION=$(shell git describe --tags --abbrev=0 --match='v[0-9]*.[0-9]*.[0-9]*' 2> /dev/null | sed 's/^.//')
COMMIT_HASH=$(shell git describe --tags --always --long --dirty)
ENV ?= production
PRODUCTION_URL="https://eu-west-1.intake.senhub.io"
DEVELOPMENT_URL="https://eu-west-1.intake-dev.senhub.io"

# Package to set version variable
PACKAGE="senhub-agent.go/internal/agent/cliArgs"

BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell go version | cut -d' ' -f3)

# Modifier vos ldflags pour inclure ces informations
LDFLAGS=-s -w \
    -X '${PACKAGE}.Version=$(VERSION)' \
    -X '${PACKAGE}.CommitHash=$(COMMIT_HASH)' \
    -X '${PACKAGE}.BuildTime=$(BUILD_TIME)' \
    -X '${PACKAGE}.GoVersion=$(GO_VERSION)' \
    -X '${PACKAGE}.Env=${ENV}' \
    -X '${PACKAGE}.ProductionURL=${PRODUCTION_URL}' \
    -X '${PACKAGE}.DevelopmentURL=${DEVELOPMENT_URL}'

version-info:
		@echo "Version:    $(VERSION)"
		@echo "Commit:     $(COMMIT_HASH)"
		@echo "Build time: $(BUILD_TIME)"
		@echo "Go version: $(GO_VERSION)"
		@echo "Env:        $(ENV)"

check-version:
    @if [ "$(VERSION)" = "" ]; then \
        echo "ERROR: No version tag found" >&2; \
        exit 1; \
    fi

bump-version:
		@read -p "New version number (current: $(VERSION)): " new_version; \
		git tag -a "v$$new_version" -m "Version $$new_version"; \
		git push origin "v$$new_version"


# Build the application
all: build test

# Build the application
build: build-windows build-linux build-darwin ## Build binaries
	@echo version: $(VERSION) - commit: $(COMMIT_HASH)

build-windows: ## Build for Windows
	    @env GOOS=windows GOARCH=amd64 go build -o $(WINDOWS) -ldflags="$(LDFLAGS)" ./cmd/agent/main.go

build-linux: ## Build for Linux
	    @env GOOS=linux GOARCH=amd64 go build -o $(LINUX_AMD64) -ldflags="$(LDFLAGS)" ./cmd/agent/main.go
	    @env GOOS=linux GOARCH=arm64 go build -o $(LINUX_ARM64) -ldflags="$(LDFLAGS)" ./cmd/agent/main.go

build-darwin: ## Build for Darwin (macOS)
	    @env GOOS=darwin GOARCH=amd64 go build -o $(DARWIN) -ldflags="$(LDFLAGS)" ./cmd/agent/main.go

install: ## Install the application
	@./scripts/setup

# Run the application
run:
	@go run cmd/agent/main.go
	@go run cmd/service/main.go

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

test-vars:
	@echo "Build variables:"
	@echo "  PACKAGE:          ${PACKAGE}"
	@echo "  PRODUCTION_URL:   ${PRODUCTION_URL}"
	@echo "  DEVELOPMENT_URL:  ${DEVELOPMENT_URL}"
	@echo "  ENV:             ${ENV}"
	@echo "Full LDFLAGS:"
	@echo "  ${LDFLAGS}"

# Clean the binary
clean:
	@echo "Cleaning..."
	@rm -f $(WINDOWS) $(LINUX) $(DARWIN)

# Live Reload
watch: clean
	@if command -v air > /dev/null; then \
            air -c .air.toml; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

.PHONY: all build build-windows build-linux build-darwin run test clean watch
