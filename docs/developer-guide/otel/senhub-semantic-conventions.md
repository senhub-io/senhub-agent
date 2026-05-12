# SenHub OpenTelemetry Semantic Conventions

**Statut :** WIP — document vivant, mis à jour à chaque lot de probes
**Dernière mise à jour :** 2026-04-18 (Lot 1: cpu)
**Audience :** développeurs de probes, mainteneurs des mappers

## 0. Objet

Ce document liste les **conventions de nommage OTel** adoptées par SenHub Agent pour chaque métrique exposée. Il couvre :

1. Les métriques qui adoptent **telles quelles** les conventions OTel officielles (namespace `system.*`, `http.*`, etc.)
2. Les extensions propriétaires sous namespace **`senhub.*`** pour les domaines non couverts (netscaler, citrix, veeam…) ou les métriques spécifiques (Windows Perfmon, Linux-specific…) avec justification et références consultées
3. Les harmonisations (ex: `cpu.mode=system` partagé Linux `system` et Windows `privileged`)

**Principes directeurs :**
- **OTel first** : adopter une convention existante plutôt que créer. Vérifier semconv officiel, OTel Collector contrib receivers, conventions vendeurs de facto (Grafana Labs, VictoriaMetrics, prometheus-community) avant de définir.
- **Stabilité** : une fois publiée, une convention ne bouge plus (les dashboards en dépendent). Évolutions = major version.
- **Traçabilité** : chaque extension `senhub.*` documentée ici avec justification + lien(s).

## 1. Sources de référence

Consultées pour chaque décision :

