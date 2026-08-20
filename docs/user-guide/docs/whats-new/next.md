# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Breaking Changes

- **PRTG push channels are now named like pull channels.** The same
  measurement reached PRTG under two different names depending on how it
  travelled: the agent pushed `cpu_core_usage` while a PRTG pull showed
  `CPU Core 0 Usage`. Both now resolve the display name from the probe's
  definition, so one device has one channel set.

    | Before (push) | After (push and pull) |
    |---|---|
    | `cpu_core_usage` | `CPU Core 0 Usage` |
    | `mem_used_percent` | `Memory Used %` |

    **PRTG treats a renamed channel as a new one**, so a sensor fed by the
    push strategy will start fresh channels and lose the history attached
    to the old names. Plan the upgrade of push-fed sensors accordingly.
    Pull-fed sensors (the HTTP Data Advanced ones) are unaffected: their
    names do not change.

    A probe that sets an explicit `prtg_metric_id` still names its own
    channel — that override is unchanged.
