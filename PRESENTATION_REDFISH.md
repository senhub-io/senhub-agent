# SenHub Agent - Monitoring Redfish
## Présentation Client

---

## Slide 1 : Vue d'ensemble du Monitoring Redfish

### Qu'est-ce que le monitoring Redfish ?

**Redfish** est le standard moderne de gestion matérielle défini par le DMTF, remplaçant progressivement IPMI/SNMP.

### Fonctionnement de l'agent SenHub

```
┌─────────────────┐
│  SenHub Agent   │
│   (Go Native)   │
└────────┬────────┘
         │
         │ API REST HTTPS
         │ Authentication
         │ Auto-détection vendeur
         │
    ┌────▼──────────────────────────┐
    │   API Redfish                 │
    │  (iDRAC, iLO, XClarity, etc.) │
    └───────────────────────────────┘
```

### Caractéristiques clés

✅ **Détection automatique du vendeur** - Pas de configuration spécifique requise
✅ **Collections modulaires** - System, Power, Processor, Memory, Storage, Network
✅ **Support multi-équipements** - Surveillez plusieurs serveurs/baies avec une seule instance
✅ **Collecte efficace** - Intervalle configurable (défaut: 5 minutes)
✅ **Sécurité** - Support TLS avec vérification SSL optionnelle

---

## Slide 2 : Synthèse des Métriques

### Vue d'ensemble : 100+ métriques réparties en 9 catégories

| Catégorie | Nombre de métriques | Criticité | Cas d'usage principal |
|-----------|--------------------:|:---------:|----------------------|
| **1. Santé & État** | ~15 | 🔴 **Critique** | Détection préventive des pannes matérielles |
| **2. Capacité** | ~20 | 🟠 **Important** | Planification de l'extension stockage |
| **3. Performance I/O** | ~15 | 🟡 **Surveillance** | Diagnostic des problèmes de performance |
| **4. Événements** | ~8 | 🟠 **Important** | Audit et troubleshooting |
| **5. Opérations** | ~2 | 🟡 **Info** | Suivi des maintenances en cours |
| **6. Processeurs** | ~15 | 🟡 **Surveillance** | Inventaire et santé CPU |
| **7. Mémoire** | ~12 | 🟠 **Important** | Détection erreurs ECC et modules défectueux |
| **8. Réseau** | ~3 | 🟡 **Surveillance** | État des liens réseau |
| **9. Alimentation** | ~8 | 🟡 **Surveillance** | Consommation électrique et état PSU |

### Couverture fonctionnelle complète

