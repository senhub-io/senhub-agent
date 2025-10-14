# Agent Doc-Manager - Spécification Universelle

## 🎯 Mission

Créer un agent intelligent de gestion documentaire capable de travailler sur **n'importe quel projet**, en apprenant et s'adaptant au style, conventions et besoins documentaires existants.

## 🌍 Contrainte Linguistique CRITIQUE

### Règle Absolue

```yaml
language_policy:
  communication:
    user_to_agent: "français"      # Toutes les demandes en français
    agent_to_user: "français"      # Toutes les réponses en français

  output_generation:
    documentation: "english"        # TOUTE la doc générée en anglais
    code_comments: "english"        # TOUS les commentaires de code en anglais
    examples: "english"             # TOUS les exemples en anglais
    commit_messages: "english"      # Messages de commit en anglais

  detection:
    analyze_existing: true          # Détecte la langue de la doc existante
    respect_existing: true          # Respecte la langue du projet analysé
    default_if_none: "english"      # Par défaut = anglais si aucune doc
```

### Exemples de Communication

**❌ INCORRECT**:
```
User: "Documente la nouvelle feature Redis cache"
Agent: "I'll create the documentation for the Redis cache feature..."
```

**✅ CORRECT**:
```
User: "Documente la nouvelle feature Redis cache"
Agent: "Je vais créer la documentation pour le cache Redis.
        Voici ce que je vais générer :
        - docs/admin-guide/REDIS-CACHE.md (en anglais)
        - Commentaires godoc pour redis_cache.go (en anglais)
        - Exemples de configuration YAML (en anglais)"

Output généré:
# Redis Cache Configuration

The Redis cache provides high-performance caching for agent metrics...

## Quick Start
```yaml
cache:
  enabled: true
  redis:
    host: "localhost:6379"
```
```

## 🧠 Capacités Fondamentales

### 1. Phase d'Analyse du Projet

L'agent doit analyser n'importe quel projet et extraire :

#### Structure Documentaire
- **Organisation des répertoires** (`docs/`, `documentation/`, `wiki/`, etc.)
- **Patterns de nommage** (README.md, GUIDE.md, reference.md, etc.)
- **Profondeur de hiérarchie** (plat, imbriqué, catégorisé)
- **Stratégie de séparation** (par audience, par feature, par sujet)

#### Langue de Documentation
```yaml
# L'agent détecte automatiquement
language_detection:
  primary_language: "english"         # Détecté depuis fichiers existants
  consistency: 98%                    # 47/48 fichiers en anglais
  mixed_language_files: 1             # CLAUDE.md a du français dans exemples

  recommendation: "maintain_english"  # Continuer en anglais
```

#### Style d'Écriture
- **Ton détecté** (formel, accessible, technique, amical)
- **Structure des phrases** (courtes/longues, voix active/passive)
- **Profondeur technique** (débutant-friendly vs expert)
- **Usage d'emojis** (oui/non, lesquels)
- **Style des exemples de code** (inline, blocs, avec/sans commentaires)

#### Patterns Structurels
- **Ordre des sections** (ce qui vient en premier : exemples, théorie, config ?)
- **Sections récurrentes** (Quick Start, Configuration, Troubleshooting)
- **Usage de tableaux** (paramètres, comparaisons, références)
- **Style de cross-référencement** (liens relatifs, absolus, ancres)

#### Audiences Ciblées
- **Audiences principales** (utilisateurs finaux, admins, développeurs)
- **Séparation documentaire** (docs séparées par audience ou mixées)
- **Profondeur par audience** (quick starts vs deep dives)

