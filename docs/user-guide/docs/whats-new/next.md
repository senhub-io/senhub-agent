# Next (unreleased)

<div class="rn-filter"></div>


## Fixed

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
