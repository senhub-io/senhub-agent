# Installation Rapide - Commandes à copier-coller

## 🔧 Configuration Git (REQUIS)

**Avant toute chose, configurez votre identité Git :**

```bash
# Configuration globale (recommandé)
git config --global user.name "Matthieu Noirbusson"
git config --global user.email "matthieu.noirbusson@sensorfactory.eu"

# Vérifier
git config --global user.name
git config --global user.email
```

**Pourquoi ?** Les agents vont créer des commits et tags qui seront signés avec ce nom.

---

## ⚡ Installation en une commande

Copiez-collez cette commande dans votre terminal macOS :

```bash
mkdir -p ~/.claude/agents && \
curl -o ~/.claude/agents/code-reviewer.md https://votre-repo/code-reviewer.md && \
curl -o ~/.claude/agents/release-manager.md https://votre-repo/release-manager.md && \
chmod 644 ~/.claude/agents/*.md && \
echo "✅ Installation terminée !" && \
ls -la ~/.claude/agents/
```

**Note :** Remplacez les URLs par vos fichiers locaux ou votre repository.

---

## 📦 Installation manuelle (recommandé)

### Étape 1 : Créer le dossier

```bash
mkdir -p ~/.claude/agents
```

### Étape 2 : Copier les fichiers

```bash
# Si vous avez les fichiers localement
cp /path/to/code-reviewer.md ~/.claude/agents/
cp /path/to/release-manager.md ~/.claude/agents/
```

### Étape 3 : Vérifier les permissions

```bash
chmod 644 ~/.claude/agents/*.md
```

### Étape 4 : Vérifier l'installation

```bash
ls -la ~/.claude/agents/
```

**Résultat attendu :**
```
total 96
drwxr-xr-x   4 user  staff   128 Oct 13 10:30 .
drwxr-xr-x   3 user  staff    96 Oct 13 10:30 ..
-rw-r--r--   1 user  staff  8245 Oct 13 10:30 code-reviewer.md
-rw-r--r--   1 user  staff 12456 Oct 13 10:30 release-manager.md
```

---

## 🛠️ Installation des outils Go

### Avec le Makefile (recommandé)

```bash
cd /path/to/your/go-project
make install-tools
```

### Installation manuelle

```bash
# golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# govulncheck
go install golang.org/x/vuln/cmd/govulncheck@latest

# GitHub CLI
brew install gh

# Configurer GitHub CLI
gh auth login
```

---

## ✅ Vérification de l'installation

### Vérifier les agents

```bash
# Lister les fichiers
ls -la ~/.claude/agents/

# Voir le début du premier agent
head -20 ~/.claude/agents/code-reviewer.md

# Voir le début du second agent
head -20 ~/.claude/agents/release-manager.md
```

### Vérifier les outils

```bash
# Vérifier golangci-lint
golangci-lint version

# Vérifier govulncheck
govulncheck -version

# Vérifier GitHub CLI
gh --version

# Vérifier que make fonctionne
cd /path/to/your/project
make version-info
```

---

## 🧪 Test rapide

### Test de code-reviewer

```bash
# Dans votre projet Go
cd /path/to/your/project

# Lancer Claude Code et taper :
```
```
Use code-reviewer to check the code quality
```

### Test de release-manager

```bash
# Dans votre projet Go
cd /path/to/your/project

# Lancer Claude Code et taper :
```
```
Use release-manager to show version info
```

---

## 🚨 Troubleshooting

### Les agents ne sont pas reconnus

```bash
# Vérifier les permissions
chmod 644 ~/.claude/agents/*.md

# Vérifier le contenu
cat ~/.claude/agents/code-reviewer.md | head -10

# Redémarrer Claude Code
# (fermer et rouvrir le terminal)
```

### Erreur "command not found"

```bash
# Vérifier que Go est installé
go version

# Vérifier que GOPATH/bin est dans PATH
echo $PATH | grep go

# Ajouter GOPATH/bin au PATH si nécessaire
echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.zshrc
source ~/.zshrc
```

### Makefile non trouvé

```bash
# Vérifier que vous êtes dans le bon répertoire
pwd
ls -la Makefile

# Si le Makefile n'existe pas, créez-le avec les targets requis
# Voir README.md pour la liste des targets
```

---

## 📋 Checklist d'installation

```
[ ] Dossier ~/.claude/agents/ créé
[ ] Fichier code-reviewer.md copié
[ ] Fichier release-manager.md copié
[ ] Permissions 644 appliquées
[ ] golangci-lint installé
[ ] govulncheck installé
[ ] GitHub CLI installé et configuré
[ ] Makefile présent dans le projet
[ ] Test de code-reviewer réussi
[ ] Test de release-manager réussi
```

---

## 🎯 Prochaines étapes

1. ✅ **Installation terminée** - Les agents sont prêts
2. 📖 **Lire la documentation** - Voir README.md
3. 🏗️ **Vérifier le Makefile** - S'assurer que tous les targets existent
4. 🚀 **Utiliser les agents** - Dans vos projets Go avec Claude Code

---

## 📚 Ressources

- **README.md** : Documentation complète
- **code-reviewer.md** : Configuration de l'agent de review
- **release-manager.md** : Configuration de l'agent de release
- **install-agents.sh** : Script d'installation automatique

---

## 💡 Exemples d'utilisation

### Review de code

```
# Review complète
> Use code-reviewer to review my changes

# Review spécifique
> Ask code-reviewer to check for race conditions in handlers/

# Review de sécurité
> Have code-reviewer analyze security in the auth module
```

### Release management

```
# Préparer une release
> Use release-manager to prepare a release

# Créer un hotfix
> Ask release-manager to create a hotfix for version 1.2.3

# Vérifier l'état
> Have release-manager show the current version
```

---

## ⚠️ Conventions importantes

### Versions SANS "v"

❌ **INCORRECT :**
```
v1.2.3
v2.0.0-rc
```

✅ **CORRECT :**
```
1.2.3
2.0.0-rc
```

Le Makefile ajoute automatiquement le "v" pour les tags Git.

### Format des commits

Utilisez les Conventional Commits :
```
feat: nouvelle fonctionnalité
fix: correction de bug
docs: documentation
test: tests
chore: maintenance
```

---

**Installation rapide terminée !**

Pour plus de détails, consultez **README.md**.
