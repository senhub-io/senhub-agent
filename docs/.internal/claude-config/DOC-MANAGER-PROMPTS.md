# Doc-Manager Agent - Exemples de Prompts

Ce fichier contient des exemples de prompts pour interagir avec l'agent doc-manager, ainsi que les sorties attendues.

## 🎯 Règle Linguistique

```
Communication Utilisateur → Agent : FRANÇAIS
Génération Agent → Documentation : ANGLAIS
```

---

## 📋 Phase 1 : Analyse Initiale

### Prompt 1.1 : Analyse Complète du Projet

**Input (Utilisateur en FR)** :
```
Analyse ce projet et crée-moi un profil de style documentaire complet.
```

**Output (Agent en FR)** :
```
🔍 Analyse du projet en cours...

📁 Structure Détectée
   - Répertoire docs : ✅ docs/
   - Total fichiers : 48 fichiers markdown
   - Organisation : Par audience (user-guide/, admin-guide/, probes/)
   - Profondeur : 3 niveaux maximum

🌍 Langue Détectée
   - Langue principale : Anglais (98% - 47/48 fichiers)
   - Fichier mixte : docs/Claude/NOTES.md (contient du français)
   - Recommandation : Maintenir l'anglais

✍️  Style d'Écriture
   - Ton : Professional Accessible (technique mais lisible)
   - Longueur phrases : Moyenne (15-25 mots)
   - Voix : Active (90% des phrases)
   - Profondeur technique : Équilibrée (exemples + théorie)

🎨 Éléments Visuels
   - Emojis : ✅ Oui (navigation et sections)
   - Emojis détectés : 📚 🚀 ⚙️ 📊 🚨 🎯 ✅ ⚠️ ❌
   - Usage : Cohérent (toujours en début de section H2)

📑 Patterns Structurels
   - Sections standard :
     1. Quick Start (toujours présent)
     2. Configuration (avec tableaux de paramètres)
     3. Examples (YAML/bash avec commentaires)
     4. Troubleshooting (optionnel, toujours en dernier)

   - Tables : Utilisées pour paramètres, métriques, comparaisons
   - Code blocks : Toujours avec langage spécifié
   - Cross-références : Liens relatifs, nombreux

👥 Audiences Identifiées
   - End Users (user-guide/) : Docs courtes, pratiques, 5-10 min
   - Administrators (admin-guide/) : Docs complètes, 20-30 min
   - Developers (CLAUDE.md) : Architectural, patterns, conventions

💻 Documentation Code
   - Langage : Go
   - Style : godoc (package + exported functions)
   - Conventions strictes :
     * Package comment obligatoire
     * Interfaces documentées avec purpose
     * Pas de "This function..." (direct)

⚙️  Formats de Configuration
   - Format préféré : YAML
   - Style commentaires : Inline avec #
   - Exemples : Réels avec placeholders (base_url: "https://director.company.com")

💾 Profil de Style créé : .doc-manager/style-profile.yaml

✅ Analyse terminée. Prêt à générer de la documentation !
```

---

### Prompt 1.2 : Afficher le Profil de Style

**Input (FR)** :
```
Affiche-moi le résumé du profil de style détecté
```

**Output (FR)** :
```
📊 Profil de Style - senhub-agent
==================================

Project: senhub-agent
Language: English (output)
Communication: Français (user interaction)

STRUCTURE
---------
Location: docs/
Organization: by_audience
Categories:
  • user-guide/ (end users, 5-10 min reads)
  • admin-guide/ (admins, 20-30 min reads)
  • probes/ (organized by_feature)
  • troubleshooting/ (problem solving)

WRITING STYLE
-------------
Language: English
Tone: Professional Accessible
Emoji: Yes (📚 🚀 ⚙️ 📊 🚨 🎯 ✅ ⚠️ ❌)
Sentence Length: Medium (15-25 words)
Technical Depth: Balanced
Voice: Active (90%)

STANDARD SECTIONS (ordre)
-------------------------
1. Quick Start (required)
2. Configuration (required, avec tableaux)
3. Examples (optional)
4. Integration (optional)
5. Troubleshooting (optional, toujours en dernier)

CODE DOCUMENTATION
------------------
Language: Go
Style: godoc
Comment Language: English
Conventions:
  • Package comment required
  • Exported functions documented
  • No "This function..." prefix

FORMAT PRÉFÉRÉ
--------------
Config: YAML
Comments: Inline (#)
Examples: Real values with placeholders

Pour voir le détail complet : cat .doc-manager/style-profile.yaml
```

---

## 📝 Phase 2 : Génération de Documentation

### Prompt 2.1 : Créer Documentation Nouvelle Feature

**Input (FR)** :
```
Documente la nouvelle feature "Redis Cache" pour le HTTP strategy.
Inclus : configuration, exemples, troubleshooting.
```

**Output (FR)** :
```
📝 Création de la documentation Redis Cache...

🎯 Analyse du contexte :
   - Type : Admin feature (HTTP strategy)
   - Audience : Administrators
   - Emplacement : docs/admin-guide/
   - Style : Professional Accessible + emojis

📄 Fichier à créer :
   - docs/admin-guide/REDIS-CACHE.md

✍️  Sections à inclure :
   1. 🚀 Quick Start
   2. ⚙️ Configuration Parameters (tableau)
   3. 📊 Examples (YAML avec commentaires)
   4. 🔗 Integration avec HTTP Strategy
   5. 🚨 Troubleshooting

🔗 Cross-références à ajouter :
   - Depuis : docs/admin-guide/HTTP-STRATEGY.md
   - Vers : docs/admin-guide/HTTPS-CONFIGURATION.md
   - Index : docs/DOCUMENTATION-INDEX.md

✅ Génération en cours... (EN ANGLAIS)
```