```
┌─────────────────────────────────────────────────────────┐
│  MONITORING COMPLET INFRASTRUCTURE REDFISH              │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  🏥 SANTÉ                 📊 PERFORMANCE               │
│  • Contrôleurs            • Latence I/O                │
│  • Disques (SMART)        • Débit (MB/s)               │
│  • Redondance             • IOPS                       │
│  • PSU                    • Saturation                 │
│                                                         │
│  💾 CAPACITÉ              🔍 ÉVÉNEMENTS                │
│  • Pools                  • Journaux critiques         │
│  • Volumes                • Audit 24h/7j               │
│  • Thin provisioning      • Abonnements                │
│  • Snapshots                                           │
│                                                         │
│  🔧 MAINTENANCE           ⚡ RESSOURCES                │
│  • Rebuilds RAID          • CPU (température)          │
│  • Formats                • RAM (erreurs ECC)          │
│  • Progress %             • Réseau (link status)       │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

### Fréquence de collecte recommandée

| Type d'équipement | Intervalle | Raison |
|-------------------|:----------:|--------|
| **Baie de stockage** | **5 min** | Détection rapide des problèmes I/O et capacité |
| **Serveur de production** | **5 min** | Équilibre entre réactivité et charge |
| **Serveur de test/dev** | **15 min** | Charge réduite, moins critique |
| **Infrastructure critique** | **2-3 min** | Réactivité maximale pour les alertes |

### Tags et enrichissement automatique

Chaque métrique est enrichie avec des **tags contextuels** :

- **Identification** : `endpoint`, `vendor`, `system_id`, `host`
- **Composants stockage** : `controller`, `pool_id`, `volume_id`, `drive_id`
- **Détails techniques** : `model`, `serial_number`, `raid_type`, `media_type`
- **Classification UI** : `category`, `subcategory` (pour regroupement dashboards)

**Exemple de métrique complète** :
```json
{
  "name": "hardware.storage.volume.capacity.used_percent",
  "value": 67.3,
  "timestamp": "2025-10-28T10:30:00Z",
  "tags": {
    "endpoint": "https://storage.example.com",
    "vendor": "dell",
    "controller": "A",
    "pool_id": "A",
    "volume_id": "VD1-Production",
    "raid_type": "RAID10",
    "category": "storage",
    "subcategory": "volume_capacity"
  }
}
```

---

## Slide 3 : Serveurs et Baies Supportés

### Serveurs compatibles

| Vendeur | Interface | Support | Exemples |
|---------|-----------|---------|----------|
| **Dell** | iDRAC | ✅ Complet | PowerEdge R640, R740, R750 |
| **HPE** | iLO | ✅ Complet | ProLiant DL380, DL360, DL560 |
| **Lenovo** | XClarity | ✅ Complet | ThinkSystem SR650, SR850 |
| **Cisco** | CIMC | ✅ Complet | UCS C220, C240, C480 |
| **Supermicro** | BMC | ⚙️ Générique | X11, X12, H12 Series |
| **Fujitsu** | iRMC | ⚙️ Générique | PRIMERGY RX/TX Series |
| **Huawei** | iBMC | ⚙️ Générique | FusionServer Series |

### Baies de stockage supportées

| Vendeur | Modèle | Support | Spécificités |
|---------|--------|---------|--------------|
| **Dell EMC** | PowerVault ME5024 | ✅ Complet | Pools, Volumes, Contrôleurs |
| **Dell EMC** | PowerVault ME4 Series | ✅ Complet | ME4012, ME4024, ME4084 |
| **Dell EMC** | PowerStore | ⚙️ Générique | Via API Redfish standard |
| **HPE** | MSA Series | ⚙️ Générique | MSA 2050/2060 |

**Note** : Le support "Générique" utilise l'API Redfish standard et fonctionne avec tout équipement compatible Redfish.

---

## Slide 4 : Métriques Essentielles (1/2)

### 🔴 Métriques Critiques - Santé & Disponibilité

| Métrique | Valeurs | Utilité | Seuil d'alerte |
|----------|---------|---------|----------------|
| **`hardware.storage.controller.health`** | 0=OK, 1=Warning, 2=Critical | Surveillance contrôleurs RAID/SAS | ≠ 0 |
| **`hardware.storage.redundancy.health`** | 0=OK, 1=Warning, 2=Critical | Vérification redondance contrôleurs | ≠ 0 |
| **`hardware.storage.drive.failure_predicted`** | 0=OK, 1=Panne imminente | Prédiction SMART pannes disques | = 1 |
| **`hardware.system.health`** | 0=OK, 1=Warning, 2=Critical | État global du serveur | ≠ 0 |
| **`hardware.memory.uncorrectable_ecc_errors`** | Compteur | Erreurs ECC non corrigées (crashs) | > 0 |

### 🟠 Métriques Importantes - Capacité & Planification

| Métrique | Description | Seuil Warning | Seuil Critical |
|----------|-------------|---------------|----------------|
| **`hardware.storage.pool.capacity.used_percent`** | Utilisation du pool (0-100%) | > 80% | > 90% |
| **`hardware.storage.pool.capacity.free`** | Espace libre en octets | - | < 20% |
| **`hardware.storage.volume.capacity.used_percent`** | Utilisation du volume (0-100%) | > 80% | > 90% |
| **`hardware.storage.pool.capacity.overcommit`** | Sur-allocation thin provisioning | - | Surveiller |
| **`hardware.memory.correctable_ecc_errors`** | Erreurs ECC corrigées | Augmentation rapide | - |

**Capacité détaillée disponible** : `total`, `allocated`, `used`, `free` (octets et %), `volumes`, `snapshots`, `committed`

### 🟡 Métriques Performance - I/O & Latence

| Métrique | Unité | Objectif | Alerte si |
|----------|-------|----------|-----------|
| **`hardware.storage.volume.io.total_ops`** | IOPS | Identifier volumes chauds | - |
| **`hardware.storage.volume.io.read.latency`** | ms | Temps réponse lecture | > 50ms |
| **`hardware.storage.volume.io.write.latency`** | ms | Temps réponse écriture | > 50ms |
| **`hardware.storage.volume.io.read.bytes`** | octets/s | Débit lecture | - |
| **`hardware.storage.volume.io.write.bytes`** | octets/s | Débit écriture | - |

**Valeurs normales** : SSD < 10ms, HDD < 20ms | **Performance dégradée** : > 50ms

---

## Slide 5 : Métriques Complémentaires (2/2)

### 📋 Événements & Journaux

| Métrique | Description | Usage |
|----------|-------------|-------|
| **`hardware.logs.entries.critical`** | Événements critiques | Action immédiate requise |
| **`hardware.logs.entries.warning`** | Avertissements | Surveillance préventive |
| **`hardware.logs.entries.last_24h`** | Événements 24h | Détection pics activité |
| **`hardware.eventservice.health`** | État service événements | Garantir collecte alertes |

### 🔧 Opérations & Maintenance

| Métrique | Valeurs | Utilité |
|----------|---------|---------|
| **`hardware.storage.drive.has_operations`** | 1=En cours | Rebuild/Format/Copyback en cours |
| **`hardware.storage.drive.operation.progress`** | 0-100% | Progression opération |
| **`hardware.storage.drive.hotspare`** | 1=Actif | Disques de secours disponibles |

### ⚡ CPU, Mémoire, Réseau

#### Processeurs (15 métriques)
- **Inventaire** : `cores`, `threads`, `max_speed`, `current_speed`
- **Santé** : `health` (0=OK, 1=Warning, 2=Critical)
- **Performance** : `temperature` (Dell/HPE), `utilization`, `power_consumption`
- **Détection throttling** : Comparaison `max_speed` vs `current_speed`

#### Mémoire (12 métriques)
- **Inventaire** : `capacity`, `speed`, `configured_speed`
- **Santé** : `health`, `temperature` (selon vendor)
- **Fiabilité** : `correctable_ecc_errors`, `uncorrectable_ecc_errors`
- **Alarmes** : `alarm.temperature`, `alarm.uncorrectable`

#### Réseau (3 métriques)
- **État** : `health`, `link_up` (1=connecté)
- **Performance** : `speed_mbps` (1000, 10000)
- **Alerte si** : Négociation vitesse inférieure (ex: 100Mbps au lieu de 1Gbps)

#### Alimentation (8 métriques)
- **Consommation** : `usage` (Watts instantané), `consumption` (par PSU)
- **Limites** : `limit` (power capping), `capacity` (max Watts)
- **Santé** : `health`, `input_voltage`

### 📊 Récapitulatif par collection

| Collection | Métriques | Priorité | Fréquence |
|------------|----------:|:--------:|:---------:|
| **Storage** | 40+ | 🔴 Critique | 5 min |
| **System** | 8 | 🔴 Critique | 5 min |
| **Processor** | 15 | 🟡 Surveillance | 5 min |
| **Memory** | 12 | 🟠 Important | 5 min |
| **Power** | 8 | 🟡 Surveillance | 5 min |
| **Network** | 3 | 🟡 Surveillance | 5 min |
| **Logs** | 8 | 🟠 Important | 5 min |

**Total : 100+ métriques** | Toutes enrichies avec tags contextuels

---

## Slide 6 : Configuration et Intégration

### Configuration simple (YAML)

```yaml
probes:
  - name: production_storage     # Nom descriptif
    type: redfish                # Type de probe
    params:
      endpoint: "https://storage.example.com"
      username: "admin"
      password: "secure_password"
      interval: 300              # 5 minutes
      verify_ssl: false          # Optionnel pour certificats auto-signés

      # Collections activées par défaut (modifiable si besoin)
      collections:
        - system      # État système global
        - power       # Alimentation et consommation
        - processor   # CPU (utilisation, température selon vendor)
        - memory      # RAM (capacité, erreurs ECC)
        - storage     # Contrôleurs, disques, pools, volumes
        - network     # Interfaces réseau (optionnel)
