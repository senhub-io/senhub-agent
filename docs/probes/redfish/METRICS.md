# Redfish Metrics Guide

Ce document décrit les métriques collectées via la probe Redfish pour les systèmes compatibles avec l'API Redfish (Dell PowerVault ME5024, Dell iDRAC, HPE iLO, Lenovo XClarity, Cisco UCS, etc.).

## Configuration de la probe

### Configuration simple (serveur unique)
```yaml
# probes.d/10-redfish.yaml — each file under probes.d/ is a YAML array of probes
- name: redfish
  type: redfish
  params:
    endpoint: "https://your-server.example.com"
    username: "admin"
    password: ${secret:redfish.password}   # auto-sealed on install
    interval: 300
```

### Configuration multiple (plusieurs serveurs)
```yaml
# probes.d/10-redfish.yaml
- name: storage_dell
  type: redfish
  params:
    endpoint: "https://dell-server.example.com"
    username: "admin"
    password: ${secret:storage_dell.password}   # auto-sealed on install

- name: storage_hpe
  type: redfish
  params:
    endpoint: "https://hpe-server.example.com"
    username: "admin"
    password: ${secret:storage_hpe.password}   # auto-sealed on install
```

**Note importante** : La probe détecte automatiquement le type de matériel (Dell, HPE, Lenovo, Cisco) via l'API Redfish et adapte la collecte de métriques en conséquence. Aucune configuration spécifique au vendor n'est requise.

## Métriques de santé

### Contrôleurs de stockage
- `hardware.storage.controller.health` - État de santé du contrôleur (0=OK, 1=Warning, 2=Critical, 3=Unknown)
- `hardware.storage.redundancy.health` - État de santé de la redondance des contrôleurs
- `hardware.storage.redundancy.controllers_active` - Nombre de contrôleurs actifs
- `hardware.storage.redundancy.controllers_min` - Nombre minimum de contrôleurs requis
- `hardware.storage.redundancy.controllers_max` - Nombre maximum de contrôleurs supportés

### Disques
- `hardware.storage.drive.health` - État de santé du disque
- `hardware.storage.drive.failure_predicted` - Prédiction de défaillance (1=défaillance prédite)
- `hardware.storage.drive.hotspare` - Statut de disque de secours (1=hotspare actif)

### Pools de stockage
- `hardware.storage.pool.health` - État de santé du pool

### Volumes
- `hardware.storage.volume.health` - État de santé du volume
- `hardware.storage.volume.encrypted` - Statut de chiffrement (1=chiffré)

### Événements et journaux
- `hardware.logs.entries.total` - Nombre total d'entrées de journal
- `hardware.logs.entries.critical` - Nombre d'entrées critiques
- `hardware.logs.entries.warning` - Nombre d'entrées d'avertissement
- `hardware.logs.entries.info` - Nombre d'entrées informatives
- `hardware.logs.entries.last_24h` - Nombre d'événements des dernières 24 heures
- `hardware.logs.entries.last_7d` - Nombre d'événements des 7 derniers jours
- `hardware.eventservice.health` - État de santé du service d'événements
- `hardware.eventservice.subscriptions` - Nombre d'abonnements aux événements

## Métriques de capacité

### Pools de stockage
- `hardware.storage.pool.capacity.total` - Capacité totale du pool (en octets)
- `hardware.storage.pool.capacity.allocated` - Espace alloué dans le pool (en octets)
- `hardware.storage.pool.capacity.allocated_percent` - Pourcentage d'espace alloué
- `hardware.storage.pool.capacity.used` - Espace réellement consommé (en octets)
- `hardware.storage.pool.capacity.used_percent` - Pourcentage d'espace consommé
- `hardware.storage.pool.capacity.free` - Espace libre (en octets)
- `hardware.storage.pool.capacity.free_percent` - Pourcentage d'espace libre
- `hardware.storage.pool.capacity.volumes` - Espace alloué aux volumes (en octets)
- `hardware.storage.pool.capacity.snapshots` - Espace alloué aux snapshots (en octets)
- `hardware.storage.pool.capacity.committed` - Espace total engagé (en octets)
- `hardware.storage.pool.capacity.overcommit` - Espace sur-alloué en thin provisioning (en octets)