**Fichier Généré (EN)** :
```markdown
# Redis Cache Configuration

The Redis cache provides high-performance distributed caching for SenHub Agent metrics with configurable TTL, automatic cleanup, and cluster support.

## 🚀 Quick Start

### Basic Configuration

Enable Redis cache in your HTTP strategy:

\`\`\`yaml
storage:
  - name: http
    params:
      port: 8080
      cache:
        enabled: true
        backend: redis
        redis:
          host: "localhost:6379"
          password: ""
          db: 0
          ttl: 300  # 5 minutes
\`\`\`

### Prerequisites

- Redis server 5.0+ running
- Network connectivity to Redis instance
- Optional: Redis password configured

## ⚙️ Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable Redis cache |
| `backend` | string | `memory` | Cache backend (`memory` or `redis`) |
| `redis.host` | string | `localhost:6379` | Redis server address |
| `redis.password` | string | `""` | Redis authentication password |
| `redis.db` | integer | `0` | Redis database number (0-15) |
| `redis.ttl` | integer | `300` | Cache TTL in seconds |
| `redis.pool_size` | integer | `10` | Connection pool size |
| `redis.timeout` | integer | `5` | Connection timeout in seconds |

### Advanced Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `redis.cluster_mode` | boolean | `false` | Enable Redis Cluster mode |
| `redis.cluster_nodes` | array | `[]` | List of cluster nodes |
| `redis.sentinel_enabled` | boolean | `false` | Use Redis Sentinel |
| `redis.sentinel_master` | string | `""` | Sentinel master name |

## 📊 Configuration Examples

### Simple Redis Cache

\`\`\`yaml
storage:
  - name: http
    params:
      cache:
        enabled: true
        backend: redis
        redis:
          host: "cache.company.com:6379"
          ttl: 600  # 10 minutes
\`\`\`

### Redis with Authentication

\`\`\`yaml
storage:
  - name: http
    params:
      cache:
        enabled: true
        backend: redis
        redis:
          host: "secure-cache.company.com:6380"
          password: "your-secure-password"
          db: 1
          ttl: 300
          timeout: 10
\`\`\`

### Redis Cluster Mode

\`\`\`yaml
storage:
  - name: http
    params:
      cache:
        enabled: true
        backend: redis
        redis:
          cluster_mode: true
          cluster_nodes:
            - "redis-1.company.com:6379"
            - "redis-2.company.com:6379"
            - "redis-3.company.com:6379"
          ttl: 300
          pool_size: 20
\`\`\`

### Redis Sentinel (High Availability)

\`\`\`yaml
storage:
  - name: http
    params:
      cache:
        enabled: true
        backend: redis
        redis:
          sentinel_enabled: true
          sentinel_master: "mymaster"
          cluster_nodes:
            - "sentinel-1:26379"
            - "sentinel-2:26379"
            - "sentinel-3:26379"
          password: "sentinel-password"
          ttl: 300
\`\`\`

## 🔗 Integration with HTTP Strategy

Redis cache is automatically used by the HTTP strategy when enabled:

1. **Metric Storage**: All collected metrics are stored in Redis
2. **Automatic Expiration**: Metrics expire based on configured TTL
3. **Distributed Access**: Multiple agent instances can share the same cache
4. **Failover**: Falls back to memory cache if Redis unavailable

### Cache Key Format

Metrics are stored with the following key pattern:

\`\`\`
senhub:agent:{agentkey}:metrics:{probe_name}:{metric_key}
\`\`\`

Example:
\`\`\`
senhub:agent:abc123:metrics:cpu:cpu_usage_total
\`\`\`

### Performance Considerations

- **Connection Pooling**: Configure `pool_size` based on concurrent requests
- **TTL**: Balance between freshness and cache efficiency
- **Network Latency**: Co-locate Redis server with agent for best performance
- **Memory**: Monitor Redis memory usage with `INFO memory`

## 🚨 Troubleshooting

### Common Issues

#### 1. Connection Refused

**Symptom**: Agent logs show "connection refused" errors

**Solutions**:
\`\`\`bash
# Check Redis is running
redis-cli ping
# Expected: PONG

# Check connectivity
telnet cache.company.com 6379

# Verify firewall rules
# Ensure port 6379 is open
\`\`\`

#### 2. Authentication Failed

**Symptom**: "NOAUTH Authentication required" error

**Solution**:
\`\`\`yaml
# Add password to configuration
redis:
  password: "your-redis-password"
\`\`\`

#### 3. High Memory Usage

**Symptom**: Redis memory consumption grows continuously

**Solutions**:
\`\`\`bash
# Check memory usage
redis-cli INFO memory

# Verify TTL is set on keys
redis-cli TTL senhub:agent:*:metrics:*

# Reduce TTL in configuration
redis:
  ttl: 180  # 3 minutes instead of 5
\`\`\`

#### 4. Cache Misses

**Symptom**: Low cache hit ratio

**Diagnostics**:
\`\`\`bash
# Monitor cache hits/misses
redis-cli INFO stats | grep hits

# Check agent logs for cache activity
./agent run --verbose --debug-modules cache,strategy.http
\`\`\`

**Solutions**:
- Increase TTL for more cache hits
- Verify probe collection intervals align with TTL
- Check for Redis eviction policy (should be `volatile-lru` or `volatile-ttl`)

### Debug Mode

Enable detailed cache logging:

\`\`\`bash
./agent run --verbose --debug-modules cache,strategy.http
\`\`\`

This shows:
- Cache connection attempts
- Key operations (SET, GET, DEL)
- Cache hit/miss statistics
- TTL expirations

### Health Check

Verify Redis cache health:

\`\`\`bash
# Agent health endpoint
curl http://localhost:8080/health

# Expected output includes cache status:
{
  "status": "ok",
  "cache": {
    "backend": "redis",
    "connected": true,
    "keys": 342,
    "memory_used": "5.2MB"
  }
}
\`\`\`

## 📚 Related Documentation

- [HTTP Strategy Configuration](./HTTP-STRATEGY.md) - Main HTTP strategy documentation
- [Performance Tuning](./PERFORMANCE.md) - Optimization guidelines
- [Monitoring Guide](../user-guide/MONITORING.md) - Metrics and monitoring

---

**Last Updated**: 2025-01-15
**Version**: SenHub Agent v0.8.0+
**Audience**: Administrators
```

