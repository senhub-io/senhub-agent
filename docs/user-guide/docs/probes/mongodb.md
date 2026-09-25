<img src="../../assets/probe-logos/mongodb.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# MongoDB

The `mongodb` probe monitors a MongoDB server or replica set via `serverStatus`
and per-database `dbStats`, covering connections, operation throughput, memory
usage, replication state and database storage.

## Quick start

```yaml
# probes.d/20-mongodb.yaml — each file under probes.d/ is a YAML array of probes
- name: mongodb
  type: mongodb
  params:
    uri: mongodb://localhost:27017
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `uri` | In practice | `mongodb://localhost:27017` | Connection URI; credentials go in it (mongodb://user:pass@host:27017/admin?authSource=admin). A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file. Example: `mongodb://monitor:secret@db01:27017/admin?authSource=admin` |
| `direct_connection` | No | `true` | Connect to the named host only; false for Atlas or replica-set aware routing |
| `timeout` | No | `10` | Connection and command timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this server |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.mongodb.up` | 1 | 1 when the agent reached the MongoDB server this cycle |
| `mongodb.uptime` | s | Server uptime in seconds |
| `mongodb.connections.current` | {connection} | Current client connections |
| `mongodb.connections.available` | {connection} | Available connection slots |
| `mongodb.operations.count` | {operation} | Operations by type (insert/query/update/delete/getmore/command), tagged with `operation` |
| `mongodb.memory.usage` | By | Memory in use, tagged with `type` (resident, virtual) |
| `mongodb.index.count` | {access} | Index accesses by database, tagged with `database` |
| `mongodb.storage.size` | By | Storage allocated per database |
| `mongodb.document.operation.count` | {document} | Document count per database |

## Operational notes

- For authenticated clusters embed credentials in the URI: `mongodb://monitor:pass@host:27017/admin?authSource=admin`.
- Replica set members: set `direct_connection: false` and provide the replica set URI (`mongodb://host1,host2,host3/?replicaSet=rs0`) for topology-aware routing.
- For MongoDB Atlas, use the Atlas connection string and set `direct_connection: false`.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.mongodb.up` | `mongodb_up` | # | 1 if the agent reached the MongoDB server this cycle, 0 otherwise |
| `mongodb.uptime` | `mongodb_uptime` | s | Seconds since the mongod process started (uptimeMillis / 1000) |
| `mongodb.connections` | `mongodb_connections_active` | # | Number of active (in-use) client connections (connections.active) |
| `mongodb.connections` | `mongodb_connections_available` | # | Number of connections available for new clients (connections.available) |
| `mongodb.connections` | `mongodb_connections_current` | # | Total number of current open connections (connections.current) |
| `mongodb.network.io` | `mongodb_network_bytes_in` | BytesFile | Total bytes received over the network (network.bytesIn) |
| `mongodb.network.io` | `mongodb_network_bytes_out` | BytesFile | Total bytes sent over the network (network.bytesOut) |
| `mongodb.network.request.count` | `mongodb_network_requests` | # | Total distinct client requests received (network.numRequests) |
| `mongodb.operation.count` | `mongodb_operations_{operation}` | # | Total operations executed by type (opcounters.*) — insert/query/update/delete/getmore/command |
| `mongodb.memory.usage` | `mongodb_memory_{type}` | BytesMemory | Memory usage in bytes (mem.resident / mem.virtual — converted from MB) |
| `mongodb.document.operation.count` | `mongodb_documents_{operation}` | # | Document operations since startup (metrics.document.*) — deleted/inserted/returned/updated |
| `mongodb.cache.operations` | `mongodb_cache_{type}` | # | WiredTiger cache page operations — read: pages read into cache; write: pages written from cache |
| `mongodb.lock.acquire.wait_count` | `mongodb_active_reads` | # | Clients queued waiting for a read lock (globalLock.currentQueue.readers) |
| `mongodb.lock.acquire.wait_count` | `mongodb_active_writes` | # | Clients queued waiting for a write lock (globalLock.currentQueue.writers) |
| `mongodb.collection.count` | `mongodb_collections_{database}` | # | Number of collections in the database (dbStats.collections) |
| `mongodb.data.size` | `mongodb_data_size_{database}` | BytesFile | Uncompressed in-memory size of all documents (dbStats.dataSize) |
| `mongodb.index.count` | `mongodb_indexes_{database}` | # | Number of indexes across all collections (dbStats.indexes) |
| `mongodb.index.size` | `mongodb_index_size_{database}` | BytesFile | Total size of all indexes on disk (dbStats.indexSize) |
| `mongodb.storage.size` | `mongodb_storage_size_{database}` | BytesFile | Total amount of disk space allocated to all collections (dbStats.storageSize) |

<!-- schema:metrics:end -->
