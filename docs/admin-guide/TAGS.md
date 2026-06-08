# Global and custom tags

SenHub Agent can attach operator-defined tags to every datapoint, so the
backend can correlate metrics across sources by site, tenant, region, etc.
Two levels are available:

- **`global_tags`** — set once on the agent; applied to **every datapoint of
  every probe**.
- **`custom_tags`** — set per probe instance; applied to **that probe's**
  datapoints only.

Both apply uniformly across all outputs (PRTG, Nagios, Prometheus, OTLP,
SenHub cloud) — there is no per-output configuration.

> **OTLP placement.** On the OTLP output, `global_tags` are emitted as
> **Resource attributes** (they describe the agent/host as a whole, so
> they sit on the one process-level Resource instead of being repeated on
> every series — lower cardinality). `custom_tags` are per-probe, so they
> stay metric attributes. Other outputs (Prometheus, PRTG, …) carry both
> as labels as before.

## Priority

On a key conflict, the most specific value wins:

```
probe custom_tags  >  agent global_tags  >  built-in probe tags
```

So a probe's `custom_tags: {site: paris}` overrides a `global_tags:
{site: lyon}`, which in turn overrides a `site` tag the probe emits itself.

## Configuration

`agent.yaml`:

```yaml
agent:
  key: "…"
  global_tags:
    site: "site-a"
    region: "ouest"
    tenant: "acme"
```

A probe (in `probes.d/…` or the monolithic `probes:` list):

```yaml
- name: db_prod
  type: postgresql
  custom_tags:
    environment: "production"
    cost_center: "CC-1042"
  params:
    host: "10.0.1.5"
    port: 5432
```

## Cardinality

Every tag becomes a label on every series. Keep both sets small — well under
~10 keys total — and use **low-cardinality** values (a site name, not a
per-request id). High-cardinality tags multiply your stored series and can
overwhelm the backend.

Both fields are optional and additive; existing configurations need no
changes and no version bump.