**Confirmation (FR)** :
```
✅ Documentation créée avec succès !

📄 Fichier généré :
   - docs/admin-guide/REDIS-CACHE.md (EN ANGLAIS)

📊 Statistiques :
   - Sections : 6 (Quick Start, Config, Examples, Integration, Troubleshooting, Related)
   - Exemples : 5 configurations YAML
   - Tableaux : 2 (basic params + advanced params)
   - Code blocks : 12 (YAML + bash)
   - Emojis : 5 (navigation sections)

🔗 Cross-références ajoutées :
   - ✅ HTTP-STRATEGY.md (lien ajouté vers Redis Cache)
   - ✅ DOCUMENTATION-INDEX.md (entrée ajoutée)
   - ✅ README.md (lien dans admin guide)

📏 Conformité au style :
   - ✅ Tone : Professional Accessible
   - ✅ Structure : Quick Start en premier
   - ✅ Examples : YAML avec commentaires
   - ✅ Troubleshooting : En dernier
   - ✅ Langue : 100% anglais

Prêt pour review et commit !
```

---

### Prompt 2.2 : Documenter un Nouveau Probe

**Input (FR)** :
```
Crée la documentation complète pour le nouveau probe "Kafka" qui monitore :
- Consumer lag
- Partition offset
- Broker health
- Topic metrics
```

**Output (FR)** :
```
📝 Création documentation Kafka Probe...

🎯 Analyse :
   - Type : Probe (messaging system)
   - Template : Basé sur probes/redfish/ et probes/otel/
   - Audience : Administrators + Developers
   - Emplacement : docs/probes/kafka/

📁 Structure à créer :
   docs/probes/kafka/
   ├── README.md (overview + configuration)
   └── METRICS.md (metrics reference)

✍️  README.md contiendra :
   1. 🚀 Quick Start
   2. ⚙️ Configuration Parameters
   3. 📊 Key Metrics Summary
   4. 🔗 Integration Examples
   5. 🚨 Troubleshooting

✍️  METRICS.md contiendra :
   1. Consumer Metrics
   2. Partition Metrics
   3. Broker Metrics
   4. Topic Metrics
   (avec tableaux détaillés)

🔗 Mises à jour automatiques :
   - docs/DOCUMENTATION-INDEX.md (ajout section Kafka)
   - docs/README.md (ajout dans probe list)
   - docs/user-guide/PROBE-CONFIGURATION.md (exemple Kafka)

✅ Génération en cours... (EN ANGLAIS)
```

**Fichiers Générés** :

1. `docs/probes/kafka/README.md` (EN)
2. `docs/probes/kafka/METRICS.md` (EN)

**Confirmation (FR)** :
```
✅ Documentation Kafka Probe créée !

📄 Fichiers générés :
   - docs/probes/kafka/README.md (overview + config)
   - docs/probes/kafka/METRICS.md (43 metrics documented)

🔗 Indexes mis à jour :
   - DOCUMENTATION-INDEX.md (nouvelle entrée Kafka)
   - docs/README.md (lien ajouté)
   - PROBE-CONFIGURATION.md (exemple ajouté)

📊 Conformité :
   - ✅ Structure : Suit le pattern probes/ existant
   - ✅ Sections : Quick Start, Config, Examples, Integration
   - ✅ Tables : Paramètres + Métriques avec unités
   - ✅ Langue : 100% anglais

Prêt pour review !
```

---

### Prompt 2.3 : Mettre à Jour Documentation Existante

**Input (FR)** :
```
Ajoute la documentation des nouveaux filtres de modules dans LOGGING.md :
- strategy.http
- cache
- transformer
```

