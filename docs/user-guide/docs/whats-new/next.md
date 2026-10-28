# Next (unreleased)

Nothing released yet since 0.6.5. Changes land here as they are merged.

<div class="rn-filter"></div>

## Process probe: `top_n` reports a per-name roll-up

With `filter.top_n`, the `process` probe now ranks process names and reports the per-name roll-up of the top, without `process.pid` and `process.creation.time` on the series. Short-lived processes no longer create new series each time they enter the top. Per-process series under `top_n` remain available with `detail.per_pid: true`, which `agent config check` flags with a cardinality warning.
