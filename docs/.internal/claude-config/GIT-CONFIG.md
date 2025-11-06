# Configuration Git pour Matthieu Noirbusson

## 🔧 Configuration requise

Pour que les commits et releases soient correctement signés avec votre nom, configurez Git :

### Option 1 : Configuration globale (recommandé)

```bash
# Configuration globale (pour tous vos projets)
git config --global user.name "Matthieu Noirbusson"
git config --global user.email "matthieu.noirbusson@sensorfactory.eu"
```

### Option 2 : Configuration par projet

```bash
# Dans le dossier de votre projet
cd /path/to/your/project
git config user.name "Matthieu Noirbusson"
git config user.email "matthieu.noirbusson@sensorfactory.eu"
```

## ✅ Vérification

```bash
# Vérifier la configuration globale
git config --global user.name
git config --global user.email

# Vérifier la configuration du projet
git config user.name
git config user.email
```

**Résultat attendu :**
```
Matthieu Noirbusson
matthieu.noirbusson@sensorfactory.eu
```

## 🔐 Signature GPG (optionnel mais recommandé)

Pour signer cryptographiquement vos commits :

### 1. Générer une clé GPG

```bash
# Générer une nouvelle clé
gpg --full-generate-key

# Choisir :
# - Type : RSA and RSA
# - Taille : 4096
# - Expiration : 0 (n'expire jamais) ou durée souhaitée
# - Nom : Matthieu Noirbusson
# - Email : votre email
```

### 2. Lister vos clés

```bash
gpg --list-secret-keys --keyid-format=long
```

**Résultat :**
```
/Users/matthieu/.gnupg/secring.gpg
------------------------------------
sec   4096R/ABCD1234EFGH5678 2025-10-13
uid                          Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
```

Notez l'ID de la clé : `ABCD1234EFGH5678`

### 3. Configurer Git pour utiliser la clé

```bash
# Configurer Git avec votre clé GPG
git config --global user.signingkey ABCD1234EFGH5678

# Signer automatiquement tous les commits
git config --global commit.gpgsign true

# Signer automatiquement tous les tags
git config --global tag.gpgsign true
```

### 4. Exporter la clé publique pour GitHub

```bash
# Exporter la clé publique
gpg --armor --export ABCD1234EFGH5678

# Copier la sortie (de -----BEGIN PGP PUBLIC KEY BLOCK----- à -----END PGP PUBLIC KEY BLOCK-----)
```

Puis l'ajouter dans GitHub :
1. Settings → SSH and GPG keys
2. New GPG key
3. Coller la clé

## 📝 Format des commits

Avec cette configuration, vos commits auront ce format :

```
commit abc123def456...
Author: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
Date:   Mon Oct 13 10:30:00 2025 +0200

    chore: release 1.2.3
    
    Prepared by: Matthieu Noirbusson
```

## 🏷️ Format des tags

```
tag v1.2.3
Tagger: Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>
Date:   Mon Oct 13 10:30:00 2025 +0200

Release 1.2.3

[Contenu du CHANGELOG]

---
Release prepared by: Matthieu Noirbusson
```

## 📋 Vérification complète

```bash
# 1. Vérifier user.name
git config user.name
# → Matthieu Noirbusson

# 2. Vérifier user.email
git config user.email
# → matthieu.noirbusson@sensorfactory.eu

# 3. Vérifier GPG (si configuré)
git config user.signingkey
# → ABCD1234EFGH5678

git config commit.gpgsign
# → true

# 4. Faire un test
git commit --allow-empty -m "test: signature commit"
git log -1 --show-signature
# → Devrait afficher "Matthieu Noirbusson" et éventuellement la signature GPG
```

## 🚨 Troubleshooting

### Erreur "gpg failed to sign the data"

```bash
# Vérifier que GPG fonctionne
echo "test" | gpg --clearsign

# Si erreur, exporter la variable d'environnement
export GPG_TTY=$(tty)

# Ajouter dans votre ~/.zshrc ou ~/.bashrc
echo 'export GPG_TTY=$(tty)' >> ~/.zshrc
source ~/.zshrc
```

### Commits avec le mauvais nom

```bash
# Vérifier la configuration en cours
git config --list | grep user

# Si le nom est incorrect, reconfigurer
git config --global user.name "Matthieu Noirbusson"
```

### Modifier les commits précédents

**ATTENTION : Ne faites cela que sur des branches non partagées**

```bash
# Modifier le dernier commit
git commit --amend --author="Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>"

# Modifier les N derniers commits
git rebase -i HEAD~N
# Remplacer "pick" par "edit" pour chaque commit
# Puis pour chaque :
git commit --amend --author="Matthieu Noirbusson <matthieu.noirbusson@sensorfactory.eu>" --no-edit
git rebase --continue
```

## 📚 Références

- [Git Configuration](https://git-scm.com/book/en/v2/Customizing-Git-Git-Configuration)
- [Signing Commits](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits)
- [GPG Keys](https://docs.github.com/en/authentication/managing-commit-signature-verification/generating-a-new-gpg-key)

---

**Configuration pour : Matthieu Noirbusson**
**Date : 2025-10-13**
