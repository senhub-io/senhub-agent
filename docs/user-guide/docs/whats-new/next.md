# Next (unreleased)

Nothing released yet since 0.6.5. Changes land here as they are merged.

<div class="rn-filter"></div>

## Before you upgrade

- **`process` with `filter.top_n` now reports the per-name roll-up, not per-process series.** The probe used to report the `top_n` busiest processes one by one, each series identified by `process.pid` and `process.creation.time`. The top set changes all the time, so the identity of the series changed with it, and the number of distinct series kept growing far beyond the number of processes followed. Processes are still ranked on `top_by`, then grouped by name; the `top_n` names with the highest summed value are reported as `process.count`, `process.group.cpu.utilization` and `process.group.memory.usage`, tagged `process.name` only. Dashboards or alerts built on the per-process series of a `top_n` probe (`process.cpu.utilization`, `process.memory.usage` and the others) stop receiving data. To keep the previous output, set `filter.per_pid: true`; the agent then logs a warning at start, because those series grow with every new process id. `by_name` and `by_user` without `top_n` are unchanged, and so are the process entities. See [Process](../probes/process.md).