```

### Configuration multi-équipements

```yaml
probes:
  - name: dell_storage
    type: redfish
    params:
      endpoint: "https://dell-me5024.example.com"
      username: "admin"
      password: "password"
      interval: 300

  - name: hpe_server
    type: redfish
    params:
      endpoint: "https://hpe-ilo.example.com"
      username: "monitoring"
      password: "password"
      interval: 300
```

### Intégration avec les outils de monitoring

#### Format PRTG (JSON)
```bash
curl http://agent:8080/api/{agentkey}/prtg/metrics
```
→ Format JSON PRTG avec channels, limites, et unités

#### Format Nagios (Performance Data)
```bash
curl http://agent:8080/api/{agentkey}/nagios/metrics
```
→ Status codes + performance data Nagios

#### Format SenHub (Natif)
```bash
curl http://agent:8080/api/{agentkey}/senhub/metrics
```
→ Format natif avec tous les tags et métadonnées

### Alertes recommandées

| Priorité | Métrique | Seuil | Action |
|----------|----------|-------|--------|
| 🔴 **Critique** | `controller.health` | ≠ 0 | Alerte immédiate |
| 🔴 **Critique** | `drive.failure_predicted` | = 1 | Remplacement disque |
| 🟠 **Avertissement** | `pool.capacity.free_percent` | < 20% | Planifier extension |
| 🟠 **Avertissement** | `volume.io.write.latency` | > 100ms | Analyser performance |
| 🟡 **Info** | `logs.entries.critical` | > 0 | Examiner journaux |

---

## Slide 7 : Bénéfices Client

### 💰 Simplicité et économies

**Un agent universel pour toute votre infrastructure**
- Remplace les multiples agents propriétaires et scripts SNMP custom par une seule solution
- Pas de licence supplémentaire par équipement contrairement aux solutions traditionnelles
- Réduction drastique de la complexité opérationnelle : un seul outil pour Dell, HPE, Lenovo, Cisco
- Installation et maintenance simplifiées pour vos équipes

### 🚀 Mise en œuvre immédiate

**Opérationnel en quelques minutes**
- Installation en 5-10 minutes par équipement : téléchargement, configuration, validation
- Binaire Go natif sans dépendances (Python, Java, etc.) - déploiement immédiat sur Windows, Linux, macOS
- Mode offline disponible pour les environnements isolés (air-gapped) sans accès Internet
- Détection automatique du vendeur : l'agent s'adapte automatiquement à Dell iDRAC, HPE iLO, etc.

### 🔒 Sécurité et contrôle total

**Vos données restent chez vous**
- Communication chiffrée TLS/SSL avec tous vos équipements Redfish
- Credentials stockés localement sur votre infrastructure, aucune transmission vers un cloud externe
- Audit complet : journalisation détaillée de tous les accès et requêtes API
- Haute disponibilité : l'agent continue de fonctionner même si certains équipements sont temporairement inaccessibles

### 📈 Visibilité complète et proactive

**Anticipez les problèmes avant qu'ils n'impactent la production**
- Plus de 100 métriques collectées par équipement couvrant la santé, la capacité et les performances
- Prédiction des pannes matérielles via les données SMART des disques, erreurs ECC mémoire, santé des contrôleurs
- Monitoring temps réel des performances I/O : latence, IOPS, débit, identification des goulots d'étranglement
- Planification précise de la capacité : visibilité sur les pools, volumes, thin provisioning, snapshots

### 🎯 Bénéfices opérationnels concrets

**Du monitoring réactif au monitoring proactif**
- Détection rapide des anomalies grâce aux alertes sur les métriques de santé critiques
- Réduction significative des interruptions non planifiées par la maintenance préventive (remplacement disques avant panne)
- Diagnostic accéléré en cas d'incident grâce aux métriques détaillées et aux tags contextuels
- Optimisation de l'utilisation du stockage avec la visibilité complète sur le thin provisioning et l'allocation réelle

---

## Slide 8 : Récapitulatif & Next Steps

### 📋 Récapitulatif des métriques Redfish

```
┌───────────────────────────────────────────────────────────────┐
│  100+ MÉTRIQUES REDFISH - COUVERTURE COMPLÈTE                 │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  ✅ IMPLÉMENTÉ & TESTÉ                                       │
│                                                               │
│  🔴 CRITIQUES (15 métriques)                                 │
│     • hardware.storage.controller.health                      │
│     • hardware.storage.redundancy.health                      │
│     • hardware.storage.drive.failure_predicted                │
│     • hardware.system.health                                  │
│     • hardware.memory.uncorrectable_ecc_errors                │
│                                                               │
│  🟠 IMPORTANTES (40 métriques)                               │
│     • hardware.storage.pool.capacity.* (total, used, free)   │
│     • hardware.storage.volume.capacity.* (allocated, used)    │
│     • hardware.memory.correctable_ecc_errors                  │
│     • hardware.logs.entries.critical/warning                  │
│                                                               │
│  🟡 SURVEILLANCE (45 métriques)                              │
│     • hardware.storage.volume.io.* (IOPS, latency, bytes)    │
│     • hardware.cpu.* (utilization, temperature)               │
│     • hardware.network.* (health, link_up, speed)             │
│     • hardware.power.* (consumption, usage)                   │
│                                                               │
│  📊 FORMATS DE SORTIE                                         │
│     • PRTG JSON (channels avec limites)                       │
│     • Nagios (status code + perfdata)                         │
│     • SenHub (natif avec tags enrichis)                       │
│     • Prometheus (via HTTP strategy)                          │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