**Output (FR)** :
```
📝 Mise à jour de docs/admin-guide/LOGGING.md...

🔍 Analyse du fichier existant :
   - Structure actuelle : 8 sections
   - Section à modifier : "Module Log Levels"
   - Style détecté : Tableaux + exemples bash
   - Langue : Anglais

✍️  Modifications à apporter :
   1. Ajouter 3 modules dans le tableau
   2. Ajouter exemple d'usage pour chaque module
   3. Mettre à jour la liste complète des modules
   4. Ajouter section "New in v0.8.0"

📊 Avant modification :
   - 13 modules documentés
   - 5 exemples d'usage

📊 Après modification :
   - 16 modules documentés (+3)
   - 8 exemples d'usage (+3)
   - Section changelog ajoutée

✅ Application des modifications... (EN ANGLAIS)
```

**Modifications Appliquées (extrait EN)** :
```markdown
## Module Log Levels

### Available Modules

| Module | Description | Default Level | Use Case |
|--------|-------------|---------------|----------|
| `strategy.http` | HTTP strategy and API endpoints | `info` | Debug HTTP requests and responses |
| `strategy.prtg` | PRTG sync strategy | `info` | Monitor PRTG integration |
| `cache` | Cache operations and debugging | `info` | Debug cache hits/misses |
| `transformer` | Metric name transformations | `info` | Debug metric naming |
| `probe.redfish` | Redfish probe operations | `info` | Debug hardware monitoring |
| ... | ... | ... | ... |

### Usage Examples

#### Debug HTTP Strategy
\`\`\`bash
./agent run --verbose --debug-modules strategy.http
\`\`\`
Shows detailed HTTP request/response logs.

#### Debug Cache Operations
\`\`\`bash
./agent run --verbose --debug-modules cache
\`\`\`
Shows cache SET/GET/DELETE operations with keys and TTL.

#### Debug Metric Transformations
\`\`\`bash
./agent run --verbose --debug-modules transformer
\`\`\`
Shows metric name transformations (technical → friendly names).

## What's New

### Version 0.8.0

New modules added:
- **strategy.http**: HTTP strategy debugging
- **cache**: Cache operations monitoring
- **transformer**: Metric transformation pipeline

See [CHANGELOG.md](../../CHANGELOG.md) for complete version history.
```

**Confirmation (FR)** :
```
✅ LOGGING.md mis à jour avec succès !

📝 Modifications appliquées :
   - ✅ 3 nouveaux modules ajoutés au tableau
   - ✅ 3 exemples d'usage ajoutés
   - ✅ Section "What's New" créée
   - ✅ Liens vers CHANGELOG ajoutés

📊 Statistiques :
   - Lignes ajoutées : 47
   - Tableaux modifiés : 1
   - Exemples ajoutés : 3
   - Langue : 100% anglais

🔗 Cross-références vérifiées :
   - ✅ CHANGELOG.md existe
   - ✅ Liens relatifs valides

✅ Prêt pour commit !
```

---

## 🔗 Phase 3 : Maintenance et Validation

### Prompt 3.1 : Rafraîchir les Index

**Input (FR)** :
```
Rafraîchis tous les index de documentation
```

**Output (FR)** :
```
🔄 Mise à jour des index en cours...

📋 Index à traiter :
   - docs/README.md (table des matières principale)
   - docs/DOCUMENTATION-INDEX.md (index complet)
   - docs/probes/README.md (si existe)

🔍 Scan des fichiers :
   - Total .md : 48 fichiers
   - Nouveaux fichiers : 2 (Redis Cache, Kafka Probe)
   - Fichiers modifiés : 1 (LOGGING.md)
   - Fichiers supprimés : 0

✍️  Mises à jour appliquées :

docs/README.md :
   - ✅ Section Admin Guide : Ajout REDIS-CACHE.md
   - ✅ Section Probes : Ajout Kafka probe
   - ✅ Ordre alphabétique maintenu

docs/DOCUMENTATION-INDEX.md :
   - ✅ Nouvelle entrée : Redis Cache (admin-guide, 20 min read)
   - ✅ Nouvelle entrée : Kafka Probe (probes, complete reference)
   - ✅ Quick Navigation : Ajout use case "monitoring Kafka"
   - ✅ Cross-references : 5 nouveaux liens

📊 Résultats :
   - Entrées ajoutées : 3
   - Liens créés : 8
   - Temps de lecture total : 2h45min (+25min)
   - Couverture : 100% (tous les fichiers indexés)

✅ Tous les index mis à jour ! (EN ANGLAIS)
```

---

### Prompt 3.2 : Valider la Cohérence

**Input (FR)** :
```
Valide la cohérence de toute la documentation
```

