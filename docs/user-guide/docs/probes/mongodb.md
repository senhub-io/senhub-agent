<img src="https://cdn.simpleicons.org/mongodb" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# MongoDB

The `mongodb` probe monitors a MongoDB server or replica set via `serverStatus`
and per-database `dbStats`, covering connections, operation throughput, memory
usage, replication state and database storage.

## Quick start

```yaml
probes:
  - name: mongodb
    type: mongodb
    params:
      uri: mongodb://localhost:27017
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `uri` | `mongodb://localhost:27017` | MongoDB connection URI. Credentials can be embedded: `mongodb://user:pass@host:27017` |
| `direct_connection` | `true` | Connect directly to the specified host (skips topology discovery). Set to `false` for Atlas or replica-set-aware routing |
| `instance_name` | — | Override for the entity instance ID (stable name across restarts) |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.mongodb.up` | 1 | 1 when the agent reached the MongoDB server this cycle |
| `mongodb.uptime` | s | Server uptime in seconds |
| `mongodb.connections.current` | {connection} | Current client connections |
| `mongodb.connections.available` | {connection} | Available connection slots |
| `mongodb.operations.count` | {operation} | Operations by type (insert/query/update/delete/getmore/command), tagged with `operation` |
| `mongodb.memory.resident` | By | Resident (physical) memory used by the server |
| `mongodb.memory.virtual` | By | Virtual memory used by the server |
| `mongodb.locks.deadlock.count` | {deadlock} | Global lock deadlocks |
| `mongodb.index.accesses` | {access} | Index accesses by database, tagged with `database` |
| `mongodb.database.storage.size` | By | Storage allocated per database |
| `mongodb.database.document.count` | {document} | Document count per database |
| `mongodb.replica_set.state` | 1 | Replica set member state (1 = PRIMARY, 2 = SECONDARY, …) |

## Operational notes

- For authenticated clusters embed credentials in the URI: `mongodb://monitor:pass@host:27017/admin?authSource=admin`.
- Replica set members: set `direct_connection: false` and provide the replica set URI (`mongodb://host1,host2,host3/?replicaSet=rs0`) for topology-aware routing.
- For MongoDB Atlas, use the Atlas connection string and set `direct_connection: false`.
