# Conventions de Tags Redfish pour les Métriques

Ce document définit la correspondance entre les propriétés standard de Redfish et les noms de tags utilisés dans le SenHub Agent. L'objectif est d'adopter une approche cohérente qui permette une requêtage simple basé sur les attributs standard de Redfish.

## Ressources Génériques

Ces attributs sont communs à la plupart des ressources Redfish:

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `Id` | `{resource_type}_id` | Identifiant unique de la ressource (ex: `system_id`) |
| `Name` | `{resource_type}_name` | Nom de la ressource (ex: `system_name`) |
| `SerialNumber` | `serial_number` | Numéro de série |
| `Model` | `model` | Modèle de la ressource |
| `Manufacturer` | `manufacturer` | Fabricant |
| `PartNumber` | `part_number` | Numéro de pièce du fabricant |
| `SKU` | `sku` | Numéro de SKU |
| `AssetTag` | `asset_tag` | Tag d'inventaire défini par l'utilisateur |
| `Status.Health` | - | Utilisé dans la valeur de la métrique de santé |
| `Status.State` | `state` | État de fonctionnement actuel |
| `Status.HealthRollup` | - | Utilisé dans les métriques de santé agrégées |

## System (ComputerSystem)

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `UUID` | `system_uuid` | UUID unique du système |
| `SystemType` | `system_type` | Type de système (Physical, Virtual, etc.) |
| `BiosVersion` | `bios_version` | Version du BIOS |
| `PowerState` | - | Utilisé dans la valeur de la métrique d'alimentation |
| `HostName` | `host` | Nom d'hôte du système |
| `IndicatorLED` | `indicator_led` | État du voyant d'identification |

## Chassis

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `ChassisType` | `chassis_type` | Type physique du châssis (Rack, Blade, Enclosure, etc.) |
| `LocationIndicatorActive` | `location_indicator` | État de l'indicateur d'emplacement physique |
| `HeightMm` | `height_mm` | Hauteur en millimètres |
| `WidthMm` | `width_mm` | Largeur en millimètres |
| `DepthMm` | `depth_mm` | Profondeur en millimètres |
| `WeightKg` | `weight_kg` | Poids en kilogrammes |
| `PowerState` | - | Utilisé dans la valeur de la métrique d'alimentation |

## Manager (BMC, iDRAC, etc.)

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `UUID` | `manager_uuid` | UUID du gestionnaire |
| `ManagerType` | `manager_type` | Type de gestionnaire (BMC, EnclosureManager, ManagementController) |
| `FirmwareVersion` | `firmware_version` | Version du firmware du gestionnaire |
| `PowerState` | - | Utilisé dans la valeur de la métrique d'alimentation |
| `Location` | `location` | Emplacement physique du gestionnaire |
| `ServiceEntryPointUUID` | `service_entry_point` | UUID du point d'entrée du service |

## Storage

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `StorageControllers.Manufacturer` | `controller_manufacturer` | Fabricant du contrôleur |
| `StorageControllers.Model` | `controller_model` | Modèle du contrôleur |
| `StorageControllers.SerialNumber` | `controller_serial` | Numéro de série du contrôleur |
| `StorageControllers.SpeedGbps` | `controller_speed_gbps` | Vitesse du contrôleur en Gbps |
| `StorageControllers.SupportedRAIDTypes` | `supported_raid_types` | Types de RAID supportés |
| `EncryptionMode` | `encryption_mode` | Mode de chiffrement du stockage |

## Drive

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `MediaType` | `media_type` | Type de média (HDD, SSD, SMR, etc.) |
| `Protocol` | `protocol` | Protocole d'interface (SATA, SAS, etc.) |
| `CapacityBytes` | - | Utilisé dans la valeur de la métrique de capacité |
| `BlockSizeBytes` | - | Utilisé dans la valeur de la métrique de taille de bloc |
| `RotationSpeedRPM` | - | Utilisé dans la métrique de vitesse de rotation |
| `Manufacturer` | `drive_manufacturer` | Fabricant du disque |
| `Location.PartLocation.ServiceLabel` | `service_label` | Étiquette de service pour les opérations de maintenance |
| `HotspareType` | `hotspare_type` | Type de disque de secours |
| `EncryptionAbility` | `encryption_ability` | Capacité de chiffrement du disque |
| `EncryptionStatus` | `encryption_status` | État actuel du chiffrement du disque |
| `PredictedMediaLifeLeftPercent` | - | Utilisé dans la métrique de durée de vie restante |

## Volume

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `RAIDType` | `raid_type` | Type de RAID configuré |
| `EncryptionType` | `encryption_type` | Type de chiffrement utilisé |
| `Encrypted` | - | Utilisé dans la métrique de statut de chiffrement |
| `VolumeType` | `volume_type` | Type de volume logique |
| `CapacityBytes` | - | Utilisé dans la valeur de la métrique de capacité |
| `StripSizeBytes` | `stripe_size` | Taille de la bande RAID en octets |
| `OptimumIOSizeBytes` | - | Utilisé dans la métrique de taille d'IO optimale |
| `AccessCapabilities` | `access_capabilities` | Capacités d'accès (Read, Write, etc.) |

## Temperature

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `Name` | `sensor_name` | Nom du capteur |
| `SensorNumber` | `sensor_number` | Numéro du capteur |
| `ReadingCelsius` | - | Utilisé dans la valeur de la métrique de température |
| `UpperThresholdCritical` | - | Utilisé dans la métrique de seuil critique supérieur |
| `LowerThresholdCritical` | - | Utilisé dans la métrique de seuil critique inférieur |
| `PhysicalContext` | `physical_context` | Contexte physique du capteur (CPU, Memory, Storage, etc.) |
| `RelatedItem@odata.count` | `related_items_count` | Nombre d'éléments associés |

## Fan

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `Name` | `fan_name` | Nom du ventilateur |
| `Reading` | - | Utilisé dans la valeur de la métrique de vitesse |
| `ReadingUnits` | `units` | Unités de mesure (RPM, Percent) |
| `PhysicalContext` | `physical_context` | Contexte physique du ventilateur |
| `MinReadingRange` | - | Utilisé dans la métrique de plage minimale |
| `MaxReadingRange` | - | Utilisé dans la métrique de plage maximale |

## Power Supply

| Propriété Redfish | Nom du Tag | Description |
|-------------------|------------|-------------|
| `Name` | `psu_name` | Nom de l'alimentation |
| `PowerOutputWatts` | - | Utilisé dans la métrique de puissance de sortie |
| `LineInputVoltage` | - | Utilisé dans la métrique de tension d'entrée |
| `PowerCapacityWatts` | - | Utilisé dans la métrique de capacité d'alimentation |
| `FirmwareVersion` | `psu_firmware` | Version du firmware de l'alimentation |
| `InputRanges` | `input_ranges` | Plages d'entrée supportées |
| `EfficiencyPercent` | - | Utilisé dans la métrique d'efficacité |

## Directives pour l'Implémentation

1. **Noms Cohérents**: Utiliser le préfixe du type de ressource pour éviter les conflits de noms entre différentes ressources (ex: `system_id` vs `chassis_id`).

2. **Sélectivité**: Inclure uniquement les tags nécessaires à l'identification et au contexte des métriques.

3. **Distinction Métriques/Tags**: Les valeurs qui changent fréquemment devraient être des métriques, pas des tags.

4. **Lisibilité**: Les noms de tags doivent être descriptifs et compréhensibles en dehors du contexte de Redfish.

5. **Normalisation**: Convertir les noms CamelCase en snake_case pour la cohérence.