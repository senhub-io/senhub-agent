# Métriques Citrix Avancées avec Filtrage Multi-Axes

## Vue d'ensemble

Le probe Citrix a été amélioré pour fournir des métriques granulaires avec un système de filtrage multi-axes permettant une analyse précise selon plusieurs dimensions :

- **Sessions** : connectées live, simultanées, déconnectées max avec filtrage par delivery group
- **Machines** : total par état avec filtrage par controller DNS name  
- **Utilisateurs** : connexions totales, échecs sur fenêtre glissante avec filtrage par delivery group et type d'échec

## Métriques de Sessions

### Sessions Connectées Live
Nombre de sessions actuellement connectées (états `connected` et `active`)

```
Métrique: sessions_connected_live
Tags: 
  - metric_type: "sessions"
  - session_filter: "connected_live"
  - delivery_group_id: [optionnel]
  - delivery_group_name: [optionnel]
```

### Sessions Simultanées 
Nombre d'utilisateurs uniques avec au moins une session active

```
Métrique: sessions_simultaneous_users
Tags:
  - metric_type: "sessions" 
  - session_filter: "simultaneous_users"
  - delivery_group_id: [optionnel]
  - delivery_group_name: [optionnel]
```

### Sessions Déconnectées Max
Nombre total de sessions déconnectées

```
Métrique: sessions_disconnected_max
Tags:
  - metric_type: "sessions"
  - session_filter: "disconnected_max"
  - delivery_group_id: [optionnel]
  - delivery_group_name: [optionnel]
```

### Filtrage par Delivery Group
Toutes les métriques de sessions peuvent être filtrées par delivery group en utilisant les tags :
- `delivery_group_id` : ID technique du delivery group
- `delivery_group_name` : Nom lisible du delivery group

## Métriques de Machines

### Machines Total par État
Nombre de machines classées par état de registration et de santé

```
Métrique: machines_by_state
Tags:
  - metric_type: "machines"
  - machine_state: "registered|unregistered|agent_error|healthy|failed"
  - state_type: "registration|fault"
  - controller_dns_name: [optionnel]
```

### Machines par Controller
Nombre de machines gérées par controller DNS name

```
Métrique: machines_by_controller
Tags:
  - metric_type: "machines"
  - controller_dns_name: nom DNS du controller
  - machine_state: "total|registered|unregistered|agent_error|healthy|failed"
```

### Filtrage par Controller DNS Name
Les métriques de machines peuvent être filtrées par `controller_dns_name` pour analyser la répartition par site ou datacenter.

## Métriques d'Utilisateurs

### Connexions Totales Actives
Nombre total de connexions d'utilisateurs actuellement actives

```
Métrique: user_connections_total_active
Tags:
  - metric_type: "user_connections"
  - connection_filter: "total_active"
  - delivery_group_id: [optionnel]
  - delivery_group_name: [optionnel]
```

### Échecs de Connexion (Fenêtre Glissante 1h)

#### Total des Échecs
```
Métrique: user_connection_failures_total
Tags:
  - metric_type: "user_connections"
  - failure_filter: "total_sliding_window"
  - time_window: "1h"
```

#### Utilisateurs avec Échecs
```
Métrique: user_connection_users_with_failures
Tags:
  - metric_type: "user_connections"
  - failure_filter: "unique_users_with_failures"
  - time_window: "1h"
  - delivery_group_id: [optionnel]
  - delivery_group_name: [optionnel]
```

#### Échecs par Type
```
Métrique: user_connection_failures_by_type
Tags:
  - metric_type: "user_connections"
  - failure_type: "client_connection_failures|configuration_errors|machine_failures|unavailable_capacity|unavailable_licenses"
  - time_window: "1h"
  - delivery_group_id: [optionnel]
  - delivery_group_name: [optionnel]
```

#### Échecs par Delivery Group
```
Métrique: user_connection_failures_by_delivery_group
Tags:
  - metric_type: "user_connections"
  - delivery_group_id: ID du delivery group
  - delivery_group_name: Nom du delivery group
  - failure_filter: "total_by_delivery_group"
  - time_window: "1h"
```

## Types d'Échecs de Connexion

1. **client_connection_failures** : Problèmes de connexion client
2. **configuration_errors** : Erreurs de configuration
3. **machine_failures** : Défaillances de machines VDA
4. **unavailable_capacity** : Capacité insuffisante
5. **unavailable_licenses** : Licences indisponibles

## Exemples de Filtrage

### Filtrer les sessions par delivery group spécifique
```
sessions_connected_live WHERE delivery_group_name = "Production Apps"
```

### Filtrer les machines par controller DNS
```
machines_by_state WHERE controller_dns_name = "ctx-ctrl-01.domain.com" AND state_type = "registration"
```

### Filtrer les échecs par type et delivery group
```
user_connection_failures_by_type WHERE failure_type = "machine_failures" AND delivery_group_name = "VDI Desktop"
```

### Analyser les échecs sur une fenêtre glissante
```
user_connection_failures_total WHERE time_window = "1h"
```

## Compatibilité

Le système conserve la compatibilité avec les métriques existantes tout en ajoutant les nouvelles métriques granulaires. Les anciennes métriques comme `total_sessions`, `machines_by_registration_state`, et `connection_failures_total` continuent d'être générées.

## Configuration

Aucune modification de configuration n'est requise. Le probe Citrix collecte automatiquement toutes les métriques avec les tags de filtrage appropriés lors de chaque cycle de collecte (par défaut toutes les 2 minutes).