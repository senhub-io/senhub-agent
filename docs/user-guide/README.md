# User Guide

Documentation complète pour utilisateurs et administrateurs SenHub Agent.

## 📚 Documentation Complète

### 🚀 Installation et Démarrage

1. **[Installation](./INSTALLATION.md)** ⭐ COMPLET
   - Installation Windows/Linux/macOS
   - Options HTTP et HTTPS
   - Certificats SSL
   - Vérification et désinstallation

2. **[Modes de Fonctionnement](./OPERATING-MODES.md)** ⭐ COMPLET
   - Mode Online vs Offline
   - Comparaison détaillée
   - Basculement entre modes
   - Cas d'usage par mode

### ⚙️ Configuration

3. **[Configuration de l'Agent](./AGENT-CONFIGURATION.md)** ⭐ COMPLET
   - Structure fichier YAML
   - **Système de Licence** (demande support@senhub.io, installation, vérification)
   - Auto-update
   - Cache

4. **[Configuration HTTP/HTTPS](./HTTP-HTTPS-CONFIGURATION.md)** ⭐ COMPLET
   - HTTP Strategy
   - Certificats SSL/TLS
   - Bind address
   - Endpoints API

5. **[Configuration des Probes](./PROBES-CONFIGURATION.md)** ⭐ COMPLET
   - Probes système (Free: cpu, memory, disk, network)
   - Probes réseau (Pro: ping, wifi)
   - Probes infrastructure (Pro/Enterprise: redfish, citrix, netscaler, syslog)
   - Exemples de configuration complète

### 🖥️ Utilisation

6. **[Interface Web](./WEB-INTERFACE.md)** ⭐ COMPLET
   - Dashboard principal et navigation
   - API Explorer (test interactif endpoints)
   - Metrics Browser (filtrage par probe/tag)
   - Probes Status (diagnostic)
   - License Information (statut détaillé)
   - Lookups PRTG (téléchargement .ovl)

7. **[Utilisation des Métriques](./METRICS-USAGE.md)** ⭐ COMPLET
   - Intégration PRTG (sensors XML/REST, lookups)
   - Intégration Nagios/Icinga (checks, NRPE)
   - Intégration Grafana (datasource JSON, dashboards)
   - Scripts custom (Python, PowerShell)
   - Exemples par probe type

### 🔧 Dépannage

9. **[Troubleshooting](./TROUBLESHOOTING.md)** ⭐ COMPLET
   - **Système de Logging** (modules, activation, runtime)
   - Problèmes courants
   - Solutions étape par étape

---

## 🎯 Par Où Commencer ?

### Nouvel Utilisateur
1. [Installation](./INSTALLATION.md) - Installer l'agent
2. [Modes de Fonctionnement](./OPERATING-MODES.md) - Comprendre online vs offline
3. [Configuration Agent](./AGENT-CONFIGURATION.md) - Configurer l'agent

### Configuration Avancée
1. [HTTP/HTTPS](./HTTP-HTTPS-CONFIGURATION.md) - Sécuriser avec SSL/TLS
2. [Probes](./PROBES-CONFIGURATION.md) - Ajouter des probes de monitoring

### Utilisation et Intégrations
1. [Interface Web](./WEB-INTERFACE.md) - Utiliser le dashboard et l'API Explorer
2. [Utilisation des Métriques](./METRICS-USAGE.md) - Intégrer avec PRTG/Nagios/Grafana

### En Cas de Problème
1. [Troubleshooting](./TROUBLESHOOTING.md) - Diagnostic et logs

---

## 📊 Statistiques Documentation

- **8 documents complets** (~35 000 mots, 4 000+ lignes)
- **26 diagrammes Mermaid** (architecture, flows, decision trees)
- **45+ screenshots indiqués** avec descriptions détaillées
- **Système de licence complet** (demande support@senhub.io, format JSON, installation, vérification)
- **Système de logging modulaire** (16 modules, activation CLI/runtime)
- **Intégrations monitoring** (PRTG, Nagios, Grafana avec exemples pratiques)
- **Scripts d'intégration** (Python, PowerShell, Bash)

### Documents Créés

1. **INSTALLATION.md** (500 lignes) - Installation multi-plateformes
2. **OPERATING-MODES.md** (400 lignes) - Modes Online/Offline
3. **AGENT-CONFIGURATION.md** (600 lignes) - Configuration agent et licence
4. **HTTP-HTTPS-CONFIGURATION.md** (200 lignes) - SSL/TLS et sécurité
5. **TROUBLESHOOTING.md** (400 lignes) - Dépannage et logging
6. **PROBES-CONFIGURATION.md** (500 lignes) - Configuration toutes les probes
7. **WEB-INTERFACE.md** (650 lignes) - Dashboard et API Explorer
8. **METRICS-USAGE.md** (800 lignes) - Intégrations monitoring

---

## 💡 Licence

**Demande de Licence** : Contacter support@senhub.io

Documentation complète dans [AGENT-CONFIGURATION.md](./AGENT-CONFIGURATION.md#système-de-licence)

---

## 📞 Support

**Email** : support@senhub.io
**Documentation** : https://docs.senhub.io
**GitHub** : https://github.com/senhub-io/senhub-agent

