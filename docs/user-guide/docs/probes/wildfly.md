<img src="https://design.jboss.org/wildfly/logo/final/wildfly_icon.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# WildFly / JBoss

The `wildfly` probe monitors WildFly (and JBoss EAP) via the HTTP Management
API, collecting JVM heap and GC metrics, Undertow web-container request
counters, JTA transaction statistics and per-datasource JDBC connection pool
metrics.

## Quick start

```yaml
probes:
  - name: wildfly
    type: wildfly
    params:
      endpoint: http://localhost:9990
      username: admin
      password: changeme
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:9990` | WildFly HTTP Management API base URL |
| `username` | `admin` | Management user username |
| `password` | — | Management user password |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.wildfly.up` | 1 | 1 when the Management API responded |
| `jvm.memory.heap.used` | By | JVM heap memory currently used |
| `jvm.memory.heap.max` | By | JVM maximum heap size |
| `jvm.gc.collections.count` | {collection} | GC collections by collector, tagged with `collector` |
| `tomcat.request.count` | {request} | HTTP requests processed by Undertow |
| `tomcat.request.error.count` | {error} | HTTP request errors |
| `tomcat.threads.current` | {thread} | Current Undertow thread pool size |
| `wildfly.transactions.committed` | {transaction} | JTA transactions committed |
| `wildfly.transactions.rolled_back` | {transaction} | JTA transactions rolled back |
| `wildfly.datasource.active` | {connection} | Active JDBC pool connections per datasource, tagged with `datasource` |
| `wildfly.datasource.available` | {connection} | Available connections in the JDBC pool |

## Operational notes

- Create a dedicated management user with the `Monitor` role: `bin/add-user.sh -u monitor -p password -g Monitor`.
- For WildFly domain mode, point the endpoint at the domain controller (port 9990).
- The probe uses the WildFly HTTP Management API (JSON over HTTP), not Jolokia — Jolokia is not required.
