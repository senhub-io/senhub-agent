# Next (unreleased)

Changes land here as they are merged. The 0.6.3 corrective release collects them.

<div class="rn-filter"></div>

## Fixes

- **doctor resolves `${env:}` with the service's environment.** It reads `Environment=` and `EnvironmentFile=` from the systemd unit and its drop-ins (as root). A reference it cannot resolve outside the service is a warning that names the variable, never a failure.
- **A monolithic `agent.yaml` beside the files `install` created is split.** The default `probes.d/00-host.yaml` and `strategies.d/00-http.yaml` are set aside as `*.install-default`; if the directories hold your own files, one message names them. The split failure and the legacy notice are logged once per start, and no seal failure is reported when the split was refused.
