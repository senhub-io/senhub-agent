<img src="https://cdn.simpleicons.org/couchdb" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:5984` | CouchDB base URL |
| `username` | — | Admin username |
| `password` | — | Admin password — reference via `${secret:couchdb.password}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.couchdb.up` | 1 | 1 when the CouchDB node answered the stats endpoint |
| `couchdb.httpd.requests` | {request} | Total HTTP requests processed |
| `couchdb.httpd.method.requests` | {request} | HTTP requests by method (GET/POST/PUT/DELETE/COPY/HEAD), tagged with `method` |
| `couchdb.httpd.status.responses` | {response} | Responses by HTTP status class (2xx/3xx/4xx/5xx), tagged with `status` |
| `couchdb.database.reads` | {read} | Database read operations |
| `couchdb.database.writes` | {write} | Database write operations |
| `couchdb.httpd.bulk_requests` | {request} | Bulk document requests |

## Operational notes

- The `/_node/_local/_stats` endpoint requires admin credentials by default.
- Metrics align with the OpenTelemetry Collector contrib `couchdbreceiver` naming where equivalents exist.