**Output (FR)** :
```
🔍 Validation de cohérence en cours...

📊 Rapport de Validation
========================

✅ LANGUE (100%)
   - 48/48 fichiers en anglais
   - 0 fichier avec mélange de langues
   - Commentaires code : 100% anglais

✅ STRUCTURE (100%)
   - Organization : by_audience (conforme)
   - Catégories : Toutes présentes
   - Hiérarchie : Max 3 niveaux (conforme)

✅ SECTIONS STANDARD (96%)
   - 46/48 fichiers suivent le pattern standard
   - ⚠️  2 fichiers sans Quick Start :
     * docs/archive/OLD-NOTES.md (archivé, OK)
     * docs/Claude/INTERNAL.md (notes internes, OK)

✅ STYLE (98%)
   - Ton : Professional Accessible (47/48)
   - Emojis : Cohérents (95%)
   - ⚠️  1 fichier sans emojis : docs/archive/LEGACY.md (OK)

🔗 CROSS-RÉFÉRENCES (99%)
   - Total liens : 356
   - Liens valides : 352 (98.9%)
   - ❌ Liens cassés : 4
     1. docs/admin-guide/LOGGING.md:42 → PERFORMANCE.md (n'existe pas)
     2. docs/admin-guide/LOGGING.md:67 → ../CHANGELOG.md (chemin incorrect)
     3. docs/probes/kafka/README.md:89 → ../../monitoring/KAFKA.md (pas créé)
     4. docs/user-guide/OFFLINE-MODE.md:234 → TROUBLESHOOTING.md (mauvais chemin)

📅 FRAÎCHEUR (85%)
   - < 1 mois : 38 fichiers (79%)
   - 1-3 mois : 6 fichiers (13%)
   - 3-6 mois : 2 fichiers (4%)
   - ⚠️  > 6 mois : 2 fichiers (4%)
     * docs/archive/OLD-PROTOCOL.md (10 mois)
     * docs/probes/citrix/LEGACY-METRICS.md (8 mois)

🎯 CODE DOCUMENTATION (92%)
   - Packages commentés : 45/47 (96%)
   - Fonctions exportées : 892/967 (92%)
   - ⚠️  75 fonctions sans doc godoc

📊 SCORE GLOBAL : 96/100
   Grade : A (Excellent)

🔧 ACTIONS RECOMMANDÉES :
   1. 🔴 Réparer 4 liens cassés (priorité haute)
   2. 🟡 Documenter 75 fonctions Go (priorité moyenne)
   3. 🟢 Archiver 2 docs obsolètes (priorité basse)

Lancer les corrections ? [o/N]
```

---

### Prompt 3.3 : Auto-Correction

**Input (FR)** :
```
Corrige automatiquement tous les problèmes détectés
```

**Output (FR)** :
```
🔧 Correction automatique en cours...

🔗 Correction des liens cassés (4) :

1. docs/admin-guide/LOGGING.md:42
   ❌ Ancien : [Performance Guide](PERFORMANCE.md)
   ✅ Nouveau : [HTTP Strategy](HTTP-STRATEGY.md#performance)
   (fichier le plus proche avec section performance)

2. docs/admin-guide/LOGGING.md:67
   ❌ Ancien : ../CHANGELOG.md
   ✅ Nouveau : ../../CHANGELOG.md
   (correction du chemin relatif)

3. docs/probes/kafka/README.md:89
   ❌ Ancien : ../../monitoring/KAFKA.md
   ✅ Action : Création du fichier manquant
   📝 Fichier créé : docs/monitoring/KAFKA.md (guide de monitoring Kafka)

4. docs/user-guide/OFFLINE-MODE.md:234
   ❌ Ancien : TROUBLESHOOTING.md
   ✅ Nouveau : ../troubleshooting/TROUBLESHOOTING-OFFLINE.md
   (correction du chemin)

📅 Mise à jour metadata (48 fichiers) :
   - ✅ Dates "Last Updated" actualisées
   - ✅ Champs "Version" vérifiés

📦 Fichiers archivés (2) :
   - docs/archive/OLD-PROTOCOL.md → Ajout banner "⚠️ ARCHIVED"
   - docs/probes/citrix/LEGACY-METRICS.md → Ajout banner "⚠️ DEPRECATED"

📊 Résultats :
   - Liens corrigés : 4/4 (100%)
   - Metadata MAJ : 48/48 (100%)
   - Fichiers créés : 1 (docs/monitoring/KAFKA.md)
   - Archives marquées : 2

✅ Toutes les corrections appliquées !

📈 Nouveau score : 99/100 (A+)
   (reste 75 fonctions Go à documenter - nécessite intervention manuelle)
```

---

## 🎓 Phase 4 : Apprentissage et Amélioration

### Prompt 4.1 : Apprendre d'une Modification

**Input (FR)** :
```
J'ai modifié REDIS-CACHE.md. Analyse mes changements et apprends de mon style.
```

**Contexte** : L'utilisateur a ajouté une section "Performance Benchmarks" avant "Troubleshooting"

**Output (FR)** :
```
🔍 Analyse des modifications apportées...

📝 Fichier : docs/admin-guide/REDIS-CACHE.md

🔄 Changements détectés :

1. NOUVELLE SECTION ajoutée (ligne 187)
   Section : "📊 Performance Benchmarks"
   Position : Entre "Integration" et "Troubleshooting"
   Contenu : 3 tableaux de benchmarks + graphiques

2. STYLE modifié
   - Ajout de données chiffrées (requests/sec, latency)
   - Graphiques ASCII art ajoutés
   - Comparaison Memory vs Redis

3. STRUCTURE modifiée
   Ordre original :
     1. Quick Start
     2. Configuration
     3. Examples
     4. Integration
     5. Troubleshooting ❌

   Nouvel ordre :
     1. Quick Start
     2. Configuration
     3. Examples
     4. Integration
     5. Performance Benchmarks ✨ (NOUVEAU)
     6. Troubleshooting ✅

📚 Apprentissage :

✅ PATTERN DÉTECTÉ :
   Type : Admin-level documentation
   Pattern : Ajout section "Performance" avant "Troubleshooting"
   Fréquence : 3/3 dernières modifications admin docs

💡 NOUVELLE RÈGLE apprise :
   Pour documentation admin-guide/ avec cache/performance :
   - Ajouter section "Performance Benchmarks"
   - Position : Après Examples/Integration, avant Troubleshooting
   - Contenu : Tableaux comparatifs + métriques chiffrées

🎯 STYLE PERSONNEL détecté :
   - Préférence pour données quantifiées
   - Utilisation de graphiques ASCII
   - Sections de comparaison (before/after)

💾 Profil de Style mis à jour :
   Version : 2.1 → 2.2
   Changement : "Added performance_benchmarks section for admin docs"
   Application future : Sera appliqué automatiquement

✅ Apprentissage terminé ! Ce pattern sera utilisé dans les prochaines générations.
```

