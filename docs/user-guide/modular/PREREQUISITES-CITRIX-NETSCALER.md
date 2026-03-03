# Prérequis Supervision Citrix et NetScaler

Ce document détaille les prérequis techniques pour superviser un environnement Citrix Virtual Apps and Desktops (CVAD) et NetScaler ADC avec SenHub Agent.

# Prérequis Citrix Virtual Apps and Desktops (CVAD)

## Composants requis

**1. Citrix Director**
- Citrix Director installé et configuré
- API OData activée (par défaut)
- Versions supportées : Citrix Virtual Apps and Desktops 7.x

**2. Connectivité réseau**
- Port 443 (HTTPS)
- Direction : Serveur SenHub Agent vers Citrix Director
- Protocole : HTTPS/TLS 1.2 ou supérieur

**3. Compte de service de supervision**
- Compte de domaine Active Directory

## Configuration du compte de service

### Permissions requises

| Composant | Permission | Description |
|-----------|-----------|-------------|
| **Citrix Studio** | Read Only Administrator | Accès en lecture seule aux ressources Citrix |
| **Citrix Director** | Accès web | Accès à l'interface web Director |
| **API OData** | Lecture | Permissions de lecture sur l'API OData |

### Type de compte

- **Type** : Compte de domaine Active Directory standard
- **Droits Windows** : Aucun droit administrateur requis
- **Format** : `DOMAINE\nom-utilisateur` (avec double backslash `\\` dans les fichiers YAML)
- **Exemple** : `ENTREPRISE\svc-monitoring`

## Procédure de création du compte

### Etape 1 : Création du compte Active Directory

```powershell
# Dans Active Directory Users and Computers ou via PowerShell
New-ADUser -Name "svc-monitoring" `
  -UserPrincipalName "svc-monitoring@domaine.com" `
  -AccountPassword (ConvertTo-SecureString "MotDePasseSecurise123!" -AsPlainText -Force) `
  -Enabled $true `
  -PasswordNeverExpires $true `
  -Description "Compte de service pour supervision Citrix SenHub"
```

### Etape 2 : Attribution des permissions Citrix

1. Ouvrir **Citrix Studio**
2. Naviguer vers **Configuration > Administrators**
3. Cliquer sur **Add Administrator**
4. Sélectionner `DOMAINE\svc-monitoring`
5. Assigner le rôle **"Read Only Administrator"**
6. Cliquer sur **OK**

### Etape 3 : Validation des permissions

1. Ouvrir un navigateur web
2. Se connecter à : `https://director.entreprise.com`
3. Utiliser les identifiants : `DOMAINE\svc-monitoring`
4. Vérifier l'accès au tableau de bord Director

## Informations à collecter

| Information | Exemple | Description |
|------------|---------|-------------|
| **URL Director** | `https://director.entreprise.com` | URL de Citrix Director (sans `/Director`) |
| **Compte de service** | `ENTREPRISE\svc-monitoring` | Compte de domaine au format DOMAINE\utilisateur |
| **Mot de passe** | `MotDePasseSecurise123!` | Mot de passe du compte de service |
| **URL DDC** (optionnel) | `https://citrix-ddc.entreprise.com` | URL du Delivery Controller (multi-site) |
| **Nom du site** (optionnel) | `SITE-PRODUCTION` | Nom du site Citrix (filtrage multi-site) |

# Prérequis NetScaler ADC

## Composants requis

**1. NITRO REST API**
- Activée par défaut sur tous les NetScaler
- Port 443 (HTTPS) ou 80 (HTTP, non recommandé en production)
- Versions supportées : NetScaler 11.x, 12.x, 13.x, ADC 13.x, 14.x

**2. Connectivité réseau**
- Port 443 (HTTPS)
- Direction : Serveur SenHub Agent vers NetScaler NSIP (Management IP)
- Protocole : HTTPS/TLS 1.2 ou supérieur

**3. Compte utilisateur NITRO API**
- Utilisateur local NetScaler (recommandé) ou LDAP/Active Directory

## Configuration du compte utilisateur

### Option 1 : Utilisateur local NetScaler (Recommandé)

**Création via CLI NetScaler :**

```bash
# Connexion SSH au NetScaler
ssh nsroot@netscaler.entreprise.com

# Création de l'utilisateur de supervision
add system user monitoring-user "MotDePasseSecurise123!" -timeout 900

# Attribution des permissions en lecture seule
bind system user monitoring-user read-only

# Vérification
show system user monitoring-user
```

**Création via GUI NetScaler :**

1. Se connecter à l'interface web NetScaler : `https://netscaler.entreprise.com`
2. Naviguer vers **System > User Administration > Users**
3. Cliquer sur **Add**
4. Renseigner : User Name `monitoring-user`, Password, Session Timeout `900`
5. Cliquer sur **Create**
6. Naviguer vers **System > User Administration > Command Policies**
7. Lier l'utilisateur à la policy **read-only** (Priority `100`)
8. Cliquer sur **OK**

### Option 2 : Authentification LDAP/Active Directory

| Paramètre | Valeur requise | Description |
|-----------|---------------|-------------|
| **Permissions** | Read-only access | Command Policy : `read-only` |
| **Session Timeout** | 900 secondes minimum | Les requêtes API peuvent prendre du temps |
| **Compte de domaine** | `DOMAINE\utilisateur` | Format standard AD |

## Permissions requises

L'utilisateur doit avoir la policy **read-only** qui donne accès en lecture seule à :