**Note importante pour Dell ME**: Pour les systèmes Dell ME (PowerVault ME5024), les pools et volumes peuvent retourner `CapacityBytes=0`. Dans ce cas, l'agent utilise automatiquement `Capacity.Data.AllocatedBytes` comme capacité effective.

### Volumes
- `hardware.storage.volume.capacity.total` - Capacité totale du volume (en octets)
- `hardware.storage.volume.capacity.allocated` - Espace alloué au volume (en octets)
- `hardware.storage.volume.capacity.allocated_percent` - Pourcentage d'espace alloué
- `hardware.storage.volume.capacity.used` - Espace réellement utilisé (en octets)
- `hardware.storage.volume.capacity.used_percent` - Pourcentage d'espace utilisé
- `hardware.storage.volume.capacity.free` - Espace libre (en octets)
- `hardware.storage.volume.capacity.free_percent` - Pourcentage d'espace libre

**Note importante pour Dell ME**: Pour les volumes Dell ME, si `CapacityBytes=0`, l'agent utilise `Capacity.Data.AllocatedBytes` extrait du JSON brut de l'API Redfish. Cette logique garantit des métriques précises même quand l'API ne retourne pas `CapacityBytes` directement.

### Disques
- `hardware.storage.drive.capacity.total` - Capacité totale du disque (en octets)

## Métriques de performance

### Volumes
- `hardware.storage.volume.io.total_ops` - Nombre total d'opérations d'I/O
- `hardware.storage.volume.io.reads` - Nombre d'opérations de lecture
- `hardware.storage.volume.io.writes` - Nombre d'opérations d'écriture
- `hardware.storage.volume.io.total_bytes` - Volume total de données transférées (en octets)
- `hardware.storage.volume.io.read.bytes` - Volume de données lues (en octets)
- `hardware.storage.volume.io.write.bytes` - Volume de données écrites (en octets)
- `hardware.storage.volume.io.read.latency` - Latence des opérations de lecture
- `hardware.storage.volume.io.write.latency` - Latence des opérations d'écriture

### Pools
- `hardware.storage.pool.io.reads` - Nombre d'opérations de lecture
- `hardware.storage.pool.io.writes` - Nombre d'opérations d'écriture
- `hardware.storage.pool.io.read.bytes` - Volume de données lues (en octets)
- `hardware.storage.pool.io.write.bytes` - Volume de données écrites (en octets)

## Métriques d'opérations

### Disques
- `hardware.storage.drive.has_operations` - Indique si des opérations sont en cours (1=oui)
- `hardware.storage.drive.operation.progress` - Progression de l'opération en pourcentage

## Tags disponibles

### Contrôleurs
- `controller_id` - Identifiant du contrôleur
- `controller_name` - Nom du contrôleur
- `controller` - Lettre du contrôleur (A/B)
- `controller_type` - Type de contrôleur (storage)
- `host` - Nom du système hôte
- `manufacturer` - Fabricant du contrôleur
- `model` - Modèle du contrôleur
- `serial_number` - Numéro de série du contrôleur

### Disques
- `drive_id` - Identifiant du disque
- `drive_name` - Nom du disque
- `model` - Modèle du disque
- `drive_manufacturer` - Fabricant du disque
- `serial_number` - Numéro de série du disque
- `media_type` - Type de média (SSD, HDD)
- `protocol` - Protocole de communication (SAS, SATA)
- `hotspare_type` - Type de disque de secours
- `encryption_ability` - Capacité de chiffrement
- `encryption_status` - Statut de chiffrement
- `service_label` - Étiquette de service
- `location_type` - Type d'emplacement
- `location_ordinal` - Valeur d'emplacement ordinal
- `operation_name` - Nom de l'opération en cours

