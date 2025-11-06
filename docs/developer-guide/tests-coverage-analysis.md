# Analyse de Couverture des Tests - SenHub Agent
Date: 2025-10-14

## Tests corrigés ✅
- **TestDetectAgentMode** : Corrigé pour refléter le nouveau comportement (clé config prévaut en mode offline)
- **Nouveau test** : Ajouté test pour mode online avec mismatch de clé

## État Global
**Tous les tests passent : ✅**
- 39 packages testés
- Couverture globale : ~32%

## Packages par priorité de tests manquants

### 🔴 CRITIQUES - 0% de couverture (pas de tests du tout)

#### 1. **cliArgs** (0%)
**Impact** : Critique - Parsing des arguments CLI
**Fichiers** : `internal/agent/cliArgs/cliArgs.go`
**Tests manquants** :
- Parsing des arguments CLI
- Validation des flags
- Valeurs par défaut
- Gestion des erreurs de parsing

#### 2. **formats/event** (0%)
**Impact** : Élevé - Format des événements
**Fichiers** : `internal/agent/formats/event/*.go`
**Tests manquants** :
- Sérialisation/désérialisation JSON
- Validation format événements
- Transformation événements

#### 3. **probes** (registry) (0%)
**Impact** : Critique - Registry des probes
**Fichiers** : `internal/agent/probes/registry.go`
**Tests manquants** :
- Enregistrement de probes
- Recherche de probes par type
- Gestion des collisions de noms
- Liste des probes disponibles

#### 4. **services/data_store** (core) (0%)
**Impact** : Critique - Cœur du data store
**Fichiers** : `internal/agent/services/data_store/data_store.go`
**Tests manquants** :
- Initialisation data store
- Routing vers strategies
- Gestion des erreurs de routing
- Shutdown propre

#### 5. **services/sensor** (0%)
**Impact** : Critique - Orchestration des probes
**Fichiers** : `internal/agent/services/sensor/*.go`
**Tests manquants** :
- Lifecycle des probes
- Collecte périodique
- Gestion des callbacks
- Recovery des erreurs

#### 6. **services/server** (0%)
**Impact** : Critique - Remote configuration
**Fichiers** : `internal/agent/services/server/*.go`
**Tests manquants** :
- Récupération config serveur
- Parsing réponses API
- Gestion timeouts/erreurs réseau
- Retry logic

#### 7. **strategies/event** (0%)
**Impact** : Moyen - Strategy événements
**Fichiers** : `internal/agent/services/data_store/strategies/event/*.go`
**Tests manquants** :
- Stockage événements
- Buffer/batching
- Gestion overflow

#### 8. **strategies/senhub** (0%)
**Impact** : Élevé - Strategy SenHub platform
**Fichiers** : `internal/agent/services/data_store/strategies/senhub/*.go`
**Tests manquants** :
- Envoi données vers SenHub
- Authentication
- Retry/backoff
- Compression

#### 9. **services/status** (0%)
**Impact** : Faible - Status reporting
**Fichiers** : `internal/agent/services/status/*.go`
**Tests manquants** :
- Health checks
- Status aggregation
- Metrics de l'agent

#### 10. **types/event** (0%)
**Impact** : Moyen - Structures d'événements
**Fichiers** : `internal/agent/types/event/*.go`
**Tests manquants** :
- Validation structures
- Sérialisation
- Helpers de création

### 🟡 PRIORITAIRES - Couverture insuffisante (<20%)

#### 1. **gateway probe** (1.4%)
**Fichiers** : `internal/agent/probes/gateway/*.go`
**Tests existants** : Basiques uniquement
**Tests manquants** :
- Ping timeout
- Packet loss calculation
- Multiple gateways
- IPv4/IPv6
- Erreurs réseau

#### 2. **host probe** (1.6%)
**Fichiers** : `internal/agent/probes/host/*.go`
**Tests existants** : Minimaux
**Tests manquants** :
- Collecte toutes métriques (CPU, RAM, Disk, Network)
- Cross-platform (Windows/Linux/macOS)
- Gestion erreurs WMI/procfs
- Performance (pas de fuites mémoire)

#### 3. **logicaldisk probe** (6.2%)
**Fichiers** : `internal/agent/probes/logicaldisk/*.go`
**Tests manquants** :
- Tous les filesystems
- Disques réseau
- Métriques IOPS
- Gestion mount points