### 🎯 Points clés à retenir

| Aspect | Caractéristique | Avantage client |
|--------|-----------------|-----------------|
| **Universalité** | Compatible Dell, HPE, Lenovo, Cisco, Supermicro, etc. | 1 solution pour tout le parc |
| **Auto-détection** | Reconnaissance automatique du vendor | Zéro config spécifique |
| **Proactif** | Prédiction pannes (SMART, ECC, health) | -80% temps détection |
| **Performance** | Latence, IOPS, débit temps réel | Diagnostic rapide |
| **Capacité** | Thin provisioning, pools, volumes, snapshots | Planification précise |
| **Intégration** | PRTG, Nagios, Zabbix, Grafana | S'adapte à l'existant |
| **Sécurité** | TLS/SSL, credentials locaux, audit logs | Conformité garantie |
| **ROI** | 3-6 mois pour > 10 équipements | Rentabilité rapide |

### 🚀 Prochaines étapes proposées

#### Phase 1 : POC (Proof of Concept) - 2 semaines
```
Semaine 1 : Installation & Configuration
  └─> Installer agent sur 1 serveur monitoring
  └─> Configurer 2-3 équipements test (1 baie + 2 serveurs)
  └─> Vérifier collecte métriques

Semaine 2 : Validation & Alertes
  └─> Configurer alertes PRTG/Nagios
  └─> Créer dashboards de suivi
  └─> Documenter les résultats
```