- [OTel Semantic Conventions](https://github.com/open-telemetry/semantic-conventions) (officiel)
- [OTEP 0119 - Standard System Metrics](https://github.com/open-telemetry/oteps/blob/main/text/0119-standard-system-metrics.md) (pour OS-specific)
- [OTel Collector contrib receivers](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver)
- [prometheus-community exporters](https://github.com/prometheus-community) (node_exporter, windows_exporter, redfish_exporter…)
- Documentation vendeurs (Grafana Labs integrations, VictoriaMetrics, DataDog)

## 2. Règles de conversion OTel → Prometheus

Appliquées par le mapper Prometheus conformément à la [spec OTel compatibility](https://opentelemetry.io/docs/specs/otel/compatibility/prometheus_and_openmetrics/) :

1. Préfixe `senhub_` ajouté au nom (tous namespaces confondus)
2. Dots → underscores dans noms et attributs
3. Caractères non conformes à `[a-zA-Z_:][a-zA-Z0-9_:]*` remplacés par `_`, underscores consécutifs dédupliqués
4. Suffixe d'unité : `s` → `_seconds`, `By` → `_bytes`, `Hz` → `_hertz`, `1` → `_ratio`, unités `{...}` en accolades → supprimées, `foo/bar` → `_foo_per_bar`
5. Counter reçoit `_total` si absent
6. Utilisation (`unit: 1`) : le mapper **convertit automatiquement** les valeurs 0-100 du cache en ratio 0-1

Exemples :
| OTel | Prometheus |
|---|---|
| `system.cpu.time` / counter / `s` / `cpu.mode=user` | `senhub_system_cpu_time_seconds_total{cpu_mode="user"}` |
| `system.cpu.utilization` / gauge / `1` / `cpu.mode=user` | `senhub_system_cpu_utilization_ratio{cpu_mode="user"}` (valeur ÷ 100) |
| `senhub.system.cpu.queue_length` / gauge / `{thread}` | `senhub_system_cpu_queue_length` |
| `system.linux.cpu.load_1m` / gauge / `{thread}` | `senhub_system_linux_cpu_load_1m` |

## 2bis. Conformité OTel stricte — principe "mapper-side"

**La conformité OTel vit dans le mapper, pas dans le cache.** Quand le probe émet un data point dont la sémantique OTel stricte nécessite un **autre format** en sortie (ex: un enum encodé en valeur numérique doit devenir N data points per-state), le mapper effectue la transformation **au moment de la sérialisation vers le format cible** (Prometheus aujourd'hui, OTLP native demain).

**Conséquence pour les futurs exports** : quand un mapper OTLP native sera ajouté (Phase 3), il émettra du strict OTel **sans aucune correction à faire** — les déviations sont déjà corrigées en amont par la logique du mapper, qui est partagée entre les formats OTel-aware (Prometheus, OTLP, Zabbix-OTel, etc.).

**Mécanisme documenté** : le bloc `otel.expand` dans les YAML transformers déclare une expansion enum → per-state. Le mapper lit cette directive et produit les N data points appropriés à chaque scrape. Voir `IMPLEMENTATION-PLAN.md §4` pour le schéma exact.

Cas typique : toutes les métriques `hw.status` (santé hardware) suivent ce pattern — 1 data point dans le cache (code enum depuis lookup) → N data points à la sérialisation, un par valeur de `hw.state`.

## 3. Labels systématiques

Sur **toute** métrique émise par une probe, le mapper Prometheus ajoute :

| Label | Source | Exemple |
|---|---|---|
| `probe_name` | nom d'instance (config) | `cpu-linux-primary` |
| `probe_type` | type registry | `cpu` |
| *labels custom_tags* | si `include_probe_tags: true` | `env=prod, site=paris` |

Le label `instance` (réservé scrape Prometheus) n'est **jamais** émis par l'agent.

## 4. Conventions adoptées par probe

### 4.1 Probe `cpu` (système)

**Source principale :** [OTel system metrics — CPU](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/)
**Source secondaire :** [windows_exporter collector.cpu](https://github.com/prometheus-community/windows_exporter/blob/master/docs/collector.cpu.md), [OTEP 0119](https://github.com/open-telemetry/oteps/blob/main/text/0119-standard-system-metrics.md)

#### 4.1.1 Métriques OTel natives utilisées

| OTel metric | Unit | Type | Notre usage |
|---|---|---|---|
| `system.cpu.time` | `s` | Counter | Temps CPU cumulatif par mode (Linux: `/proc/stat`) |
| `system.cpu.utilization` | `1` | Gauge | Utilisation instantanée (%) normalisée en ratio par le mapper |

**Attributs utilisés :**

- `cpu.mode` (OTel bien-connu) — valeurs :
  - `user` — temps user-space (Linux cpu_user, Windows user_time)
  - `system` — temps kernel (Linux cpu_system, Windows privileged_time) **[harmonisé]**
  - `idle` — temps idle (Linux cpu_idle)
  - `nice` — low-priority user (Linux cpu_nice)
  - `iowait` — attente I/O (Linux cpu_iowait)
  - `interrupt` — temps interrupts matériels (Linux cpu_irq, Windows interrupt_time)
  - `softirq` — interrupts logiciels (Linux cpu_softirq) — extension bien-connue
  - `steal` — volé par hyperviseur (Linux cpu_steal)
  - `dpc` — Deferred Procedure Calls (Windows dpc_time) — **extension, alignée windows_exporter**
- `cpu.logical_number` (OTel bien-connu) — numéro du core logique en string (`"0"`, `"1"`, …)

**Harmonisation `system` ↔ `privileged`** : OTel accepte `kernel` ou `system`. Nous harmonisons sur `system` pour que dashboards cross-OS interrogent un seul mode et obtiennent Linux kernel time ET Windows privileged time.

#### 4.1.2 Métriques OTEP 0119 (load average)

| OTel metric | Unit | Type | Notre usage |
|---|---|---|---|
| `system.linux.cpu.load_1m` | `{thread}` | Gauge | cpu_load1 |
| `system.linux.cpu.load_5m` | `{thread}` | Gauge | cpu_load5 |
| `system.linux.cpu.load_15m` | `{thread}` | Gauge | cpu_load15 |

Préfixe `linux` indique explicitement la spécificité OS conformément à l'OTEP 0119. Non émis sur Windows.

#### 4.1.3 Extensions `senhub.*` (Windows-specific)

**Justification :** windows_exporter expose ces métriques en counters (totals depuis boot). Notre probe les capture sous forme de **rates instantanés** depuis Perfmon (DPCs/sec, Interrupts/sec). OTel ne définit pas de convention pour ces rates — extension créée.

| Senhub metric | Unit | Type | Source probe | Équiv. windows_exporter |
|---|---|---|---|---|
| `senhub.system.cpu.dpcs_per_second` | `1/s` | Gauge | cpu_dpc_rate, dpc_rate | `windows_cpu_dpcs_total` (counter) — rate = `rate(...)` |
| `senhub.system.cpu.dpcs_queued_per_second` | `1/s` | Gauge | cpu_dpc_queued, dpc_queued | *(aucun, Perfmon-specific)* |
| `senhub.system.cpu.interrupts_per_second` | `1/s` | Gauge | cpu_interrupts, interrupt_sec | `windows_cpu_interrupts_total` (counter) — rate = `rate(...)` |
| `senhub.system.cpu.queue_length` | `{thread}` | Gauge | cpu_queue_length, processor_queue_length | *(aucun)* |

Attributs: `cpu.logical_number` (optionnel, présent si mesuré par core).

> **Évolution V2 possible** : refactorer la probe pour émettre en counter cumulatif et aligner pleinement sur windows_exporter (`senhub_system_cpu_dpcs_total` etc.). Discuté plus tard.

### 4.2 Probe `memory` (système)

**Source principale :** [OTel system metrics — Memory](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/)
**Source secondaire :** [OTEP 0119 §Paging](https://github.com/open-telemetry/oteps/blob/main/text/0119-standard-system-metrics.md) *(draft — adopté avec risque de migration si l'OTEP est renommé)*

#### 4.2.1 Métriques OTel natives utilisées

| OTel metric | Unit | Type | Notre usage |
|---|---|---|---|
| `system.memory.limit` | `By` | UpDownCounter | Total RAM installée (Win `memory_total`) |
| `system.memory.usage` | `By` | UpDownCounter | Occupation RAM par état (attribut `system.memory.state`) |
| `system.memory.utilization` | `1` | Gauge | % RAM utilisée (cross-platform, `memory_used_percent`) |
| `system.paging.utilization` | `1` | Gauge | % pagefile (`pagefile_usage`) — OTEP 0119 draft |

**Attribut `system.memory.state`**

Valeurs officielles OTel : `buffers, cached, free, used`

**Harmonisation Windows `available` → `free`** : les deux désignent la mémoire immédiatement disponible pour allocation. Simplifie les dashboards cross-OS.

**Extensions `system.memory.state`** (Windows-specific, non OTel-standard) :

| Valeur | Source | Description |
|---|---|---|
| `committed` | `memory_committed` | Virtual memory committed by the memory manager |
| `modified` | `memory_modified_page_list` | Memory modified but not yet written to disk |
| `nonpaged_pool` | `memory_nonpaged_pool` | Kernel memory that cannot be paged out |
| `paged_pool` | `memory_paged_pool` | Kernel memory that can be paged out |

#### 4.2.2 Extensions `senhub.*` (paging rates Windows)

**Justification :** notre probe expose les paging Windows sous forme de **rates instantanés** depuis Perfmon. OTEP 0119 propose `system.paging.faults` et `system.paging.operations` en counters. Nous créons des variantes `_per_second` en gauge le temps de la migration. À aligner sur OTel standard lors de la refonte de la probe (counter cumulatif).

| Senhub metric | Unit | Type | Attributs |
|---|---|---|---|
| `senhub.system.paging.faults_per_second` | `1/s` | Gauge | – |
| `senhub.system.paging.operations_per_second` | `1/s` | Gauge | `direction: in` ou `out` |
| `senhub.system.paging.utilization_peak` | `1` | Gauge | – *(pas d'équivalent OTEP 0119)* |

### 4.3 Probe `network` (système)

**Source principale :** [OTel system metrics — Network](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/)

**Alignement 100 % OTel natif** — aucune extension `senhub.*` introduite.

#### 4.3.1 Métriques OTel utilisées

| OTel metric | Unit | Type | Notre usage |
|---|---|---|---|
| `system.network.io` | `By` | Counter | Bytes transmis/reçus (total cumulatif) |
| `system.network.packet.count` | `{packet}` | Counter | Paquets transmis/reçus |
| `system.network.errors` | `{error}` | Counter | Erreurs de transmission/réception |
| `system.network.packet.dropped` | `{packet}` | Counter | Paquets rejetés volontairement (discards) |

**Attributs utilisés :**

- `network.io.direction` — valeurs officielles : `receive`, `transmit`
- `network.interface.name` — nom de l'interface (`eth0`, `ens1`, `Ethernet 2`, …)

### 4.4 Probe `logicaldisk` (filesystem + disk I/O)

**Source principale :** [OTel system-metrics §Filesystem](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/) et `§Disk`
**Source secondaire :** [node_exporter filesystem_*](https://github.com/prometheus/node_exporter) (inode conventions)

**Note terminologique :** le type de probe en config reste `logicaldisk` (nom historique, compat JWT license + Windows Perfmon `\LogicalDisk\`). Les métriques exposées suivent le namespace OTel `system.filesystem.*` (capacity) et `senhub.system.disk.*` (I/O rates Windows). C'est le namespace OTel qui est visible côté dashboards.

#### 4.4.1 Métriques OTel natives utilisées

| OTel metric | Unit | Type | Notre usage |
|---|---|---|---|
| `system.filesystem.limit` | `By` | UpDownCounter | Capacité totale (`fs_total_bytes`) |
| `system.filesystem.usage` | `By` | UpDownCounter | Occupation par état (attribut `system.filesystem.state`) |
| `system.filesystem.utilization` | `1` | Gauge | Ratio d'occupation (attribut `system.filesystem.state`) |

**Attribut `system.filesystem.state`**

Valeurs officielles OTel : `free, reserved, used`

**Extension `system.filesystem.state=available`** — Linux `statfs` expose `f_bavail` (espace disponible aux processus non-root, distinct de `f_bfree`). Mappé à `available` pour préserver l'info.

**Unit conversions par le mapper :**
- Windows `disk_free_mb` (MB) → OTel unit `By` (bytes) : **mapper ×1048576** (MiB).
- Pourcentages (0-100) → OTel ratio (0-1) : **mapper ÷100**.

#### 4.4.2 Extensions `senhub.*` (inodes — Linux)

**Justification :** OTel `system.filesystem.*` est centré sur l'octet. node_exporter expose `node_filesystem_files` (total inodes) et `node_filesystem_files_free`. Nous créons un sous-espace inode miroir de `system.filesystem.*` pour cohérence.

| Senhub metric | Unit | Type | Attributs |
|---|---|---|---|
| `senhub.system.filesystem.inode.limit` | `{inode}` | UpDownCounter | – |
| `senhub.system.filesystem.inode.usage` | `{inode}` | UpDownCounter | `system.filesystem.state: free` ou `used` |
| `senhub.system.filesystem.inode.utilization` | `1` | Gauge | `system.filesystem.state: used` |

#### 4.4.3 Extensions `senhub.*` (disk I/O rates — Windows)

**Justification :** OTel `system.disk.*` définit des counters cumulatifs (`system.disk.operations`, `system.disk.io`). Notre probe Windows capture des **rates instantanés** depuis Perfmon (`\LogicalDisk\Disk Reads/sec` etc.). Extensions `_per_second` en gauge — alignement OTel complet possible après refonte probe (V2).

| Senhub metric | Unit | Type | Attributs |
|---|---|---|---|
| `senhub.system.disk.operations_per_second` | `1/s` | Gauge | `disk.io.direction: read` ou `write` |
| `senhub.system.disk.io_per_second` | `By/s` | Gauge | `disk.io.direction: read` ou `write` |
| `senhub.system.disk.queue_length` | `{operation}` | Gauge | – |

#### 4.4.4 Attributs (tag → attribute mapping)

| Tag interne | Attribut OTel |
|---|---|
| `device` | `system.device` (ex: `/dev/sda1`) |
| `mount_point` | `system.filesystem.mountpoint` (ex: `/`, `/var`) |
| `drive` (Windows) | `system.filesystem.mountpoint` (ex: `C:`, `D:`) — harmonisé Linux/Windows |
| `fs_type` | `system.filesystem.type` (ex: `ext4`, `ntfs`) |

### 4.5 Probes `ping_gateway` et `ping_webapp` (ICMP connectivité)

**Source principale :** aucune OTel (pas de semconv ICMP)
**Source secondaire :** [Prometheus blackbox_exporter](https://github.com/prometheus/blackbox_exporter) (`probe_icmp_*` convention)

**Note :** nos probes ICMP font de la **mesure continue agrégée** (moyennes sur fenêtre), pas des probes ponctuelles comme blackbox_exporter. Les noms sont adaptés en conséquence sous namespace `senhub.probe.*`.

#### 4.5.1 Extensions `senhub.*`

| Senhub metric | Unit | Type | Attributs |
|---|---|---|---|
| `senhub.probe.icmp.duration_seconds` | `s` | Gauge | `url.full` *(optionnel — présent pour ping_webapp, absent pour ping_gateway)* |
| `senhub.probe.icmp.packet_loss_ratio` | `1` | Gauge | `url.full` *(optionnel)* |

**Unit conversions par le mapper :** ms → s (÷1000) pour latency ; % → ratio (÷100) pour packet loss.

Distinction ping_gateway vs ping_webapp : même nom de métrique, ping_gateway n'émet **pas** le label `url.full` (cible = default gateway détectée au runtime).

### 4.6 Probe `load_webapp` (HTTP phase timing)

**Source principale :** aucune OTel directement applicable (`http.client.*` est orienté histogramme sur requêtes ponctuelles — notre modèle est continu avec moyennes)
**Source secondaire :** [blackbox_exporter](https://github.com/prometheus/blackbox_exporter/blob/master/prober/http.go) — `probe_http_duration_seconds{phase=…}` avec phases `resolve, connect, tls, processing, transfer`

#### 4.6.1 Extension `senhub.probe.http.*`

| Senhub metric | Unit | Type | Attributs |
|---|---|---|---|
| `senhub.probe.http.duration_seconds` | `s` | Gauge | `phase`, `url.full` |

**Valeurs `phase`** (aligné blackbox_exporter + extension `total`) :

| Valeur | Signification |
|---|---|
| `resolve` | Résolution DNS |
| `connect` | Établissement TCP |
| `tls` | Handshake TLS |
| `processing` | Time To First Byte (TTFB) |
| `total` | Durée complète request → full response *(extension — blackbox utilise `probe_duration_seconds` séparément)* |

**Unit conversion :** ms → s (÷1000) par le mapper.

### 4.7 Probe `wifi_signal_strength` (connectivité WiFi)

**Source principale :** aucune OTel (pas de semconv wifi)
**Source secondaire :** aucune convention communautaire établie

Extension complète sous namespace `senhub.system.network.wifi.*`.

#### 4.7.1 Extensions `senhub.*`

| Senhub metric | Unit | Type | Attributs |
|---|---|---|---|
| `senhub.system.network.wifi.signal_strength.dbm` | `dBm` | Gauge | `senhub.network.wifi.ssid`, `senhub.network.wifi.bssid` |
| `senhub.system.network.wifi.quality_ratio` | `1` | Gauge | `senhub.network.wifi.ssid`, `senhub.network.wifi.bssid` *(÷100)* |

**Attributs :**

| Attribut | Source | Description |
|---|---|---|
| `senhub.network.wifi.ssid` | tag `ssid` | Nom du réseau (ESSID) |
| `senhub.network.wifi.bssid` | tag `bssid` | Adresse MAC du point d'accès (BSSID) |

> Pas de YAML transformer existant pour `wifi_signal_strength` — créé lors du lot 2.

### 4.8 Probes `syslog`, `event` (conduits de flux log)

**Nature :** ces probes sont des **conduits de flux log** (collecte + retransmission), pas des collecteurs de métriques. Elles reçoivent des événements/logs et les relaient vers des consommateurs (cloud SenHub, OTLP log export, etc.). Ce ne sont pas des sources de signaux Prometheus.

> **Note 2026-05-12 :** la probe `otel` (réception OTLP) a été retirée de la registry — implémentation stub jamais terminée. Une réimplementation complète (vrai serveur OTLP gRPC/HTTP) est nécessaire avant réactivation.

**Décision :** aucune métrique métier exposée via l'endpoint `/metrics`. Déclaration explicite par `otel.skip: true` dans les YAML pour respecter le contrat "pas de métrique sans mapping" (le skip EST un mapping explicite, documenté et auditable).

#### 4.8.1 Schéma `otel.skip`

```yaml
otel:
  skip: true
  reason: "<explication obligatoire pour la revue>"
```

Le mapper Prometheus ignore ces métriques ; elles n'apparaissent pas dans `/metrics`. Les champs `prtg:` / `nagios:` restent fonctionnels (retro-compat).

#### 4.8.2 Évolution future — instrumentation opérationnelle

Ces probes pourront être **outillées** (chantier dédié, hors scope du mapping OTel-first actuel) pour exposer leurs propres **métriques opérationnelles** :

| Candidat futur | Unit | Type |
|---|---|---|
| `senhub.probe.syslog.events_received` | `{event}` | Counter |
| `senhub.probe.syslog.events_dropped` | `{event}` | Counter |
| `senhub.probe.syslog.buffer_fill_ratio` | `1` | Gauge |
| `senhub.probe.event.events_received` | `{event}` | Counter |

Ces métriques nécessitent une refonte du code probe pour maintenir des compteurs internes. Séparé.

#### 4.8.3 Probes concernées

- **syslog** : métrique `syslog_event` marquée `skip: true`.
- **event** : YAML créé avec métrique `event_event` marquée `skip: true`.

### 4.9 Probe `redfish` (monitoring hardware serveur)

**Source principale :** [OTel hardware namespace](https://opentelemetry.io/docs/specs/semconv/hardware/) — 16 catégories (power_supply, physical_disk, logical_disk, disk_controller, enclosure, etc.)
**Source secondaire :** [jenningsloy318/redfish_exporter](https://github.com/jenningsloy318/redfish_exporter) (référence pattern Prometheus)

#### 4.9.1 Métriques OTel natives utilisées

| OTel metric | Unit | Type | Notre usage |
|---|---|---|---|
| `hw.status` | `1` | UpDownCounter | Santé avec `hw.type` ∈ {power_supply, physical_disk, logical_disk, disk_controller, enclosure} — pattern expand sur `hw.state` |
| `hw.physical_disk.size` | `By` | UpDownCounter | Capacité totale drive |
| `hw.logical_disk.limit` | `By` | UpDownCounter | Capacité totale volume |
| `hw.logical_disk.usage` | `By` | UpDownCounter | Occupation volume (allocated/free) avec `hw.logical_disk.state` |
| `hw.logical_disk.utilization` | `1` | Gauge | Ratio d'occupation volume |

**Attribut `hw.state`** — valeurs émises via expansion:
- Officielles OTel : `ok`, `degraded`, `failed`, `predicted_failure`
- **Extension `unknown`** — pour le code Redfish 3 (Unknown) qui n'existe pas en OTel standard. Valeur honnête : "Redfish n'a pas pu déterminer l'état".

**Mapping des codes lookup `sfs.redfish.health`** :
- 0 (OK) → `hw.state=ok`
- 1 (Warning) → `hw.state=degraded`
- 2 (Critical) → `hw.state=failed`
- 3 (Unknown) → `hw.state=unknown` *(extension)*

#### 4.9.2 Extensions `senhub.*`

Extensions créées pour les concepts absents du namespace OTel hardware officiel :

| Senhub metric | Type | Raison |
|---|---|---|
| `senhub.hardware.physical_disk.has_active_operations` | Gauge bool | Pas d'OTel |
| `senhub.hardware.physical_disk.operation.progress_ratio` | Gauge `1` | Pas d'OTel |
| `senhub.hardware.physical_disk.link_speed_bits_per_second` | Gauge `bit/s` | Pas d'OTel (Redfish expose NegotiatedSpeed Gbps, mapper ×1e9) |
| `senhub.hardware.physical_disk.location_indicator_active` | Gauge bool | Pas d'OTel |
| `senhub.hardware.physical_disk.block_size` | Gauge `By` | Pas d'OTel |
| `senhub.hardware.logical_disk.encrypted` | Gauge bool | Pas d'OTel |
| `senhub.hardware.logical_disk.io.operations` | Counter `{operation}` | Pas d'OTel I/O logical_disk (vs `system.disk.operations` qui est host-level) |
| `senhub.hardware.logical_disk.io` | Counter `By` | Idem — bytes I/O par volume |
| `senhub.hardware.storage.pool.*` | (multiple) | Pools RAID — absent de la taxonomie hw.type OTel |
| `senhub.hardware.system.power_state` | UpDownCounter | Enum Redfish (Off/On/Powering On/Powering Off/Unknown) |
| `senhub.hardware.eventservice.status` | UpDownCounter | Redfish-specific |
| `senhub.hardware.redundancy.status` | UpDownCounter | Groupe de redondance contrôleurs |
| `senhub.hardware.redundancy.controllers.count` | UpDownCounter | Compte avec `senhub.hardware.redundancy.bound` ∈ {active, min, max} |

#### 4.9.3 Attributs introduits

Alignement OTel quand possible (`hw.id`, `hw.name`, `hw.parent`, `hw.model`, `hw.serial_number`, `hw.physical_disk.type`, `hw.logical_disk.raid_level`, `hw.logical_disk.state`) et extensions pour le reste :

- `senhub.hardware.physical_disk.interface` — SAS/SATA/NVMe
- `senhub.hardware.physical_disk.slot` — slot number
- `senhub.hardware.enclosure.id` — enclosure identifier
- `senhub.hardware.disk_controller.slot` — controller slot
- `senhub.hardware.storage.pool.name` / `.id` / `.state` / `.raid_level`
- `senhub.hardware.redundancy.set` / `.state` / `.mode` / `.scope` / `.bound`

#### 4.9.4 Métriques skipées

- `hardware.storage.volume.io.total_ops` et `hardware.storage.volume.io.total_bytes` — redondantes avec reads+writes, skip avec justification (derivables en PromQL par `sum without(disk_io_direction)`).

### 4.10 Probe `veeam` (backup & replication)

**Source principale :** aucune convention OTel pour backup
**Source secondaire :** [peekjef72/veeam_exporter](https://github.com/peekjef72/veeam_exporter) et variantes communautaires (patterns convergents sur job states, repo capacity)

**Décision :** toutes les métriques sous extensions `senhub.veeam.*`. Stratégie de collapse systématique (totaux/counts en labels de state plutôt que noms de métriques séparés).

#### 4.10.1 Extensions `senhub.veeam.*`

**Jobs (overview + détail) :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.jobs.total` | `{job}` | Gauge |
| `senhub.veeam.jobs.by_last_result` | `{job}` | Gauge (attribut `senhub.veeam.job.last_result` ∈ {success, warning, failed, running}) |
| `senhub.veeam.job.status` | `1` | UpDownCounter (**expand** `senhub.veeam.job.state` ∈ {none, success, warning, failed, running}) |
| `senhub.veeam.job.seconds_since_last_run` | `s` | Gauge |
| `senhub.veeam.job.objects` | `{object}` | Gauge |
| `senhub.veeam.job.bottleneck.status` | `1` | UpDownCounter (**expand** `senhub.veeam.job.bottleneck` ∈ {none, source, proxy, network, target}) |
| `senhub.veeam.job.last_run.bytes` | `By` | Gauge (attribut `senhub.veeam.job.data_phase` ∈ {processed, read, transferred}) |

**Repository :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.repository.limit` | `By` | UpDownCounter |
| `senhub.veeam.repository.usage` | `By` | UpDownCounter (attribut `senhub.veeam.repository.state` ∈ {used, free}) |
| `senhub.veeam.repository.utilization` | `1` | Gauge (attribut `senhub.veeam.repository.state` ∈ {free}) |

**License :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.license.status` | `1` | UpDownCounter (**expand** `senhub.veeam.license.state` ∈ {valid, expired, invalid}) |
| `senhub.veeam.license.days_remaining` | `{day}` | Gauge |
| `senhub.veeam.license.instances` | `{instance}` | Gauge (attribut `senhub.veeam.license.instances_state` ∈ {total, used, remaining}) |

**Proxies :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.proxy.status` | `1` | UpDownCounter (**expand** `senhub.veeam.proxy.state` ∈ {disabled, offline, online}) |
| `senhub.veeam.proxies` | `{proxy}` | Gauge (attribut `senhub.veeam.proxies_state` ∈ {total, enabled, disabled}) |

**Protected objects :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.object.restore_points` | `{restore_point}` | Gauge |
| `senhub.veeam.object.last_run_failed` | `1` | Gauge bool |
| `senhub.veeam.objects` | `{object}` | Gauge (attribut `senhub.veeam.objects_state` ∈ {total, failed}) |

**Infrastructure (managed servers) :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.server.status` | `1` | UpDownCounter (**expand** `senhub.veeam.server.state` ∈ {unavailable, available}) |
| `senhub.veeam.servers` | `{server}` | Gauge (attribut `senhub.veeam.servers_state` ∈ {total, available, unavailable}) |

#### 4.10.2 Attributs (tag → attribute mapping)

| Tag interne | Attribut OTel |
|---|---|
| `job_name` | `senhub.veeam.job.name` |
| `job_type` | `senhub.veeam.job.type` |
| `repo_name` | `senhub.veeam.repository.name` |
| `proxy_name` | `senhub.veeam.proxy.name` |
| `object_name` | `senhub.veeam.object.name` |
| `object_type` | `senhub.veeam.object.type` |
| `server_name` | `senhub.veeam.server.name` |
| `server_type` | `senhub.veeam.server.type` |

#### 4.10.3 Récap

33 métriques internes → 20 noms OTel uniques grâce au collapse via labels. 5 métriques utilisent le pattern `expand` pour les enums de statut (job, bottleneck, license, proxy, server).

### 4.11 Probe `citrix` (Virtual Apps and Desktops)

**Source principale :** aucune convention OTel pour Citrix CVAD
**Source secondaire :** aucun exporter Prometheus standard (Dynatrace, ControlUp, Nexthink sont propriétaires) — design from scratch cohérent avec nos conventions

Toutes les métriques sous `senhub.citrix.*`. Collapse systématique par catégorie fonctionnelle.

#### 4.11.1 Extensions `senhub.citrix.*`

**Sessions :**
- `senhub.citrix.sessions.count` (gauge, `{session}`) + `senhub.citrix.session.state` ∈ {connected, disconnected}

**Machines (infrastructure) :**
- `senhub.citrix.machines.total` (gauge, `{machine}`) — total dans le delivery group
- `senhub.citrix.machines.by_registration_state` (gauge, `{machine}`) + `senhub.citrix.machine.registration_state` ∈ {registered, unregistered, faulty, maintenance}

**Logon performance :**
- `senhub.citrix.logon.duration_1h_average` (gauge, `s`)
- `senhub.citrix.logon.last_session_duration` (gauge, `s`)
- `senhub.citrix.logon.sessions_opened` (gauge, `{session}`)
- `senhub.citrix.logon.phase_duration` (gauge, `s`) + `senhub.citrix.logon.phase` ∈ {brokering, vm_start, hdx, authentication, gpo, scripts, profile, interactive} — **8 phases collapsées**

**Connection failures :**
- `senhub.citrix.connection_failures.total` (gauge, `{failure}`)
- `senhub.citrix.connection_failures.by_category` (gauge, `{failure}`) + `senhub.citrix.connection_failure.category` ∈ {client_connection, configuration, machine, capacity_unavailable, licenses_unavailable, other}

**Load index (VDA utilisation) :**
- `senhub.citrix.load_index.ratio` (gauge, `1`) + `senhub.citrix.load_index.dimension` ∈ {effective, cpu, memory, disk, network, sessions} — **mapper ÷100**
- `senhub.citrix.machines.overloaded` (gauge, `{machine}`)

**License :**
- `senhub.citrix.license.sessions_active` (gauge, `{session}`)
- `senhub.citrix.license.peak_concurrent_users` (gauge, `{user}`)
- `senhub.citrix.license.unique_users` (gauge, `{user}`)
- `senhub.citrix.license.grace.sessions_remaining` (gauge, `{session}`)
- `senhub.citrix.license.grace.active` (gauge, `1`) bool
- `senhub.citrix.license.grace.time_remaining` (gauge, `s`) — **mapper ×3600** (heures → secondes)

**Machine fault states (Director) :**
- `senhub.citrix.machines.multi_session_fault_total` (gauge, `{machine}`) — distinct de `by_registration_state{faulty}` (source DDC vs Director)
- `senhub.citrix.machines.by_fault_state` (gauge, `{machine}`) + `senhub.citrix.machine.fault_state` ∈ {boot_failure, stuck_at_boot, unregistered, max_capacity, vm_not_found, unknown}

#### 4.11.2 Récap

**45 métriques internes → 19 noms OTel** via collapse par état/catégorie/phase. Aucun `expand` nécessaire (pas d'enum via lookup — chaque état est déjà un data point séparé).

Conversions côté mapper : `%` → ratio (÷100) pour load_index ; heures → secondes (×3600) pour grace time remaining.

### 4.12 Probe `netscaler` (Citrix ADC)

**Source principale :** aucune convention OTel pour NITRO/NetScaler
**Source secondaire :** [citrix-adc-metrics-exporter officiel](https://github.com/netscaler/netscaler-adc-metrics-exporter) (`citrixadc_*` pattern) — transposé sous `senhub.netscaler.*`

Scope massif (100 métriques) organisé par **16 entités NITRO** :
system, ns, ssl (global), lbvserver, service, servicegroup, ssl.certificate, ha, disk, interface, cs (vserver+policy), gslb (vserver+site+service), cache, compression, aaa, vpn, appfw.

#### 4.12.1 OTel native utilisé

- `system.filesystem.usage` + `system.filesystem.utilization` pour les métriques **disk** (partition locale de l'appliance). Le label `probe_type=netscaler` distingue des métriques filesystem de l'OS hôte.

Pas d'autre natif OTel (NITRO n'a pas d'équivalent semconv).

#### 4.12.2 Extensions `senhub.netscaler.*` — vue d'ensemble

Namespace structure:
- `senhub.netscaler.system.*` — CPU/mémoire/réseau/TCP/HTTP (global appliance)
- `senhub.netscaler.ns.*` — throughput global
- `senhub.netscaler.ssl.*` — SSL global et certificats
- `senhub.netscaler.lbvserver.*` / `.csvserver.*` / `.gslb.*` — load balancing
- `senhub.netscaler.service.*` / `.servicegroup.*` — backends
- `senhub.netscaler.interface.*` — interfaces réseau
- `senhub.netscaler.cache.*` / `.compression.*` — accélération
- `senhub.netscaler.aaa.*` / `.vpn.*` — auth et gateway
- `senhub.netscaler.appfw.*` — Web Application Firewall
- `senhub.netscaler.ha.*` — High Availability

#### 4.12.3 Métriques avec `otel.expand` (11 états)

Tous les enums `state` (lbvserver, service, servicegroup, csvserver, gslbvserver, gslbsite, gslbservice, interface, aaa.vserver, vpn.vserver, ssl.certificate, ha.role, ha.node, ha.sync) — valeurs NITRO courantes:

**Vserver/service/servicegroup/cs/gslb** (`lbvserver.state` enum) :
1=down, 2=unknown, 3=busy, 4=out_of_service, 5=trofs, 7=up, 8=trofs_down

**Interface** : 0=disabled, 1=enabled
**SSL certificate** : 0=invalid, 1=valid
**HA role** : 0=unknown, 1=secondary, 2=primary
**HA node/sync** : 0=down/failed, 1=up/success

#### 4.12.4 Collapses majeurs

- **rx/tx** partout → `network.io.direction` ∈ {receive, transmit}
  - System network throughput (Mbps), packets.rate, packets (total counter)
  - Interface io (bytes total), throughput (Mbps), errors, packets.dropped
  - LB vserver throughput
  - HA heartbeat packets + rate
- **Cache hits/misses** → `senhub.netscaler.cache.lookups` + `senhub.netscaler.cache.lookup_result` ∈ {hit, miss}
- **Compression compressed/original bytes** → `senhub.netscaler.compression.bytes` + `senhub.netscaler.compression.bytes_type`
- **AAA auth successes/failures** → `senhub.netscaler.aaa.vserver.auth_attempts` + `senhub.netscaler.aaa.auth_result` ∈ {success, failure}
- **ServiceGroup members active/inactive** → `senhub.netscaler.servicegroup.members` + `senhub.netscaler.servicegroup.member_state`
- **CS policy hits/undefine_hits** → `senhub.netscaler.cspolicy.evaluations` + `senhub.netscaler.cspolicy.result` ∈ {hit, undefined}
- **AppFW requests/responses blocked** → `senhub.netscaler.appfw.blocked` + `senhub.netscaler.http.message_type`
- **AppFW violations par type** (sqli, xss, buffer_overflow) → `senhub.netscaler.appfw.violations.by_type`
- **CPU data/management plane** → `senhub.netscaler.system.cpu.utilization` + `senhub.netscaler.cpu.plane` ∈ {data, management}
- **HTTP requests/responses rates** → `senhub.netscaler.system.http.messages.rate` + `senhub.netscaler.http.message_type`
- **TCP client/server connections** → `senhub.netscaler.system.tcp.connections.active` + `senhub.netscaler.tcp.side`
- **NS throughput total/http** → `senhub.netscaler.ns.throughput` + `senhub.netscaler.traffic_type`

#### 4.12.5 Conversions d'unités par le mapper

- `%` → ratio (÷100) — CPU, memory, cache hit ratio, disk %, compression ratio
- `Mbits/s` / `Mbps` → `bit/s` (×1e6) — system/interface/ns throughput, link speed
- `KB` → `By` (×1024) — disk, cache memory
- `μs` → `s` (÷1e6) — gslb site RTT

#### 4.12.6 Récap

**100 métriques internes → ~65 noms OTel uniques** grâce aux collapses.
**11 métriques** utilisent `otel.expand` pour les enums NITRO.
**3 métriques disk** mappées à OTel native `system.filesystem.*`.
**~62 extensions** sous `senhub.netscaler.*` pour les domaines NITRO spécifiques.

## 5. Conventions — lot 4 complet

Tous les probes sont mappés. La phase 0.5 est terminée.


## 6. Processus d'ajout d'une convention

1. Lire les sources §1 pour le domaine concerné
2. Si convention existe → adopter telle quelle (attributs, unités, types)
3. Si inexistante → créer sous `senhub.*`, documenter ici avec :
   - Justification (pourquoi pas de convention existante)
   - Sources consultées (liens)
   - Alignement sur un pattern existant (windows_exporter, node_exporter…) si pertinent
4. Valider avec l'équipe avant publication
5. Mettre à jour le YAML de la probe concernée

## 6bis. Single vocabulary, two transports — Prometheus + OTLP

À partir de **0.1.89-beta** l'agent expose les mêmes métriques via deux
transports : pull Prometheus (`/metrics`) et push OTLP/gRPC (storage
`otlp`). Les deux chemins consomment **le même flux d'`OtelRecord`**
produit par `internal/agent/services/data_store/otelmapper/`.

```
probe data
   │
   ▼
otelmapper.Resolve  ──►  []OtelRecord  ──┬──►  Prometheus serializer  →  /metrics
                                          │
                                          └──►  OTLP exporter           →  otelcol / vmagent
```

**Conséquence pratique :** le chemin OTLP n'introduit aucune nouvelle
convention. Tout ce qui est documenté dans §4 s'applique à l'identique
côté push. Ce que change le mapper de sortie :

| Sink              | Préfixe `senhub_` | Dots dans nom | Suffixes d'unité  | Ratios (`unit:1`) |
|-------------------|-------------------|---------------|--------------------|-------------------|
| Prometheus        | ajouté            | `_`           | `_seconds/_bytes/...` | converti côté serializer |
| OTLP (wire OTLP)  | **non**           | `.` conservés | absent (porté par le champ `unit`) | géré côté mapper |

Le `prometheusremotewrite` du collecteur applique ensuite ses propres
règles, qui correspondent **exactement** aux règles du serializer
Prometheus de l'agent — sauf le préfixe `senhub_` qui est local au
serializer. Un opérateur qui ingère le push OTLP dans VictoriaMetrics
interroge :

```promql
# Push OTLP via collecteur (prometheusremotewrite)
system_memory_usage_bytes{system_memory_state="used"}
# Pull Prometheus direct
senhub_system_memory_usage_bytes{system_memory_state="used"}
```

Les **dimensions** (probe_name, probe_type, attributs sémantiques type
`cpu.mode`, `system.memory.state`, `hw.state`) sont **identiques** sur
les deux chemins. Aliasing PromQL → un seul vocabulaire à apprendre.

### Resource attributes (OTLP only)

Le push OTLP attache des **resource attributes** par batch que le pull
Prometheus n'a pas (Prometheus colle ces dimensions sur chaque série
directement). Mappage standard :

| Attribut OTel              | Source côté agent                            |
|----------------------------|----------------------------------------------|
| `service.name`             | `storage[otlp].params.resource.service.name` (défaut `senhub-agent`) |
| `service.instance.id`      | 8 premiers caractères de `agent.key` par défaut, override possible   |
| `service.version`          | version de build (ldflags)                   |
| `deployment.environment`   | operator override                            |
| Extras                     | n'importe quel autre couple clé-valeur sous `resource:` |

Les receivers convertissent généralement ces attributs en labels
Prometheus via `resource_to_telemetry_conversion: enabled: true` côté
collector. Sans cette option, le push OTLP perd `service.name` dans
VictoriaMetrics — bug courant à diagnostiquer.

### Logs signal — convention OTel respectée

Le signal logs (probes `syslog`, `event`, `linux_logs`) est purement
OTel : aucune convention `senhub.*` au niveau du log record lui-même,
les attributs sont les attributs standards (`syslog.facility`,
`syslog.hostname`, `syslog.appname`, `host.name`, `systemd.unit`,
`process.pid`, `process.executable.name`). Seul le payload du probe
`event` (libre par construction) est namespacé `senhub.event.*`.

Mapping severité : la table RFC 5424 → OTel SeverityNumber appliquée
côté producteur (helper `agentstate.SyslogPriorityToSeverity`). Les
chemins du probe `event` (qui accepte des sévérités texte type EMERG,
ERR, WARNING, …) utilisent une table équivalente — mêmes valeurs
numériques en sortie OTel.

## 7. Versioning

Ce document n'a pas (encore) de numéro de version. Une fois la V1 complète (15 probes mappées) publiée dans 0.1.88, il passera en SemVer 1.0.0. Tout changement de nom/attribut/unité = major bump.