#### 4. **network probe** (10.2%)
**Fichiers** : `internal/agent/probes/network/*.go`
**Tests manquants** :
- Toutes interfaces (ethernet, wifi, VPN)
- Métriques détaillées (packets, errors)
- Gestion interfaces virtuelles
- Hot-plug interfaces

#### 5. **cpu probe** (12.0%)
**Fichiers** : `internal/agent/probes/cpu/*.go`
**Tests manquants** :
- Multi-core metrics
- Load average
- CPU frequency
- Cross-platform

#### 6. **citrix probe** (12.1%)
**Fichiers** : `internal/agent/probes/citrix/*.go`
**Tests existants** : Basiques
**Tests manquants critiques** :
- **Site filtering complet**
- **Logon duration calculation** (écart 11s vs 18s à investiguer)
- DDC fallback
- Session metrics
- Connection failures
- OData filtering

#### 7. **webapp probe** (13.5%)
**Fichiers** : `internal/agent/probes/webapp/*.go`
**Tests manquants** :
- HTTP/HTTPS requests
- Timeouts
- Response validation
- TLS verification
- Redirects

#### 8. **memory probe** (14.8%)
**Fichiers** : `internal/agent/probes/memory/*.go`
**Tests manquants** :
- RAM vs Swap
- Cache metrics
- Cross-platform

#### 9. **redfish probe** (20.7%)
**Fichiers** : `internal/agent/probes/redfish/*.go`
**Tests existants** : Structure basique
**Tests manquants** :
- Tous vendors (Dell, HPE, Lenovo, Cisco)
- Tous collectors (thermal, power, storage)
- Authentication
- Session management
- Error handling

### 🟢 ACCEPTABLE - Couverture moyenne (20-50%)

#### 1. **syslog probe** (27.4%)
**Améliorer** :
- RFC 5424 parsing complet
- UDP vs TCP
- Multi-facility

#### 2. **http strategy** (32.4%)
**Tests existants** : Bons
**À améliorer** :
- TLS/HTTPS complet
- Tous endpoints (PRTG, Nagios, Prometheus)
- Configuration validation API
- Authentication edge cases

#### 3. **agent core** (37.3%)
**Améliorer** :
- Service orchestration
- Shutdown graceful
- Error recovery

#### 4. **auto_update** (52.5%)
**Améliorer** :
- Download retry
- Signature verification
- Rollback

#### 5. **configuration service** (53.7%)
**Améliorer** :
- Migration edge cases
- File watching
- Hot reload

### ✅ BIEN TESTÉS - Couverture élevée (>75%)

- **configParser** : 100% ✅
- **validators** : 91.7% ✅
- **debugshipper** : 87.5% ✅
- **periodic_scheduler** : 85.7% ✅
- **transformers** : 77.5% ✅
- **prtg strategy** : 77.1% ✅

## Recommandations par ordre de priorité

### Phase 1 : Tests critiques manquants (1-2 jours)
1. **cliArgs** : Tests de parsing CLI (2h)
2. **probes/registry** : Tests du registry (2h)
3. **services/data_store** : Tests du core data store (4h)
4. **services/sensor** : Tests orchestration probes (4h)

### Phase 2 : Probes principales (2-3 jours)
5. **host probe** : Augmenter de 1.6% → 60% (6h)
6. **gateway probe** : Augmenter de 1.4% → 50% (4h)
7. **citrix probe** : Augmenter de 12.1% → 60% (8h) + fix logon duration
8. **redfish probe** : Augmenter de 20.7% → 50% (6h)

### Phase 3 : Strategies et services (1-2 jours)
9. **strategies/senhub** : Créer tests (4h)
10. **services/server** : Tests remote config (4h)
11. **http strategy** : Compléter couverture → 60% (4h)

### Phase 4 : Tests complémentaires (1 jour)
12. Probes restants (cpu, memory, network, disk, webapp) → 50%
13. services/status
14. formats/event

## Estimation totale : 6-8 jours de développement

## Commandes utiles

### Lancer tous les tests avec couverture
```bash
go test ./... -coverprofile=coverage.out
```

### Voir la couverture par package
```bash
go test ./... -cover
```

### Générer un rapport HTML de couverture
```bash
go tool cover -html=coverage.out -o coverage.html
```

### Lancer tests d'un package spécifique
```bash
go test -v ./internal/agent/probes/citrix -run TestMetricsCollector
```

### Voir les fonctions non testées
```bash
go tool cover -func=coverage.out | grep "0.0%"
```
