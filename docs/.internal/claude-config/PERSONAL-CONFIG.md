# Configuration personnelle - Matthieu Noirbusson

## 👤 Informations d'identité

**Nom complet :** Matthieu Noirbusson  
**Email :** matthieu.noirbusson@sensorfactory.eu  
**Organisation :** SensorFactory

---

## ⚡ Configuration Git rapide

### Commande unique à exécuter

```bash
git config --global user.name "Matthieu Noirbusson" && \
git config --global user.email "matthieu.noirbusson@sensorfactory.eu" && \
echo "✅ Configuration Git terminée !" && \
git config --global user.name && \
git config --global user.email
```

### Vérification

```bash
git config --global --list | grep user
```

**Résultat attendu :**
```
user.name=Matthieu Noirbusson
user.email=matthieu.noirbusson@sensorfactory.eu
```

---

## 📝 Signatures dans les artefacts

### Commits
```
Author: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>

chore: release 1.2.3

Prepared by: Matthieu Noirbusson
```

### Tags
```
Tagger: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>

Release 1.2.3
[...]
Release prepared by: Matthieu Noirbusson
```

### CHANGELOG
```markdown
---
**Release prepared by:** Matthieu Noirbusson
```

### Code Reviews
```markdown
---
Reviewed by: Matthieu Noirbusson
Date: 2025-10-13
```

---

## 🔐 Configuration GPG (optionnel)

### Génération de clé

```bash
# Générer une nouvelle clé GPG
gpg --full-generate-key

# Lors de la création, utiliser :
# - Nom : Matthieu Noirbusson
# - Email : matthieu.noirbusson@sensorfactory.eu
# - Type : RSA 4096
# - Expiration : 2y (2 ans) ou selon préférence
```

### Configuration Git GPG

```bash
# Obtenir l'ID de la clé
gpg --list-secret-keys --keyid-format=long

# Configurer Git (remplacer KEYID par votre ID de clé)
git config --global user.signingkey KEYID
git config --global commit.gpgsign true
git config --global tag.gpgsign true

# Exporter la clé publique pour GitHub
gpg --armor --export KEYID
```

### Ajouter sur GitHub

1. Aller sur https://github.com/settings/keys
2. Cliquer sur "New GPG key"
3. Coller la clé publique exportée
4. Sauvegarder

---

## 📋 Checklist de configuration complète

```
Configuration de base :
[✓] git config user.name = "Matthieu Noirbusson"
[✓] git config user.email = "matthieu.noirbusson@sensorfactory.eu"
[ ] Test commit avec signature
[ ] Vérification dans git log

Configuration GPG (optionnel) :
[ ] Clé GPG générée avec le bon nom/email
[ ] git config user.signingkey configuré
[ ] git config commit.gpgsign = true
[ ] git config tag.gpgsign = true
[ ] Clé publique ajoutée sur GitHub
[ ] Test de signature GPG réussi

Agents Claude Code :
[ ] Agents installés dans ~/.claude/agents/
[ ] Test de code-reviewer
[ ] Test de release-manager
[ ] Vérification des signatures dans les artefacts
```

---

## 🧪 Tests de vérification

### Test 1 : Commit simple

```bash
cd /path/to/your/project
git commit --allow-empty -m "test: signature Matthieu Noirbusson

Prepared by: Matthieu Noirbusson"

git log -1 --format="%an <%ae>"
# Résultat attendu : Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
```

### Test 2 : Tag annoté

```bash
git tag -a test-1.0.0 -m "Test release

Prepared by: Matthieu Noirbusson"

git show test-1.0.0
# Vérifier que Tagger = Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>

# Nettoyer
git tag -d test-1.0.0
```

### Test 3 : Signature GPG (si configurée)

```bash
git commit --allow-empty -m "test: gpg signature"
git log -1 --show-signature
# Devrait afficher : "gpg: Good signature from 'Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>'"
```

---

## 🔧 Commandes utiles

### Voir la configuration actuelle

```bash
# Configuration globale
git config --global --list

# Configuration du projet courant
git config --list

# Seulement user.*
git config --global --list | grep user
```

### Modifier la configuration

```bash
# Changer le nom
git config --global user.name "Matthieu Noirbusson"

# Changer l'email
git config --global user.email "matthieu.noirbusson@sensorfactory.eu"
```

### Corriger un commit existant

```bash
# Modifier l'auteur du dernier commit
git commit --amend --author="Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>" --no-edit

# Vérifier
git log -1 --format="%an <%ae>"
```

---

## 📚 Fichiers de documentation

- **GIT-CONFIG.md** - Configuration détaillée de Git et GPG
- **SIGNATURES.md** - Explications complètes sur les signatures
- **README.md** - Documentation principale des agents
- **QUICKSTART.md** - Démarrage rapide en 2 minutes

---

## 💡 Notes importantes

1. **Email professionnel** : Utiliser systématiquement `matthieu.noirbusson@sensorfactory.eu`
2. **Cohérence** : Toujours le même nom "Matthieu Noirbusson" (pas d'abréviation)
3. **Signature GPG** : Recommandée mais optionnelle pour démarrer
4. **Configuration globale** : Utilisée par défaut pour tous les projets
5. **Configuration locale** : Possible par projet si besoin spécifique

---

## 🚨 Troubleshooting

### Le nom/email n'apparaît pas dans les commits

```bash
# Vérifier la configuration active
git config user.name
git config user.email

# Si vide, configurer à nouveau
git config --global user.name "Matthieu Noirbusson"
git config --global user.email "matthieu.noirbusson@sensorfactory.eu"
```

### Erreur GPG "failed to sign"

```bash
# Ajouter dans ~/.zshrc ou ~/.bashrc
export GPG_TTY=$(tty)

# Recharger
source ~/.zshrc

# Redémarrer gpg-agent
gpgconf --kill gpg-agent
```

### Mauvais auteur dans les commits passés

⚠️ **Ne modifier que les commits NON POUSSÉS**

```bash
# Dernier commit
git commit --amend --author="Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>" --no-edit

# Plusieurs commits
git rebase -i HEAD~5
# Remplacer "pick" par "edit" pour chaque commit à modifier
# Puis :
git commit --amend --author="Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>" --no-edit
git rebase --continue
```

---

**Configuration personnalisée pour :** Matthieu Noirbusson  
**Organisation :** SensorFactory  
**Email :** matthieu.noirbusson@sensorfactory.eu  
**Date de création :** 2025-10-13
