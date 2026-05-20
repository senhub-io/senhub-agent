---
title: Cloud output (senhub strategy)
paths:
  - internal/agent/services/data_store/strategies/senhub/**
---

## What this is

The `senhub` strategy pushes batches of datapoints to the SenHub cloud intake (`intake.senhub.io`). It is the **only** push-to-SaaS path. It runs in addition to (not instead of) on-prem outputs like PRTG/Nagios/Prometheus.

Default cadence: every 5 seconds (`DEFAULT_SENHUB_INTERVAL`). The agent's authentication key is sent on every batch as Bearer / URL parameter (depending on the endpoint).

## Buffer-driven flow

The strategy is the only consumer of the in-memory `Buffer` (see `data-store` rule):

1. `AddDataPoints` appends to the buffer.
2. A ticker (`periodic_scheduler`) calls `buffer.Sync()` every `DEFAULT_SENHUB_INTERVAL`.
3. The synced batch goes through `validators` then HTTP POST.
4. On HTTP failure, `buffer.AbortSync(batch)` puts the batch back so the next tick retries.

## Failure modes to handle

- **Network down**: `AbortSync` — batch retries on next tick. Don't drop.
- **Auth rejected (401/403)**: log Error, keep retrying. The user may be rotating credentials; the agent should recover automatically once a valid key is back.
- **400 / 422 (payload malformed)**: log Error with first ~200 bytes of payload, **drop** the batch (don't loop forever on bad data).
- **5xx**: `AbortSync` — server-side issue, will recover.
- **Buffer full**: log Warn, oldest entries are evicted (ring semantics). Indicates the intake is unreachable for longer than `retention_minutes`.

## Format

The on-the-wire format is SenHub-proprietary (legacy from before OTel-first). Don't change it without coordinating with the cloud side. New metric names from probes flow through without code changes here.

## Tags & labels

`senhub` strategy emits ALL probe tags as labels. `IncludeProbeTags` is implicit (not gated by config). This includes the OTel-canonical resource-like attributes (`db.system.name`, `server.address`, `server.port`, etc.) — they end up as cloud labels.

## Authentication

- Agent key (UUID) is the only identity. License JWT is sent separately for enforcement.
- Both come from `LocalConfiguration` (or `RemoteConfiguration` in online mode).
- Never log the agent key or license token. Always mask via `***` if dumping for debugging.

## What NOT to do

- Don't add per-metric logic here. This strategy is engine-agnostic. Engine-specific shaping lives in probe code + transformer YAML.
- Don't bypass the buffer. Direct HTTP from probe code creates back-pressure issues and breaks retry semantics.
- Don't introduce a parallel cloud endpoint. There's one intake. If a new region/tenant is needed, plumb it through configuration, not by adding a new strategy.
