# SenHub Agent - Guide Utilisateur

## Introduction

SenHub Agent est un système de monitoring puissant et flexible qui collecte des métriques système en temps réel et les expose via des APIs REST compatibles avec les principaux outils de supervision (PRTG, Nagios, Prometheus, etc.).

## 🚀 Démarrage Rapide

### Mode Online (Recommandé)
```bash
# Installation avec clé d'authentification
./agent install --authentication-key "votre-cle-agent"

# Démarrage
./agent start

# Vérification du statut
./agent status
```

### Mode Offline (Autonome)
```bash
# Installation offline avec configuration locale
./agent install --offline

# Installation HTTPS sécurisée
./agent install --offline --enable-https

# Démarrage
./agent run --offline
```

## 📊 Interface Web

Une fois l'agent démarré, accédez à l'interface web :

- **Dashboard** : `http://localhost:8080/web/{votre-cle}/dashboard`
- **API Explorer** : `http://localhost:8080/web/{votre-cle}/explorer`
- **Documentation** : `http://localhost:8080/web/{votre-cle}/docs`

### Dashboard Principal

Le dashboard affiche en temps réel :
- ✅ **Statut de l'agent** (version, uptime, port)
- 💊 **Health check** (serveur HTTP, cache, métriques)
- 📊 **Ressources système** (mémoire, goroutines, CPU)
- 🔍 **Probes actives** avec compteur de métriques

### API Explorer

Outil interactif pour :
- 🔍 Tester les endpoints API en direct
- 🏷️ Filtrer les métriques par tags
- 📋 Voir les exemples de requêtes
- 🎯 Explorer les schémas de données

## 🔧 Configuration

### Configuration Automatique (Mode Online)
L'agent récupère automatiquement sa configuration depuis SenHub Platform.

### Configuration Manuelle (Mode Offline)
Fichier `agent-config.yaml` :

```yaml
agent:
  key: "offline-hostname-timestamp-random"
  mode: offline

storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "nagios", "web"]

probes:
  - name: cpu
    params: {interval: 30}
  - name: memory  
    params: {interval: 30}
  - name: network
    params: {interval: 60}
  - name: logicaldisk
    params: {interval: 30}
```

## 🔍 Probes Disponibles

### Probes Système
- **cpu** : Utilisation CPU par cœur et total
- **memory** : Mémoire physique et virtuelle
- **network** : Trafic réseau par interface
- **logicaldisk** : Espace disque et I/O

### Probes Infrastructure
- **redfish** : Monitoring serveurs (Dell, HPE, Lenovo, Cisco)
- **syslog** : Collecte d'événements syslog
- **otel** : Métriques OpenTelemetry

### Probes Applications
- **ping_webapp** : Disponibilité URLs
- **load_webapp** : Temps de réponse et codes HTTP
- **ping_gateway** : Connectivité réseau

## 📈 Formats de Sortie

### PRTG Network Monitor
```bash
# Par probe
curl "http://localhost:8080/api/{cle}/prtg/metrics/cpu"

# Avec filtres
curl "http://localhost:8080/api/{cle}/prtg/metrics/cpu?tags=instance:0"
```

**Réponse PRTG :**
```json
{
  "prtg": {
    "result": [
      {
        "channel": "CPU Usage - Core 0",
        "value": 15.2,
        "unit": "custom",
        "customunit": "%",
        "float": 1
      }
    ]
  }
}
```

### Nagios/Icinga
```bash
curl "http://localhost:8080/api/{cle}/nagios/metrics/system_health"
```

**Réponse Nagios :**
```json
{
  "status": 0,
  "status_text": "OK", 
  "message": "System health: CPU 15.2%, Memory 45.1%",
  "perfdata": "cpu_usage=15.2%;80;90 memory_used=45.1%;85;95"
}
```

### Prometheus
```bash
curl "http://localhost:8080/api/{cle}/prometheus/metrics"
```

## 🏷️ Système de Tags

### Tags Automatiques
- `probe_name` : Nom de la probe source
- `hostname` : Nom de l'hôte
- `instance` : Instance/index du composant

### Tags Spécialisés
- **Réseau** : `interface`, `adapter_type`
- **Disque** : `drive_letter`, `filesystem`
- **Redfish** : `component_type`, `vendor`, `model`

### Filtrage par Tags
```bash
# CPU core spécifique
curl "...?tags=instance:0"

# Interface réseau spécifique  
curl "...?tags=interface:eth0"

# Plusieurs filtres
curl "...?tags=probe_name:redfish,component_type:thermal"
```

## 🔧 Administration

### Debug en Temps Réel
```bash
# Activation du debug global
./agent run --authentication-key "cle" --verbose

# Debug sélectif par module
./agent run --authentication-key "cle" --verbose --debug-modules "strategy.http,probe.redfish"
```