### Pools
- `pool_id` - Identifiant du pool
- `pool_name` - Nom du pool
- `description` - Description du pool
- `supported_raid_types` - Types RAID supportés
- `max_block_size_bytes` - Taille maximale de bloc
- `thin_provisioned` - Indication de thin provisioning

### Volumes
- `volume_id` - Identifiant du volume
- `volume_name` - Nom du volume
- `pool_id` - Identifiant du pool associé
- `raid_type` - Type RAID utilisé
- `write_cache_policy` - Politique de cache d'écriture
- `block_size_bytes` - Taille de bloc
- `access_capabilities` - Capacités d'accès (Read, Write)
- `encryption_type` - Type de chiffrement

### Événements et journaux
- `host` - Nom du système hôte
- `manager_id` - Identifiant du gestionnaire
- `manager_name` - Nom du gestionnaire
- `model` - Modèle du gestionnaire
- `log_service_id` - Identifiant du service de journalisation
- `log_service_name` - Nom du service de journalisation

## Utilisation recommandée

### Alertes essentielles
- Surveiller `hardware.storage.controller.health` pour les défaillances de contrôleur
- Surveiller `hardware.storage.redundancy.health` pour les problèmes de redondance
- Surveiller `hardware.storage.drive.failure_predicted` pour les disques en préfaillance
- Surveiller `hardware.storage.drive.has_operations` pour les opérations de maintenance en cours
- Surveiller `hardware.logs.entries.critical` pour les événements critiques générés par le système

### Capacité
- Surveiller `hardware.storage.pool.capacity.free_percent` pour l'espace disponible
- Surveiller `hardware.storage.volume.capacity.used_percent` pour l'utilisation des volumes

### Performance
- Surveiller `hardware.storage.volume.io.total_ops` pour l'activité générale
- Surveiller `hardware.storage.volume.io.read.latency` et `hardware.storage.volume.io.write.latency` pour les problèmes de performance

### Événements et journaux
- Surveiller `hardware.logs.entries.critical` et `hardware.logs.entries.warning` pour détecter les problèmes système
- Utiliser `hardware.logs.entries.last_24h` pour suivre l'activité récente du système
- Comparer les tendances entre `hardware.logs.entries.last_24h` et `hardware.logs.entries.last_7d` pour identifier les pics d'événements
- Utiliser `hardware.eventservice.health` pour vérifier que le service d'événements fonctionne correctement

## Débogage et résolution de problèmes

### Logs de debug pour Dell ME

L'agent génère automatiquement des logs de debug détaillés pour les systèmes Dell ME afin de tracer les calculs de capacité :

```
Dell ME pool: Using AllocatedBytes as effective capacity pool_id=A capacity_bytes=0 allocated_bytes=15357952425984 effective_capacity=15357952425984
Dell ME volume: Using AllocatedBytes as effective capacity volume_id=VD1-ME5024 capacity_bytes=0 allocated_bytes=5219072606208
```

### Vérification des métriques

Pour vérifier que les métriques sont correctement calculées :

1. **Pools** : Vérifiez que `hardware.storage.pool.capacity.used` correspond au pourcentage affiché sur l'interface du ME5024
2. **Volumes** : Vérifiez que `hardware.storage.volume.capacity.allocated` n'est plus 0.01 TB mais reflète la vraie taille du volume
3. **Cohérence** : Le total des volumes alloués doit correspondre à l'espace utilisé dans le pool

### Problèmes connus et solutions

**Problème** : Métriques `allocated` affichent 0.01 TB au lieu de la vraie capacité
**Solution** : Mise à jour vers la version 0.0.86-beta qui corrige le calcul Dell ME

**Problème** : Logs "zerolog: could not write event: short write"
**Solution** : Corrigé dans la version 0.0.86-beta avec le masquage amélioré des mots de passe

## Extraction des données

Pour extraire les données complètes de l'API Redfish d'un système PowerVault ME5024, utilisez l'outil `redfish-explorer` :

```bash
./redfish-explorer -endpoint https://lb-me5024mgmt1.batistyl.fr -username admin -password password -export me5024_data
```