#### Documentation du Code
- **Style de commentaires inline** (godoc, JSDoc, docstrings, etc.)
- **Documentation de fonctions** (ce qui est documenté, ce qui ne l'est pas)
- **Exemples de code** (inclus ou fichiers séparés)
- **Documentation API** (auto-générée ou manuelle)

#### Exemples de Configuration
- **Format préféré** (YAML, JSON, TOML, INI)
- **Style de commentaires** (inline, bloc, exemples)
- **Valeurs réelles vs placeholders** (vraies IPs ou 192.0.2.1)

### 2. Profil de Style (Phase d'Adaptation)

Une fois l'analyse faite, l'agent crée un **Profil de Style** :

```yaml
project_style:
  name: "senhub-agent"  # Auto-détecté depuis le repo
  language: "english"   # CRITIQUE : langue des docs

  structure:
    docs_location: "docs/"
    organization: "by_audience"  # by_audience | by_feature | by_topic | flat
    categories:
      - name: "user-guide"
        audience: "end_users"
      - name: "admin-guide"
        audience: "administrators"
      - name: "probes"
        organization: "by_feature"

  writing_style:
    language: "english"               # TOUJOURS respecter cette langue
    tone: "professional_accessible"   # formal | professional_accessible | casual | technical
    emoji_usage: true
    emoji_categories:
      - "📚"  # documentation
      - "🚀"  # quick start
      - "⚙️"  # configuration
      - "🎯"  # goals/objectives
    sentence_length: "medium"         # short | medium | long
    technical_depth: "balanced"       # beginner | balanced | expert
    voice: "active"                   # active | passive

  structural_patterns:
    standard_sections:
      - name: "Quick Start"
        position: 1
        required: true
      - name: "Configuration"
        position: 2
        required: true
      - name: "Examples"
        position: 3
        required: false
      - name: "Troubleshooting"
        position: -1  # toujours en dernier
        required: false

    table_usage:
      parameters: true
      comparisons: true
      style: "github_markdown"

    code_blocks:
      always_specify_language: true
      include_comments: true
      show_output: "when_helpful"

  code_documentation:
    language: "go"
    comment_language: "english"  # CRITIQUE : commentaires toujours en anglais
    style: "godoc"
    conventions:
      - "Package comment required"
      - "Exported functions documented"
      - "Interface documented with purpose"
      - "Examples in _test.go files"

  configuration_format:
    primary: "yaml"
    comment_style: "inline"
    comment_language: "english"  # Commentaires YAML en anglais
    examples: "real_with_placeholders"

  audiences:
    - name: "end_users"
      docs_location: "user-guide/"
      reading_time: "5-10 minutes"
      depth: "practical"
      focus: "getting_started"

    - name: "administrators"
      docs_location: "admin-guide/"
      reading_time: "20-30 minutes"
      depth: "comprehensive"
      focus: "all_parameters"

    - name: "developers"
      docs_location: "CLAUDE.md"
      reading_time: "complete"
      depth: "architectural"
      focus: "patterns_and_conventions"
```

### 3. Génération de Documentation (Phase de Production)

Basé sur le style appris, l'agent peut :

#### Créer une Nouvelle Documentation

**Demande utilisateur (en français)** :
```
"Documente la nouvelle feature de cache Redis"
```

**Réponse agent (en français)** :
```
Je vais créer la documentation pour le cache Redis en suivant le style du projet :
- Fichier : docs/admin-guide/REDIS-CACHE.md
- Langue : Anglais
- Structure : Quick Start → Configuration → Examples → Troubleshooting
- Style : Professional accessible avec emojis
```

**Sortie générée (EN ANGLAIS)** :
```markdown
# Redis Cache Configuration

## 🚀 Quick Start

The Redis cache provides high-performance caching for agent metrics with configurable TTL and automatic cleanup.

### Basic Configuration

\`\`\`yaml
storage:
  - name: http
    params:
      cache:
        enabled: true
        backend: redis
        redis:
          host: "localhost:6379"
          password: ""
          db: 0
          ttl: 300  # 5 minutes
\`\`\`

## ⚙️ Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `host` | string | `localhost:6379` | Redis server address |
| `password` | string | `""` | Redis authentication password |
| `db` | integer | `0` | Redis database number |
| `ttl` | integer | `300` | Time to live in seconds |

## 📊 Performance Considerations

...
```

#### Mettre à Jour une Documentation Existante

**Demande (FR)** :
```
"Ajoute les nouveaux filtres de modules dans LOGGING.md"
```

**Action (EN)** :
- Maintient la structure existante
- Ajoute la section en anglais
- Met à jour les cross-références
- Préserve le format d'exemples

#### Maintenir l'Index de Documentation

**Demande (FR)** :
```
"Nouvelle documentation probe ajoutée"
```

**Action automatique (EN)** :
- Met à jour `DOCUMENTATION-INDEX.md`
- Ajoute les cross-références
- Met à jour les tables de navigation
- Maintient l'ordre alphabétique/catégoriel

### 4. Validation & Cohérence (Phase de Qualité)

L'agent vérifie :

#### Cohérence Linguistique
- ✅ Toute la documentation en anglais
- ✅ Tous les commentaires de code en anglais
- ✅ Tous les exemples en anglais
- ⚠️ Détection de mélange de langues

#### Cohérence Structurelle
- ✅ Tous les docs suivent l'ordre de sections standard
- ✅ Sections requises présentes
- ✅ Nommage de fichiers conforme

#### Cohérence de Style
- ✅ Ton uniforme dans tous les docs
- ✅ Usage d'emojis cohérent
- ✅ Formatage des code blocks uniforme

## 🔧 Implémentation de l'Agent

### Workflow

```
1. ANALYSER (communication en français)
   └─> Scanner la documentation du projet
   └─> Extraire patterns et conventions
   └─> Détecter la LANGUE de documentation
   └─> Construire le Profil de Style

2. ADAPTER (configuration en français)
   └─> Charger le Profil de Style
   └─> Configurer templates de sortie (EN)
   └─> Définir règles de ton et structure

3. PRODUIRE (sortie EN ANGLAIS)
   └─> Générer/Mettre à jour la documentation
   └─> Appliquer le style appris
   └─> Cross-référencer automatiquement
   └─> TOUT EN ANGLAIS

4. VALIDER (rapport en français)
   └─> Vérifier la cohérence
   └─> Valider les liens
   └─> Mettre à jour les métadonnées
   └─> Rapport en français à l'utilisateur
```

### Commandes (en Français)

#### Commandes d'Analyse
```bash
doc-manager analyser
# → Scanne le projet, crée le Profil de Style
# → Sortie : "✅ Projet analysé : 48 fichiers, langue=anglais, tone=professional_accessible"

doc-manager analyser --output=style-profile.yaml
# → Sauvegarde le Profil de Style

doc-manager afficher-style
# → Affiche le style détecté et les conventions
```

#### Commandes de Génération
```bash
doc-manager creer --type=probe --name=kafka
# → Communication : "Je crée la doc pour le probe Kafka..."
# → Génération : Documentation EN ANGLAIS matching project style

doc-manager creer --type=user-guide --name=quick-start
# → Communication : "Je crée un guide utilisateur avec profondeur appropriée..."
# → Génération : Guide EN ANGLAIS

doc-manager maj LOGGING.md --section="New Features"
# → Communication : "Je mets à jour LOGGING.md section New Features..."
# → Génération : Contenu EN ANGLAIS maintenant le style
```

#### Commandes de Maintenance
```bash
doc-manager rafraichir-index
# → "Mise à jour de DOCUMENTATION-INDEX.md..."
# → Génération : Index EN ANGLAIS

doc-manager verifier-liens
# → "Validation de 342 liens..."
# → Rapport : "✅ 340 OK, ⚠️ 2 cassés dans LOGGING.md"

doc-manager maj-metadata
# → "Mise à jour des dates last-updated..."
# → Génération : Métadonnées EN ANGLAIS
```

## 📋 Templates de Profil de Style

### Détection Minimale

Pour projets avec documentation minimale :

```yaml
# Détecté : Projet a 1-3 README seulement
style_profile:
  language: "english"  # Défaut si incertain

  structure:
    organization: "flat"
    single_readme: true

  writing_style:
    tone: "technical"  # défaut pour projets code
    emoji_usage: false  # conservateur par défaut

  audiences:
    - name: "developers"
      single_audience: true
```

### Projets Bien Documentés

Pour projets matures comme senhub-agent :

```yaml
# Détecté : 48+ fichiers markdown, structure organisée
style_profile:
  language: "english"  # Détecté : 98% des docs en anglais

  structure:
    organization: "by_audience"
    categories: ["user-guide", "admin-guide", "probes", "troubleshooting"]

  writing_style:
    tone: "professional_accessible"
    emoji_usage: true
    technical_depth: "balanced"

  audiences: ["end_users", "administrators", "developers"]
```

## 🎯 Cas d'Usage

### Cas 1 : Documentation Nouvelle Feature

**Scénario** : Développeur ajoute un "Kafka Probe" à senhub-agent

**Demande (FR)** :
```
"Documente le nouveau probe Kafka avec métriques consumer lag et throughput"
```

**Workflow Agent** :
1. **Détection contexte** : C'est un probe (depuis emplacement répertoire)
2. **Chargement template** : Basé sur docs CPU/Memory existants
3. **Application style** : Même structure, ton, format d'exemples
4. **Génération (EN)** :
   - `docs/probes/kafka/README.md` (overview + config)
   - `docs/probes/kafka/METRICS.md` (metrics reference)
5. **Mise à jour indexes** : Ajout à DOCUMENTATION-INDEX.md, docs/README.md
6. **Cross-référencement** : Lien depuis liste probes, probes similaires

**Réponse à l'utilisateur (FR)** :
```
✅ Documentation Kafka créée :
   - docs/probes/kafka/README.md (en anglais)
   - docs/probes/kafka/METRICS.md (en anglais)
   - Indexes mis à jour
   - 5 cross-références ajoutées
```

### Cas 2 : Projet Différent (Python Flask)

**Scénario** : Agent déployé sur un projet Python Flask

**Analyse (FR à l'utilisateur)** :
```
🔍 Analyse du projet en cours...
✅ Documentation trouvée dans documentation/
✅ 12 fichiers markdown détectés
✅ Langue détectée : Anglais (100%)
✅ Langage principal : Python
✅ Fichiers de config : .ini, .yaml

📊 Résultats d'analyse de style :
   Ton : Formel
   Organisation : par sujet (api/, guides/, reference/)
   Emojis : Non
   Exemples : Extensifs (Python)
   Audience : Développeurs

💾 Profil de Style sauvegardé dans .doc-manager/style.yaml
```

**Profil Créé** :
```yaml
style_profile:
  language: "english"  # Respecté
  structure:
    docs_location: "documentation/"
    organization: "by_topic"
  writing_style:
    tone: "formal"
    emoji_usage: false
  code_documentation:
    language: "python"
    style: "google_docstrings"
```

**Génération** : Docs EN ANGLAIS matching style Flask project

### Cas 3 : Détection de Mélange de Langues

**Scénario** : Projet avec docs partiellement en français

**Analyse** :
```yaml
language_detection:
  primary_language: "french"
  consistency: 65%  # 13/20 fichiers en français
  mixed_files:
    - "README.md"  # Français
    - "docs/api.md"  # Anglais
    - "INSTALL.md"  # Français

  recommendation: "standardize_to_french"  # ou "english" selon contexte
```

**Dialogue avec l'utilisateur (FR)** :
```
⚠️  Détection de mélange de langues :
   - 65% des docs en français
   - 35% des docs en anglais

❓ Quelle langue souhaitez-vous pour la documentation ?
   [1] Français (continuer majoritaire)
   [2] Anglais (standardiser projet)
   [3] Garder le mélange (non recommandé)

Choix : _
```

**Si choix = Anglais** :
```
✅ Configuration sauvegardée : output_language = "english"
📝 Toute génération future sera en anglais
🔄 Voulez-vous migrer les docs françaises vers l'anglais ? [o/N]
```

## 🧪 Tests & Validation

### Auto-Test de l'Agent

```bash
doc-manager tester-detection-style
```

**Sortie (FR)** :
```
✅ Structure documentaire détectée
   Emplacement : docs/
   Organisation : by_audience (confiance : 95%)
   Catégories : user-guide, admin-guide, probes

✅ Style d'écriture détecté
   Langue : Anglais (98% des fichiers)
   Ton : professional_accessible (confiance : 90%)
   Emojis : OUI (📚 🚀 ⚙️ 🎯)
   Profondeur technique : balanced

⚠️  Détection incertaine
   Audiences : 3 détectées (end_users, admins, developers)
   Souhaitez-vous en ajouter ? [o/N]
```

### Tests de Cohérence

```bash
doc-manager tester-coherence
```

**Sortie (FR)** :
```
📊 Rapport de Cohérence
=====================

✅ Langue : 100% anglais
✅ Structure : Conforme au profil
✅ Ton : Cohérent sur 48/48 fichiers
⚠️  Emojis : Incohérent dans 2 fichiers
   - docs/old/LEGACY.md : Pas d'emojis (fichier archivé)
   - docs/troubleshooting/DEBUG.md : Emojis différents

🔗 Cross-références : 342 liens
   ✅ 340 valides
   ❌ 2 cassés dans LOGGING.md (lignes 42, 67)

Suggestions de correction :
  1. Harmoniser emojis dans DEBUG.md
  2. Réparer liens cassés dans LOGGING.md
  3. Ajouter cross-ref : HTTP-STRATEGY.md → HTTPS-CONFIGURATION.md
```

## 🔌 Points d'Intégration

### Git Hooks

```bash
# Pre-commit : Valider cohérence docs
doc-manager valider --rapide

# Post-commit : Mettre à jour metadata
doc-manager maj-metadata --fichiers-modifies
```

### CI/CD

```yaml
# .github/workflows/docs-check.yml
- name: Validation Documentation
  run: |
    doc-manager analyser
    doc-manager valider --strict
    doc-manager verifier-liens --echouer-si-casse
```

## 📊 Métriques & Rapports

```bash
doc-manager stats
```

**Sortie (FR)** :
```
Rapport de Santé Documentaire
==============================

📁 Structure
   Total documents : 48
   Organisation : ✅ by_audience
   Index manquant : ❌ 0

✍️  Cohérence de Style
   Langue : 98% anglais (47/48) ⚠️ 1 fichier français
   Correspondance ton : 96%
   Correspondance structure : 100%
   Emojis : cohérents

🔗 Cross-Références
   Total liens : 342
   Liens cassés : 2 ⚠️
   Liens manquants : 5 (suggestions ci-dessous)

📅 Fraîcheur
   Mis à jour < 1 mois : 35 (73%)
   Mis à jour < 3 mois : 42 (88%)
   Obsolète (>6 mois) : 6 ⚠️

🎯 Couverture
   Guides utilisateurs : ✅ complet
   Guides admin : ✅ complet
   Docs API : ⚠️  incomplet (67%)
   Commentaires code : ✅ 92% de couverture

Suggestions :
  - Réparer liens cassés dans LOGGING.md (ligne 42, 67)
  - MAJ docs obsolètes : CITRIX-OLD-NOTES.md (8 mois)
  - Ajouter cross-ref : HTTP-STRATEGY.md → HTTPS-CONFIGURATION.md
  - ⚠️  1 fichier en français : docs/archive/NOTES-FR.md
```

## 🎓 Apprentissage & Amélioration

### Boucle de Feedback

```bash
# L'utilisateur corrige une doc générée
doc-manager apprendre-des-modifications README.md
```

**Sortie (FR)** :
```
🔍 Analyse des modifications apportées...
✅ Détecté : Ajout section "Prerequisites" avant "Quick Start"
✅ Détecté : Reformulation ton plus direct
✅ Détecté : Ajout d'exemples curl supplémentaires

💾 Profil de Style mis à jour :
   - Section "Prerequisites" ajoutée (position: 0.5)
   - Préférence exemples curl : true

📝 Ces patterns seront appliqués aux futures générations
```

### Tracking d'Évolution du Style

```yaml
style_profile:
  version: 3
  last_updated: "2025-01-15"

  changes:
    - version: 2
      date: "2025-01-10"
      change: "Added troubleshooting section requirement"
      reason: "User consistently added it manually"

    - version: 3
      date: "2025-01-15"
      change: "Increased emoji usage from false to true"
      reason: "Project adopted visual navigation"
```

## 🚀 Améliorations Futures

### Phase 1 : Agent Core (MVP)
- ✅ Analyse et détection de style
- ✅ Génération basique (créer, mettre à jour)
- ✅ Validation des liens
- ✅ Maintenance d'index
- ✅ **Respect strict langue : Communication FR / Sortie EN**

### Phase 2 : Intelligence
- 🔄 Apprentissage depuis modifications
- 🔄 Tracking évolution de style
- 🔄 Suggestions contextuelles
- 🔄 Auto-détection sections obsolètes

### Phase 3 : Features Avancées
- 📋 Support multi-langues (auto-traduction en gardant style)
- 📋 Documentation visuelle (diagrammes, flowcharts)
- 📋 Génération docs interactives
- 📋 Métriques de couverture documentation

### Phase 4 : Powered by AI
- 🤖 Requêtes en langage naturel ("Documente toutes les features Redis")
- 🤖 Auto-génération depuis analyse de code
- 🤖 Suggestion de documentation manquante
- 🤖 Scoring qualité et suggestions d'amélioration

## 📖 Exemple : Agent en Action

### Scénario : Onboarding Nouveau Projet

```bash
$ cd /path/to/nouveau-projet

$ doc-manager init
🔍 Analyse de la structure du projet...
✅ Documentation trouvée dans docs/
✅ 12 fichiers markdown détectés
✅ Langage principal identifié : Python
✅ Fichiers de configuration trouvés : .ini, .yaml
✅ Langue de documentation : Anglais (100%)

📊 Résultats d'Analyse de Style :
   Langue : Anglais
   Ton : Formel
   Organisation : par sujet (api/, guides/, reference/)
   Emojis : Non
   Exemples : Extensifs (Python)
   Audience : Développeurs

💾 Profil de Style sauvegardé dans .doc-manager/style.yaml

$ doc-manager creer --type=guide --name=deployment
📝 Création d'un nouveau guide...
✅ Généré : docs/guides/deployment.md (EN ANGLAIS)
   - Structure : Suit le pattern du projet (pas de Quick Start, détails tech en premier)
   - Ton : Formel
   - Exemples : Scripts Python de déploiement inclus
   - Cross-refs : Lié à configuration.md, api/endpoints.md

$ doc-manager valider
✅ Toute la documentation cohérente avec le style du projet
✅ Langue : 100% anglais
⚠️  3 liens cassés trouvés (auto-corrigés)
📊 Couverture documentation : 87%
```

---

## 🎯 Résumé

Cet agent doc-manager est conçu pour être :

1. **Universel** : Fonctionne sur n'importe quel projet, n'importe quel langage, n'importe quel style de doc
2. **Adaptatif** : Apprend depuis la documentation existante
3. **Cohérent** : Maintient le style à travers tous les docs
4. **Intelligent** : S'améliore au fil du temps avec les retours
5. **Automatisé** : Gère les tâches routinières (indexes, liens, metadata)
6. **Bilingue** : **Communication en français, génération en anglais**

L'agent n'impose pas un style — il **apprend et matche les conventions existantes de votre projet**.

### 🔑 Règle d'Or Linguistique

```
┌─────────────────────────────────────────────────┐
│  TOUJOURS :                                     │
│  - Parler à l'utilisateur en FRANÇAIS           │
│  - Générer la documentation en ANGLAIS          │
│  - Respecter la langue détectée du projet       │
│                                                  │
│  ✅ User request (FR) → Agent response (FR)     │
│  ✅ Agent generation → Documentation (EN)        │
│  ✅ Code comments → ENGLISH                     │
│  ✅ YAML comments → ENGLISH                     │
└─────────────────────────────────────────────────┘
```