---

### Prompt 4.2 : Suggestion Proactive

**Input (FR)** :
```
Suggère-moi des améliorations pour la documentation
```

**Output (FR)** :
```
💡 Suggestions d'Amélioration Documentation
===========================================

🔍 Analyse basée sur :
   - 48 fichiers existants
   - 892 fonctions Go
   - Patterns d'usage détectés
   - Feedback précédents

📊 HAUTE PRIORITÉ

1. 🔴 Documentation API manquante (Score: 67%)
   Fichiers manquants :
   - docs/api/HTTP-API.md (détaillé dans README mais pas de doc dédiée)
   - docs/api/CONFIGURATION-API.md (Universal Config API fragmenté)

   Impact : Haute (développeurs et intégrateurs)
   Effort : 2-3 heures

   Action proposée :
   "Veux-tu que je crée une section docs/api/ complète ?"

2. 🔴 Guide de migration v1 → v2 manquant
   Détecté : CONFIG-VERSION-CHANGELOG.md mentionne migration
   Manque : Guide pratique étape par étape

   Impact : Haute (utilisateurs existants)
   Effort : 1 heure

   Action proposée :
   "Je peux créer docs/admin-guide/MIGRATION-GUIDE.md"

3. 🟡 Exemples de configuration incomplets
   Analyse :
   - Probes Citrix/Redfish : ✅ Exemples complets
   - Probe Kafka : ⚠️ Seulement config basique
   - Probe OTEL : ⚠️ Pas d'exemples avancés

   Impact : Moyenne (nouveaux utilisateurs)
   Effort : 30 min par probe

   Action proposée :
   "Enrichir les exemples pour Kafka et OTEL ?"

📚 MOYENNE PRIORITÉ

4. 🟡 Diagrammes d'architecture absents
   Détecté : CLAUDE.md décrit l'architecture en texte
   Manque : Schémas visuels

   Suggestion :
   - docs/architecture/OVERVIEW.md avec diagrammes
   - Format : Mermaid diagrams (supporté par GitHub)

   "Génère-t-on des diagrammes d'architecture ?"

5. 🟡 Glossaire manquant
   Termes techniques détectés sans définition :
   - "Probe", "Strategy", "DataStore" (architecture)
   - "DDC", "OData", "Redfish" (technologies)
   - "TTL", "SAN", "CVAD" (acronymes)

   Proposition : docs/GLOSSARY.md

   "Créer un glossaire des termes techniques ?"

🎯 BASSE PRIORITÉ

6. 🟢 Quick Reference Cards
   Suggestion : Cartes de référence rapide PDF
   - Top 10 commands
   - Common configurations
   - Troubleshooting flowchart

   Format : Markdown → PDF (1 page)

7. 🟢 Video tutorials / GIFs
   Détecté : Aucun contenu visuel animé
   Suggestion : Ajouter GIFs pour :
   - Installation process
   - Web interface tour
   - PRTG integration

🤖 SUGGESTIONS INTELLIGENTES

8. 💡 Pattern détecté : Documentation probe = README + METRICS
   Observation : Tous les probes suivent ce pattern SAUF :
   - docs/probes/citrix/ (a 5 fichiers : README, METRICS, DEBUG, SITE_FILTERING, etc.)

   Question :
   "Citrix est-il un cas particulier ou devrait-on créer des docs similaires
   pour les autres probes complexes (Redfish, OTEL) ?"

9. 💡 Opportunité : Documentation interactive
   Technologies disponibles :
   - Docusaurus (React-based)
   - MkDocs (Python, Material theme)
   - VitePress (Vue-based)

   Avantages :
   - Recherche instantanée
   - Versioning intégré
   - Dark mode
   - Navigation améliorée

   "Migrer vers un site de documentation interactif ?"

10. 💡 Auto-documentation depuis code
    Observation : 92% de fonctions documentées en godoc
    Opportunité : Générer docs/api/ depuis godoc

    Outils disponibles :
    - godoc (standard)
    - pkgsite (go.dev style)
    - swag (API REST documentation)

    "Mettre en place génération auto de docs API depuis code ?"

📊 STATISTIQUES COMPARATIVES

Votre projet vs projets similaires :

| Métrique | Vous | Moyenne Open Source | Grade |
|----------|------|---------------------|-------|
| Docs Coverage | 96% | 72% | A+ |
| Code Documentation | 92% | 65% | A |
| Examples Quality | 95% | 70% | A+ |
| Cross-references | 356 liens | ~50 | A+ |
| Freshness (<3 mois) | 92% | 45% | A+ |
| **Manque** : API docs | 67% | 80% | B- |
| **Manque** : Visual content | 0% | 25% | F |

💬 FEEDBACK UTILISATEURS (détecté depuis issues/PR)

Analyse des 15 dernières interactions :
- 🟢 8x "Documentation très claire" (Quick Start)
- 🟡 3x "Manque exemples avancés" (Probes)
- 🔴 2x "API documentation confusing" (HTTP Strategy)
- 🔴 2x "Migration guide needed" (Config v2)

🎯 RECOMMANDATION FINALE

Top 3 actions pour améliorer à 100% :

1. **Créer docs/api/** (1 journée)
   - HTTP-API-REFERENCE.md
   - CONFIGURATION-API-REFERENCE.md
   - AUTO-UPDATE-API.md

2. **Créer MIGRATION-GUIDE.md** (2 heures)
   - Guide v1 → v2
   - Breaking changes
   - Script de migration

3. **Enrichir exemples probes** (3 heures)
   - Kafka : Exemples cluster, SSL, authentication
   - OTEL : Exemples Jaeger, Prometheus, Grafana
   - Redfish : Exemples multi-vendor

Total effort : ~2 jours pour passer de 96% → 99%

Quelle action veux-tu que je traite en premier ?
```

