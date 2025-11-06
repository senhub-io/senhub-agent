# Structure du projet senhub-agent avec Claude Code

## 📂 Structure finale

```
senhub-agent/
├── .github/
│   └── workflows/
├── cmd/
│   └── agent/
├── internal/
│   └── ...
├── dist/                           # Généré (dans .gitignore)
├── docs/
│   ├── claude/                     # 📚 NOUVEAU - Documentation Claude Code
│   │   ├── README.md              # Index de la documentation Claude
│   │   ├── QUICKSTART.md          # Démarrage rapide
│   │   ├── agents-README.md       # Documentation complète
│   │   ├── INSTALL.md             # Guide d'installation
│   │   ├── GIT-CONFIG.md          # Configuration Git/GPG
│   │   ├── SIGNATURES.md          # Explications signatures
│   │   ├── PERSONAL-CONFIG.md     # Config Matthieu Noirbusson
│   │   ├── code-reviewer.md       # Source agent review (référence)
│   │   └── release-manager.md     # Source agent release (référence)
│   └── ...                        # Autres docs du projet
├── scripts/
│   ├── claude/                     # 🛠️ NOUVEAU - Scripts Claude Code
│   │   ├── install-agents.sh      # Installation automatique
│   │   └── commands.sh            # Commandes utiles
│   └── ...                        # Autres scripts du projet
├── .gitignore                      # Mis à jour
├── Makefile                        # Existe déjà
├── README.md                       # Existe déjà (à mettre à jour)
└── go.mod                          # Existe déjà
```

## 🎯 Emplacement des fichiers agents

### Configuration personnelle (macOS)
```
~/.claude/agents/
├── code-reviewer.md               # Agent actif
└── release-manager.md             # Agent actif
```

Ces fichiers sont copiés automatiquement par le script d'installation.

### Documentation projet (Git)
```
senhub-agent/docs/claude/
├── code-reviewer.md               # Source de référence
└── release-manager.md             # Source de référence
```

Ces fichiers sont versionnés dans Git pour référence et partage avec l'équipe.

## ⚡ Installation automatique

### Méthode 1 : Script automatique (recommandé)

```bash
# 1. Aller à la racine du projet
cd /Users/matthieu/Documents/GitHub/senhub-agent

# 2. Rendre le script exécutable
chmod +x setup-senhub-agents.sh

# 3. Exécuter le script
./setup-senhub-agents.sh
```

**Le script va automatiquement :**
- ✅ Créer `docs/claude/` et `scripts/claude/`
- ✅ Copier les agents dans `~/.claude/agents/`
- ✅ Copier la documentation dans `docs/claude/`
- ✅ Copier les scripts dans `scripts/claude/`
- ✅ Créer un README dans `docs/claude/`
- ✅ Mettre à jour `.gitignore`
- ✅ Configurer Git (si nécessaire)

### Méthode 2 : Installation manuelle

```bash
cd /Users/matthieu/Documents/GitHub/senhub-agent

# 1. Créer les dossiers
mkdir -p docs/claude scripts/claude

# 2. Installer les agents
mkdir -p ~/.claude/agents
cp code-reviewer.md ~/.claude/agents/
cp release-manager.md ~/.claude/agents/
chmod 644 ~/.claude/agents/*.md

# 3. Copier la documentation
cp README.md docs/claude/agents-README.md
cp QUICKSTART.md docs/claude/
cp INSTALL.md docs/claude/
cp GIT-CONFIG.md docs/claude/
cp SIGNATURES.md docs/claude/
cp PERSONAL-CONFIG.md docs/claude/
cp code-reviewer.md docs/claude/
cp release-manager.md docs/claude/

# 4. Copier les scripts
cp install-agents.sh scripts/claude/
cp commands.sh scripts/claude/
chmod +x scripts/claude/*.sh

# 5. Configuration Git
git config user.name "Matthieu Noirbusson"
git config user.email "matthieu.noirbusson@sensorfactory.eu"
```

## 📝 Mise à jour du README principal

Ajoutez cette section dans `senhub-agent/README.md` :

```markdown
## 🤖 Claude Code Agents

Ce projet utilise des agents Claude Code pour automatiser le développement :

- **code-reviewer** - Review automatique de code (qualité, sécurité, race conditions)
- **release-manager** - Gestion complète des releases (versioning, build, publication)

### 🚀 Installation

```bash
# Installation automatique
./setup-senhub-agents.sh

# Puis installer les outils Go
make install-tools
```

### 📚 Documentation

- [Démarrage rapide](docs/claude/QUICKSTART.md) - 2 minutes
- [Documentation complète](docs/claude/agents-README.md) - Tout savoir
- [Installation](docs/claude/INSTALL.md) - Guide détaillé

### 🎯 Utilisation

```bash
# Dans Claude Code
> Use code-reviewer to review my changes
> Use release-manager to prepare a release
```

**Toutes les releases sont signées par : Matthieu Noirbusson**
```

