# Next (unreleased)

<div class="rn-filter"></div>


## Fixed

### Hosts opted into betas now converge to the stable release

A host running `auto_update.include_beta: true` resolved `latest` to the newest
**beta** and stayed there — it never moved to the stable release that
superseded it. A recette host opted into betas silently stopped tracking
production.

Each channel is published with an alias record first, carrying the resolved
version (`{"latest", "0.5.3"}`, `{"latest-beta", "0.5.3-beta"}`), and the merge
of the two channels de-duplicates by version keeping the first record — so for
the newest release the alias record is usually the only one left. Version
selection then discarded records named `latest`, which made the newest stable
release invisible, while the beta alias, named `latest-beta`, escaped the same
filter and won.

Selection now looks at the version a record carries, never at the name of the
record. A beta genuinely ahead of the newest stable still wins, so opting into
betas keeps delivering them.

Stable hosts were never affected: they resolve `latest` through a different
path. (#730)

### No more registry warning on every HTTP push for the log conduit probes

`filetail`, `linux_logs`, `windows_eventlog` and `snmp_trap` publish their
records straight to the log rail; the only datapoints they hand to the pull cache
are their own throughput and health counters. Those probe types were not declared
in the cache's discriminant-tag registry, so every HTTP-strategy push logged:

```
Probe type not in DiscriminantTagsRegistry - using no discriminant tags
  metric_name=senhub.filetail.records_emitted probe_type=filetail
```

The four types are now declared with an empty discriminant set, which is the
correct shape rather than a gap: these counters carry no per-instance tag, and
the probe name is already part of every cache key — so two `filetail` probes
have always produced two distinct series, and still do. Only the log noise
changes. (#724)
