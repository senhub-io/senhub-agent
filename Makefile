EXECUTABLE=senhub-agent
DIST_DIR=dist
WINDOWS=$(DIST_DIR)/$(EXECUTABLE)_windows_amd64.exe
LINUX_AMD64=$(DIST_DIR)/$(EXECUTABLE)_linux_amd64
LINUX_ARM64=$(DIST_DIR)/$(EXECUTABLE)_linux_arm64
DARWIN=$(DIST_DIR)/$(EXECUTABLE)_darwin_amd64
VERSION=$(shell git tag -l | grep -E '^[0-9]+\.[0-9]+\.[0-9]+.*$$' | sort -V | tail -n1)
COMMIT_HASH=$(shell git describe --tags --always --long --dirty)
ENV ?= production
PRODUCTION_URL="https://eu-west-1.intake.senhub.io"
DEVELOPMENT_URL="https://eu-west-1.intake-dev.senhub.io"

# Package to set version variable
PACKAGE="senhub-agent.go/internal/agent/cliArgs"

BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell go version | cut -d' ' -f3)
COVERAGE_FILE=coverage.out

# Couleurs pour l'affichage
GREEN=\033[0;32m
YELLOW=\033[0;33m
RED=\033[0;31m
NC=\033[0m # No Color

# Update ldflags to include this information
LDFLAGS=-s -w \
    -X '${PACKAGE}.Version=$(VERSION)' \
    -X '${PACKAGE}.CommitHash=$(COMMIT_HASH)' \
    -X '${PACKAGE}.BuildTime=$(BUILD_TIME)' \
    -X '${PACKAGE}.GoVersion=$(GO_VERSION)' \
    -X '${PACKAGE}.Env=${ENV}' \
    -X '${PACKAGE}.ProductionURL=${PRODUCTION_URL}' \
    -X '${PACKAGE}.DevelopmentURL=${DEVELOPMENT_URL}'

# ========================================
# VERSION MANAGEMENT
# ========================================

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

# Manual version management (use for development and RC versions)
bump-version:
		@current_version=$$(echo "$(VERSION)" | sed 's/-rc//'); \
		if [[ "$(VERSION)" == *"-rc"* ]]; then \
			echo "Current version: $(VERSION) (Release Candidate)"; \
			read -p "Do you want to create a release version? [Y/n] " make_release; \
		if [[ "$$make_release" != "n" && "$$make_release" != "N" ]]; then \
			new_version="$$current_version"; \
		else \
			read -p "Enter new RC version [$$current_version-rc]: " new_version; \
				: "$${new_version:=$$current_version-rc}"; \
			fi; \
			else \
				echo "Current version: $(VERSION)"; \
				read -p "Is this a release candidate? [Y/n] " is_rc; \
				if [[ "$$is_rc" != "n" && "$$is_rc" != "N" ]]; then \
					read -p "Enter new version [$$current_version-rc]: " new_version; \
					: "$${new_version:=$$current_version-rc}"; \
				else \
					read -p "Enter new version [$$current_version]: " new_version; \
					: "$${new_version:=$$current_version}"; \
				fi; \
			fi; \
			echo "Creating new version: v$$new_version"; \
			git tag -a "v$$new_version" -m "Version $$new_version"; \
			git push origin "v$$new_version"

# Delete a version tag (useful for corrections)
delete-version:
	@echo "Current tags:"; \
	git tag -l; \
	read -p "Enter tag to delete: v" version_to_delete; \
	git tag -d "v$$version_to_delete"; \
	git push origin ":refs/tags/v$$version_to_delete"

# ========================================
# BUILD TARGETS
# ========================================

# Create dist directory
create-dist:
	@mkdir -p $(DIST_DIR)

# Build the application
all: create-dist build test ## Build all binaries and run tests

build: build-windows build-linux build-darwin ## Build binaries
	@echo version: $(VERSION) - commit: $(COMMIT_HASH)

build-windows: create-dist ## Build for Windows
	    @env GOOS=windows GOARCH=amd64 go build -o $(WINDOWS) -ldflags="$(LDFLAGS)" ./cmd/agent/

build-linux: create-dist ## Build for Linux
	    @env GOOS=linux GOARCH=amd64 go build -o $(LINUX_AMD64) -ldflags="$(LDFLAGS)" ./cmd/agent/
	    @env GOOS=linux GOARCH=arm64 go build -o $(LINUX_ARM64) -ldflags="$(LDFLAGS)" ./cmd/agent/

build-darwin: create-dist ## Build for Darwin (macOS)
	    @env GOOS=darwin GOARCH=amd64 go build -o $(DARWIN) -ldflags="$(LDFLAGS)" ./cmd/agent/

# ========================================
# PACKAGING TARGETS
# ========================================

# Create ZIP packages for all binaries
package: build ## Create ZIP packages for all platforms
	@echo "$(GREEN)📦 Creating ZIP packages...$(NC)"
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_windows_amd64.zip $(EXECUTABLE)_windows_amd64.exe
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_linux_amd64.zip $(EXECUTABLE)_linux_amd64
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_linux_arm64.zip $(EXECUTABLE)_linux_arm64
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_darwin_amd64.zip $(EXECUTABLE)_darwin_amd64
	@echo "$(GREEN)✅ ZIP packages created in $(DIST_DIR)/$(NC)"
	@ls -la $(DIST_DIR)/*.zip

# Create ZIP package for specific platform
package-windows: build-windows ## Create ZIP package for Windows
	@echo "$(GREEN)📦 Creating Windows ZIP package...$(NC)"
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_windows_amd64.zip $(EXECUTABLE)_windows_amd64.exe
	@echo "$(GREEN)✅ Windows ZIP package created: $(DIST_DIR)/$(EXECUTABLE)_windows_amd64.zip$(NC)"

package-linux: build-linux ## Create ZIP packages for Linux
	@echo "$(GREEN)📦 Creating Linux ZIP packages...$(NC)"
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_linux_amd64.zip $(EXECUTABLE)_linux_amd64
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_linux_arm64.zip $(EXECUTABLE)_linux_arm64
	@echo "$(GREEN)✅ Linux ZIP packages created$(NC)"

package-darwin: build-darwin ## Create ZIP package for macOS
	@echo "$(GREEN)📦 Creating macOS ZIP package...$(NC)"
	@cd $(DIST_DIR) && zip -9 $(EXECUTABLE)_darwin_amd64.zip $(EXECUTABLE)_darwin_amd64
	@echo "$(GREEN)✅ macOS ZIP package created: $(DIST_DIR)/$(EXECUTABLE)_darwin_amd64.zip$(NC)"

install: ## Install the application
	@./scripts/setup

# ========================================
# DEVELOPMENT TARGETS
# ========================================

# Run the application
run:
	@go run cmd/agent/main.go

# Live Reload (development tool)
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

# ========================================
# TESTING & QUALITY TARGETS
# ========================================

# Test the application (original)
test:
	@echo "Testing..."
	@go test ./... -v

# Database probes integration tests — gated behind a build tag so
# they're opt-in. Spins up MySQL + Postgres via the docker-compose
# fixture under test/database/, waits for the engines to be ready,
# runs the probes against them, then tears the fixture down.
test-database: ## Integration tests against a real MySQL + Postgres
	@echo "$(GREEN)🐳 Starting database fixture...$(NC)"
	@docker compose -f test/database/docker-compose.yml up -d --wait
	@echo "$(GREEN)🧪 Running database integration tests...$(NC)"
	@MYSQL_TEST_DSN='root:test@tcp(127.0.0.1:3306)/' \
		POSTGRES_TEST_DSN='host=127.0.0.1 port=5432 user=postgres password=test dbname=postgres sslmode=disable' \
		go test -tags=database_integration -v \
			./internal/agent/probes/mysql/... \
			./internal/agent/probes/postgresql/...
	@echo "$(GREEN)🧹 Tearing down database fixture...$(NC)"
	@docker compose -f test/database/docker-compose.yml down -v
	@echo "$(GREEN)✅ Database integration tests done$(NC)"

# NEW: Test avec détection de race conditions
test-race: ## Test avec détection de race conditions
	@echo "$(GREEN)🏃‍♂️ Tests avec détection de race conditions...$(NC)"
	@go test -race -v ./...
	@echo "$(GREEN)✅ Tests race terminés$(NC)"

# NEW: Tests de performance
benchmark: ## Tests de performance
	@echo "$(GREEN)⚡ Tests de performance...$(NC)"
	@go test -bench=. -benchmem ./...
	@echo "$(GREEN)✅ Benchmarks terminés$(NC)"

# NEW: Rapport de couverture
coverage: ## Rapport de couverture de tests
	@echo "$(GREEN)📊 Génération du rapport de couverture...$(NC)"
	@go test -coverprofile=$(COVERAGE_FILE) ./...
	@go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "$(GREEN)✅ Rapport généré: coverage.html$(NC)"
	@echo "$(YELLOW)📈 Résumé de la couverture:$(NC)"
	@go tool cover -func=$(COVERAGE_FILE) | tail -1

# NEW: Analyse de qualité du code
lint: ## Analyse de qualité du code (golangci-lint)
	@echo "$(GREEN)🔍 Analyse de qualité du code...$(NC)"
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "$(RED)❌ golangci-lint non installé. Exécutez 'make install-tools'$(NC)"; \
		exit 1; \
	}
	@golangci-lint run --timeout=5m
	@echo "$(GREEN)✅ Analyse lint terminée$(NC)"

# NEW: Correction automatique des problèmes
lint-fix: ## Corrige automatiquement les problèmes de style
	@echo "$(GREEN)🔧 Correction automatique des problèmes...$(NC)"
	@go fmt ./...
	@go mod tidy
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run --fix --timeout=5m || echo "$(YELLOW)⚠️ golangci-lint non disponible$(NC)"
	@echo "$(GREEN)✅ Corrections appliquées$(NC)"

# NEW: Audit de sécurité
security: ## Audit de sécurité (gosec + govulncheck)
	@echo "$(GREEN)🛡️ Audit de sécurité...$(NC)"
	@command -v govulncheck >/dev/null 2>&1 || { \
		echo "$(RED)❌ govulncheck non installé. Exécutez 'make install-tools'$(NC)"; \
		exit 1; \
	}
	@if command -v gosec >/dev/null 2>&1; then \
		echo "$(YELLOW)🔒 Analyse gosec...$(NC)"; \
		gosec ./...; \
	else \
		echo "$(YELLOW)⚠️ gosec non installé - ignoré$(NC)"; \
	fi
	@echo "$(YELLOW)🔍 Vérification des vulnérabilités...$(NC)"
	@govulncheck ./...
	@echo "$(GREEN)✅ Audit de sécurité terminé$(NC)"

# NEW: Installation des outils de qualité
install-tools: ## Installe tous les outils de qualité
	@echo "$(GREEN)📦 Installation des outils de développement...$(NC)"
	@echo "$(YELLOW)Installing golangci-lint...$(NC)"
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "$(YELLOW)Skipping gosec (repository issue)...$(NC)"
	@echo "$(YELLOW)Note: Install gosec manually with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest$(NC)"
	@echo "$(YELLOW)Installing govulncheck...$(NC)"
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "$(YELLOW)Installing staticcheck...$(NC)"
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@echo "$(GREEN)✅ Tous les outils sont installés$(NC)"

# NEW: Vérification complète avant commit
pre-commit: lint-fix test-race lint ## Vérifications avant commit
	@echo "$(GREEN)✅ Prêt pour le commit !$(NC)"

# NEW: Vérification complète pour CI/CD
quality-check: lint test-race security ## Vérification complète de qualité
	@echo "$(GREEN)🎉 CONTRÔLES DE QUALITÉ RÉUSSIS ! 🎉$(NC)"
	@echo "$(GREEN)✅ Tests + race conditions: OK$(NC)"
	@echo "$(GREEN)✅ Qualité du code: OK$(NC)"
	@echo "$(GREEN)✅ Sécurité: OK$(NC)"

# NEW: Release avec contrôles de qualité
release: quality-check build ## Prépare une release avec contrôles qualité
	@echo "$(GREEN)🚀 Release prête avec contrôles de qualité validés$(NC)"

# ========================================
# UTILITY TARGETS
# ========================================

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
	@rm -rf $(DIST_DIR)
	@rm -f $(COVERAGE_FILE)
	@rm -f coverage.html
	@go clean -testcache

# NEW: Aide avec tous les targets
help: ## Affiche cette aide
	@echo "$(GREEN)senhub-agent - Commandes disponibles:$(NC)"
	@echo ""
	@echo "$(YELLOW)🔨 Build & Deploy:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '(build|package|install|run|watch|clean)' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(YELLOW)🧪 Tests & Qualité:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '(test|lint|security|coverage|benchmark)' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(YELLOW)🔄 Workflows:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '(pre-commit|quality-check|release)' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(YELLOW)🛠️  Outils:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -E '(install-tools|help)' | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'

.PHONY: all build build-windows build-linux build-darwin package package-windows package-linux package-darwin run test test-race benchmark coverage lint lint-fix security install-tools pre-commit quality-check release clean watch create-dist help
