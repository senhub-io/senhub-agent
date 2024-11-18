EXECUTABLE=senhub-agent
WINDOWS=$(EXECUTABLE)_windows_amd64.exe
LINUX=$(EXECUTABLE)_linux_amd64
DARWIN=$(EXECUTABLE)_darwin_amd64
VERSION=$(shell git describe --tags --abbrev=0 --match='v[0-9]*.[0-9]*.[0-9]*' 2> /dev/null | sed 's/^.//')
COMMIT_HASH=$(shell git describe --tags --always --long --dirty)
ENV ?= production

# Package to set version variable
PACKAGE="senhub-agent.go/internal/agent/cliArgs"

# Build the application
all: build test

# Build the application
build: build-windows build-linux build-darwin ## Build binaries
	@echo version: $(VERSION) - commit: $(COMMIT_HASH)

build-windows: ## Build for Windows
		@env GOOS=windows GOARCH=amd64 go build -o $(WINDOWS) -ldflags="-s -w -X ${PACKAGE}.version=$(VERSION) -X ${PACKAGE}.commit_hash=$(COMMIT_HASH) -X ${PACKAGE}.env=${ENV}"  ./cmd/agent/main.go

build-linux: ## Build for Linux
		@env GOOS=linux GOARCH=amd64 go build -o $(LINUX) -ldflags="-s -w -X ${PACKAGE}.version=$(VERSION) -X ${PACKAGE}.commit_hash=$(COMMIT_HASH) -X ${PACKAGE}.env=${ENV}"  ./cmd/agent/main.go

build-darwin: ## Build for Darwin (macOS)
		@env GOOS=darwin GOARCH=amd64 go build -o $(DARWIN) -ldflags="-s -w -X ${PACKAGE}.version=$(VERSION) -X ${PACKAGE}.commit_hash=$(COMMIT_HASH) -X ${PACKAGE}.env=${ENV}"  ./cmd/agent/main.go



# Run the application
run:
	@go run cmd/agent/main.go

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

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
