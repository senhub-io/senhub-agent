# Configuration des Probes

Ce document explique comment configurer les différentes probes de monitoring dans SenHub Agent.

## Migration automatique v1→v2

**Note importante**: Si vous utilisez une ancienne configuration (version 1), l'agent migrera automatiquement votre configuration vers le format v2 au démarrage. Cette migration est transparente et ne nécessite aucune action de votre part.

Pour plus de détails sur les versions de configuration et la migration, consultez `/docs/admin-guide/CONFIG-VERSION-CHANGELOG.md`.

## Principe général

### Configuration simple (une instance par type)

Pour la plupart des probes, le **nom de la section de configuration est le type de probe** :

```yaml
probes:
  cpu:        # Type de probe = "cpu"
    enabled: true
    interval: 60
    
  memory:     # Type de probe = "memory"
    enabled: true
    interval: 60
    
  network:    # Type de probe = "network"
    enabled: true
    interval: 30
    
  redfish:    # Type de probe = "redfish"
    enabled: true
    endpoint: "https://server.example.com"
    username: "admin"
    password: "password"
```

### Configuration multiple (plusieurs instances du même type)

Pour avoir plusieurs instances de la même probe, utilisez le **même nom** avec des **paramètres différents** :

```json
[
  {
    "name": "ping_webapp",
    "params": { "url": "https://app1.example.com" }
  },
  {
    "name": "ping_webapp", 
    "params": { "url": "https://app2.example.com" }
  },
  {
    "name": "redfish",
    "params": {
      "endpoint": "https://server1.example.com",
      "username": "admin",
      "password": "password"
    }
  },
  {
    "name": "redfish",
    "params": {
      "endpoint": "https://server2.example.com", 
      "username": "admin",
      "password": "password"
    }
  }
]
```

Chaque combinaison `name + params` crée une instance unique avec un ID distinct généré automatiquement.

## Types de probes disponibles

### Probes système (instance unique)
- `cpu` - Utilisation CPU
- `memory` - Utilisation mémoire
- `network` - Métriques réseau
- `logicaldisk` - Utilisation disque
- `wifi_signal_strength` - Force du signal WiFi

### Probes réseau (multiples instances possibles)
- `ping_gateway` - Connectivité passerelle
- `ping_webapp` - Disponibilité applications web
- `load_webapp` - Performance applications web

### Probes infrastructure (multiples instances possibles)
- `redfish` - Monitoring matériel via API Redfish
- `syslog` - Collecte logs système
- `event` - Collecte événements personnalisés

## Règles de nommage

### ✅ Configuration simple
```yaml
probes:
  redfish:           # Nom = Type
    endpoint: "..."
```

### ✅ Configuration multiple
```json
[
  {
    "name": "redfish",
    "params": { "endpoint": "https://server1.example.com", "username": "admin", "password": "pass1" }
  },
  {
    "name": "redfish", 
    "params": { "endpoint": "https://server2.example.com", "username": "admin", "password": "pass2" }
  }
]
```

### ❌ Configuration incorrecte
```yaml
probes:
  redfish:
    type: redfish      # ❌ Champ "type" n'existe pas
    endpoint: "..."
    
  server1:
    # ❌ Manque probe_type pour nom personnalisé
    endpoint: "..."
```

## Exemples complets

### Configuration système basique
```yaml
probes:
  cpu:
    enabled: true
    interval: 60
    
  memory:
    enabled: true
    interval: 60
    
  logicaldisk:
    enabled: true
    interval: 300
```

### Configuration infrastructure complète
```yaml
probes:
  # Monitoring système local
  cpu:
    enabled: true
    interval: 60
    
  memory:
    enabled: true
    interval: 60
    
  # Monitoring serveurs distants
  server_primary:
    probe_type: redfish
    enabled: true
    endpoint: "https://server-primary.example.com"
    username: "admin"
    password: "secure-password"
    interval: 300
    
  server_secondary:
    probe_type: redfish
    enabled: true
    endpoint: "https://server-secondary.example.com"
    username: "admin"
    password: "secure-password"
    interval: 300
    
  # Monitoring applications
  app_production:
    probe_type: ping_webapp
    enabled: true
    url: "https://app.example.com"
    interval: 30
    
  app_api:
    probe_type: load_webapp
    enabled: true
    url: "https://api.example.com/health"
    interval: 60
```

## Note importante

Le champ `probe_type` n'est requis que pour les configurations avec **noms personnalisés**. Pour la configuration simple, le nom de la section définit automatiquement le type de probe.