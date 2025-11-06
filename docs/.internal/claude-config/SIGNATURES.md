# Signatures - Matthieu Noirbusson

## 📝 Vue d'ensemble

Tous les éléments générés par les agents Claude Code seront signés au nom de **Matthieu Noirbusson** :

### ✅ Ce qui est signé

1. **Commits Git**
   ```
   commit abc123...
   Author: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
   
   chore: release 1.2.3
   
   Prepared by: Matthieu Noirbusson
   ```

2. **Tags Git**
   ```
   tag v1.2.3
   Tagger: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
   
   Release 1.2.3
   [...]
   Release prepared by: Matthieu Noirbusson
   ```

3. **CHANGELOG**
   ```markdown
   ## [1.2.3] - 2025-10-13
   [...]
   ---
   Release prepared by: Matthieu Noirbusson
   ```

4. **Rapports de Code Review**
   ```
   [Rapport de review]
   ---
   Reviewed by: Matthieu Noirbusson
   Date: 2025-10-13
   ```

5. **GitHub Releases**
   - Les releases contiendront le CHANGELOG avec la signature
   - Le tag associé sera signé par Matthieu Noirbusson

## 🔧 Configuration requise

### Étape 1 : Configuration Git de base (OBLIGATOIRE)

```bash
# Configuration globale
git config --global user.name "Matthieu Noirbusson"
git config --global user.email "matthieu.noirbusson@sensorfactory.eu"

# Vérification
git config --global user.name    # → Matthieu Noirbusson
git config --global user.email   # → matthieu.noirbusson@sensorfactory.eu
```

### Étape 2 : Signature GPG (OPTIONNEL mais recommandé)

Pour une signature cryptographique vérifiable :

```bash
# 1. Générer une clé GPG
gpg --full-generate-key
# Choisir : RSA 4096, nom "Matthieu Noirbusson"

# 2. Obtenir l'ID de la clé
gpg --list-secret-keys --keyid-format=long
# Noter l'ID : par exemple ABCD1234EFGH5678

# 3. Configurer Git
git config --global user.signingkey ABCD1234EFGH5678
git config --global commit.gpgsign true
git config --global tag.gpgsign true

# 4. Exporter la clé publique pour GitHub
gpg --armor --export ABCD1234EFGH5678
# Copier et ajouter dans GitHub Settings → GPG keys
```

Voir **GIT-CONFIG.md** pour plus de détails.

## 📋 Vérification

### Test simple

```bash
# 1. Créer un commit de test
cd /path/to/your/project
git commit --allow-empty -m "test: signature

Prepared by: Matthieu Noirbusson"

# 2. Vérifier
git log -1 --format="%an <%ae>%n%B"
# Devrait afficher :
# Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
# test: signature
# 
# Prepared by: Matthieu Noirbusson
```

### Test avec GPG (si configuré)

```bash
git log -1 --show-signature
# Devrait afficher "gpg: Good signature from 'Matthieu Noirbusson'"
```

## 🎯 Exemples concrets

### Exemple 1 : Release 1.2.3

**Commit créé par l'agent :**
```
commit 1a2b3c4d5e6f...
Author: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
Date:   Mon Oct 13 14:30:00 2025 +0200

    chore: release 1.2.3
    
    Prepared by: Matthieu Noirbusson
```

**Tag créé :**
```
tag v1.2.3
Tagger: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
Date:   Mon Oct 13 14:30:00 2025 +0200

## [1.2.3] - 2025-10-13

### Added
- Nouvelle fonctionnalité X

### Fixed
- Correction du bug Y

---
Release prepared by: Matthieu Noirbusson
```

**CHANGELOG_1.2.3.md :**
```markdown
## [1.2.3] - 2025-10-13

### Added
- Nouvelle fonctionnalité X

### Fixed
- Correction du bug Y

---

### Commits inclus :
- feat: ajout fonctionnalité X (abc123)
- fix: correction bug Y (def456)

### Checksums des binaires :
[checksums]

---

**Release prepared by:** Matthieu Noirbusson
```

### Exemple 2 : Code Review

```
📊 Résumé de la review

La review couvre 8 fichiers avec 156 changements.
Qualité globale : bonne
Problèmes critiques détectés : 0

✅ OUTILS AUTOMATIQUES
[...]

🟡 AMÉLIORATIONS RECOMMANDÉES
[...]

📋 ACTIONS REQUISES
[...]

---
Reviewed by: Matthieu Noirbusson
Date: 2025-10-13
```

## ⚠️ Points d'attention

### Emails multiples

Si vous utilisez différents emails pour différents projets :

```bash
# Configuration par projet (dans le dossier du projet)
cd /path/to/project
git config user.email "matthieu.noirbusson@sensorfactory.eu"  # Email principal

# La configuration globale reste pour les autres projets
```

### Signature GPG sur macOS

Si GPG pose problème :

```bash
# Exporter GPG_TTY
export GPG_TTY=$(tty)

# Ajouter dans ~/.zshrc
echo 'export GPG_TTY=$(tty)' >> ~/.zshrc
source ~/.zshrc

# Redémarrer gpg-agent
gpgconf --kill gpg-agent
```

### Modifier des commits passés

**⚠️ Ne faites cela QUE sur des branches non partagées**

```bash
# Modifier l'auteur du dernier commit
git commit --amend --author="Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>" --no-edit

# Modifier les 3 derniers commits
git rebase -i HEAD~3
# Remplacer "pick" par "edit"
# Pour chaque :
git commit --amend --author="Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>" --no-edit
git rebase --continue
```

## 📊 Checklist de configuration

```
Configuration de base :
[ ] git config user.name = "Matthieu Noirbusson"
[ ] git config user.email configuré
[ ] Test commit réussi
[ ] Signature visible dans git log

Configuration GPG (optionnel) :
[ ] Clé GPG générée
[ ] git config user.signingkey configuré
[ ] git config commit.gpgsign = true
[ ] git config tag.gpgsign = true
[ ] Clé publique ajoutée sur GitHub
[ ] Test signature GPG réussi

Agents Claude Code :
[ ] Agents installés dans ~/.claude/agents/
[ ] Configuration Git vérifiée
[ ] Test de signature dans un commit
[ ] Test de création de CHANGELOG
```

## 🔗 Ressources

- **GIT-CONFIG.md** : Configuration détaillée de Git et GPG
- **README.md** : Documentation complète des agents
- **QUICKSTART.md** : Guide de démarrage rapide

---

**Toutes les signatures seront au nom de : Matthieu Noirbusson**

**Date de création : 2025-10-13**
