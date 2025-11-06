# Agent Doc-Manager - Spécialisation Go

## 🎯 Mission

Spécialiser l'agent doc-manager pour les projets **Go** avec toutes les best practices du langage et de son écosystème.

## 🌍 Contrainte Linguistique (inchangée)

```yaml
language_policy:
  communication: "français"      # Toutes interactions en français
  output: "english"              # Toute doc/code généré en anglais
```

---

## 📚 Go Documentation Best Practices

### 1. godoc Conventions

#### Package Documentation

**Règles strictes** :
```go
// Package <name> provides <concise description>.
//
// <Optional detailed explanation in subsequent paragraphs>
//
// Example usage:
//
//	probe := NewCpuProbe(config, logger)
//	datapoints, err := probe.Collect()
//
package cpu
```

**✅ BON (style senhub-agent)** :
```go
// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish
```

**❌ MAUVAIS** :
```go
// redfish package  (pas de majuscule, pas de description)
package redfish
```

#### Types Documentation

```go
// RedfishProbe implements hardware monitoring using Redfish API.
// It supports multiple vendors (Dell, HPE, Cisco, Lenovo) with
// specialized collectors for each.
type RedfishProbe struct {
    endpoint  string
    collector Collector
}
```

**Règles** :
- Commence par le nom du type
- Description concise (1 phrase)
- Détails supplémentaires si nécessaire (paragraphes suivants)
- PAS de "This type...", "This struct..." → Direct !

#### Functions Documentation

```go
// NewRedfishProbe creates a new instance of the Redfish probe.
// It requires endpoint, username, and password in the config map.
// Returns an error if required configuration is missing.
func NewRedfishProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
    // implementation
}
```

