# Next (unreleased)

Changes merged since 0.5.5, not yet released.

<div class="rn-filter"></div>

## Fixes

- **A value from a probe that runs less often than the push is exported
  as current between two runs.** 0.5.5 stamped every point with the time
  it was measured, so a gauge from a probe running every 30 minutes gave
  one sample per run and an alert with a short lookback resolved while
  the condition still held. The point now carries the export time while
  the probe still vouches for it: observed by this process, and less
  than one and a half times the probe's `interval` plus one push interval
  ago. Past that, or for a series restored from the checkpoint and not
  observed since, the measurement time stands and eviction removes the
  series as before, so a target removed from a probe never runs ahead of
  its last observation. The lookback migration of 0.5.5 is no longer
  needed. (#890)

- **`senhub-agent update` no longer needs the archive and the binary in
  memory.** Applying an update held the ZIP, then the decompressed binary,
  in memory: about 250 MB for a 100 MB binary, which got the updater killed
  on a 2 GB host without swap. The archive is now written next to the
  binary, verified as it streams, opened from disk, and the binary copied
  straight into the file that replaces the old one. Releases from this
  version on are signed over the archive's digest so the check happens
  during the download; earlier releases keep installing, at the cost of
  the archive alone. (#891)

- **`config check` names the environment variable behind an empty
  credential.** Run from a shell on a host whose token comes from the
  service unit, the check reported an Authorization header with no
  credential and pointed at the configuration. It now lists first every
  `${env:NAME}` reference whose variable is not set in the shell running
  it, says where the service gets it from, and when the credential error
  follows, says that this is why and that the file may be correct. (#892)

- **`config check` accepts every output compiled into the build.** The
  list of strategy names it knew was written by hand and stopped at
  `otlp`, so a `zabbix` output was first reported as unknown.
