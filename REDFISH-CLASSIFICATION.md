# Classification des métriques Redfish pour l'interface utilisateur

Ce document décrit le système de classification utilisé pour organiser les métriques Redfish dans l'interface utilisateur de surveillance. Cette approche de classification permet un regroupement logique et cohérent des métriques, quels que soient les fabricants des périphériques.

## Structure de classification

Le système utilise quatre dimensions de classification, chacune implémentée comme un tag sur les métriques:

### 1. Catégorie (tag: `category`)

Classifie les métriques selon leur type fonctionnel:

| Catégorie | Description | Exemples de métriques |
|-----------|-------------|----------------------|
| `health` | État de santé et statut des composants | `hardware.storage.controller.health`, `hardware.storage.drive.failure_predicted` |
| `capacity` | Métriques liées au stockage et à l'utilisation de l'espace | `hardware.storage.pool.capacity.used_percent`, `hardware.storage.volume.capacity.free` |
| `performance` | Métriques liées aux performances d'E/S | `hardware.storage.volume.io.total_ops`, `hardware.storage.volume.io.read.latency` |
| `operations` | Opérations de maintenance en cours | `hardware.storage.drive.has_operations`, `hardware.storage.drive.operation.progress` |
| `events` | Métriques liées aux journaux et événements | `hardware.logs.entries.critical`, `hardware.eventservice.health` |

### 2. Composant (tag: `component`)

Identifie le type de composant matériel ou logique concerné:

| Composant | Description | Exemples de métriques |
|-----------|-------------|----------------------|
| `controller` | Contrôleurs de stockage | `hardware.storage.controller.health` |
| `drive` | Disques physiques | `hardware.storage.drive.health`, `hardware.storage.drive.size` |
| `pool` | Pools de stockage | `hardware.storage.pool.capacity.total` |
| `volume` | Volumes logiques | `hardware.storage.volume.io.reads` |
| `logs` | Journaux système | `hardware.logs.entries.total` |
| `eventservice` | Service d'événements | `hardware.eventservice.subscriptions` |
| `system` | Métriques au niveau système | `hardware.system.health` |
| `thermal` | Composants thermiques | `hardware.thermal.temperature` |
| `power` | Alimentation électrique | `hardware.power.consumption` |
| `network` | Interfaces réseau | `hardware.network.port.status` |

### 3. Section (tag: `section`)

Organise les métriques en groupes principaux pour l'interface utilisateur:

| Section | Description | Composants inclus |
|---------|-------------|------------------|
| `overview` | Vue d'ensemble du système | Métriques critiques de toutes les sections |
| `storage` | Stockage | `controller`, `drive`, `pool`, `volume` |
| `hardware` | Infrastructure physique | `system`, `thermal`, `power`, `network` |
| `events` | Événements et alertes | `logs`, `eventservice` |

### 4. Dashboard (tag: `dashboard`)

Identifie les métriques à inclure dans des tableaux de bord spécifiques:

| Dashboard | Description | Métriques incluses |
|-----------|-------------|------------------|
| `overview` | Métriques critiques pour la vue d'ensemble | Métriques de santé clés, capacité utilisée, entrées de journal critiques |

## Utilisation dans l'interface utilisateur

L'interface utilisateur peut utiliser cette classification pour créer une hiérarchie de navigation intuitive:

1. **Premier niveau**: Sélection de section (`overview`, `storage`, `hardware`, `events`)
2. **Second niveau**: Sélection de composant au sein de la section choisie
3. **Troisième niveau**: Filtrage par catégorie de métrique (santé, capacité, performance)

## Implémentation technique

Cette classification est implémentée via la fonction `AddClassificationTags` qui analyse le nom de chaque métrique et ajoute les tags appropriés. Les requêtes suivantes peuvent être utilisées pour sélectionner des métriques:

```
section=storage,component=drive          # Toutes les métriques des disques
category=health                          # Tous les indicateurs de santé
dashboard=overview                       # Métriques recommandées pour la vue d'ensemble
section=storage,category=performance     # Métriques de performance de stockage
```

## Extensibilité

Cette structure de classification est conçue pour être extensible:

- De nouveaux composants peuvent être ajoutés en suivant les conventions de nommage
- La classification est indépendante des structures de métriques spécifiques aux fabricants
- L'approche par tag permet des regroupements flexibles et personnalisables

## Exemple de regroupement dans l'interface utilisateur

```
|-- Vue d'ensemble
|   |-- Santé du système
|   |-- Événements critiques
|   |-- Utilisation du stockage
|   `-- Performance
|
|-- Stockage
|   |-- Contrôleurs
|   |   |-- Santé
|   |   `-- Informations
|   |-- Disques
|   |   |-- Santé
|   |   |-- Opérations
|   |   `-- Capacité
|   |-- Pools
|   |   |-- Santé
|   |   |-- Capacité
|   |   `-- Performance
|   `-- Volumes
|       |-- Santé
|       |-- Capacité
|       `-- Performance
|
|-- Infrastructure
|   |-- Thermique
|   |   |-- Températures
|   |   `-- Ventilateurs
|   |-- Alimentation
|   |   |-- Santé
|   |   `-- Consommation
|   `-- Réseau
|       |-- Adaptateurs
|       `-- Ports
|
`-- Événements
    |-- Journaux système
    |   |-- Résumé
    |   `-- Tendances
    `-- Service d'événements
```