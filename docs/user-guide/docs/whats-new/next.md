# Next (unreleased)

<div class="rn-filter"></div>


## Fixed

### `update` now refreshes the binary the service actually runs

On a hardened Linux install the agent is on disk twice: the CLI copy in
`PATH`, and the copy the systemd unit execs
(`/var/lib/senhub-agent/bin/senhub-agent`), which the unprivileged daemon owns
so it can replace it during auto-update. `sudo senhub-agent update <version>`
only ever replaced the copy it ran from, so the service kept running the old
release while the CLI reported the new one — and the closing "Restart the agent
to use the new version" made it look like the upgrade had landed.

`update` now reconciles both copies and names each file it wrote. Ownership of
the service copy is handed back to the unit's `User=`, so the daemon can still
self-update afterwards. Re-running the command is also the repair for a host
whose service copy already fell behind.

A service copy running a **newer** release than the one being installed is
reported and left untouched rather than downgraded — the daemon legitimately
runs ahead of the CLI.

`senhub-agent --version` reports the skew instead of hiding it:

```
Version: 0.5.3 (commit: a8e67f7)
Service binary: 0.5.4 (/var/lib/senhub-agent/bin/senhub-agent)
Note: the systemd service runs a different build than this CLI binary.
      'sudo senhub-agent update <version>' updates both copies.
```

The other binary's version is read from its build metadata, never by executing
it. Nothing changes for a single-copy install (legacy root unit, Windows, MSI):
both behaviours stay silent when there is only one binary. (#723)


## Known follow-ups

- Hosts running `auto_update.include_beta: true` resolve `latest` to the newest
  beta and never move to the stable release that supersedes it. Stable hosts are
  unaffected. (#730)
