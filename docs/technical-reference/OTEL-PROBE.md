# OpenTelemetry Probe pour SenHub Agent

Ce document décrit l'utilisation du probe OpenTelemetry dans le SenHub Agent, qui permet de collecter des télémétries (métriques, traces, logs) depuis des endpoints OTLP (OpenTelemetry Protocol).

## Introduction

Le probe OpenTelemetry est un composant du SenHub Agent qui permet de recevoir des données de télémétrie depuis des applications instrumentées avec OpenTelemetry. Il prend en charge les protocoles HTTP et gRPC pour la collecte des données, offrant ainsi une flexibilité maximale pour s'intégrer dans différentes architectures.

### Fonctionnalités principales

- Réception de données au format OTLP (OpenTelemetry Protocol)
- Support des protocoles HTTP et gRPC
- Collecte simultanée de métriques, traces et logs
- Configuration flexible des endpoints et des options d'authentification
- Support TLS pour les communications sécurisées
- Intégration transparente avec le système de métriques de SenHub

## Dépendances requises

Pour utiliser le probe OpenTelemetry, ajoutez les dépendances suivantes à votre fichier `go.mod`:

```go
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

## Configuration du probe

### Configuration de base

Voici un exemple de configuration de base du probe OpenTelemetry dans le fichier de configuration du SenHub Agent:

```yaml
probes:
  - name: otel
    interval: 60  # intervalle de collecte en secondes
    telemetry_types:
      - metrics
      - traces
      - logs
```

### Configuration du collecteur HTTP

```yaml
probes:
  - name: otel
    interval: 60
    http:
      endpoint: "http://localhost:4318"
      timeout: 30  # timeout en secondes
      headers:
        Content-Type: "application/json"
        Authorization: "Bearer <token>"
      telemetry_types:
        - metrics
        - logs
```

### Configuration du collecteur gRPC

```yaml
probes:
  - name: otel
    interval: 60
    grpc:
      endpoint: "localhost:4317"
      timeout: 30  # timeout en secondes
      insecure: false  # utiliser une connexion sécurisée TLS (true pour désactiver TLS)
      telemetry_types:
        - metrics
        - traces
```

### Configuration avancée (HTTP et gRPC en parallèle)

```yaml
probes:
  - name: otel
    interval: 60
    telemetry_types:
      - metrics
      - traces
      - logs
    http:
      endpoint: "http://localhost:4318"
      timeout: 30
      telemetry_types:
        - metrics
        - logs
    grpc:
      endpoint: "localhost:4317"
      timeout: 30
      insecure: false
      telemetry_types:
        - traces
```

## Métriques collectées

Le probe OpenTelemetry peut collecter toutes les métriques envoyées par les applications instrumentées avec OpenTelemetry. Les métriques sont normalisées selon les conventions d'OpenTelemetry et respectent les conventions de nommage décrites dans [OTEL-METRICS.md](OTEL-METRICS.md).

### Types de métriques supportés

- **Compteurs** (Counters) - Valeurs qui augmentent monotoniquement
- **Jauges** (Gauges) - Valeurs qui peuvent augmenter et diminuer
- **Histogrammes** - Distributions de valeurs avec des buckets
- **UpDown Counters** - Compteurs qui peuvent augmenter et diminuer

### Exemple de métriques collectées

```
Nom: http.server.request.duration
Type: Histogram
Tags:
  - host: web-server-01
  - method: GET
  - route: /api/users
  - status_code: 200
```

```
Nom: system.memory.usage
Type: Gauge
Tags:
  - host: app-server-02
  - state: used
```

## Sécurité

### Configuration TLS

Pour les connexions gRPC, la sécurité TLS est activée par défaut. Vous pouvez configurer les paramètres TLS comme suit:

```yaml
probes:
  - name: otel
    grpc:
      endpoint: "localhost:4317"
      insecure: false  # true pour désactiver TLS
      ca_file: "/path/to/ca.pem"  # optionnel - certificat CA personnalisé
      cert_file: "/path/to/cert.pem"  # optionnel - certificat client
      key_file: "/path/to/key.pem"  # optionnel - clé privée client
```

### Authentification

Plusieurs méthodes d'authentification sont supportées:

#### Basic Auth (HTTP)

```yaml
probes:
  - name: otel
    http:
      endpoint: "http://localhost:4318"
      username: "user"
      password: "pass"
```

#### Token Authentication (HTTP & gRPC)

Pour HTTP:
```yaml
probes:
  - name: otel
    http:
      endpoint: "http://localhost:4318"
      headers:
        Authorization: "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

Pour gRPC:
```yaml
probes:
  - name: otel
    grpc:
      endpoint: "localhost:4317"
      token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

## Dépannage

### Problèmes courants et solutions

1. **Connexion refusée**
   - Vérifiez que l'endpoint OTLP est correctement configuré et actif
   - Vérifiez que le port est ouvert et accessible
   - Testez la connexion avec `telnet <host> <port>` pour vérifier l'accessibilité

2. **Erreurs d'authentification**
   - Vérifiez que les credentials sont corrects
   - Assurez-vous que les headers d'authentification sont bien configurés
   - Vérifiez que le token n'est pas expiré

3. **Erreurs TLS**
   - Vérifiez que les certificats sont valides et non expirés
   - Assurez-vous que le hostname correspond au nom dans le certificat
   - Vérifiez que les chemins des fichiers CA, cert et key sont corrects

4. **Pas de données reçues**
   - Vérifiez que les types de télémétrie (metrics, traces, logs) sont correctement configurés
   - Vérifiez que l'application source envoie bien des données
   - Augmentez le niveau de logging pour voir plus de détails

### Logging et diagnostic

Pour activer le logging détaillé du probe OpenTelemetry, ajoutez la configuration suivante:

```yaml
logging:
  level: debug
  probes:
    otel: trace  # niveau de log spécifique pour le probe OpenTelemetry
```

### Validation de la configuration

Utilisez la commande suivante pour valider la configuration du probe:

```bash
senhub-agent validate --config path/to/config.yaml
```

## Exemples d'utilisation

### Collecte de métriques d'une application web

```yaml
probes:
  - name: otel
    interval: 30
    telemetry_types:
      - metrics
    http:
      endpoint: "http://webapp:4318"
      timeout: 15
```

### Collecte de traces depuis un service microservices

```yaml
probes:
  - name: otel
    interval: 60
    telemetry_types:
      - traces
    grpc:
      endpoint: "tracing-service:4317"
      timeout: 30
      insecure: false
      token: "${OTEL_AUTH_TOKEN}"  # utilisation d'une variable d'environnement
```

### Collecte complète pour un environnement de production

```yaml
probes:
  - name: otel
    interval: 60
    telemetry_types:
      - metrics
      - traces
      - logs
    http:
      endpoint: "https://otel-collector.example.com:4318"
      timeout: 30
      headers:
        Authorization: "Bearer ${OTEL_TOKEN}"
      verify_ssl: true
    retry:
      max_attempts: 3
      initial_delay: 5
      max_delay: 30
      multiplier: 2
```

## Ressources additionnelles

- [Documentation officielle OpenTelemetry](https://opentelemetry.io/docs/)
- [Spécification du protocole OTLP](https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/otlp.md)
- [Guide d'instrumentation OpenTelemetry pour Go](https://opentelemetry.io/docs/instrumentation/go/)