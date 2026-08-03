<img src="https://api.iconify.design/mdi/update.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# OS Updates

The `os_updates` probe reports the patch posture of the machine the agent runs
on: how many OS updates are pending, how many of them are security updates, and
whether the OS is waiting for a reboot. It replaces hand-deployed scripts (for
example an `exec` probe wrapping `apt-check`) with a native, cross-platform
probe that also covers Windows.

All queries are read-only and run without privilege escalation.

## Quick start

```yaml
# probes.d/40-os-updates.yaml — each file under probes.d/ is a YAML array of probes
- name: os-updates
  type: os_updates
  params:
    interval: 1800
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `interval` | `3600` | Collection interval in seconds. Update status changes slowly; 30-60 minutes is a sensible range. |
| `command_timeout` | `120` | Timeout in seconds for the package-manager queries on Linux. |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.os.updates.up` | 1 | 1 when the update backend answered, 0 when it failed or the platform is unsupported |
| `senhub.os.updates.pending` | update | Number of updates available and not yet installed |
| `senhub.os.updates.pending.security` | update | Number of pending updates classified as security fixes |
| `senhub.os.updates.reboot_required` | 1 | 1 when the OS reports a pending reboot, 0 otherwise |

Every metric carries the backend that answered as an attribute
(`os.package_manager`: `apt`, `dnf`, `yum` or `wua`).

## Per-OS behaviour

### Debian / Ubuntu (apt)

- Counts come from `/usr/lib/update-notifier/apt-check` when it is installed
  (package `update-notifier-common`); it reports the exact security count
  maintained by the distribution.
- Without apt-check, the probe falls back to simulating an upgrade
  (`apt-get -s upgrade`) and counting packages, marking those coming from a
  `*-security` archive as security updates.
- The reboot flag is the existence of `/var/run/reboot-required`.
- The probe does not refresh package lists (`apt-get update` is never run);
  counts reflect the lists maintained by the system's own update timers.

### RHEL and derivatives (dnf / yum)

- Counts come from `dnf -q updateinfo list` and
  `dnf -q updateinfo list --security` (or `yum` on older systems). The count is
  advisory-package pairs, which is what `updateinfo` reports.
- The reboot flag comes from `needs-restarting -r` (package `dnf-utils` /
  `yum-utils`); if the tool is not installed the flag is reported as 0.
- Depending on the metadata cache age, the first query after boot may take
  longer while dnf refreshes its metadata.

### Windows (Windows Update Agent)

- The probe queries the Windows Update Agent COM API
  (`IsInstalled=0 and IsHidden=0 and Type='Software'`). The result honours the
  machine's update source: WSUS-managed hosts report what WSUS approved.
- Security updates are the ones carrying an MSRC severity or the
  "Security Updates" category.
- The reboot flag is the Windows Update pending-reboot state.
- The first search on a host that has not scanned recently can take minutes;
  the probe reports it at the next interval. Keep the interval at 1800 seconds
  or more.

### Other platforms

On unsupported platforms (macOS) the probe emits `senhub.os.updates.up=0`.

## Operational notes

- This probe is host-local: it reads the update state of the machine the agent
  runs on, not a remote system.
- When the backend fails (package manager busy, WUA service stopped), the probe
  keeps emitting `senhub.os.updates.up=0` and suppresses the counts for that
  cycle instead of dropping the series.
- Alerting suggestion: warn on `senhub.os.updates.pending.security > 0`
  sustained for more than a day, and on `senhub.os.updates.reboot_required = 1`.
