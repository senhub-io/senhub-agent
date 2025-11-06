# Installation des Agents Claude Code - senhub-agent
## Guide étape par étape pour Matthieu Noirbusson

---

## 📦 Étape 1 : Télécharger les fichiers

Vous avez téléchargé ces fichiers depuis Claude :

```
Fichiers téléchargés/
├── code-reviewer.md
├── release-manager.md
├── README.md
├── QUICKSTART.md
├── INSTALL.md
├── GIT-CONFIG.md
├── SIGNATURES.md
├── PERSONAL-CONFIG.md
├── install-agents.sh
├── commands.sh
├── setup-senhub-agents.sh         # Le plus important !
└── PROJET-STRUCTURE.md
```

---

## 🚀 Étape 2 : Installation automatique

### Commandes à exécuter

```bash
# 1. Aller dans votre dossier de téléchargements (ajustez le chemin)
cd ~/Downloads/claude-agents/  # ou le dossier où vous avez téléchargé les fichiers

# 2. Copier le script d'installation dans le projet
cp setup-senhub-agents.sh /Users/matthieu/Documents/GitHub/senhub-agent/

# 3. Aller dans le projet
cd /Users/matthieu/Documents/GitHub/senhub-agent

# 4. Copier aussi tous les autres fichiers nécessaires
cp ~/Downloads/claude-agents/*.md .
cp ~/Downloads/claude-agents/*.sh .

# 5. Rendre le script exécutable
chmod +x setup-senhub-agents.sh

# 6. Exécuter le script d'installation
./setup-senhub-agents.sh
```

### Ce que le script va faire :

✅ Créer `docs/claude/` et `scripts/claude/`  
✅ Installer les agents dans `~/.claude/agents/`  
✅ Copier toute la documentation dans `docs/claude/`  
✅ Copier les scripts dans `scripts/claude/`  
✅ Mettre à jour `.gitignore`  
✅ Configurer Git avec votre nom et email  

---

## ✅ Étape 3 : Vérification

```bash
# Vérifier que les agents sont installés
ls -la ~/.claude/agents/
# Devrait afficher :
# - code-reviewer.md
# - release-manager.md

# Vérifier la structure du projet
ls -la docs/claude/
ls -la scripts/claude/

# Vérifier la configuration Git
git config user.name
git config user.email
# Devrait afficher :
# Matthieu Noirbusson
# matthieu.noirbusson@sensorfactory.eu
```

---

## 🛠️ Étape 4 : Installer les outils Go

```bash
# Dans le projet senhub-agent
cd /Users/matthieu/Documents/GitHub/senhub-agent

# Installer tous les outils nécessaires
make install-tools

# Vérifier que tout est installé
golangci-lint --version
govulncheck -version
gh --version
```

Si vous n'avez pas GitHub CLI :

```bash
brew install gh
gh auth login
```

---

## 📝 Étape 5 : Mettre à jour le README principal (optionnel)

Éditez `/Users/matthieu/Documents/GitHub/senhub-agent/README.md` et ajoutez :

```markdown
## 🤖 Claude Code Agents

Ce projet utilise des agents Claude Code pour automatiser :
- ✅ Review de code (qualité, sécurité, race conditions)  
- ✅ Gestion des releases (versioning, build, publication)

### Documentation

- [Démarrage rapide](docs/claude/QUICKSTART.md)
- [Documentation complète](docs/claude/agents-README.md)

### Utilisation

```bash
# Dans Claude Code
> Use code-reviewer to review my changes
> Use release-manager to prepare a release
```

**Toutes les releases sont signées par : Matthieu Noirbusson**
```

---

## 💾 Étape 6 : Commiter les changements

```bash
cd /Users/matthieu/Documents/GitHub/senhub-agent

# Vérifier les changements
git status

# Ajouter les nouveaux fichiers
git add docs/claude/ scripts/claude/ .gitignore

# Si vous avez modifié le README
git add README.md

# Commit avec signature
git commit -m "docs: add Claude Code agents documentation

- Add comprehensive documentation for code review and release agents
- Add automated installation script in scripts/claude/
- Configure signatures for Matthieu Noirbusson
- Add documentation in docs/claude/

Features:
- Automated code review with quality, security, and race detection
- Complete release management with versioning and build automation
- All artifacts signed by Matthieu Noirbusson

Prepared by: Matthieu Noirbusson"

# Push vers GitHub
git push
```

---

## 🧪 Étape 7 : Tester les agents

### Test 1 : Code Review

