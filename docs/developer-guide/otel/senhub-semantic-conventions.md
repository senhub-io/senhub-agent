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

### 4.8 Probes `syslog`, `event`, `otel` (conduits de flux log)

**Nature :** ces trois probes sont des **conduits de flux log** (collecte + retransmission), pas des collecteurs de métriques. Elles reçoivent des événements/logs et les relaient vers des consommateurs (cloud SenHub, futur OTLP log export, etc.). Ce ne sont pas des sources de signaux Prometheus.

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
| `senhub.probe.otel.spans_received` | `{span}` | Counter |
| `senhub.probe.otel.logs_received` | `{log}` | Counter |
| `senhub.probe.otel.metrics_received` | `{metric}` | Counter |

Ces métriques nécessitent une refonte du code probe pour maintenir des compteurs internes. Séparé.

#### 4.8.3 Probes concernées

- **syslog** : métrique `syslog_event` marquée `skip: true`.
- **event** : YAML créé avec métrique `event_event` marquée `skip: true`.
- **otel** : pas de YAML (probe stub, n'émet aucun data point).

## 5. Conventions en cours (prochains lots)

Sections à ajouter dans les prochains lots :
- 4.9 `netscaler` / `citrix` / `redfish` / `veeam` — lot 4

## 6. Processus d'ajout d'une convention

1. Lire les sources §1 pour le domaine concerné
2. Si convention existe → adopter telle quelle (attributs, unités, types)
3. Si inexistante → créer sous `senhub.*`, documenter ici avec :
   - Justification (pourquoi pas de convention existante)
   - Sources consultées (liens)
   - Alignement sur un pattern existant (windows_exporter, node_exporter…) si pertinent
4. Valider avec l'équipe avant publication
5. Mettre à jour le YAML de la probe concernée

## 7. Versioning

Ce document n'a pas (encore) de numéro de version. Une fois la V1 complète (15 probes mappées) publiée dans 0.1.88, il passera en SemVer 1.0.0. Tout changement de nom/attribut/unité = major bump.
