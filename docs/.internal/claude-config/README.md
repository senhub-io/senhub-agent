# Documentation des Agents Claude Code pour Go

## 📋 Table des matières

1. [Vue d'ensemble](#vue-densemble)
2. [Installation](#installation)
3. [Configuration](#configuration)
4. [Utilisation](#utilisation)
5. [Conventions](#conventions)
6. [Troubleshooting](#troubleshooting)

---

## Vue d'ensemble

Ce projet fournit deux agents spécialisés pour Claude Code :

### 1. **code-reviewer** 
Agent de review de code Go qui utilise les commandes Makefile pour l'analyse qualité.

**Fonctionnalités :**
- ✅ Analyse automatique avec golangci-lint
- ✅ Détection de race conditions
- ✅ Audit de sécurité (gosec + govulncheck)
- ✅ Rapport de couverture de tests
- ✅ Review manuelle selon les best practices Go

### 2. **release-manager**
Agent de gestion de releases qui orchestre le processus de versioning et publication.

**Fonctionnalités :**
- ✅ Contrôles qualité automatiques avant release
- ✅ Analyse des commits et recommandation de version
- ✅ Build multi-plateforme automatique
- ✅ Génération de CHANGELOG
- ✅ Création de tags Git et GitHub Releases
- ✅ Confirmations obligatoires avant actions distantes

---

## Installation

### Option 1 : Installation manuelle (recommandé)

```bash
# 1. Créer le dossier
mkdir -p ~/.claude/agents

# 2. Copier les fichiers
cp code-reviewer.md ~/.claude/agents/
cp release-manager.md ~/.claude/agents/

# 3. Vérifier les permissions
chmod 644 ~/.claude/agents/*.md

# 4. Vérifier l'installation
ls -la ~/.claude/agents/
```

### Option 2 : Script d'installation

```bash
# Rendre le script exécutable
chmod +x install-agents.sh

# Exécuter
./install-agents.sh
```

### Installation des outils Go

Dans votre projet Go :

```bash
# Utiliser le Makefile
make install-tools

# OU manuellement
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
brew install gh  # GitHub CLI
```

---

## Configuration

### Configuration Git (REQUIS)

**Avant d'utiliser les agents, configurez votre identité Git :**

```bash
# Configuration globale (tous vos projets)
git config --global user.name "Matthieu Noirbusson"
git config --global user.email "matthieu.noirbusson@sensorfactory.eu"

# Vérifier
git config --global user.name
git config --global user.email
```

**Pourquoi ?** Les agents créent des commits et tags qui seront signés avec ce nom :
- Commits de release : `chore: release X.Y.Z` signé par Matthieu Noirbusson
- Tags Git : Tagger = Matthieu Noirbusson
- CHANGELOG : `Release prepared by: Matthieu Noirbusson`
- Rapports de review : `Reviewed by: Matthieu Noirbusson`

**Pour une signature GPG (optionnel mais recommandé), voir [GIT-CONFIG.md](GIT-CONFIG.md)**

### Structure des fichiers

```
~/.claude/
└── agents/
    ├── code-reviewer.md
    └── release-manager.md
```

### Makefile requis

Votre projet Go doit avoir un Makefile avec ces targets :

**Pour code-reviewer :**
- `make lint` - Linting avec golangci-lint
- `make lint-fix` - Corrections automatiques
- `make test` - Tests standards
- `make test-race` - Tests avec race detector
- `make coverage` - Rapport de couverture
- `make security` - Audit de sécurité
- `make pre-commit` - Vérifications rapides
- `make quality-check` - Tous les contrôles

**Pour release-manager :**
- `make version-info` - Afficher la version
- `make quality-check` - Contrôles qualité
- `make build` - Build multi-plateforme
- `make package` - Créer les packages ZIP
- `make release` - quality-check + build
- `make bump-version` - Créer et pusher un tag (interactif)

---

## Utilisation

### Code Reviewer

#### Invocation de base

```
> Use code-reviewer to review my changes
```

#### Invocations spécifiques

```
> Ask code-reviewer to check for race conditions in the handlers package
> Have code-reviewer analyze security in auth.go
> Use code-reviewer to review this PR
> Ask code-reviewer to check test coverage
```

#### Workflow typique

1. L'agent exécute `make pre-commit`
2. Il génère le rapport de couverture avec `make coverage`
3. Il effectue une review manuelle du code
4. Il produit un rapport structuré avec :
   - Résumé exécutif
   - Résultats des outils automatiques
   - Problèmes par criticité (🔴🟡🟢)
   - Points positifs
   - Métriques de qualité
   - Recommandations d'action

#### Exemple de sortie

```
La review couvre 12 fichiers avec 245 changements.
Qualité globale : bonne
Problèmes critiques détectés : 0

✅ OUTILS AUTOMATIQUES

Formatage (go fmt) : ✓ PASS
Linting (golangci-lint) : ✓ PASS
Tests unitaires : ✓ PASS (87 tests)
Race detector : ✓ PASS
Couverture : 84%
Sécurité (gosec) : ✓ PASS
Vulnérabilités (govulncheck) : ✓ PASS

🟡 AMÉLIORATIONS RECOMMANDÉES :

[handlers/user.go:45] Utiliser context.WithTimeout
Impact : Sécurité/Performance
Effort : Faible

📊 MÉTRIQUES :
Score global : 8/10
```

### Release Manager

#### Invocation de base

```
> Use release-manager to prepare a release
```

#### Invocations spécifiques

```
> Ask release-manager to create a hotfix for version 1.2.3
> Have release-manager check if we're ready for release
> Use release-manager to show version info
> Ask release-manager to prepare a release candidate
```

#### Workflow typique d'une release

1. **Phase 0 : Vérification**
   - État du repository
   - Outils installés
   - Branche courante
   - 🛑 Confirmation utilisateur

2. **Phase 1 : Qualité**
   - Exécution de `make quality-check`
   - 🛑 Rapport et confirmation

3. **Phase 2 : Analyse commits**
   - Récupération des commits depuis dernière version
   - Analyse du type de changements
   - Recommandation de version (MAJOR/MINOR/PATCH)
   - 🛑 Décision de version (SANS "v")

4. **Phase 3 : Build**
   - `make release` (quality + build)
   - `make package` (création des ZIP)
   - 🛑 Validation des artefacts

5. **Phase 4 : CHANGELOG**
   - Génération automatique
   - 🛑 Édition par l'utilisateur

6. **Phase 5 : Tag et Push**
   - Commit du CHANGELOG
   - 🛑 CONFIRMATION CRITIQUE
   - Exécution de `make bump-version`

7. **Phase 6 : GitHub Release**
   - 🛑 Confirmation publication
   - Création de la release avec assets

8. **Phase 7 : Post-release**
   - Affichage du résumé
   - Prochaines étapes

#### Exemple de confirmation critique

```
🔍 ACTION CRITIQUE : Création et push du tag via Makefile

Le Makefile va exécuter 'make bump-version' qui va :
1. Créer un tag annoté (ajoutera automatiquement le "v")
2. Pusher le tag vers origin

⚠️ ATTENTION : Le Makefile push AUTOMATIQUEMENT le tag !
⚠️ Cette action est IRRÉVERSIBLE une fois le tag poussé.

Détails du tag :
- Version saisie : 1.2.3 (SANS "v")
- Tag Git créé : v1.2.3 (le "v" sera ajouté automatiquement)
- Message : Contenu de CHANGELOG_1.2.3.md
- Commit : abc123def456

⚠️⚠️⚠️ CONFIRMATION FINALE ⚠️⚠️⚠️

Tapez 'PUSH TAG 1.2.3' (SANS "v") exactement pour confirmer :
```

---

## Conventions

### Versioning

**❌ NE JAMAIS utiliser le préfixe "v" :**
```
# INCORRECT
v1.2.3
v2.0.0-rc
v1.5.2

# CORRECT
1.2.3
2.0.0-rc
1.5.2
```

**Le Makefile ajoute automatiquement le "v" pour les tags Git.**

### Semantic Versioning

- **MAJOR (X.0.0)** : Breaking changes, incompatibilités
- **MINOR (x.Y.0)** : Nouvelles fonctionnalités, compatibles
- **PATCH (x.y.Z)** : Corrections de bugs uniquement

### Conventional Commits

Les agents analysent les commits avec ces préfixes :
- `feat:` - Nouvelle fonctionnalité (MINOR)
- `fix:` - Correction de bug (PATCH)
- `feat!:` ou `BREAKING CHANGE` - Breaking change (MAJOR)
- `docs:` - Documentation
- `chore:` - Maintenance
- `test:` - Tests

### Release Candidates

Format : `X.Y.Z-rc` ou `X.Y.Z-rc1`, `X.Y.Z-rc2`, etc.

```
# Processus RC
1. Créer RC : 1.2.3-rc
2. Tester en staging
3. Si OK → Release : 1.2.3
4. Si KO → Nouvelle RC : 1.2.3-rc2
```

---

## Troubleshooting

### Les agents ne sont pas reconnus

```bash
# Vérifier que les fichiers existent
ls -la ~/.claude/agents/

# Vérifier les permissions
chmod 644 ~/.claude/agents/*.md

# Vérifier le contenu (premières lignes)
head -20 ~/.claude/agents/code-reviewer.md

# Redémarrer Claude Code
# (fermer et rouvrir le terminal)
```

### Erreur "make: command not found"

```bash
# Vérifier que vous êtes dans le bon répertoire
pwd

# Vérifier que le Makefile existe
ls -la Makefile

# Sur macOS, installer les outils de développement
xcode-select --install
```

### Erreur "golangci-lint: command not found"

```bash
# Installer via le Makefile
make install-tools

# OU manuellement
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### Erreur "gh: command not found"

```bash
# Sur macOS avec Homebrew
brew install gh

# Configurer gh
gh auth login
```

### L'agent demande toujours confirmation même pour des actions locales

**C'est normal !** L'agent est configuré pour demander confirmation avant :
- Toute action Git distante (push, tag)
- Publication de packages
- Création de releases

C'est une mesure de sécurité pour éviter les actions accidentelles.

### Le Makefile ajoute "v" mais l'agent dit de ne pas l'utiliser

**C'est correct !** 
- Vous tapez : `1.2.3` (SANS "v")
- Le Makefile crée : tag Git `v1.2.3` (AVEC "v")

Cette séparation est intentionnelle pour garder une convention cohérente dans les discussions tout en respectant les conventions Git des tags avec "v".

### Comment annuler une release en cours ?

**Avant le push du tag :**
```bash
# Annuler le tag local
git tag -d 1.2.3

# Nettoyer les fichiers
make clean
```

**Après le push du tag :**
```bash
# Utiliser make delete-version (ATTENTION : action critique)
make delete-version

# Supprimer la GitHub release
gh release delete 1.2.3

# Créer une nouvelle version corrective immédiatement
# (Exemple : 1.2.4 si 1.2.3 était défectueuse)
```

---

## Commandes rapides

### Code Review

```bash
# Review express (2 min)
make lint && make test

# Review standard (5 min)
make pre-commit && make coverage

# Review complète (10 min)
make quality-check && make benchmark
```

### Release

```bash
# Vérifier l'état
make version-info
git status

# Contrôles qualité
make quality-check

# Build et package
make release
make package

# Release (via l'agent)
> Use release-manager to prepare a release
```

---

## Ressources

### Makefile

Les agents s'appuient sur un Makefile standard avec ces targets essentiels :

```makefile
# Qualité
lint:                  # golangci-lint run
lint-fix:             # go fmt + go mod tidy + golangci-lint --fix
test:                 # go test ./...
test-race:            # go test -race ./...
coverage:             # go test -coverprofile + HTML report
security:             # gosec + govulncheck
pre-commit:           # lint-fix + test-race + lint
quality-check:        # lint + test-race + security

# Build
build:                # Build multi-plateforme
package:              # Créer les ZIP
release:              # quality-check + build

# Version
version-info:         # Afficher version/commit/build
bump-version:         # Tag + push (interactif)
delete-version:       # Supprimer tag (interactif)

# Outils
install-tools:        # Installer golangci-lint, govulncheck, etc.
```

### Outils requis

- **golangci-lint** : Linter Go
- **gosec** : Analyse de sécurité Go
- **govulncheck** : Détection de vulnérabilités
- **gh** : GitHub CLI pour les releases
- **make** : Orchestrateur de build

### Documentation externe

- [Claude Code Documentation](https://docs.claude.com/en/docs/claude-code)
- [golangci-lint](https://golangci-lint.run/)
- [gosec](https://github.com/securecodewarrior/gosec)
- [Semantic Versioning](https://semver.org/)
- [Conventional Commits](https://www.conventionalcommits.org/)

---

## Support

Pour toute question ou problème :

1. Vérifier la section [Troubleshooting](#troubleshooting)
2. Vérifier que les outils sont installés : `make install-tools`
3. Vérifier que le Makefile contient les targets requis
4. Consulter les logs de Claude Code

---

## Maintenance

### Mise à jour des agents

```bash
# Sauvegarder les versions actuelles
cp ~/.claude/agents/code-reviewer.md ~/.claude/agents/code-reviewer.md.backup
cp ~/.claude/agents/release-manager.md ~/.claude/agents/release-manager.md.backup

# Copier les nouvelles versions
cp code-reviewer.md ~/.claude/agents/
cp release-manager.md ~/.claude/agents/

# Vérifier
ls -la ~/.claude/agents/
```

### Personnalisation

Les agents peuvent être personnalisés en éditant directement les fichiers `.md`.

**Exemples de personnalisation :**
- Ajuster les seuils de couverture de tests
- Modifier le format des rapports
- Ajouter des vérifications spécifiques à votre projet
- Changer les messages de confirmation

---

## Changelog

### Version 1.0.0 (2025-10-13)

**Ajouté :**
- Agent code-reviewer pour Go
- Agent release-manager pour Go
- Support complet du Makefile comme orchestrateur
- Confirmations obligatoires avant actions distantes
- Gestion des versions SANS préfixe "v"
- Support des Release Candidates
- Gestion des hotfix

---

**Document généré le : 2025-10-13**
**Version : 1.0.0**