```bash
# Ouvrir Claude Code dans le terminal
cd /Users/matthieu/Documents/GitHub/senhub-agent

# Dans Claude Code, taper :
```

```
Use code-reviewer to check the current code quality
```

**Résultat attendu :** L'agent va exécuter `make pre-commit` et vous donner un rapport avec signature.

### Test 2 : Release Manager

```
Use release-manager to show the current version info
```

**Résultat attendu :** L'agent va exécuter `make version-info` et afficher la version actuelle.

### Test 3 : Signature Git

```bash
# Créer un commit de test
git commit --allow-empty -m "test: signature verification

Prepared by: Matthieu Noirbusson"

# Vérifier la signature
git log -1 --format="%an <%ae>"
# Devrait afficher : Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>

# Annuler le commit de test
git reset HEAD~1
```

---

## 📋 Résumé de la structure finale

```
senhub-agent/
├── docs/
│   └── claude/                     ✅ Documentation Claude
│       ├── README.md
│       ├── QUICKSTART.md
│       ├── agents-README.md
│       ├── INSTALL.md
│       ├── GIT-CONFIG.md
│       ├── SIGNATURES.md
│       ├── PERSONAL-CONFIG.md
│       ├── code-reviewer.md
│       └── release-manager.md
├── scripts/
│   └── claude/                     ✅ Scripts Claude
│       ├── install-agents.sh
│       └── commands.sh
└── .gitignore                      ✅ Mis à jour

~/.claude/agents/                   ✅ Agents actifs
├── code-reviewer.md
└── release-manager.md
```

---

## 🎯 Utilisation quotidienne

### Avant un commit

```bash
# Review automatique
> Use code-reviewer to review my changes
```

### Avant une release

```bash
# Préparer la release
> Use release-manager to prepare a release

# L'agent va vous guider à travers :
# 1. Contrôles de qualité
# 2. Analyse des commits
# 3. Build et packaging
# 4. Génération du CHANGELOG
# 5. Création du tag et push
# 6. Publication GitHub Release
```

### Commandes Make utiles

```bash
make lint           # Linting rapide
make test-race      # Tests + race conditions
make security       # Audit de sécurité
make pre-commit     # Vérifs avant commit
make quality-check  # TOUS les contrôles
make build          # Build multi-plateforme
make package        # Créer les ZIP
make release        # Qualité + build
make bump-version   # Créer une release (avec agent c'est mieux)
```

---

## 🚨 En cas de problème

### Les agents ne fonctionnent pas

```bash
# Vérifier l'installation
ls -la ~/.claude/agents/

# Réinstaller
cd /Users/matthieu/Documents/GitHub/senhub-agent
./setup-senhub-agents.sh
```

### Git utilise le mauvais nom

```bash
# Vérifier
git config user.name
git config user.email

# Corriger
git config user.name "Matthieu Noirbusson"
git config user.email "matthieu.noirbusson@sensorfactory.eu"
```

### Outils manquants

```bash
cd /Users/matthieu/Documents/GitHub/senhub-agent
make install-tools
```

---

## 📚 Documentation

| Quand | Lire quoi |
|-------|-----------|
| 🟢 **Maintenant** | `docs/claude/QUICKSTART.md` |
| 🟡 **Si problème** | `docs/claude/INSTALL.md` |
| 🟡 **Pour GPG** | `docs/claude/GIT-CONFIG.md` |
| 🔵 **Pour tout savoir** | `docs/claude/agents-README.md` |

---

## ✅ Checklist finale

```
Installation :
[✓] Fichiers téléchargés
[✓] Script setup-senhub-agents.sh exécuté
[✓] Agents dans ~/.claude/agents/
[✓] Documentation dans docs/claude/
[✓] Scripts dans scripts/claude/
[✓] .gitignore mis à jour

Configuration :
[✓] Git configuré (Matthieu Noirbusson)
[✓] Email configuré (matthieu.noirbusson@sensorfactory.eu)
[✓] Outils installés (make install-tools)

Vérification :
[✓] Test code-reviewer réussi
[✓] Test release-manager réussi
[✓] Signature Git vérifiée

Git :
[✓] Changements commitées
[✓] Push vers GitHub

🎉 TERMINÉ !
```

---

## 🎉 C'est prêt !

Vous pouvez maintenant utiliser les agents Claude Code dans tous vos workflows de développement sur senhub-agent.

**Questions ? → Consultez `docs/claude/agents-README.md`**

---

**Installation pour :** Matthieu Noirbusson  
**Email :** matthieu.noirbusson@sensorfactory.eu  
**Projet :** senhub-agent  
**Date :** 2025-10-13