---

## 🌍 Phase 5 : Adaptation Multi-Projets

### Prompt 5.1 : Nouveau Projet (Python Flask)

**Input (FR)** :
```
Change de projet : analyse /Users/matthieu/projects/flask-api
```

**Output (FR)** :
```
🔄 Changement de projet détecté...

🧹 Nettoyage du contexte précédent :
   - Profil senhub-agent sauvegardé
   - Cache vidé
   - Templates réinitialisés

🔍 Analyse du nouveau projet : flask-api
==========================================

📁 Structure détectée :
   - Répertoire principal : /Users/matthieu/projects/flask-api
   - Documentation : documentation/ (pas docs/)
   - Fichiers : 8 fichiers markdown
   - Langage : Python (*.py détecté)

🐍 Framework détecté : Flask
   - requirements.txt trouvé
   - app.py trouvé
   - Blueprints détectés : 3

🌍 Langue analysée :
   - documentation/ : 100% anglais
   - Code comments : 95% anglais (5% français dans tests)
   - Recommandation : Maintenir anglais

✍️  Style détecté :

STRUCTURE :
   - Organization : by_topic (pas by_audience)
   - Catégories : api/, guides/, deployment/
   - Profondeur : 2 niveaux max

WRITING STYLE :
   - Tone : Formel (très technique)
   - Emojis : ❌ AUCUN (style minimaliste)
   - Sentence length : Courtes (10-15 mots)
   - Technical depth : Expert (suppose connaissances Python/Flask)

SECTIONS STANDARD :
   Différent de senhub-agent !
   1. Prerequisites (toujours en premier)
   2. Installation
   3. Configuration
   4. Usage
   5. API Reference (très détaillé)

   ⚠️  PAS de "Quick Start" !
   ⚠️  PAS de "Troubleshooting" !

CODE DOCUMENTATION :
   - Style : Google Docstrings (Python)
   - Coverage : ~85%
   - Type hints : Utilisés (Python 3.8+)

CONFIGURATION :
   - Format : .ini (pas YAML !)
   - Commentaires : Bloc au-dessus (pas inline)

📊 Profil Créé :
```yaml
project: flask-api
language: english
communication: français

structure:
  docs_location: "documentation/"
  organization: "by_topic"

writing_style:
  tone: "formal"
  emoji_usage: false  # ← Différent !
  technical_depth: "expert"

standard_sections:  # ← Très différent !
  - "Prerequisites"
  - "Installation"
  - "Configuration"
  - "Usage"
  - "API Reference"

code_documentation:
  language: "python"  # ← Différent !
  style: "google_docstrings"
```

💾 Nouveau profil sauvegardé : .doc-manager/flask-api-style.yaml

✅ Agent adapté au nouveau projet !

🎯 Différences majeures avec senhub-agent :
   1. ❌ Pas d'emojis (style minimaliste)
   2. ❌ Pas de Quick Start (Prerequisites first)
   3. ❌ Pas de Troubleshooting sections
   4. ✅ API Reference très détaillée (focus)
   5. ✅ Configuration .ini (pas YAML)
   6. ✅ Python docstrings (pas godoc)

Je suis maintenant configuré pour flask-api.
Toute génération suivra ce nouveau style.

Que veux-tu que je fasse sur ce projet ?
```

---

### Prompt 5.2 : Génération Adaptée au Nouveau Projet

**Input (FR)** :
```
Documente le nouveau endpoint POST /api/users pour flask-api
```

**Output (FR)** :
```
📝 Création documentation endpoint users...

🎯 Style appliqué : flask-api (formel, sans emojis, API-first)

📄 Fichier à créer/modifier :
   - documentation/api/users.md (nouveau)
   - documentation/api/README.md (mise à jour)

✍️  Structure (DIFFÉRENTE de senhub-agent) :
   1. Prerequisites
   2. Endpoint Definition
   3. Request Schema
   4. Response Schema
   5. Usage Examples
   6. Error Codes

   ⚠️  PAS de Quick Start (non utilisé dans ce projet)
   ⚠️  PAS d'emojis (style minimaliste)

✅ Génération... (EN ANGLAIS, style formel)
```

