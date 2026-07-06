<img src="https://upload.wikimedia.org/wikipedia/commons/f/fe/Apache_Tomcat_logo.svg" alt="" class="probe-page-logo probe-page-logo-wm">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache Tomcat

The `tomcat` probe monitors Apache Tomcat via Jolokia HTTP REST, reporting
active HTTP sessions, request throughput, JVM heap and garbage collection,
and the Tomcat thread pool state.

## Quick start

```yaml
# probes.d/10-tomcat.yaml — each file under probes.d/ is a YAML array of probes
- name: tomcat
  type: tomcat
  params:
    jolokia_url: http://localhost:8080/jolokia
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `jolokia_url` | `http://localhost:8080/jolokia` | URL to the Jolokia agent endpoint on the Tomcat instance |
| `username` | — | Jolokia Basic-auth username (if required) |
| `password` | — | Jolokia Basic-auth password — reference a stored secret via `${secret:tomcat.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.tomcat.up` | 1 | 1 when Jolokia is reachable |
| `tomcat.sessions.active` | {session} | Active HTTP sessions per web application context, tagged with `context` |
| `tomcat.request.count` | {request} | Requests processed per connector, tagged with `connector` |
| `tomcat.request.error.count` | {error} | Request errors per connector |
| `tomcat.request.elapsed_time` | s | Total time spent on requests per connector |
| `tomcat.threads.current` | {thread} | Current thread pool size |
| `tomcat.threads.busy` | {thread} | Threads currently handling a request |
| `tomcat.threads.max` | {thread} | Maximum thread pool size |
| `jvm.memory.heap.used` | By | JVM heap in use |
| `jvm.memory.heap.max` | By | JVM maximum heap size |
| `jvm.gc.collections.count` | {collection} | GC collections by collector (Minor/Major), tagged with `collector` |

## Operational notes

- Jolokia must be deployed in Tomcat as a WAR or Java agent. The Jolokia WAR can be deployed at `/jolokia`; the Java agent is added to `CATALINA_OPTS`.
- Multiple Tomcat instances on the same host can be monitored with multiple probe entries pointing to different Jolokia URLs.
