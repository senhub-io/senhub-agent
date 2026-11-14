# Next (unreleased)

Nothing released yet since 0.7.1. Changes land here as they are merged.

- **Windows MSI: a new install updates itself.** The installer wrote `auto_update.enabled: false`, so an agent installed from the MSI stayed on its first version, although the MSI path is built to apply each new signed MSI. A new install now turns automatic updates on. Pass `AUTO_UPDATE=0` to `msiexec` when Intune, SCCM or another tool manages the version. An existing configuration is not changed: an agent installed before keeps its setting; to turn updates on there, set `enabled: true` under `auto_update` in `%ProgramData%\SenHub\agent.yaml`.
- **`config check` reports keys of the `agent:` block that do nothing.** A key the agent does not read inside `agent:` was dropped without a word: a misspelt `licence:`, or settings of older versions such as `update_check_interval`, `mode` or `generated`. `config check` now names each one in a warning (exit code 1), as it already did for top-level and probe keys. Remove them from `agent.yaml`; nothing else changes, the update check still runs every hour, at start and on every configuration change.

- **Learned neighbours no longer come and go on every read.** A switch forgets an idle MAC address within minutes, and the agent deleted the neighbour each time, then created it again at the next read: on a large network, thousands of false changes an hour in the topology. A `network.neighbor`, read over SNMP or from the host's ARP table, is no longer deleted when a read stops listing it. It announces a liveness interval of 24 hours and the topology retires it when no read has listed it for that long, so a neighbour that really left stays visible for up to a day. Above `max_neighbors`, the neighbours already published stay published while they are listed, instead of changing with every arrival.

- **`senhub.neighbors.omitted` is replaced by `senhub.neighbors.truncated`.** The number of neighbours cut by `max_neighbors` changed at almost every read on a cut device, and each change was recorded in the topology. The device entity now carries `senhub.neighbors.truncated`, a boolean that changes only when the cut starts or stops; `senhub.neighbors.limit` is unchanged, and the exact count stays in the agent log line `neighbor table bound: some neighbors omitted`.

<div class="rn-filter"></div>