**Fichier Généré (EN, style formel)** :
```markdown
# Users API Endpoint

## Prerequisites

- Flask application running
- Authentication token configured
- Database migrations applied
- PostgreSQL 12+ available

## Endpoint Definition

Create a new user account.

**URL**: `/api/users`
**Method**: `POST`
**Authentication**: Required (Bearer token)
**Content-Type**: `application/json`

## Request Schema

| Field | Type | Required | Constraints | Description |
|-------|------|----------|-------------|-------------|
| `username` | string | Yes | 3-50 chars, alphanumeric | Unique username |
| `email` | string | Yes | Valid email format | User email address |
| `password` | string | Yes | Min 8 chars | User password (will be hashed) |
| `role` | string | No | enum: user, admin | User role (default: user) |

### Request Example

\`\`\`json
{
  "username": "john_doe",
  "email": "john@example.com",
  "password": "SecurePass123!",
  "role": "user"
}
\`\`\`

## Response Schema

### Success Response (201 Created)

| Field | Type | Description |
|-------|------|-------------|
| `id` | integer | Generated user ID |
| `username` | string | Username |
| `email` | string | Email address |
| `role` | string | Assigned role |
| `created_at` | string | ISO 8601 timestamp |

\`\`\`json
{
  "id": 42,
  "username": "john_doe",
  "email": "john@example.com",
  "role": "user",
  "created_at": "2025-01-15T10:30:00Z"
}
\`\`\`

### Error Response (4xx, 5xx)

| Field | Type | Description |
|-------|------|-------------|
| `error` | string | Error type |
| `message` | string | Human-readable error message |
| `details` | object | Additional error context (optional) |

\`\`\`json
{
  "error": "validation_error",
  "message": "Username already exists",
  "details": {
    "field": "username",
    "constraint": "unique"
  }
}
\`\`\`

## Usage Examples

### cURL

\`\`\`bash
curl -X POST https://api.example.com/api/users \\
  -H "Authorization: Bearer YOUR_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{
    "username": "john_doe",
    "email": "john@example.com",
    "password": "SecurePass123!"
  }'
\`\`\`

### Python requests

\`\`\`python
import requests

url = "https://api.example.com/api/users"
headers = {
    "Authorization": "Bearer YOUR_TOKEN",
    "Content-Type": "application/json"
}
data = {
    "username": "john_doe",
    "email": "john@example.com",
    "password": "SecurePass123!"
}

response = requests.post(url, json=data, headers=headers)
print(response.json())
\`\`\`

### JavaScript fetch

\`\`\`javascript
fetch('https://api.example.com/api/users', {
  method: 'POST',
  headers: {
    'Authorization': 'Bearer YOUR_TOKEN',
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    username: 'john_doe',
    email: 'john@example.com',
    password: 'SecurePass123!'
  })
})
.then(response => response.json())
.then(data => console.log(data));
\`\`\`

## Error Codes

| Status Code | Error Type | Description | Action Required |
|-------------|------------|-------------|-----------------|
| 400 | `validation_error` | Invalid request data | Check request schema |
| 401 | `authentication_required` | Missing or invalid token | Provide valid Bearer token |
| 409 | `conflict` | Username or email already exists | Choose different values |
| 422 | `unprocessable_entity` | Data validation failed | Review field constraints |
| 500 | `internal_error` | Server error | Contact support |

## Implementation Details

Function: `create_user()`
Module: `app.api.users`
Decorator: `@jwt_required()`

Database transaction is wrapped in try-catch with automatic rollback on error.

## Related Endpoints

- `GET /api/users/{id}` - Retrieve user details
- `PUT /api/users/{id}` - Update user information
- `DELETE /api/users/{id}` - Delete user account
- `GET /api/users` - List all users (admin only)

## Security Considerations

- Password is hashed using bcrypt before storage
- Email is validated using regex pattern
- Rate limiting: 10 requests per minute per IP
- Input sanitization applied to all fields

---

**Last Updated**: 2025-01-15
**API Version**: 1.0
**Authentication**: Required
```

**Confirmation (FR)** :
```
✅ Documentation endpoint créée !

📄 Fichier : documentation/api/users.md

📊 Style appliqué (flask-api) :
   - ✅ Tone : Formel (technique)
   - ✅ Emojis : AUCUN (minimaliste)
   - ✅ Structure : Prerequisites → Definition → Schemas → Examples → Errors
   - ✅ Exemples : 3 langages (curl, Python, JavaScript)
   - ✅ Tables : Détaillées (tous les champs documentés)

🔍 Différences avec senhub-agent :
   - ❌ Pas de Quick Start
   - ❌ Pas d'emojis
   - ✅ Focus API (schémas + error codes)
   - ✅ Style formel

✅ Génération 100% adaptée au projet flask-api !
```

---

## 📖 Résumé des Patterns

### Communication
- **TOUJOURS** : Utilisateur → Agent en FRANÇAIS
- **TOUJOURS** : Agent → Documentation en ANGLAIS
- **TOUJOURS** : Réponses agent à l'utilisateur en FRANÇAIS

### Adaptation
- L'agent **apprend** du style du projet
- L'agent **s'adapte** aux conventions existantes
- L'agent **n'impose pas** son style

### Intelligence
- **Détection** : Langue, tone, structure, emojis
- **Apprentissage** : Des modifications utilisateur
- **Suggestions** : Proactives basées sur l'analyse

---

**Prochaine étape** : Implémenter l'agent avec ces prompts comme référence ! 🚀
