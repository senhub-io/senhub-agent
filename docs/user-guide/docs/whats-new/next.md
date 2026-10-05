# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Before you upgrade

- **Zabbix counter items become rates.** In the generated templates, every
  item built from a cumulative counter (network bytes and packets, disk
  I/O, CPU time, request totals) now carries the Change per second
  preprocessing step and a per-second unit (`Bps`, `/s`, `s/s`). The
  items keep their keys, so after you re-run `zabbix setup` and the
  templates are updated, the same item switches from a total to a rate:
  its history from before the upgrade holds totals and the new values are
  rates, so a graph spanning the upgrade shows a step. Rewrite the
  triggers and calculated items you wrote against these items to compare
  a rate.
