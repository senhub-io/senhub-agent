# Next (unreleased)

:material-progress-clock: In progress — these changes will ship as the next stable release.

!!! warning "DRAFT — needs product review"
    The running list of changes landed since 0.2.0, accumulated here until the
    next stable release is cut (then it becomes that version's notes). Curated
    from the real commits — before release, please:

    - Confirm what is **shipping vs still maturing** (some network-topology work
      is recent and may be partial in a first cut).
    - Decide whether the **open-source flip** is public yet — don't announce a
      public repo before it actually is.
    - Adjust the highlights to your own voice.

!!! highlight "Highlights"
    - **Five new collectors — all free.** Poll SNMP devices, receive SNMP
      traps, ingest OpenTelemetry from other agents, tail flat-file logs, and
      read the Windows Event Log.
    - **Network topology discovery.** The agent now maps how your devices
      connect — from SNMP (LLDP, routing, bridge tables) and host routing/ARP.
    - **Native packages.** Install from Linux `.deb` / `.rpm` or a signed
      Windows MSI.
    - **Runs without root.** The Linux daemon no longer needs root — only
      installing/removing the service does.
    - **Tag everything.** Add your own tags agent-wide or per probe.

<div class="rn-filter"></div>

## New

<ul class="rn">
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <code>snmp_poll</code> probe — collect metrics from SNMP MIB modules, with dynamic per-OID typed metrics and resolution from operator-supplied local MIB files. <em>(Free tier.)</em> (#156)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <code>snmp_trap</code> probe — receive SNMP v2c / v3 traps (and acknowledge v2c informs) and forward them as OpenTelemetry logs. <em>(Free tier.)</em> (#161)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">OTLP</span> <code>otlp_receiver</code> probe — embed an OTLP receiver so the agent can act as an edge collector: OpenTelemetry in (gRPC/HTTP) → out to all sinks. <em>(Free tier.)</em> (#173)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">Windows</span> <code>windows_eventlog</code> probe — stream the Windows Event Log as OpenTelemetry logs. <em>(Free tier.)</em> (#154)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">logs</span> <code>filetail</code> probe — tail any flat-file log and forward it as OpenTelemetry logs. <em>(Free tier.)</em> (#155)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">topology</span> Network topology discovery — the agent emits devices and how they connect: <code>adjacent_to</code> (LLDP), <code>routes_via</code> (routing table + ARP) and <code>forwards_to</code> (bridge forwarding table), from both SNMP and host routing.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">topology</span> Entity rail — probes declare the systems they monitor; the agent tracks their lifecycle and emits them as OpenTelemetry entities.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">config</span> Universal tags — agent-wide <code>global_tags</code> and per-probe <code>custom_tags</code> on every probe.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">OTLP</span> OTLP/HTTP transport, alongside the existing gRPC.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">packaging</span> Linux <code>.deb</code> and <code>.rpm</code> packages.</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">packaging</span> Signed Windows MSI installer (WiX).</li>
</ul>

## Improved

<ul class="rn">
<li><span class="tag t-improved">Improved</span> <span class="tag t-area">config</span> Multi-file configuration is complete end-to-end (install / migrate / watch / check).</li>
</ul>

## Changed

<ul class="rn">
<li><span class="tag t-removed">Changed</span> <span class="tag t-area">license</span> The compact licence format was dropped — licences are JWT-only now (open-source preparation).</li>
</ul>

## Fixed

<ul class="rn">
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">security</span> <span class="tag t-area">CLI</span> The Linux daemon now runs without root — only installing/removing the service needs elevated rights. (#223)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">CLI</span> Correct <code>--version</code> handling and rejection of unknown top-level arguments. (#134)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">memory</span> <code>swap_*</code> metrics now map to OpenTelemetry paging semantics. (#137)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">config</span> Tolerant numeric parameter decoding for YAML and JSON. (#136)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">syslog</span> Fall back to RFC 5424 keys when RFC 3164 ones are empty. (#135)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">SNMP</span> <code>snmp_trap</code> owns its UDP listener so traps arrive on Windows.</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">Windows</span> <code>windows_eventlog</code> drives the wevtapi pull subscription correctly (it was draining nothing).</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">filetail</span> Strip the trailing carriage return from Windows CRLF log lines.</li>
</ul>

## Security

<ul class="rn">
<li><span class="tag t-security">Security</span> <span class="tag t-area">deps</span> Go toolchain bumped to 1.26.4 (standard-library CVE fixes).</li>
</ul>

## Internal

<ul class="rn">
<li><span class="tag t-internal">Internal</span> Open-source split — Apache-2.0 <code>LICENSE</code> + <code>NOTICE</code>, paid probes and enterprise tooling moved to a separate repository, customer/infra references anonymized, probes self-register. <em>(Confirm the public repo is live before announcing.)</em></li>
<li><span class="tag t-internal">Internal</span> <code>probesdk</code> — a public mirror of the probe API plus an entity-detection API, so out-of-tree probes can emit metrics and entities.</li>
</ul>