## 🔒 Mise à jour du .gitignore

Le script ajoute automatiquement ces lignes :

```gitignore
# Claude Code - fichiers temporaires
coverage.html
coverage.out
CHANGELOG_*.md
```

## ✅ Commit des changements

```bash
cd /Users/matthieu/Documents/GitHub/senhub-agent

# Ajouter les nouveaux fichiers
git add docs/claude/ scripts/claude/ .gitignore

# Option : Mettre à jour le README principal
git add README.md

# Commit
git commit -m "docs: add Claude Code agents documentation

- Add comprehensive documentation for code review and release agents
- Add automated installation script
- Configure signatures for Matthieu Noirbusson
- Add documentation in docs/claude/ and scripts in scripts/claude/

Prepared by: Matthieu Noirbusson"

# Push
git push
```

## 📋 Checklist

```
Installation :
[ ] Télécharger tous les fichiers artifacts
[ ] Placer setup-senhub-agents.sh à la racine de senhub-agent
[ ] Placer tous les autres fichiers dans le même dossier
[ ] Exécuter ./setup-senhub-agents.sh
[ ] Vérifier ~/.claude/agents/ contient les 2 agents

Configuration :
[ ] git config user.name = "Matthieu Noirbusson"
[ ] git config user.email = "matthieu.noirbusson@sensorfactory.eu"
[ ] make install-tools exécuté
[ ] Test : golangci-lint --version
[ ] Test : govulncheck -version

Vérification :
[ ] docs/claude/ existe avec tous les fichiers
[ ] scripts/claude/ existe avec les scripts
[ ] .gitignore mis à jour
[ ] README.md principal mis à jour (optionnel)

Git :
[ ] git add docs/claude/ scripts/claude/ .gitignore
[ ] git commit avec signature
[ ] git push

Test :
[ ] Ouvrir Claude Code dans le projet
[ ] Tester : "Use code-reviewer to check quality"
[ ] Tester : "Use release-manager to show version"
```

## 🎓 Utilisation quotidienne

### Code Review

```bash
# Avant un commit
> Use code-reviewer to review my changes

# Review d'une PR
> Ask code-reviewer to analyze the security of auth.go

# Vérifier les race conditions
> Have code-reviewer check for race conditions in handlers/
```

### Release Management

```bash
# Préparer une release
> Use release-manager to prepare a release

# Créer un hotfix
> Ask release-manager to create a hotfix for 1.2.3

# Voir la version actuelle
> Have release-manager show the current version info
```

## 🔧 Commandes Make essentielles

```bash
# Développement
make run                    # Exécuter l'application
make test                   # Tests standards
make test-race              # Tests + race detector
make lint                   # Linting
make security               # Audit de sécurité

# Qualité
make pre-commit             # Vérifs avant commit
make quality-check          # TOUS les contrôles

# Build
make build                  # Build multi-plateforme
make package                # Créer les ZIP
make release                # quality-check + build

# Release
make version-info           # Version actuelle
make bump-version           # Créer et pusher un tag
```

## 📚 Documentation

| Fichier | Description | Quand le lire ? |
|---------|-------------|-----------------|
| `docs/claude/QUICKSTART.md` | Guide de démarrage rapide | 🟢 Première utilisation |
| `docs/claude/agents-README.md` | Documentation complète | 🟡 Pour comprendre en détail |
| `docs/claude/INSTALL.md` | Guide d'installation | 🟡 En cas de problème |
| `docs/claude/GIT-CONFIG.md` | Configuration Git/GPG | 🟡 Pour la signature GPG |
| `docs/claude/SIGNATURES.md` | Explications signatures | 🟡 Pour comprendre les signatures |
| `docs/claude/PERSONAL-CONFIG.md` | Config personnalisée | 🟢 Configuration rapide |

## 🚨 Troubleshooting

### Les agents ne sont pas reconnus

```bash
# Vérifier l'installation
ls -la ~/.claude/agents/

# Réinstaller si nécessaire
cd /Users/matthieu/Documents/GitHub/senhub-agent
./setup-senhub-agents.sh
```

### Commandes make ne fonctionnent pas

```bash
# Vérifier que vous êtes à la racine
pwd
# Devrait afficher : /Users/matthieu/Documents/GitHub/senhub-agent

# Installer les outils
make install-tools
```

### Git n'utilise pas le bon nom/email

```bash
# Vérifier
git config user.name
git config user.email

# Reconfigurer si nécessaire
git config user.name "Matthieu Noirbusson"
git config user.email "matthieu.noirbusson@sensorfactory.eu"
```

---

**Projet :** senhub-agent  
**Auteur :** Matthieu Noirbusson  
**Email :** matthieu.noirbusson@sensorfactory.eu  
**Date :** 2025-10-13
