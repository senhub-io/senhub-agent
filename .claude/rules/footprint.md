---
title: Footprint — what the agent costs when it does nothing new
paths:
  - internal/agent/**
---

## Rules

1. **A line emitted once per collection or export cycle in healthy
   operation is Debug.** Info is for state changes: startup, shutdown,
   a connection established or lost, the first success after a failure,
   a configuration applied. Warn and Error are for abnormal conditions.
   "Collected N metrics", "pushed batch", "tick" and "doCall ok" are
   Debug.
2. **A disabled output or feature allocates nothing.** No buffer, queue,
   goroutine, client, listener or file handle is created for something
   the configuration turned off.
3. **No ticker or goroutine for an inactive function.** A timer exists
   only while the thing it drives is enabled, and stops when it is
   disabled or reconfigured away.

## Checking a change

Run the agent for two minutes at Info level on the output of
`agent config init` and read the log: after startup, a healthy agent
should add no line per cycle. A repeating Warn on a persistent abnormal
condition is a separate problem to rate-limit, not a reason to keep a
healthy-path line at Info.