**Règles** :
- Commence par le nom de la fonction
- Décrit ce que fait la fonction (verbe d'action)
- Documente les paramètres importants
- Documente la valeur de retour
- Mentionne les erreurs possibles

#### Interfaces Documentation

```go
// Probe defines the interface that all probes must implement.
// It provides methods for lifecycle management and data collection.
type Probe interface {
    // GetName returns the unique identifier of the probe
    GetName() string

    // Collect gathers metrics and returns collected datapoints
    Collect() ([]datapoint.DataPoint, error)
}
```

**Règles** :
- Interface documentée avec son purpose
- Chaque méthode documentée
- Style concis et clair

### 2. Examples in Tests

Go encourage les exemples dans les fichiers `*_test.go` :

```go
// Example basic usage
func ExampleNewRedfishProbe() {
    config := map[string]interface{}{
        "endpoint":  "https://192.168.1.100",
        "username":  "admin",
        "password":  "password",
        "interval":  300,
    }

    logger := zerolog.New(os.Stdout)
    probe, err := NewRedfishProbe(config, &logger)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Probe created: %s\n", probe.GetName())
    // Output: Probe created: redfish
}

// Example with custom collections
func ExampleNewRedfishProbe_customCollections() {
    config := map[string]interface{}{
        "endpoint":    "https://server.local",
        "username":    "admin",
        "password":    "secret",
        "collections": []string{"system", "thermal", "power"},
    }

    logger := zerolog.New(os.Stdout)
    probe, _ := NewRedfishProbe(config, &logger)

    datapoints, _ := probe.Collect()
    fmt.Printf("Collected %d datapoints\n", len(datapoints))
}
```

**Conventions** :
- Nom : `Example<FunctionName>` ou `Example<FunctionName>_scenario`
- Commentaire `// Output:` pour validation automatique
- Montrent l'usage réel du code
- Apparaissent automatiquement dans `godoc`

### 3. Table-Driven Tests

Pattern standard Go pour les tests :

```go
func TestNewCpuProbe(t *testing.T) {
    logger := zerolog.New(os.Stderr)

    tests := []struct {
        name    string
        config  map[string]interface{}
        wantErr bool
        errMsg  string
    }{
        {
            name:    "Valid configuration",
            config:  map[string]interface{}{"interval": 30},
            wantErr: false,
        },
        {
            name:    "Missing required config",
            config:  map[string]interface{}{},
            wantErr: true,
            errMsg:  "missing endpoint",
        },
        {
            name: "Invalid interval type",
            config: map[string]interface{}{
                "interval": "not-a-number",
            },
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            probe, err := NewCpuProbe(tt.config, &logger)

            if tt.wantErr {
                if err == nil {
                    t.Errorf("Expected error but got none")
                }
                if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
                    t.Errorf("Error message = %v, want substring %v", err, tt.errMsg)
                }
                return
            }

            if err != nil {
                t.Errorf("Unexpected error: %v", err)
            }

            if probe == nil {
                t.Error("Expected probe to be created")
            }
        })
    }
}
```

### 4. Project Structure

Structure standard pour projets Go :

```
senhub-agent/
├── cmd/
│   └── agent/              # Application entry point
│       ├── main.go
│       ├── install.go      # Sub-commands
│       ├── run.go
│       └── status.go
│
├── internal/               # Private application code
│   └── agent/
│       ├── agent.go        # Core agent logic
│       ├── probes/         # Probe implementations
│       │   ├── types/      # Shared probe interfaces
│       │   ├── cpu/
│       │   ├── memory/
│       │   └── redfish/
│       └── services/       # Agent services
│           ├── logger/
│           ├── data_store/
│           └── sensor/
│
├── pkg/                    # Public library code (if any)
│
├── api/                    # API definitions (OpenAPI/Swagger)
│
├── docs/                   # Documentation
│   ├── README.md
│   ├── user-guide/
│   ├── admin-guide/
│   └── architecture/       # Architecture Decision Records (ADR)
│
├── scripts/                # Build and utility scripts
│
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── CHANGELOG.md
└── LICENSE
```

**Conventions** :
- `cmd/` : Applications (binaires)
- `internal/` : Code privé (non importable par autres projets)
- `pkg/` : Code public (bibliothèques réutilisables)
- `docs/` : Documentation markdown

---

## 🤖 Agent Spécialisé Go

### Détection Automatique

L'agent détecte un projet Go via :

```yaml
go_project_detection:
  required_files:
    - go.mod (MUST have)

  optional_indicators:
    - go.sum
    - Makefile (avec targets go)
    - cmd/ directory
    - internal/ directory
    - *_test.go files

  verification:
    - Parse go.mod pour le module name
    - Détecte Go version
    - Identifie dependencies principales
```

### Profile Style Go Généré

Quand l'agent détecte un projet Go :

```yaml
project_style:
  language: "go"
  module_name: "senhub-agent.go"  # depuis go.mod
  go_version: "1.23.2"             # depuis go.mod

  documentation:
    output_language: "english"      # TOUJOURS

    # godoc conventions
    code_comments:
      style: "godoc"
      package_comment: required
      exported_symbols: required
      unexported_symbols: optional
      format:
        - "Start with symbol name"
        - "Be concise"
        - "No 'This function...'"

    # Examples location
    examples:
      location: "*_test.go files"
      naming: "Example<FunctionName>"
      validation: "// Output: comment"

    # Tests
    tests:
      style: "table_driven"
      file_pattern: "*_test.go"
      coverage_target: 80

  project_structure:
    standard: "go_project_layout"
    entry_points: "cmd/"
    private_code: "internal/"
    public_code: "pkg/"

  markdown_docs:
    # Respecte le style général (emojis, sections, etc.)
    # mais adapté pour Go
    sections:
      - "Quick Start"
      - "Installation" (go install ou binary)
      - "Configuration"
      - "Examples" (code Go + config YAML)
      - "API Reference" (link to godoc)
      - "Development" (go build, test, etc.)
      - "Contributing"
```

### Génération de Documentation Go

#### 1. Package Documentation

**Input utilisateur (FR)** :
```
"Documente le package transformer avec ses interfaces et types"
```

**Output agent (EN)** :
```go
// Package transformers provides metric name transformation capabilities
// for converting technical metric names to user-friendly display names.
//
// The transformer system supports multiple transformation styles:
//   - Technical: Preserves original metric names
//   - Friendly: Converts to human-readable names
//   - PRTG: Optimized for PRTG Network Monitor
//
// Transformations are defined in YAML files and loaded at runtime.
// Each probe can have its own transformation definitions.
//
// Example usage:
//
//	registry := NewTransformerRegistry(logger)
//	transformer, err := registry.LoadTransformer("redfish", "friendly")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Transform a metric name
//	friendlyName := transformer.TransformMetricName(
//	    "thermal.cpu.0.temperature",
//	    map[string]string{"index": "0"},
//	)
//	// Result: "CPU Temperature - Processor 0"
//
package transformers
```

#### 2. Interface Documentation

**Input (FR)** :
```
"Documente l'interface MetricTransformer"
```

**Output (EN)** :
```go
// MetricTransformer defines the interface for transforming metric names
// and retrieving associated metadata like units and lookup tables.
//
// Implementations must provide name transformation, unit resolution,
// and optional lookup table references for metrics.
type MetricTransformer interface {
    // TransformMetricName converts a technical metric key to a display name.
    // It uses the provided tags to fill template variables in the transformation.
    //
    // Example:
    //   key: "thermal.cpu.{index}.temperature"
    //   tags: {"index": "0"}
    //   returns: "CPU Temperature - Processor 0"
    TransformMetricName(key string, tags map[string]string) string

    // GetUnit returns the measurement unit for the given metric key.
    // Returns empty string if no unit is defined.
    //
    // Example:
    //   key: "thermal.cpu.0.temperature"
    //   returns: "°C"
    GetUnit(key string) string

    // GetLookup returns the lookup table name for enum-type metrics.
    // Returns empty string if no lookup is defined.
    //
    // Example:
    //   key: "system.health.status"
    //   returns: "health_status.lookup"
    GetLookup(key string) string
}
```

#### 3. Function Documentation avec Examples

**Input (FR)** :
```
"Documente la fonction NewTransformerRegistry avec exemple"
```

**Output (EN)** :

Fichier : `transformer.go`
```go
// NewTransformerRegistry creates a new transformer registry with the given logger.
// The registry manages transformer instances and caches them for reuse.
//
// The base logger is used to create module-specific loggers for transformer operations.
func NewTransformerRegistry(baseLogger *logger.Logger) *TransformerRegistry {
    moduleLogger := logger.NewModuleLogger(baseLogger, "transformer")
    return &TransformerRegistry{
        transformers: make(map[string]MetricTransformer),
        moduleLogger: moduleLogger,
    }
}
```

Fichier : `transformer_test.go`
```go
// Example creating and using a transformer registry
func ExampleNewTransformerRegistry() {
    logger := zerolog.New(os.Stdout).Level(zerolog.InfoLevel)
    registry := NewTransformerRegistry(&logger)

    // Load a transformer
    transformer, err := registry.LoadTransformer("redfish", "friendly")
    if err != nil {
        log.Fatal(err)
    }

    // Transform a metric name
    friendlyName := transformer.TransformMetricName(
        "thermal.cpu.0.temperature",
        map[string]string{"index": "0"},
    )

    fmt.Println(friendlyName)
    // Output: CPU Temperature - Processor 0
}

// Example with error handling
func ExampleTransformerRegistry_LoadTransformer() {
    logger := zerolog.New(os.Stdout)
    registry := NewTransformerRegistry(&logger)

    // Attempt to load non-existent transformer
    _, err := registry.LoadTransformer("unknown_probe", "friendly")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
    // Output: Error: failed to load probe definition: no such file
}
```

#### 4. README.md Spécifique Go

**Input (FR)** :
```
"Crée le README.md pour le package transformers"
```

**Output (EN)** :
```markdown
# Transformers Package

The `transformers` package provides metric name transformation capabilities for the SenHub Agent, converting technical metric names to user-friendly display names.

## Features

- Multiple transformation styles (technical, friendly, PRTG-optimized)
- YAML-based transformation definitions
- Template-based name generation with variable substitution
- Unit and lookup table resolution
- Per-probe transformation configurations
- Automatic caching of transformer instances

## Installation

This is an internal package. To use it in your Go project:

\`\`\`go
import "senhub-agent.go/internal/agent/services/data_store/transformers"
\`\`\`

## Quick Start

### Basic Usage

\`\`\`go
package main

import (
    "fmt"
    "log"

    "github.com/rs/zerolog"
    "senhub-agent.go/internal/agent/services/data_store/transformers"
)

func main() {
    logger := zerolog.New(os.Stdout)

    // Create transformer registry
    registry := transformers.NewTransformerRegistry(&logger)

    // Load transformer for Redfish probe (friendly style)
    transformer, err := registry.LoadTransformer("redfish", "friendly")
    if err != nil {
        log.Fatal(err)
    }

    // Transform a metric name
    displayName := transformer.TransformMetricName(
        "thermal.cpu.0.temperature",
        map[string]string{"index": "0"},
    )

    fmt.Println(displayName)
    // Output: CPU Temperature - Processor 0

    // Get unit
    unit := transformer.GetUnit("thermal.cpu.0.temperature")
    fmt.Println(unit) // Output: °C
}
\`\`\`

### Custom Transformation Definitions

Create a YAML file in `internal/agent/services/data_store/transformers/definitions/`:

\`\`\`yaml
# myprobe.yaml
probe_name: myprobe
version: "1.0"

metrics:
  - name: "custom.metric.{id}.value"
    display_name: "Custom Metric {id}"
    unit: "count"
    description: "Custom metric counter"

  - name: "custom.status"
    display_name: "Status"
    unit: ""
    lookup: "status.lookup"
\`\`\`

## API Reference

See [pkg.go.dev](https://pkg.go.dev/senhub-agent.go/internal/agent/services/data_store/transformers) for complete API documentation.

### Core Types

- **MetricTransformer**: Interface for metric transformation
- **TransformerRegistry**: Manages transformer instances
- **ProbeDefinition**: YAML-based transformation definitions

### Key Functions

- `NewTransformerRegistry(logger)`: Create new registry
- `LoadTransformer(probe, style)`: Load transformer for probe
- `TransformMetricName(key, tags)`: Transform metric name

## Configuration

Transformer definitions are located in:
- `internal/agent/services/data_store/transformers/definitions/<probe>.yaml`
- Shared config: `definitions/shared/units.yaml`, `definitions/shared/templates.yaml`

## Testing

Run tests:

\`\`\`bash
go test ./internal/agent/services/data_store/transformers/...
\`\`\`

With coverage:

\`\`\`bash
go test -cover ./internal/agent/services/data_store/transformers/...
\`\`\`

## Examples

See [transformer_test.go](transformer_test.go) for complete examples:
- Basic transformation
- Multiple transformation styles
- Custom probe definitions
- Error handling

## Contributing

1. Follow godoc conventions for all exported symbols
2. Add table-driven tests for new functionality
3. Include `Example` functions in tests
4. Update YAML definitions when adding probes

## License

See [LICENSE](../../LICENSE) file.
```

---

## 📋 Documentation Types pour Projets Go

### 1. Code Documentation (godoc)

**Priorité** : HAUTE
**Location** : Dans les fichiers `.go`
**Style** : godoc conventions

L'agent génère automatiquement :
- Package comments
- Type/struct comments
- Interface comments avec méthodes
- Function comments
- Examples dans `*_test.go`

### 2. README.md (Root)

**Priorité** : HAUTE
**Location** : Racine du projet

**Sections standard** :
```markdown
# Project Name

## Features
## Installation
## Quick Start
## Configuration
## Documentation
## Development
## Contributing
## License
```

### 3. Architecture Decision Records (ADR)

**Priorité** : MOYENNE
**Location** : `docs/architecture/`

**Format** :
```markdown
# ADR-001: Use Redfish API for Hardware Monitoring

## Status
Accepted

## Context
We need to monitor hardware metrics from servers (temperature, power, health).
Multiple APIs available: IPMI, Redfish, proprietary solutions.

## Decision
We will use the Redfish API as our primary hardware monitoring interface.

## Consequences
### Positive
- Industry standard (DMTF)
- RESTful API (easy integration)
- Vendor-neutral (Dell, HPE, Cisco, Lenovo)

### Negative
- Requires Redfish-enabled hardware
- Not all servers support all Redfish features
- Need vendor-specific collectors for edge cases

## Implementation
- Generic collector for standard Redfish
- Vendor-specific collectors for extensions
- See: internal/agent/probes/redfish/
```

### 4. CHANGELOG.md

**Priorité** : HAUTE
**Location** : Racine du projet
**Format** : [Keep a Changelog](https://keepachangelog.com/)

```markdown
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Redis cache support for HTTP strategy
- Kafka probe for message queue monitoring

### Changed
- Improved Redfish storage metrics for Dell PowerVault
- Updated logging to use module-specific loggers

### Fixed
- Citrix logon duration calculation now excludes reconnections
- Fixed race condition in metric cache cleanup

## [0.1.56-beta] - 2025-01-15

### Added
- Offline mode with local configuration
- HTTPS support with auto-generated certificates
- Universal configuration validation API

### Changed
- Configuration format v2 (name + type fields for probes)
- Automatic migration from config v1 to v2

### Fixed
- Windows service installation path resolution
- Port binding issue in offline HTTPS mode

## [0.1.50-beta] - 2025-01-10
...
```

### 5. CONTRIBUTING.md

**Priorité** : MOYENNE
**Location** : Racine du projet

**Contenu spécifique Go** :
```markdown
# Contributing to SenHub Agent

## Development Setup

### Prerequisites
- Go 1.23.2 or later
- Make
- Git

### Clone and Build

\`\`\`bash
git clone https://github.com/senhub/agent.git
cd agent
make build
\`\`\`

## Code Standards

### Go Code Style

Follow standard Go conventions:
- Run `gofmt` before committing (enforced by pre-commit hook)
- Use `golangci-lint` for linting
- Maintain test coverage above 80%

### Documentation

All exported symbols must have godoc comments:

\`\`\`go
// Package probe provides monitoring probe implementations.
package probe

// Probe defines the interface for data collection probes.
type Probe interface {
    // Collect gathers metrics and returns datapoints.
    Collect() ([]DataPoint, error)
}
\`\`\`

### Testing

- Write table-driven tests
- Include `Example` functions
- Aim for >80% coverage

\`\`\`bash
# Run tests
make test

# With coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
\`\`\`

## Submitting Changes

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Run `make test` and `make lint`
5. Commit with conventional commit messages
6. Push and create a pull request

### Commit Message Format

\`\`\`
<type>(<scope>): <subject>

<body>

<footer>
\`\`\`

Types: feat, fix, docs, style, refactor, test, chore

Example:
\`\`\`
feat(redfish): add Dell PowerVault ME5 storage metrics

- Added volume IOPS tracking
- Implemented disk rebuild detection
- Added storage pool capacity metrics

Closes #123
\`\`\`
```

---

## 🎯 Prompts Spécialisés Go

### Prompt Go 1 : Documenter un Package

**Input (FR)** :
```
"Documente le package redfish au complet :
- Package comment
- Tous les types exportés
- Toutes les fonctions exportées
- Ajoute des examples dans les tests"
```

**Output (FR)** :
```
📝 Documentation du package redfish en cours...

🎯 Tâches à réaliser :
   1. ✅ Package comment (style godoc)
   2. ✅ Types exportés (8 trouvés)
   3. ✅ Interfaces (3 trouvées)
   4. ✅ Fonctions exportées (12 trouvées)
   5. ✅ Examples (création de 5 exemples)

📄 Fichiers modifiés :
   - internal/agent/probes/redfish/redfishProbe.go
   - internal/agent/probes/redfish/collector_interface.go
   - internal/agent/probes/redfish/redfish_client.go
   - internal/agent/probes/redfish/redfishProbe_test.go (examples)

📊 Statistiques :
   - Package comment : ✅ Ajouté
   - Types documentés : 8/8 (100%)
   - Functions documentées : 12/12 (100%)
   - Examples créés : 5 (dans *_test.go)

✅ Documentation godoc complète en anglais !

💡 Tester avec : go doc -all internal/agent/probes/redfish
```

### Prompt Go 2 : Créer des Tests Table-Driven

**Input (FR)** :
```
"Crée des tests table-driven pour la fonction TransformMetricName
avec 10 cas de test couvrant tous les scenarios"
```

**Output (FR)** :
```
🧪 Création de tests table-driven...

📋 Cas de test identifiés :
   1. Transformation simple (sans template)
   2. Transformation avec {index}
   3. Transformation avec {component}
   4. Multiple placeholders
   5. Tags manquants (fallback)
   6. Pattern non trouvé (fallback)
   7. Metric key vide
   8. Tags nil
   9. Pattern avec regex
   10. Unicode dans les noms

📄 Fichier : transformer_test.go

✅ Tests créés (EN) :
```

**Fichier généré** :
```go
func TestProbeTransformer_TransformMetricName(t *testing.T) {
    logger := zerolog.New(os.Stderr)

    tests := []struct {
        name     string
        probe    string
        style    string
        key      string
        tags     map[string]string
        want     string
    }{
        {
            name:  "Simple transformation without template",
            probe: "test",
            key:   "memory_used",
            tags:  nil,
            want:  "Memory Used",
        },
        {
            name:  "Transformation with index placeholder",
            probe: "test",
            key:   "cpu_core_0_usage",
            tags:  map[string]string{"index": "0"},
            want:  "CPU Core 0 Usage",
        },
        {
            name:  "Transformation with component placeholder",
            probe: "test",
            key:   "thermal.sensor.temp",
            tags:  map[string]string{"component": "CPU"},
            want:  "CPU Temperature",
        },
        {
            name:  "Multiple placeholders",
            probe: "test",
            key:   "storage.volume.0.iops",
            tags:  map[string]string{"index": "0", "volume_name": "SSD1"},
            want:  "Volume SSD1 (0) - IOPS",
        },
        {
            name:  "Missing tags - fallback",
            probe: "test",
            key:   "thermal.{sensor}.temp",
            tags:  map[string]string{}, // sensor tag missing
            want:  "Thermal Unknown Temp", // fallback behavior
        },
        {
            name:  "Pattern not found - auto-readable",
            probe: "test",
            key:   "custom_metric_value",
            tags:  nil,
            want:  "Custom Metric Value", // makeReadable()
        },
        {
            name:  "Empty metric key",
            probe: "test",
            key:   "",
            tags:  nil,
            want:  "",
        },
        {
            name:  "Nil tags map",
            probe: "test",
            key:   "simple_metric",
            tags:  nil,
            want:  "Simple Metric",
        },
        {
            name:  "Special characters in key",
            probe: "test",
            key:   "metric-with_special.chars",
            tags:  nil,
            want:  "Metric With Special Chars",
        },
        {
            name:  "Unicode in display name",
            probe: "test",
            key:   "temp_celsius",
            tags:  map[string]string{"unit": "°C"},
            want:  "Temperature (°C)",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            registry := NewTransformerRegistry(&logger)
            transformer, err := registry.LoadTransformer(tt.probe, tt.style)
            if err != nil {
                // Create fallback for test probe
                transformer = registry.createFallbackTransformer(tt.probe, tt.style)
            }

            got := transformer.TransformMetricName(tt.key, tt.tags)
            if got != tt.want {
                t.Errorf("TransformMetricName() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

**Confirmation (FR)** :
```
✅ Tests table-driven créés !

📊 Couverture :
   - 10 cas de test
   - Scenarios : nominal, edge cases, error cases
   - Format : Table-driven (best practice Go)

🧪 Lancer avec :
   go test -v -run TestProbeTransformer_TransformMetricName ./internal/agent/services/data_store/transformers/
```

### Prompt Go 3 : Générer Architecture Decision Record

**Input (FR)** :
```
"Crée un ADR pour expliquer pourquoi on utilise le pattern Strategy
pour le data store"
```

**Output (FR)** :
```
📝 Création de l'ADR...

📄 Fichier : docs/architecture/ADR-005-data-store-strategy-pattern.md
📅 Date : 2025-01-15
🎯 Sujet : Strategy Pattern pour Data Store

✅ ADR créé en anglais avec sections standard
```

**Fichier généré (EN)** :
```markdown
# ADR-005: Strategy Pattern for Data Store

## Status
Accepted

## Context

The SenHub Agent needs to route collected metrics to multiple destinations:
- SenHub Platform (online mode)
- PRTG Network Monitor
- HTTP REST API (offline mode)
- Event storage service
- Future integrations (Prometheus, InfluxDB, etc.)

Each destination has different requirements:
- Data format (JSON, custom protocols, REST API)
- Delivery mechanism (HTTP POST, streaming, local storage)
- Authentication methods
- Error handling and retry logic

We need a flexible architecture that allows:
1. Adding new destinations without modifying core agent code
2. Running multiple destinations simultaneously
3. Independent lifecycle management per destination
4. Configuration-driven destination selection

## Decision

We will use the **Strategy Pattern** to implement the data store layer.

Each storage destination is implemented as a `Strategy` that implements a common interface:

\`\`\`go
type Strategy interface {
    GetName() string
    AddDataPoints([]datapoint.DataPoint) error
    AddEvents([]event.Event) error
    Start(chan struct{}) error
    Shutdown(context.Context) error
}
\`\`\`

The `DataStore` acts as the context that manages multiple strategies:

\`\`\`go
type DataStore struct {
    strategies []Strategy
}

func (ds *DataStore) AddDataPoints(points []datapoint.DataPoint) error {
    for _, strategy := range ds.strategies {
        if err := strategy.AddDataPoints(points); err != nil {
            // Handle error (log, retry, etc.)
        }
    }
}
\`\`\`

## Alternatives Considered

### 1. Single Monolithic Router
**Pros**: Simpler initial implementation
**Cons**: Hard to extend, coupling between destinations, difficult testing

### 2. Plugin System with Dynamic Loading
**Pros**: Maximum flexibility, runtime loading
**Cons**: Complexity, security concerns, go plugin limitations

### 3. Observer Pattern
**Pros**: Decoupled notification
**Cons**: Less control over execution order and error handling

## Consequences

### Positive
- **Easy to extend**: New strategies added by implementing the interface
- **Testable**: Each strategy can be tested independently with mocks
- **Configuration-driven**: Strategies loaded from configuration file
- **Concurrent execution**: Strategies can run in parallel
- **Independent lifecycle**: Each strategy manages its own resources

### Negative
- **Abstraction overhead**: Interface adds slight complexity
- **Error aggregation**: Need to handle multiple strategy errors
- **Coordination**: Complex scenarios need careful orchestration

### Neutral
- **Strategy registration**: Need a registry pattern for strategy discovery

## Implementation

### Current Implementations
1. **SenHub Strategy** (`strategy_senhub.go`)
   - Sends metrics to SenHub Platform
   - HTTP POST with authentication
   - Retry logic for network failures

2. **PRTG Strategy** (`strategy_prtg.go`)
   - Exposes PRTG-compatible endpoint
   - Metric caching with TTL
   - Friendly name transformations

3. **HTTP Strategy** (`strategy_http.go`)
   - Local REST API for offline mode
   - Multiple format support (PRTG, Nagios, Prometheus)
   - Web interface integration

4. **Event Strategy** (`strategy_event.go`)
   - Queue-based event handling
   - Batch processing
   - Persistent storage option

### Registry Pattern
\`\`\`go
var strategyRegistry = map[string]StrategyConstructor{
    "senhub": NewSenHubStrategy,
    "prtg":   NewPRTGStrategy,
    "http":   NewHTTPStrategy,
    "event":  NewEventStrategy,
}
\`\`\`

### Configuration Example
\`\`\`yaml
storage:
  - name: senhub
    params:
      url: "https://api.senhub.io"

  - name: http
    params:
      port: 8080
      endpoints: ["prtg", "nagios"]
\`\`\`

## Related Decisions
- ADR-001: Probe architecture (similar pattern)
- ADR-003: Configuration management
- ADR-006: Error handling strategy (impacts all strategies)

## References
- [Strategy Pattern - Gang of Four](https://en.wikipedia.org/wiki/Strategy_pattern)
- Go interface best practices
- [internal/agent/services/data_store/](../../internal/agent/services/data_store/)

---

**Decision Date**: 2025-01-15
**Decision Maker**: Architecture Team
**Review Date**: 2025-07-15 (6 months)
```

---

## 🔧 Agent Configuration Spécialisé Go

Quand l'agent détecte un projet Go, il ajoute automatiquement :

```yaml
go_specialization:
  enabled: true

  code_documentation:
    enforce_godoc: true
    package_comment_required: true
    exported_symbols_documented: true

  tests:
    style: "table_driven"
    example_functions: true
    coverage_threshold: 80

  markdown_docs:
    include_go_examples: true
    link_to_godoc: true
    show_go_install: true

  architecture:
    adr_enabled: true
    adr_location: "docs/architecture/"
    adr_format: "standard"

  project_structure:
    verify_standard_layout: true
    suggest_improvements: true
```

---

## 📊 Métriques Qualité Go

L'agent track des métriques spécifiques Go :

```bash
doc-manager stats --go-metrics
```

**Output (FR)** :
```
📊 Métriques Qualité Go - senhub-agent
=====================================

📦 CODE DOCUMENTATION (godoc)
   Packages : 24/24 documentés (100%)
   Types exportés : 156/158 documentés (98.7%)
   Functions exportées : 892/967 documentés (92.2%)
   Interfaces : 15/15 documentées (100%)

   ⚠️  Manquants :
   - internal/agent/services/cache/redis.go:42 (RedisConfig struct)
   - internal/agent/services/cache/redis.go:87 (Connect function)
   - cmd/agent/migrate.go:15 (migrateConfig function)
   ... (72 autres)

🧪 TESTS
   Coverage totale : 84.3%
   Table-driven tests : 156/178 (87.6%)
   Example functions : 23 fichiers

   📈 Par package :
   - internal/agent/probes/cpu : 95.2%
   - internal/agent/probes/redfish : 88.7%
   - internal/agent/services/data_store : 92.1%
   - internal/agent/services/configuration : 76.4% ⚠️

   ⚠️  < 80% coverage :
   - internal/agent/services/configuration (76.4%)
   - cmd/agent (71.2%)

📁 PROJECT STRUCTURE
   ✅ Suit le Go project layout standard
   ✅ cmd/ pour binaires
   ✅ internal/ pour code privé
   ✅ pkg/ vide (pas de lib publique - OK)
   ✅ go.mod présent et valide

   💡 Suggestions :
   - Créer api/ pour OpenAPI specs
   - Ajouter docs/architecture/ pour ADRs

📝 MARKDOWN DOCS
   Total : 48 fichiers
   Contiennent exemples Go : 12 (25%)
   Liens vers godoc : 8 (16.7%)

   💡 Suggestions :
   - Ajouter plus d'exemples Go dans docs/
   - Lier davantage vers pkg.go.dev

🏗️  ARCHITECTURE
   ADRs présents : 0 ⚠️

   💡 Suggestion :
   - Créer docs/architecture/
   - Documenter décisions majeures (Strategy pattern, etc.)

📊 SCORE GLOBAL GO : 87/100 (B+)
   - Documentation code : 95/100 (A)
   - Tests : 84/100 (B)
   - Structure : 90/100 (A-)
   - Markdown docs : 75/100 (C+)
   - Architecture : 70/100 (C)

🎯 Pour atteindre A+ (95+) :
   1. Documenter 75 fonctions manquantes (2-3h)
   2. Augmenter coverage à 85%+ (4-5h)
   3. Créer 5 ADRs pour décisions clés (3-4h)
   4. Ajouter exemples Go dans 10+ docs (2h)

Total effort : ~2 jours
```

---

## 🚀 Résumé

L'agent doc-manager spécialisé Go apporte :

### ✅ Automatisations Go
1. **godoc enforcement** : Vérifie et génère commentaires conformes
2. **Example generation** : Crée des `Example` functions dans `*_test.go`
3. **Table-driven tests** : Template pour tests Go idiomatiques
4. **ADR creation** : Architecture Decision Records pour décisions importantes
5. **Project layout verification** : Vérifie conformité au standard Go

### ✅ Conventions Respectées
- Package comments obligatoires
- Types/Functions exportés documentés
- Pas de "This function..." (direct au but)
- Examples exécutables et validés
- Table-driven tests systématiques
- Structure projet standard (cmd/, internal/, pkg/)

### ✅ Outils Intégrés
- `go doc` validation
- `go test` coverage tracking
- Liens automatiques vers pkg.go.dev
- Suggestions `go install`

**Communication** : Toujours en français
**Génération** : Toujours en anglais
**Style** : 100% Go idiomatique

Voulez-vous que je créeune version exécutable de cet agent spécialisé Go ?