| Ressource | Permission | Description |
|-----------|-----------|-------------|
| **LB vServers** | Lecture | État, statistiques de charge |
| **Services** | Lecture | État, throughput, transactions |
| **Service Groups** | Lecture | État, membres actifs |
| **Certificats SSL** | Lecture | Nom, expiration |
| **Ressources système** | Lecture | CPU, mémoire, réseau, disque |
| **High Availability** | Lecture | État du cluster, sync status |

## Configuration du timeout de session

**Important** : Le timeout de session doit être configuré à **900 secondes minimum** (15 minutes).

```bash
# Via CLI NetScaler
set system user monitoring-user -timeout 900

# Vérification
show system user monitoring-user | grep -i timeout
```

## Règles de pare-feu

| Source | Destination | Port | Protocole | Description |
|--------|-------------|------|-----------|-------------|
| Serveur SenHub Agent | NetScaler NSIP | 443 | HTTPS | NITRO REST API |

## Certificats SSL

**Production** : Valider les certificats SSL (installer le certificat CA du NetScaler sur le serveur agent ou utiliser un certificat signé par une CA publique).

**Test uniquement** : `insecure_skip_verify: true` (ne jamais utiliser en production).

## Informations à collecter

| Information | Exemple | Description |
|------------|---------|-------------|
| **URL Management** | `https://netscaler.entreprise.com` | URL d'accès au NetScaler (NSIP) |
| **Nom d'utilisateur** | `monitoring-user` | Nom d'utilisateur NITRO API |
| **Mot de passe** | `MotDePasseSecurise123!` | Mot de passe de l'utilisateur |
| **Type de déploiement** | Standalone / HA Cluster | Configuration High Availability ? |
| **Validation SSL** | Oui / Non | Valider les certificats SSL ? |

# Configuration High Availability (HA)

## NetScaler en cluster HA

Si le NetScaler est déployé en mode High Availability :

**Fonctionnement de la supervision :**
- Se connecter à **UN SEUL noeud** (primaire ou secondaire)
- SenHub Agent collecte automatiquement les métriques **des deux noeuds**
- Les noeuds sont identifiés par IP et node ID (0 ou 1)

**Métriques collectées par noeud :**

| Métrique | Description | Valeurs |
|----------|-------------|---------|
| **ha.state** | Rôle du noeud | PRIMARY, SECONDARY, UNKNOWN |
| **ha.node.state** | État opérationnel | UP, DOWN |
| **ha.sync_status** | État de synchronisation config | SUCCESS, FAILED |
| **ha.sync_failures** | Compteur d'échecs de sync | Nombre entier |

**Tags :** `ha_node_id` (0 ou 1), `ha_node_ip`, `is_local_node` (true/false)

# Résumé des comptes à créer

## Citrix CVAD

| Paramètre | Valeur |
|-----------|--------|
| **Type de compte** | Compte de domaine Active Directory |
| **Format** | `DOMAINE\nom-utilisateur` |
| **Permissions Citrix** | Read Only Administrator (Citrix Studio) |
| **Accès réseau** | Port 443 vers Citrix Director |

## NetScaler ADC

| Paramètre | Valeur |
|-----------|--------|
| **Type de compte** | Utilisateur local NetScaler (recommandé) |
| **Permissions** | read-only command policy |
| **Session timeout** | 900 secondes minimum |
| **Accès réseau** | Port 443 vers NetScaler NSIP |

# Tests de validation

## Validation Citrix Director

**Test 1 : Accès web Director**
- Naviguer vers `https://director.entreprise.com`
- Se connecter avec `DOMAINE\svc-monitoring`
- Vérifier l'accès au tableau de bord

**Test 2 : Accès API OData**
```bash
curl -u "DOMAINE\\svc-monitoring:MotDePasseSecurise123!" \
  --ntlm \
  "https://director.entreprise.com/Odata/v4/Data/Machines"
```
Résultat attendu : Réponse JSON avec la liste des machines VDA.

## Validation NetScaler NITRO API

**Test 1 : Connexion NITRO API**
```bash
curl -k -X POST https://netscaler.entreprise.com/nitro/v1/config/login \
  -H "Content-Type: application/json" \
  -d '{"login":{"username":"monitoring-user","password":"MotDePasseSecurise123!"}}'
```
Résultat attendu : `{"errorcode": 0, "message": "Done", "sessionid": "##D0B8D5E2..."}`

**Test 2 : Récupération de statistiques**
```bash
curl -k -X GET https://netscaler.entreprise.com/nitro/v1/stat/system \
  -H "Cookie: sessionid=##D0B8D5E2..."
```
Résultat attendu : Réponse JSON avec statistiques système.

# Checklist de déploiement

## Citrix CVAD

- [ ] Citrix Director installé et accessible
- [ ] Compte de service AD créé
- [ ] Rôle "Read Only Administrator" assigné dans Citrix Studio
- [ ] Connexion web Director validée
- [ ] Connexion API OData validée
- [ ] Connectivité réseau validée (port 443)

## NetScaler ADC

- [ ] Utilisateur NITRO API créé
- [ ] Command policy `read-only` assignée
- [ ] Session timeout configuré (900+ secondes)
- [ ] Connexion NITRO API validée
- [ ] Statistiques NITRO récupérées avec succès
- [ ] Connectivité réseau validée (port 443)
- [ ] Type de déploiement identifié (Standalone / HA)

# Support

- **Email** : support@senhub.io
- **Documentation** : https://docs.senhub.io
