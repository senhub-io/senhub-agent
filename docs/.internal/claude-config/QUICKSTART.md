# Quick Start Guide - Agents Claude Code pour Go

## 🔧 Configuration Git (IMPORTANT - 30 secondes)

```bash
# Configurer votre identité
git config --global user.name "Matthieu Noirbusson"
git config --global user.email "matthieu.noirbusson@sensorfactory.eu"

# Vérifier
git config --global user.name
```

## 🚀 Installation (2 minutes)

```bash
# 1. Créer le dossier
mkdir -p ~/.claude/agents

# 2. Copier les fichiers
cp code-reviewer.md ~/.claude/agents/
cp release-manager.md ~/.claude/agents/

# 3. Permissions
chmod 644 ~/.claude/agents/*.md

# ✅ Vérifier
ls -la ~/.claude/agents/
```

## 🛠️ Outils Go (1 minute)

```bash
cd /path/to/your/project
make install-tools
```

## 🎯 Utilisation

### Code Review
```
> Use code-reviewer to review my changes
> Ask code-reviewer to check for race conditions
```

### Release
```
> Use release-manager to prepare a release
> Ask release-manager to show version info
```

## ⚡ Commandes Makefile essentielles

```bash
make lint              # Linting
make test-race         # Tests + race detector
make security          # Audit sécurité
make pre-commit        # Vérifs rapides
make quality-check     # TOUS les contrôles
make build             # Build multi-plateforme
make package           # Créer ZIP
make release           # quality + build
make bump-version      # Tag + push (interactif)
```

## ⚠️ Convention IMPORTANTE

**Versions SANS "v" :**
- ✅ Correct : `1.2.3`, `2.0.0-rc`
- ❌ Incorrect : `v1.2.3`, `v2.0.0-rc`

Le Makefile ajoute le "v" automatiquement.

## 📋 Workflow Release (5 étapes)

```
1. Qualité      → make quality-check
2. Analyse      → Commits depuis dernière version
3. Build        → make release && make package
4. CHANGELOG    → Édition manuelle
5. Publication  → make bump-version + GitHub Release
```

Chaque étape demande confirmation ! ✋

## 🔧 Troubleshooting

```bash
# Agents non reconnus ?
chmod 644 ~/.claude/agents/*.md
# → Redémarrer Claude Code

# command not found ?
echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.zshrc
source ~/.zshrc

# Makefile non trouvé ?
pwd  # Vérifier que vous êtes dans le bon dossier
```

## 📚 Fichiers fournis

- **code-reviewer.md** - Agent de review de code
- **release-manager.md** - Agent de gestion de releases  
- **README.md** - Documentation complète
- **INSTALL.md** - Guide d'installation détaillé
- **commands.sh** - Toutes les commandes bash

## 🎓 En résumé

1. ✅ Copiez les 2 fichiers .md dans `~/.claude/agents/`
2. ✅ Installez les outils avec `make install-tools`
3. ✅ Utilisez dans Claude Code : `Use code-reviewer...`
4. ✅ Toujours taper les versions SANS "v" : `1.2.3`

**C'est parti ! 🚀**

---

Pour plus de détails → **README.md**
