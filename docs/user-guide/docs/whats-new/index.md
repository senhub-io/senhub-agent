# What's new

Release notes for SenHub Agent — what changed in each version, written to be
readable whether or not you write code. Every line is tagged by the **kind of
change** so you can skim for what matters to you.

## Tag legend

<p class="rn">
<span class="tag t-new">New</span> a new capability &nbsp;
<span class="tag t-improved">Improved</span> an existing feature got better &nbsp;
<span class="tag t-fixed">Fixed</span> a bug fix &nbsp;
<span class="tag t-perf">Performance</span> faster / lighter &nbsp;
<span class="tag t-security">Security</span> a security fix &nbsp;
<span class="tag t-breaking">Breaking</span> needs action on upgrade &nbsp;
<span class="tag t-deprecated">Deprecated</span> on its way out &nbsp;
<span class="tag t-removed">Removed</span> gone &nbsp;
<span class="tag t-internal">Internal</span> under-the-hood
</p>

Lines may also carry an **area** tag (the subsystem affected), e.g.
<span class="tag t-area">NetScaler</span> <span class="tag t-area">OTLP</span>
<span class="tag t-area">CLI</span>, so you can filter to the part you run.

## Releases

| Version | Date | Headline |
|---|---|---|
| [**Next — 0.6.0 (unreleased)**](next.md) | in progress | OTLP logs & traces, file-based license, cleaner Windows uninstall, Redis mTLS |
| [**0.5.2**](0.5.2.md) | 2026-07-10 | PowerStore depth, Veeam plugin jobs, pull-view ratio fix |
| [**0.5.1**](0.5.1.md) | 2026-07-06 | Dell PowerStore probe |
| [**0.5.0**](0.5.0.md) | 2026-07-06 | Security hardening (audit-360), secret store, signed Windows MSI |
| [**0.4.2**](0.4.2.md) | 2026-06-30 | Entity stabilization, Pro catalogue fix, systemd install hardening |
| [**0.4.1**](0.4.1.md) | 2026-06-23 | Auto-update reliability fix |
| [**0.4.0**](0.4.0.md) | 2026-06-22 | The entity model: host nameplate, network interfaces, compute VMs, attribute governance |
| [**0.3.2**](0.3.2.md) | 2026-06-17 | Probe catalog explosion — ~40 new free-tier probes and the first entity/topology model |
| [**0.2.3**](0.2.3.md) | 2026-06-12 | Security wave: signed updates, loopback defaults, OTLP ingress guard, SNMPv3 |
| [**0.2.2**](0.2.2.md) | 2026-06-11 | Active checks, exec + Prometheus scrape, alerting packs |
| [**0.2.1**](0.2.1.md) | 2026-06-10 | Topology-aware edge collector — five free collectors, open source |
| [**0.2.0**](0.2.0.md) | 2026-05-19 | Offline-first, OS-canonical paths — a major housekeeping release |
| 0.1.98 | 2026-05-17 | IBM i probe, multi-file config |
| 0.1.93 | 2026-05-17 | Prometheus output, OTel-first databases |

!!! note "Looking for an older line?"
    Switch the version selector (top bar) to **0.1** to read the docs as they
    were for the 0.1.x series. Release notes for every version stay here under
    **latest**.
