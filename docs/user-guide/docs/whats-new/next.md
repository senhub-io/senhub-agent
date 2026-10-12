# Next (unreleased)

Changes land here as they are merged. The 0.6.3 corrective release collects them.

<div class="rn-filter"></div>

## Features

- **Official Grafana dashboards for Azure Container Apps and Container App Jobs.** `azure-container-apps.json` (coverage, per-application health, scan failures by reason, volume, logs) and `azure-container-app-jobs.json` (executions by status, duration, last run, logs) in `docs/grafana/`. They read the per-application metrics, not log counts.

## Fixes

- **doctor resolves `${env:}` with the service's environment.** It reads `Environment=` and `EnvironmentFile=` from the systemd unit and its drop-ins (as root). A reference it cannot resolve outside the service is a warning that names the variable, never a failure.
- **A monolithic `agent.yaml` beside the files `install` created is split.** The default `probes.d/00-host.yaml` and `strategies.d/00-http.yaml` are set aside as `*.install-default`; if the directories hold your own files, one message names them. The split failure and the legacy notice are logged once per start, and no seal failure is reported when the split was refused.
- **The OTLP logs queue announces itself at start.** One Info line gives its path, size cap and retention, so a healthy queue is no longer indistinguishable from a disabled one.
- **A cut update download is retried.** When the stream breaks midway, the whole request is repeated up to three more times with a growing pause, with a `senhub-agent/<version>` User-Agent and no transparent compression of the archive.
- **The OTLP receiver coverage counters start at zero.** `senhub.agent.otlp_receiver.received` and `.received.without_host_id` exist from the start for each enabled signal, so "nothing received yet" is no longer indistinguishable from a dead receiver.
- **No error when the journal reader is stopped with the agent.** `linux_logs` no longer logs `journalctl subprocess died unexpectedly` when systemd terminates journalctl as part of stopping the service.
- **`logicaldisk` stops warning about `/run/user/*` every cycle.** A per-user runtime directory the service account cannot read is skipped quietly; any other mount it cannot read is named once.
- **A Redis TLS failure names the handshake.** The `redis` probe runs the TLS handshake right after connecting and reports `TLS handshake with <host>:<port> failed: ...` instead of an `AUTH send failed` that hid the cause.
- **Container and Azure pages corrected.** Image tags move to 0.6.3; the container page explains `SENHUB_HOST_ID` and the upgrade trap of an existing `agent.yaml`; the Container Apps page states that `discovery` excludes `app` and `resource_group`; both Azure pages use the probe names `CA` and `CAJ`, the jobs page gains a "From environment variables" section, and the configuration page documents that `${env:}` inside a `SENHUB_PROBE_*` value is not expanded and that the variables win key by key.
- **Jobs probe docs.** `bookmark_path` defaults to `<probe name>.executions` in the state directory, and an entry without `app` or with `discovery:` is refused.
- **One `host.name` on every signal.** Metrics, the OTLP resource, the host entity, the systemd unit entities and the info endpoint all use the same canonical host name.
- **Every log record carries an OTel severity number.** Whatever level word a source writes, the number is derived (the text stays as written); a journald entry without a priority is informational instead of an emergency.
- **Relative paths follow the configuration directory.** `bookmark_path` and the `*_file` parameters (CA, certificate, key, password files), and `config/nagios.yaml`, are resolved against the directory of the configuration, not the working directory; `config check` from another directory no longer reports a false permission error.
- **doctor flags a stale `senhub-agent` on the PATH.** When another binary on the PATH reports a version different from the running service, doctor warns, naming both paths and versions and pointing to the unit's binary.
- **Identity re-keys are announced once, not at every start.** The retirement of the legacy database and service identities (0.5.4 re-key) was repeated during the first cycles of every agent start, which a topology backend read as the producer reporting the node gone. A marker file in the state directory now records that it was announced; if the directory is not writable the announcement repeats and one warning is logged.
- **host.id is always lowercase.** The `/etc/machine-id` fallback was not lowercased; the same id in two cases would be two hosts downstream (no host in production was affected).
