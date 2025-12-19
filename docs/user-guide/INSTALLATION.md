# SenHub Agent - Guide d'Installation

Ce guide vous accompagne dans l'installation de SenHub Agent sur votre infrastructure. Que vous gériez des serveurs Windows, Linux ou macOS, vous trouverez ici toutes les étapes pour installer l'agent en quelques minutes et commencer à collecter vos métriques.

## Table des Matières

- [Vue d'Ensemble](#vue-densemble)
- [Prérequis Système](#prérequis-système)
- [Téléchargement](#téléchargement)
- [Installation Windows](#installation-windows)
- [Installation Linux](#installation-linux)
- [Installation macOS](#installation-macos)
- [Premiers Pas](#premiers-pas)
- [Désinstallation](#désinstallation)

---

## Vue d'Ensemble

SenHub Agent est un agent de monitoring léger et polyvalent qui collecte des métriques système et infrastructure. L'installation se fait en **mode offline** par défaut, ce qui signifie que l'agent fonctionne de manière autonome sans connexion externe requise.

### Qu'est-ce que le Mode Offline ?

En mode offline, l'agent :
- Fonctionne de manière **autonome** sur votre serveur
- Expose une **interface web locale** pour consulter les métriques
- Se configure via un **fichier YAML local**
- N'envoie **aucune donnée externe**
- Est parfait pour les environnements air-gap, edge computing, ou développement

```mermaid
graph LR
    A[SenHub Agent] -->|Collecte| B[Métriques<br/>CPU, Memory, etc.]
    B -->|Stockage| C[Cache Local]
    C -->|Exposition| D[Interface Web<br/>:8080/:8443]
    D -->|Accès| E[Navigateur<br/>PRTG/Nagios]

    style A fill:#81d4fa
    style C fill:#fff9c4
    style D fill:#c8e6c9
```

> **💡 Note** : Un **mode online** existe également (réservé pour connexion à la plateforme SenHub centralisée), mais ce guide se concentre sur l'installation offline, le mode le plus courant.

---

## Prérequis Système

Avant de commencer, assurez-vous que votre système répond aux exigences minimales suivantes.

### Systèmes d'Exploitation Supportés

L'agent SenHub fonctionne sur toutes les plateformes modernes :

| Plateforme | Versions Supportées | Architecture |
|------------|---------------------|--------------|
| **Windows** | Windows Server 2012+ / Windows 10+ | x64 |
| **Linux** | Ubuntu 18.04+, RHEL 7+, CentOS 7+, Debian 10+ | x64, ARM64 |
| **macOS** | macOS 10.13+ (High Sierra et supérieurs) | x64, ARM64 (M1/M2) |

### Ressources Requises

Les besoins sont très modestes :

| Ressource | Minimum | Recommandé |
|-----------|---------|------------|
| **CPU** | 1 core | 2 cores |
| **RAM** | 256 MB | 512 MB |
| **Disque** | 100 MB | 500 MB |

> **Note** : La consommation réelle varie selon le nombre de probes actives et leur fréquence de collecte. En pratique, l'agent utilise généralement moins de 100 MB de RAM en configuration standard.

### Ports Réseau

L'agent expose une interface HTTP/HTTPS locale pour accéder aux métriques :

| Port | Protocole | Usage | Requis |
|------|-----------|-------|--------|
| **8080** | HTTP | Interface web, API (défaut) | ✅ Mode HTTP |
| **8443** | HTTPS | Interface web, API sécurisée | ✅ Mode HTTPS |
| **443** | HTTPS | Plateforme SenHub (si mode online) | ❌ Mode offline |
| **514** | UDP/TCP | Réception syslog (si probe syslog activée) | ⚠️ Optionnel |

**Flux sortants pour le mode online** (si utilisé) :
- `eu-west-1.intake.senhub.io:443` (HTTPS) - Communication avec la plateforme SenHub

> **💡 Astuce** : Pour une utilisation en environnement air-gap complet, seul le port 8080 ou 8443 est nécessaire (accessible uniquement depuis votre réseau local).

### Permissions Nécessaires

L'installation du service système requiert des droits administrateur :

- **Windows** : Administrateur
- **Linux** : root ou sudo
- **macOS** : root ou sudo

> **💡 Alternative** : L'agent peut aussi être lancé manuellement sans privilèges élevés si vous n'avez pas besoin du service système (utile pour les tests en mode console).

---

## Téléchargement

Les binaires SenHub Agent sont disponibles sur le serveur de releases officiel.

### URL de Téléchargement

```
https://eu-west-1.intake.senhub.io/releases
```

Sélectionnez la version et l'architecture correspondant à votre système :

**Windows** :
- `senhub-agent_windows_amd64.exe` (x64)

**Linux** :
- `senhub-agent_linux_amd64` (x64)
- `senhub-agent_linux_arm64` (ARM64 - Raspberry Pi, etc.)

**macOS** :
- `senhub-agent_darwin_amd64` (Intel x64)
- `senhub-agent_darwin_arm64` (Apple Silicon M1/M2/M3)

> **📸 SCREENSHOT À INSÉRER** : Page de releases montrant la liste des versions et binaires disponibles

### Vérification de l'Intégrité (Optionnel)

Pour vérifier que le binaire n'a pas été altéré :

```bash
# Télécharger le checksum
curl -O https://eu-west-1.intake.senhub.io/releases/{version}/checksums.txt

# Vérifier
sha256sum -c checksums.txt
```

---

## Installation Windows

Cette section vous guide dans l'installation de SenHub Agent sur Windows Server ou Windows 10/11. L'installation crée un service Windows qui démarre automatiquement avec le système.

### Étape 1 : Télécharger le Binaire

Téléchargez le binaire Windows depuis :
```
https://eu-west-1.intake.senhub.io/releases/senhub-agent_windows_amd64.exe
```

### Étape 2 : Préparer l'Environnement

Créez un dossier pour l'agent et déplacez le binaire :

```powershell
# Ouvrir PowerShell en Administrateur
New-Item -ItemType Directory -Force -Path "C:\Program Files\SenHub"
cd "C:\Program Files\SenHub"

# Déplacer le binaire téléchargé
Move-Item "C:\Users\YOUR_USER\Downloads\senhub-agent_windows_amd64.exe" .
```

### Étape 3 : Choisir le Mode d'Installation

Vous avez le choix entre deux modes d'installation selon vos besoins de sécurité.

#### Option A : Installation HTTP (Développement/Test)

**Quand l'utiliser** : Environnement de développement, accès localhost uniquement.

```powershell
.\senhub-agent_windows_amd64.exe install --offline
```

**Ce qui est configuré** :
- Port : `8080`
- Bind : `127.0.0.1` (localhost uniquement)
- Protocole : HTTP (non chiffré)
- Accès : `http://localhost:8080/web/{key}/dashboard`

> **🔑 Note Importante** : Notez bien la clé agent (UUID) affichée lors de l'installation, vous en aurez besoin pour accéder à l'interface web.

**📸 SCREENSHOT À INSÉRER** : PowerShell montrant la sortie de l'installation avec la clé agent mise en évidence

#### Option B : Installation HTTPS (Production Recommandée)

**Quand l'utiliser** : Environnement de production, accès depuis d'autres machines du réseau.

```powershell
.\senhub-agent_windows_amd64.exe install --offline --enable-https
```

**Ce qui est configuré** :
- Port : `8443`
- Bind : `0.0.0.0` (accessible depuis le réseau)
- Protocole : HTTPS (chiffré TLS 1.2+)
- Certificats : Auto-générés (self-signed)
- Accès : `https://monitoring.company.local:8443/web/{key}/dashboard`

**Certificats générés** :
```
C:\Program Files\SenHub\certs\
├── agent-cert.pem  (Certificat SSL)
└── agent-key.pem   (Clé privée)
```

L'installation génère automatiquement un certificat SSL avec SANs pour `localhost` et `127.0.0.1`. Pour ajouter d'autres noms d'hôtes :

```powershell
.\senhub-agent_windows_amd64.exe install --offline --enable-https `
  --https-hosts "monitoring.company.local,192.168.1.100"
```

**📸 SCREENSHOT À INSÉRER** : Explorateur Windows montrant le dossier `C:\Program Files\SenHub\certs\` avec les fichiers de certificats

### Étape 4 : Démarrer le Service

Une fois installé, démarrez le service :

```powershell
.\senhub-agent_windows_amd64.exe start
```

Vérifiez qu'il fonctionne :

```powershell
.\senhub-agent_windows_amd64.exe status
```

Ou via la console de services Windows :

```powershell
Get-Service "SenHub Agent"
```

**📸 SCREENSHOT À INSÉRER** : Services Windows montrant "SenHub Agent" avec statut "Running"

### Étape 5 : Configuration du Firewall

Si vous utilisez HTTPS et souhaitez accéder à l'agent depuis d'autres machines, ouvrez le port dans le firewall :

```powershell
# Autoriser le port 8443 (HTTPS)
New-NetFirewallRule -DisplayName "SenHub Agent HTTPS" `
  -Direction Inbound -Protocol TCP -LocalPort 8443 -Action Allow

# Ou pour HTTP (port 8080)
New-NetFirewallRule -DisplayName "SenHub Agent HTTP" `
  -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
```

### Fichiers et Dossiers Windows

Après installation, l'agent crée cette structure :

```
C:\Program Files\SenHub\
├── senhub-agent_windows_amd64.exe    # Binaire principal
├── agent-config.yaml                 # Configuration (généré à l'installation)
└── certs\                            # Certificats SSL (si HTTPS)
    ├── agent-cert.pem
    └── agent-key.pem

C:\ProgramData\SenHub\Logs\
└── agent.log                         # Logs de l'agent
```

---

## Installation Linux

L'installation sur Linux est simple et se fait via le binaire autonome. Cette section couvre Ubuntu, Debian, RHEL, CentOS et autres distributions modernes.

### Étape 1 : Télécharger le Binaire

Téléchargez le binaire correspondant à votre architecture :

```bash
# Pour x64 (la majorité des serveurs)
wget https://eu-west-1.intake.senhub.io/releases/senhub-agent_linux_amd64

# Pour ARM64 (Raspberry Pi, serveurs ARM)
wget https://eu-west-1.intake.senhub.io/releases/senhub-agent_linux_arm64
```

### Étape 2 : Installer le Binaire

Rendez-le exécutable et déplacez-le vers `/usr/local/bin` :

```bash
chmod +x senhub-agent_linux_amd64
sudo mv senhub-agent_linux_amd64 /usr/local/bin/senhub-agent
```

Vérifiez l'installation :

```bash
senhub-agent version
```

Vous devriez voir la version de l'agent s'afficher.

**📸 SCREENSHOT À INSÉRER** : Terminal montrant la sortie de `senhub-agent version`

### Étape 3 : Choisir le Mode d'Installation

Comme pour Windows, vous pouvez choisir entre HTTP (développement) et HTTPS (production).

#### Option A : Installation HTTP (Développement)

```bash
sudo senhub-agent install --offline
```

L'agent sera accessible sur `http://localhost:8080`

#### Option B : Installation HTTPS (Production Recommandée)

```bash
sudo senhub-agent install --offline --enable-https
```

L'agent sera accessible sur `https://localhost:8443` (ou via l'IP du serveur depuis le réseau).

**Pour spécifier des noms d'hôtes personnalisés** :

```bash
sudo senhub-agent install --offline --enable-https \
  --https-hosts "monitoring.company.local,192.168.1.100"
```

Cela génère un certificat SSL avec les SANs appropriés pour éviter les avertissements de sécurité du navigateur.

### Étape 4 : Démarrer le Service

L'installation crée automatiquement un service systemd. Activez-le et démarrez-le :

```bash
sudo systemctl enable senhub-agent
sudo systemctl start senhub-agent
```

Vérifiez le statut :

```bash
sudo systemctl status senhub-agent
```

Vous devriez voir :
```
● senhub-agent.service - SenHub Agent
   Loaded: loaded (/etc/systemd/system/senhub-agent.service; enabled)
   Active: active (running) since ...
```

**📸 SCREENSHOT À INSÉRER** : Terminal avec sortie de `systemctl status senhub-agent` montrant "active (running)" en vert

### Étape 5 : Configuration du Firewall

Ouvrez le port nécessaire dans votre firewall.

**UFW (Ubuntu/Debian)** :

```bash
sudo ufw allow 8443/tcp comment 'SenHub Agent HTTPS'
sudo ufw reload
```

**firewalld (RHEL/CentOS/Rocky Linux)** :

```bash
sudo firewall-cmd --permanent --add-port=8443/tcp
sudo firewall-cmd --reload
```

### Configuration du Service Systemd

Le fichier de service créé automatiquement se trouve dans `/etc/systemd/system/senhub-agent.service` :

```ini
[Unit]
Description=SenHub Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/senhub-agent run --offline
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

Ce service démarre automatiquement au boot et se relance en cas d'erreur.

### Fichiers et Dossiers Linux

```
/usr/local/bin/
└── senhub-agent                      # Binaire principal

/etc/senhub-agent/
└── agent-config.yaml                 # Configuration

/var/lib/senhub-agent/
└── certs/                            # Certificats SSL (si HTTPS)
    ├── agent-cert.pem
    └── agent-key.pem

/var/log/senhub-agent/
└── agent.log                         # Logs
```

---

## Installation macOS

L'installation sur macOS fonctionne de manière similaire à Linux, avec la création d'un LaunchDaemon pour gérer le service.

### Étape 1 : Télécharger le Binaire

Téléchargez le binaire correspondant à votre Mac :

```bash
# Pour Mac Intel (x64)
curl -LO https://eu-west-1.intake.senhub.io/releases/senhub-agent_darwin_amd64

# Pour Mac Apple Silicon (M1/M2/M3)
curl -LO https://eu-west-1.intake.senhub.io/releases/senhub-agent_darwin_arm64
```

### Étape 2 : Installer le Binaire

```bash
# Rendre exécutable
chmod +x senhub-agent_darwin_amd64  # ou arm64

# Déplacer vers /usr/local/bin
sudo mv senhub-agent_darwin_amd64 /usr/local/bin/senhub-agent
```

### Étape 3 : Autoriser l'Exécution (Sécurité macOS)

macOS bloque par défaut les binaires téléchargés depuis Internet. Autorisez l'exécution :

```bash
sudo xattr -d com.apple.quarantine /usr/local/bin/senhub-agent
```

**Alternative** : Si une popup de sécurité apparaît au lancement, allez dans **Préférences Système → Sécurité et confidentialité** et cliquez sur "Ouvrir quand même".

**📸 SCREENSHOT À INSÉRER** : Dialogue macOS "L'application ne peut pas être ouverte car elle provient d'un développeur non identifié"

### Étape 4 : Installer le Service

```bash
# Installation HTTPS (recommandé)
sudo senhub-agent install --offline --enable-https
```

Cela crée un LaunchDaemon dans `/Library/LaunchDaemons/io.senhub.agent.plist` qui démarre l'agent automatiquement au boot.

### Étape 5 : Démarrer le Service

```bash
# Charger le LaunchDaemon
sudo launchctl load /Library/LaunchDaemons/io.senhub.agent.plist

# Vérifier qu'il tourne
sudo launchctl list | grep senhub
```

Vous devriez voir une ligne avec `io.senhub.agent`.

**📸 SCREENSHOT À INSÉRER** : Terminal macOS avec sortie de `launchctl list | grep senhub`

### Configuration du LaunchDaemon

Le fichier plist créé automatiquement :

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>io.senhub.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/senhub-agent</string>
        <string>run</string>
        <string>--offline</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

### Fichiers et Dossiers macOS

```
/usr/local/bin/
└── senhub-agent                      # Binaire principal

/usr/local/etc/senhub-agent/
└── agent-config.yaml                 # Configuration

/usr/local/var/senhub-agent/
└── certs/                            # Certificats SSL (si HTTPS)
    ├── agent-cert.pem
    └── agent-key.pem

/Library/Logs/SenHub/
└── agent.log                         # Logs
```

---

## Premiers Pas

Félicitations ! Votre agent SenHub est maintenant installé et en cours d'exécution. Voici comment vérifier que tout fonctionne correctement et accéder à vos premières métriques.

### 1. Récupérer la Clé Agent

La clé agent (authentication key) est un UUID généré automatiquement lors de l'installation. Vous en avez besoin pour accéder à l'interface web et aux API.

**Méthode 1 : Consulter la configuration**

```bash
# Linux
cat /etc/senhub-agent/agent-config.yaml | grep "authentication_key:"

# macOS
cat /usr/local/etc/senhub-agent/agent-config.yaml | grep "authentication_key:"

# Windows
type "C:\Program Files\SenHub\agent-config.yaml" | findstr "authentication_key:"
```

**Méthode 2 : Consulter les logs d'installation**

La clé est affichée lors de l'installation dans les logs.

### 2. Vérifier le Service

Assurez-vous que le service tourne correctement.

**Windows** :
```powershell
Get-Service "SenHub Agent"
# Devrait afficher : Status = Running
```

**Linux** :
```bash
sudo systemctl status senhub-agent
# Devrait afficher : Active: active (running)
```

**macOS** :
```bash
sudo launchctl list | grep senhub
# Devrait retourner une ligne avec le PID
```

### 3. Consulter les Logs

Les logs confirment que l'agent collecte bien les métriques.

**Consulter les 20 dernières lignes** :

```bash
# Linux
sudo tail -20 /var/log/senhub-agent/agent.log

# macOS
sudo tail -20 /Library/Logs/SenHub/agent.log

# Windows
Get-Content "C:\ProgramData\SenHub\Logs\agent.log" -Tail 20
```

**Logs attendus (démarrage réussi)** :

```
2025-12-19T10:00:00Z INF Agent started version=0.1.80 mode=offline module=agent.core
2025-12-19T10:00:00Z INF HTTP server started port=8443 tls=true module=strategy.http
2025-12-19T10:00:01Z INF Probe started probe=cpu interval=30s module=probe.cpu
2025-12-19T10:00:01Z INF Probe started probe=memory interval=30s module=probe.memory
2025-12-19T10:00:01Z INF Probe started probe=logicaldisk interval=60s module=probe.logicaldisk
2025-12-19T10:00:01Z INF Probe started probe=network interval=60s module=probe.network
```

Si vous voyez ces lignes, tout fonctionne correctement !

### 4. Tester l'API REST

Avant d'ouvrir le navigateur, testez rapidement l'API :

```bash
# Remplacez {AGENT_KEY} par votre clé réelle
curl -k https://localhost:8443/api/{AGENT_KEY}/info/system
```

**Réponse attendue** :

```json
{
  "hostname": "PROD-SERVER-01",
  "os": "linux",
  "os_version": "Ubuntu 22.04.3 LTS",
  "agent_version": "0.1.80",
  "uptime_seconds": 135,
  "mode": "offline",
  "cache": {
    "retention_minutes": 10
  }
}
```

**📸 SCREENSHOT À INSÉRER** : Terminal avec la commande curl et la réponse JSON formatée

### 5. Accéder à l'Interface Web

Ouvrez votre navigateur et accédez au dashboard :

**Mode HTTP** :
```
http://localhost:8080/web/{AGENT_KEY}/dashboard
```

**Mode HTTPS** :
```
https://localhost:8443/web/{AGENT_KEY}/dashboard
```

> **💡 Note HTTPS** : Si vous utilisez un certificat auto-signé, votre navigateur affichera un avertissement de sécurité. Cliquez sur "Avancé" puis "Continuer vers le site" (les libellés varient selon le navigateur).

**Ce que vous devriez voir** :
- Vue d'ensemble du système (hostname, OS, uptime)
- Statut de la licence (Free tier par défaut)
- Liste des probes actives (cpu, memory, logicaldisk, network)
- Métriques en temps réel (graphiques, valeurs)

**📸 SCREENSHOT À INSÉRER** : Dashboard complet montrant métriques CPU, Memory, Disk, Network avec graphiques

### 6. Explorer l'API

Le dashboard inclut un **API Explorer** interactif pour tester tous les endpoints disponibles.

**Naviguer vers** : `https://localhost:8443/web/{AGENT_KEY}/api-explorer`

**Essayez ces endpoints** :

| Endpoint | Description | Format |
|----------|-------------|--------|
| `/api/{key}/info/probes` | Liste des probes actives | JSON |
| `/api/{key}/metrics` | Toutes les métriques | JSON |
| `/api/{key}/prtg/metrics/cpu` | Métriques CPU pour PRTG | XML |
| `/api/{key}/license/status` | Statut de la licence | JSON |

**📸 SCREENSHOT À INSÉRER** : API Explorer montrant un appel à `/info/probes` avec réponse JSON

### Checklist de Validation

Vérifiez que tout fonctionne :

- [ ] Service démarré et actif
- [ ] Logs sans erreurs critiques (`ERR` ou `FATAL`)
- [ ] Interface web accessible
- [ ] API répond avec code 200
- [ ] Dashboard affiche des métriques CPU/Memory
- [ ] Probes collectent des données (`/info/probes` retourne des compteurs > 0)

Si tous les points sont cochés, votre installation est réussie ! 🎉

### Prochaines Étapes

Maintenant que l'agent est installé, vous pouvez :

1. **Comprendre les modes** : Lire [OPERATING-MODES.md](./OPERATING-MODES.md) pour les différences online/offline
2. **Configurer l'agent** : Voir [AGENT-CONFIGURATION.md](./AGENT-CONFIGURATION.md) pour personnaliser la configuration
3. **Ajouter des probes** : Consulter [PROBES-CONFIGURATION.md](./PROBES-CONFIGURATION.md) pour monitorer Redfish, Citrix, NetScaler, etc.
4. **Intégrer avec PRTG/Nagios** : Lire [METRICS-USAGE.md](./METRICS-USAGE.md)

---

## Désinstallation

Si vous devez désinstaller l'agent, suivez ces étapes.

### Désinstallation Standard

Cette méthode supprime le service mais conserve la configuration et les logs.

**Windows** :

```powershell
# Arrêter le service
.\senhub-agent.exe stop

# Désinstaller le service
.\senhub-agent.exe uninstall

# Supprimer les fichiers manuellement
Remove-Item -Recurse "C:\Program Files\SenHub"
```

**Linux** :

```bash
# Arrêter le service
sudo systemctl stop senhub-agent

# Désinstaller
sudo senhub-agent uninstall

# Supprimer le binaire
sudo rm /usr/local/bin/senhub-agent
```

**macOS** :

```bash
# Arrêter le service
sudo launchctl unload /Library/LaunchDaemons/io.senhub.agent.plist

# Désinstaller
sudo senhub-agent uninstall

# Supprimer le binaire
sudo rm /usr/local/bin/senhub-agent
```

### Désinstallation Complète (Purge)

Cette méthode supprime **tout** : service, configuration, certificats, logs.

```bash
# Toutes plateformes
sudo senhub-agent uninstall --purge
```

**Fichiers supprimés** :
- Configuration (`agent-config.yaml`)
- Certificats SSL (`certs/`)
- Logs (`agent.log`)
- Cache local

---

## Dépannage Installation

Voici les problèmes courants et leurs solutions.

### Problème : Le service ne démarre pas

**Symptômes** : `systemctl status senhub-agent` montre "failed" ou le service s'arrête immédiatement.

**Solution** :

1. **Consulter les logs détaillés** :

```bash
# Linux
sudo journalctl -u senhub-agent -n 50

# Windows
Get-Content "C:\ProgramData\SenHub\Logs\agent.log" -Tail 50

# macOS
sudo tail -50 /Library/Logs/SenHub/agent.log
```

2. **Erreurs courantes** :

**"Port already in use"** :
```bash
# Identifier quel processus utilise le port
sudo lsof -i :8443  # Linux/macOS
netstat -ano | findstr :8443  # Windows

# Solution : Changer le port
senhub-agent install --offline --enable-https --https-port 9443
```

**"Permission denied"** :
- Vérifiez que vous avez les droits admin/root
- Sur Linux : Vérifiez les permissions du binaire (`chmod +x`)

**"Configuration file not found"** :
- Relancez `senhub-agent install --offline` pour régénérer la config

### Problème : Certificats HTTPS invalides

**Symptômes** : Le navigateur refuse la connexion avec erreur SSL.

**Solution** :

```bash
# Régénérer les certificats
sudo senhub-agent stop
sudo rm -rf ./certs/  # ou /var/lib/senhub-agent/certs/
sudo senhub-agent install --offline --enable-https \
  --https-hosts "monitoring.local,192.168.1.100"
sudo senhub-agent start
```

### Problème : Interface web inaccessible depuis le réseau

**Symptômes** : `curl http://localhost:8080` fonctionne, mais pas depuis une autre machine.

**Solutions** :

1. **Vérifier le bind address** :

```bash
# La configuration doit avoir bind_address: "0.0.0.0"
cat /etc/senhub-agent/agent-config.yaml | grep bind_address
```

Si vous voyez `127.0.0.1`, réinstallez avec HTTPS qui utilise `0.0.0.0` par défaut.

2. **Vérifier le firewall** :

```bash
# Tester si le port est ouvert
sudo netstat -tlnp | grep 8443

# Si le port n'est pas listé, vérifier le firewall
sudo ufw status  # Ubuntu
sudo firewall-cmd --list-ports  # RHEL/CentOS
```

### Support

Si vous rencontrez d'autres problèmes :

- **Documentation complète** : Voir [TROUBLESHOOTING.md](./TROUBLESHOOTING.md)
- **Email** : support@senhub.io
- **GitHub Issues** : https://github.com/senhub-io/senhub-agent/issues

---

**Vous êtes prêt !** L'installation est terminée. Consultez maintenant [AGENT-CONFIGURATION.md](./AGENT-CONFIGURATION.md) pour personnaliser votre configuration et [PROBES-CONFIGURATION.md](./PROBES-CONFIGURATION.md) pour ajouter des probes de monitoring avancées.
