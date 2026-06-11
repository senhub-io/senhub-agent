# Next (unreleased)

:material-progress-clock: In progress — these changes will ship as the next stable release.

<div class="rn-filter"></div>

## New

<ul class="rn">
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <code>snmp_poll</code> supports <strong>SNMPv3</strong> (USM auth + privacy, security level derived from the configured protocols). (#156)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <code>snmp_poll</code> gains <code>mib_paths</code>: a custom mapping may omit <code>metric</code> and have its name resolved from operator-supplied MIB files at startup. (#291)</li>
</ul>

## Changed

<ul class="rn">
<li><span class="tag t-removed">Changed</span> <span class="tag t-area">outputs</span> The never-implemented Zabbix HTTP endpoint (always 501) is removed; <code>endpoints: [zabbix]</code> now fails fast at startup. The Zabbix integration is redesigned as a native active agent, deferred (#169).</li>
</ul>

## Internal

<ul class="rn">
<li><span class="tag t-internal">Internal</span> Shared <code>snmpcore</code> package — one printability semantics, value rendering, version mapping and v3 USM tables consumed by both SNMP probes. (#291)</li>
</ul>
