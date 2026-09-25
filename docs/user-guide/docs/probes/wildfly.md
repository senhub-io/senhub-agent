<img src="../../assets/probe-logos/wildfly.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# WildFly / JBoss

The `wildfly` probe monitors WildFly (and JBoss EAP) via the HTTP Management
API, collecting JVM heap and GC metrics, Undertow web-container request
counters, JTA transaction statistics and per-datasource JDBC connection pool
metrics.

## Quick start

```yaml
# probes.d/10-wildfly.yaml — each file under probes.d/ is a YAML array of probes
- name: wildfly
  type: wildfly
  params:
    endpoint: http://localhost:9990
    username: admin
    password: ${secret:wildfly.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:2b2e534993ce9b4dde368404c191f605049ac7d76e22a5275ef21cdc4ae3392d -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:9990` | Base URL of the management interface. Example: `http://wildfly.example.com:9990` |
| `username` | In practice | `admin` | Management user |
| `password` | In practice | - | Management user's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `timeout` | No | `10` | Request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this server instead of the one derived from the endpoint |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.wildfly.up` | 1 | 1 when the Management API responded |
| `jvm.memory.heap.used` | By | JVM heap memory currently used |
| `jvm.memory.heap.max` | By | JVM maximum heap size |
| `jvm.gc.collections.count` | {collection} | GC collections by collector, tagged with `collector` |
| `wildfly.request.count` | {request} | HTTP requests processed by Undertow |
| `wildfly.error.count` | {error} | HTTP request errors |
| `wildfly.datasource.connections.active` | # | In-use connections in the datasource pool |
| `wildfly.transaction.committed` | {transaction} | JTA transactions committed |
| `wildfly.transaction.rolledback` | {transaction} | JTA transactions rolled back |
| `wildfly.datasource.connections.active` | {connection} | Active JDBC pool connections per datasource, tagged with `datasource` |
| `wildfly.datasource.connections.available` | {connection} | Available connections in the JDBC pool |

## Operational notes

- Create a dedicated management user with the `Monitor` role: `bin/add-user.sh -u monitor -p password -g Monitor`.
- For WildFly domain mode, point the endpoint at the domain controller (port 9990).
- The probe uses the WildFly HTTP Management API (JSON over HTTP), not Jolokia — Jolokia is not required.

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.wildfly.up` | `senhub.wildfly.up` | WildFly Up | # | 1 when the WildFly Management API responded successfully; 0 otherwise |
| `jvm.memory.heap.used` | `jvm.memory.heap.used` | WildFly JVM Heap Used | B | JVM heap memory currently used |
| `jvm.memory.heap.committed` | `jvm.memory.heap.committed` | WildFly JVM Heap Committed | B | JVM heap memory committed to the JVM process |
| `jvm.memory.heap.max` | `jvm.memory.heap.max` | WildFly JVM Heap Max | B | Maximum JVM heap memory available |
| `wildfly.request.count` | `wildfly.request.count` | WildFly Requests | # | Total number of requests processed by Undertow |
| `wildfly.error.count` | `wildfly.error.count` | WildFly Errors | # | Total number of error responses from Undertow |
| `wildfly.bytes.sent` | `wildfly.bytes.sent` | WildFly Bytes Sent | B | Total bytes sent by Undertow |
| `wildfly.bytes.received` | `wildfly.bytes.received` | WildFly Bytes Received | B | Total bytes received by Undertow |
| `wildfly.transaction.committed` | `wildfly.transaction.committed` | WildFly Transactions Committed | # | Total number of committed JTA transactions |
| `wildfly.transaction.rolledback` | `wildfly.transaction.rolledback` | WildFly Transactions Rolled Back | # | Total number of aborted (rolled back) JTA transactions |
| `wildfly.datasource.connections.active` | `wildfly.datasource.connections.active` | WildFly DS {datasource} Active Connections | # | Number of active (in-use) connections in the JDBC datasource pool |
| `wildfly.datasource.connections.available` | `wildfly.datasource.connections.available` | WildFly DS {datasource} Available Connections | # | Number of available (idle) connections in the JDBC datasource pool |

<!-- schema:metrics:end -->
