# OpenTelemetry Metrics in SenHub Agent

Ce document décrit les conventions utilisées pour les métriques dans SenHub Agent.

## Conventions de Nommage

Les métriques suivent les conventions de nommage OpenTelemetry:

1. **Structure hiérarchique** - Utilisation des points pour séparer les niveaux hiérarchiques
2. **Snake case** - Utilisation du format snake_case pour les mots composés
3. **Sans unités** - Les unités ne sont pas incluses dans les noms de métriques

## Catégories de Métriques

### Métriques Matérielles (`hardware.*`)

Les métriques liées au matériel suivent la convention `hardware.<composant>.<attribut>`.

Exemples:
- `hardware.system.health` - État de santé global du système
- `hardware.temperature` - Température d'un composant
- `hardware.fan.speed` - Vitesse d'un ventilateur
- `hardware.power.usage` - Consommation d'énergie
- `hardware.storage.drive.size` - Taille d'un disque

## Valeurs Énumérées (Lookups)

Pour les états et statuts, nous utilisons des valeurs numériques standardisées:

### États de Santé (`*.health`)

Les états de santé suivent cette convention:
- `0` - OK / Healthy
- `1` - Warning / Degraded
- `2` - Critical / Error
- `3` - Unknown

### États d'Alimentation (`hardware.system.power.state`)

Les états d'alimentation suivent cette convention:
- `0` - Off
- `1` - On
- `2` - Powering On (transitional)
- `3` - Powering Off (transitional)
- `4` - Unknown / Other

## Tags (Attributs)

Les tags suivants sont utilisés pour contextualiser les métriques:

- `host` - Nom d'hôte du système
- `controller` - Identifiant du contrôleur (A, B)
- `chassis_id` - Identifiant du châssis
- `system_id` - Identifiant du système
- `sensor_name` - Nom du capteur
- `manufacturer` - Fabricant du matériel
- `model` - Modèle du matériel

## Exemples de Métriques

1. Température d'un capteur:
```
Nom: hardware.temperature
Valeur: 42.5
Tags: 
  - host: server-01
  - controller: A
  - sensor_name: CPU1_Temp
```

2. État de santé d'un disque:
```
Nom: hardware.storage.drive.health
Valeur: 0 (OK)
Tags:
  - host: server-01
  - controller: A
  - drive_id: Disk1
  - model: ST1000NM0008
```

3. Capacité d'un volume:
```
Nom: hardware.storage.volume.size
Valeur: 1000000000000
Tags:
  - host: server-01
  - controller: A
  - volume_id: vol1
  - raid_type: RAID5
```

# OpenTelemetry Probe

Le probe OpenTelemetry permet de collecter des télémétries (métriques, traces, logs) depuis des endpoints OTLP (OpenTelemetry Protocol) en utilisant HTTP ou gRPC.

## Configuration

### Configuration générale

```yaml
probes:
  - name: otel
    interval: 60  # intervalle de collecte en secondes
    telemetry_types:
      - metrics
      - traces
      - logs
    http:  # configuration du collecteur HTTP
      endpoint: "http://localhost:4318"
      timeout: 30  # timeout en secondes
      headers:
        Authorization: "Bearer <token>"
      username: "user"  # optionnel - pour l'authentification basic
      password: "pass"  # optionnel
      telemetry_types:  # optionnel - surcharge les types de télémétrie globaux
        - metrics
        - logs
    grpc:  # configuration du collecteur gRPC
      endpoint: "localhost:4317"
      timeout: 30  # timeout en secondes
      insecure: false  # utiliser une connexion non-sécurisée
      token: "<auth-token>"  # optionnel
      telemetry_types:  # optionnel - surcharge les types de télémétrie globaux
        - metrics
        - traces
```

## Dépendances

Pour utiliser ce probe, les dépendances suivantes doivent être ajoutées au fichier `go.mod`:

```
require (
  go.opentelemetry.io/otel v1.24.0
  go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.24.0
  go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.24.0
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.24.0
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.24.0
  go.opentelemetry.io/otel/exporters/otlp/otlplogs/otlplogsgrpc v0.48.0
  go.opentelemetry.io/otel/exporters/otlp/otlplogs/otplogshttp v0.48.0
  go.opentelemetry.io/otel/sdk v1.24.0
  go.opentelemetry.io/otel/sdk/metric v1.24.0
  go.opentelemetry.io/proto/otlp v1.1.0
  google.golang.org/grpc v1.63.0
)
```

## Fonctionnement

Le probe OpenTelemetry offre les fonctionnalités suivantes:

1. Collection de télémétrie via le protocole OTLP (HTTP ou gRPC)
2. Support pour les métriques, traces et logs OpenTelemetry
3. Configuration flexible des endpoints et des paramètres d'authentification
4. Fonctionnement en parallèle des collecteurs HTTP et gRPC

## Types de télémétrie

- **métriques** : Données numériques qui décrivent l'état ou la performance d'un système
- **traces** : Informations sur le chemin d'exécution d'une requête à travers un système
- **logs** : Enregistrements textuels d'événements survenus dans un système

## Ajout au registre des probes

Pour activer ce probe, ajoutez-le au registre des probes dans `internal/agent/probes/registry.go`:

```go
var probeConstructors = map[string]ProbeConstructor{
    // Autres probes...
    "otel": otel.NewOtelProbe,
}
```

N'oubliez pas d'ajouter l'import correspondant:

```go
import (
    // Autres imports...
    "senhub-agent.go/internal/agent/probes/otel"
)
```