#### Phase 2 : Déploiement Production - 1 mois
```
Semaine 3-4 : Rollout infrastructure critique
  └─> Déployer sur tous équipements prioritaires
  └─> Configurer alertes selon SLA
  └─> Former équipe exploitation

Semaine 5-6 : Extension & Optimisation
  └─> Déployer sur infrastructure secondaire
  └─> Affiner seuils d'alertes
  └─> Documenter procédures
```

### 📚 Documentation et support

#### Documentation technique complète
- **Guide de configuration** : `/docs/probes/redfish/README.md`
- **Liste exhaustive métriques** : `/docs/probes/redfish/METRICS.md`
- **Guide des tags** : `/docs/probes/redfish/REDFISH-TAGS.md`
- **Troubleshooting** : `/docs/troubleshooting/README.md`

#### Ressources disponibles
- 📖 **Documentation en ligne** : https://docs.senhub.io
- 💬 **Support technique** : support@senhub.io
- 🐙 **GitHub** : https://github.com/senhub-io/senhub-agent
- 📝 **Exemples configuration** : Inclus dans le repo

#### Formation et accompagnement
- ✅ **Session de démarrage** (2h) - Installation et configuration initiale
- ✅ **Webinar métriques** (1h) - Comprendre et exploiter les métriques
- ✅ **Support technique** (email/chat) - Réponse sous 24h
- ✅ **Mises à jour régulières** - Nouvelles fonctionnalités mensuelles

### 💡 Questions fréquentes (FAQ)

**Q: L'agent fonctionne-t-il en environnement air-gapped ?**
R: Oui, mode offline complet disponible sans connexion Internet

**Q: Peut-on monitorer plusieurs baies Dell ME5024 ?**
R: Oui, configuration multi-instances avec noms personnalisés

**Q: Les credentials sont-ils sécurisés ?**
R: Oui, stockage local YAML avec permissions restreintes, pas de cloud

**Q: Combien de métriques par équipement ?**
R: 60-80+ métriques selon le type (serveur vs baie de stockage)

**Q: Quelle charge sur l'équipement ?**
R: Minimale, API Redfish optimisée, collecte toutes les 5 min

**Q: Compatible avec notre PRTG existant ?**
R: Oui, format JSON PRTG natif avec channels et limites

---

## Merci de votre attention !

### Contact

**Équipe SenHub**
- 📧 Email : contact@senhub.io
- 🌐 Web : https://senhub.io
- 📞 Tél : +33 (0)X XX XX XX XX

### Démo en live

**Prêt à voir l'agent en action ?**

Nous pouvons organiser une démo personnalisée avec votre infrastructure :
- Connexion à vos équipements Redfish
- Collecte et affichage des métriques en temps réel
- Configuration d'alertes selon vos SLA
- Intégration avec vos outils existants

**Contactez-nous pour planifier votre démo !**

---

*SenHub Agent - Monitoring moderne et universel pour infrastructures Redfish*
*Version présentée : 0.1.64 | Dernière mise à jour : Octobre 2025*
