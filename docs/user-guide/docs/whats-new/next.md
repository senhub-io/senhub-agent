# Next (unreleased)

Nothing released yet since 0.7.1. Changes land here as they are merged.

- **Windows MSI: a new install updates itself.** The installer wrote `auto_update.enabled: false`, so an agent installed from the MSI stayed on its first version, although the MSI path is built to apply each new signed MSI. A new install now turns automatic updates on. Pass `AUTO_UPDATE=0` to `msiexec` when Intune, SCCM or another tool manages the version. An existing configuration is not changed: an agent installed before keeps its setting; to turn updates on there, set `enabled: true` under `auto_update` in `%ProgramData%\SenHub\agent.yaml`.

<div class="rn-filter"></div>