### API de Debug Runtime
```bash
# Voir les niveaux de log actuels
curl "http://localhost:8080/api/{cle}/debug/logs"

# Modifier les niveaux en live
curl -X POST "http://localhost:8080/api/{cle}/debug/logs" \
  -H "Content-Type: application/json" \
  -d '{"module_levels": [{"module": "strategy.http", "level": "debug"}]}'
```

### Modules de Debug Disponibles
- `strategy.http` - Stratégie HTTP et cache
- `probe.redfish` - Probe Redfish  
- `probe.host` - Probes système
- `cache` - Opérations de cache
- `configuration` - Gestion configuration
- `scheduler` - Ordonnancement des probes

## 🔒 Sécurité HTTPS

### Auto-génération de Certificats
```bash
./agent install --offline --enable-https
```

### Certificats Personnalisés
```bash
./agent install --offline --enable-https \
  --cert-file /path/to/cert.pem \
  --key-file /path/to/key.pem \
  --min-tls-version 1.3
```

### Configuration HTTPS
```yaml
storage:
  - name: http
    params:
      port: 8443
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        mode: "auto"
        auto_cert:
          organization: "Mon Entreprise"
          common_name: "agent.entreprise.com"
          san_hosts: ["localhost", "192.168.1.100"]
          validity_days: 365
```

## 📋 Surveillance Redfish

### Configuration de Base
```yaml
probes:
  - name: redfish
    params:
      endpoint: "https://192.168.1.100/redfish/v1/"
      username: "admin"
      password: "password"
      collections: ["system", "thermal", "power"]
      interval: 300
      insecure: true
```

### Collections Disponibles
- **system** : État général, processeurs, mémoire
- **thermal** : Températures et ventilateurs
- **power** : Alimentation et consommation
- **processor** : Détails processeurs
- **memory** : Modules mémoire détaillés
- **storage** : Disques et contrôleurs
- **network** : Interfaces réseau

### Vendeurs Supportés
- **Dell** : PowerEdge, PowerVault ME5024
- **HPE** : ProLiant, Synergy
- **Lenovo** : ThinkSystem
- **Cisco** : UCS
- **Générique** : Serveurs compatibles Redfish

## 🚨 Monitoring d'Événements

### Syslog
```yaml
probes:
  - name: syslog
    params:
      port: 514
      protocol: "udp"
      bind_address: "0.0.0.0"
```

### Événements Redfish
Les événements système sont automatiquement collectés depuis les serveurs Redfish et convertis en métriques numériques.

## 🔧 Dépannage

### Problèmes Courants

**Agent ne démarre pas :**
```bash
# Vérifier les logs
./agent run --authentication-key "cle" --verbose

# Vérifier la configuration
./agent run --offline --config-path ./test-config.yaml
```

**Pas de métriques :**
- Vérifier que les probes sont actives dans le dashboard
- Contrôler les logs avec `--debug-modules "probe.host,cache"`
- Tester les endpoints API directement

**Problèmes HTTPS :**
- Vérifier les certificats : `openssl x509 -in cert.pem -text -noout`
- Tester avec curl : `curl -k https://localhost:8443/health`

### URLs de Test

```bash
# Health check global
curl "http://localhost:8080/health"

# Liste des probes actives
curl "http://localhost:8080/api/{cle}/info/probes"

# Métriques d'une probe
curl "http://localhost:8080/api/{cle}/prtg/metrics/cpu"

# Schema d'une probe
curl "http://localhost:8080/api/{cle}/info/schema/cpu"
```

## 📞 Support

### Logs et Diagnostics
- Logs détaillés : `--verbose --debug-modules "module1,module2"`
- Health check : `/health`
- Info système : `/api/{cle}/info/system`

### Ressources
- Interface web : Dashboard intégré
- API Explorer : Tests en temps réel
- Documentation : Endpoints et exemples

### Configuration de Production

**Recommandations :**
- Utiliser HTTPS en production
- Configurer des intervalles appropriés (30-300s)
- Monitorer la consommation mémoire
- Sauvegarder les configurations

**Exemple Production :**
```yaml
agent:
  key: "production-key"
  mode: online

storage:
  - name: http
    params:
      port: 8443
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "nagios"]
      tls:
        enabled: true
        cert_file: "/etc/ssl/agent.crt"
        key_file: "/etc/ssl/agent.key"
        min_tls_version: "1.3"

probes:
  - name: cpu
    params: {interval: 60}
  - name: memory
    params: {interval: 60}
  - name: redfish
    params:
      endpoint: "https://server.lan/redfish/v1/"
      username: "monitoring"
      password: "secure-password"
      interval: 300
      collections: ["system", "thermal"]
```