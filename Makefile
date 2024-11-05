# Simple Makefile for a Go project

# Build the application
all: build test

# Build the application
build:
	@echo "Building Agent..."
	@go build -o senhub-agent cmd/agent/main.go

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
	@rm -f main

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

.PHONY: all build run test clean watch
