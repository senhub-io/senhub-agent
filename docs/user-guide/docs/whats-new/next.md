# Next (unreleased)

Changes merged since 0.5.6, not yet released.

<div class="rn-filter"></div>

## Fixes

- **A value from a probe that runs less often than the push is exported as
  current between two runs.** A gauge from a probe running every 30 minutes
  gave one sample per run, so an alert with a short lookback resolved while
  the condition still held. (#890)

- **`senhub-agent update` no longer needs the archive and the binary in
  memory.** It streams both, where it used about 250 MB for a 100 MB binary
  and was killed on a small host. (#891)

- **`config check` names the environment variable behind an empty
  credential**, instead of pointing at a configuration that may be correct.
  (#892)

- **The two libraries a registry scanner reports are raised**, and the
  image's base moves to a supported Alpine. (#900)

## Features

- **The Zabbix output**, as a native active agent: autoregistration,
  low-level discovery, generated templates and an optional passive
  listener. Recetted on a real Zabbix 7.0 from a Linux and a Windows host.

- **The Azure Container Apps probe reports the collection's own state** in
  detail, and **follows every application of a subscription** when a
  discovery block is set, instead of one named application.

- **The image publication scans before it pushes**, and the dependency scan
  runs in the development chain, with the scanner a customer registry runs.
