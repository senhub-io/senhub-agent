# OpenTelemetry Metrics in SenHub Agent

Ce document décrit les conventions utilisées pour les métriques dans SenHub Agent.

## Conventions de Nommage

Les métriques suivent les conventions de nommage OpenTelemetry:

1. **Structure hiérarchique** - Utilisation des points pour séparer les niveaux hiérarchiques
2. **Snake case** - Utilisation du format snake_case pour les mots composés
3. **Sans unités** - Les unités ne sont pas incluses dans les noms de métriques

## Catégories de Métriques

### Métriques Matérielles (`hardware.*`)

Les métriques liées au matériel suivent la convention `hardware.<composant>.<attribut>`.

Exemples:
- `hardware.system.health` - État de santé global du système
- `hardware.temperature` - Température d'un composant
- `hardware.fan.speed` - Vitesse d'un ventilateur
- `hardware.power.usage` - Consommation d'énergie
- `hardware.storage.drive.size` - Taille d'un disque

## Valeurs Énumérées (Lookups)

Pour les états et statuts, nous utilisons des valeurs numériques standardisées:

### États de Santé (`*.health`)

Les états de santé suivent cette convention:
- `0` - OK / Healthy
- `1` - Warning / Degraded
- `2` - Critical / Error
- `3` - Unknown

### États d'Alimentation (`hardware.system.power.state`)

Les états d'alimentation suivent cette convention:
- `0` - Off
- `1` - On
- `2` - Powering On (transitional)
- `3` - Powering Off (transitional)
- `4` - Unknown / Other

## Tags (Attributs)

Les tags suivants sont utilisés pour contextualiser les métriques:

- `host` - Nom d'hôte du système
- `controller` - Identifiant du contrôleur (A, B)
- `chassis_id` - Identifiant du châssis
- `system_id` - Identifiant du système
- `sensor_name` - Nom du capteur
- `manufacturer` - Fabricant du matériel
- `model` - Modèle du matériel

## Exemples de Métriques

1. Température d'un capteur:
```
Nom: hardware.temperature
Valeur: 42.5
Tags: 
  - host: server-01
  - controller: A
  - sensor_name: CPU1_Temp
```

2. État de santé d'un disque:
```
Nom: hardware.storage.drive.health
Valeur: 0 (OK)
Tags:
  - host: server-01
  - controller: A
  - drive_id: Disk1
  - model: ST1000NM0008
```

3. Capacité d'un volume:
```
Nom: hardware.storage.volume.size
Valeur: 1000000000000
Tags:
  - host: server-01
  - controller: A
  - volume_id: vol1
  - raid_type: RAID5
```