# Next (unreleased)

:material-progress-clock: In progress — these changes will ship as the next stable release.

!!! warning "DRAFT — needs product review"
    This is the running list of changes landed since 0.2.0, accumulated here
    until the next stable release is cut (then it becomes that version's notes).
    It was seeded from the commits after the 0.2.0 tag — treat every line as a
    starting point:

    - Confirm what is **shipping vs still cooking** — some topology / SNMP work
      landed as "prep" / "not wired" and may be partial.
    - Decide whether the **open-source flip** is public yet (don't announce a
      public repo before it's actually public).
    - Rewrite the highlights in your own voice; tags and grouping are mine.

!!! highlight "Highlights (draft)"
    - **SNMP monitoring + network topology** — a new `snmp_poll` probe collects
      metrics from MIB modules and maps how your devices connect (via LLDP,
      routing tables, bridge tables and ARP).
    - **Native packages** — install from Linux `.deb` / `.rpm` packages or a
      signed Windows MSI.
    - **Tag everything** — add your own tags to every probe, agent-wide or
      per-probe.
    - **OTLP over HTTP** — push OpenTelemetry over HTTP, in addition to gRPC.

<div class="rn-filter"></div>

## New

<ul class="rn">
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <code>snmp_poll</code> probe — collects metrics from SNMP MIB modules.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <span class="tag t-area">topology</span> Network topology discovery — emits devices and how they connect (<code>adjacent_to</code> from LLDP, <code>routes_via</code> from the routing table, <code>forwards_to</code> from the bridge FDB, ARP convergence). <em>(May be partial — verify.)</em></li>
<li><span class="tag t-new">New</span> <span class="tag t-area">topology</span> Entity / topology rail — probes declare the systems they monitor; lifecycle tracking and OTLP entity emission (host, network devices, routes).</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">config</span> Universal tags — agent-wide <code>global_tags</code> and per-probe <code>custom_tags</code> on every probe.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">OTLP</span> OTLP/HTTP transport alongside gRPC.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">packaging</span> Linux <code>.deb</code> and <code>.rpm</code> packages.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">packaging</span> Signed Windows MSI installer (WiX).</li>
</ul>

## Improved

<ul class="rn">
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">config</span> Multi-file configuration is complete end-to-end (install / migrate / watch / check).</li>
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">CLI</span> Extensible command registry — commands self-register.</li>
</ul>

## Fixed

<ul class="rn">
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">CLI</span> Correct <code>--version</code> handling and rejection of unknown top-level arguments. (#134)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">memory</span> <code>swap_*</code> metrics now map to OpenTelemetry paging semantics. (#137)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">config</span> Tolerant numeric parameter decoding for YAML and JSON. (#136)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">syslog</span> Fall back to RFC 5424 keys when RFC 3164 ones are empty. (#135)</li>
</ul>

## Changed

<ul class="rn">
<li><span class="tag t-removed">Changed</span> <span class="tag t-area">license</span> Dropped the compact licence format — JWT-only (open-source preparation).</li>
</ul>

## Security

<ul class="rn">
<li><span class="tag t-security">Security</span> <span class="tag t-area">deps</span> Go toolchain bumped to 1.26.4 (standard-library CVE fixes).</li>
</ul>

## Internal

<ul class="rn">
<li><span class="tag t-internal">Internal</span> Open-source split — Apache-2.0 <code>LICENSE</code> + <code>NOTICE</code>, paid probes and enterprise tooling moved to a separate repository, customer/infra references anonymized, probes self-register. <em>(Confirm the public repo is live before announcing.)</em></li>
</ul>
