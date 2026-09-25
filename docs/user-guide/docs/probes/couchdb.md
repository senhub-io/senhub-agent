<img src="../../assets/probe-logos/couchdb.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# CouchDB

The `couchdb` probe monitors a CouchDB node via the `/_node/_local/_stats`
HTTP endpoint, reporting HTTP request counts (by method and status code),
database read/write throughput and I/O byte counters.

## Quick start

```yaml
# probes.d/10-couchdb.yaml — each file under probes.d/ is a YAML array of probes
- name: couchdb
  type: couchdb
  params:
    endpoint: http://localhost:5984
    username: admin
    password: ${secret:couchdb.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:5984` | Base URL of the node. Example: `http://couch01:5984` |
| `username` | In practice | - | Admin user; the stats endpoint needs admin credentials by default |
| `password` | In practice | - | Admin user's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this node |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.couchdb.up` | 1 | 1 when the CouchDB node answered the stats endpoint |
| `couchdb.httpd.requests` | {request} | Total HTTP requests processed |
| `couchdb.httpd.method.requests` | {request} | HTTP requests by method (GET/POST/PUT/DELETE/COPY/HEAD), tagged with `method` |
| `couchdb.httpd.status.responses` | {response} | Responses by HTTP status class (2xx/3xx/4xx/5xx), tagged with `status` |
| `couchdb.database.reads` | {read} | Database read operations |
| `couchdb.database.writes` | {write} | Database write operations |

## Operational notes

- The `/_node/_local/_stats` endpoint requires admin credentials by default.
- Metrics align with the OpenTelemetry Collector contrib `couchdbreceiver` naming where equivalents exist.

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
| `senhub.couchdb.up` | `senhub.couchdb.up` | CouchDB Up | # | 1 when the CouchDB node answered the stats endpoint, 0 otherwise |
| `couchdb.httpd.requests` | `couchdb.httpd.requests` | CouchDB HTTP Requests | # | Total number of HTTP requests processed by CouchDB |
| `couchdb.httpd.method.requests` | `couchdb.httpd.method.requests` | CouchDB HTTP {method} Requests | # | HTTP requests broken down by method (GET, POST, PUT, DELETE) |
| `couchdb.httpd.status.responses` | `couchdb.httpd.status.responses` | CouchDB HTTP {status} Responses | # | HTTP responses broken down by status code (200, 201, 400, 401, 404, 500) |
| `couchdb.open.databases` | `couchdb.open.databases` | CouchDB Open Databases | # | Number of databases currently open |
| `couchdb.open.files` | `couchdb.open.files` | CouchDB Open OS Files | # | Number of file descriptors currently open by CouchDB |
| `couchdb.database.reads` | `couchdb.database.reads` | CouchDB Database Reads | # | Total number of database read operations |
| `couchdb.database.writes` | `couchdb.database.writes` | CouchDB Database Writes | # | Total number of database write operations |
| `couchdb.io.bytes.read` | `couchdb.io.bytes.read` | CouchDB IO Bytes Read | B | Total bytes read from disk by CouchDB (io_input) |
| `couchdb.io.bytes.written` | `couchdb.io.bytes.written` | CouchDB IO Bytes Written | B | Total bytes written to disk by CouchDB (io_output) |

<!-- schema:metrics:end -->
