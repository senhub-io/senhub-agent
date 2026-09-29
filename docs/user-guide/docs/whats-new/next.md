# Next (unreleased)

Changes since 0.6.0, collected as they are merged.

<div class="rn-filter"></div>

## Fixes

- **The console's Agent card, `senhub-agent status` and `/health` report
  measured values.** CPU was a constant 0 %, and "Memory" was the Go
  heap, about a tenth of what the operating system charges to the agent.
  Memory is now the resident set (the working set on Windows), with the
  heap alongside, and CPU the agent's share of the whole machine over the
  last interval. Uptime counts from the process start. `/health` returned
  `"version": "HTTP Strategy v1.0"` and `info/system` a nested version of
  `"unknown"`; both carry the release now. `info/system` reported
  `cache.probe_count: 0` and a `cache.memory_usage` that was the process
  heap: the first is the real count, the second is gone.
