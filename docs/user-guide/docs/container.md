# Running the agent in a container

The image carries one static binary, an unprivileged user, and an
entrypoint that writes a configuration when none is mounted. Everything
else is a parameter.

```bash
docker run -d --name senhub-agent \
  -e OTLP_BEARER_TOKEN=<your token> \
  -v senhub-state:/var/lib/senhub-agent \
  ghcr.io/senhub-io/senhub-agent:0.6.0
```

That is the whole of it for a first run: one variable, one mount.

## The one mount that matters

`/var/lib/senhub-agent` holds everything that makes this agent *this*
agent: its host identity, its own key, and the bookmarks its log probes
keep. **Mount it, or every container arrives as a new host** and its
log probes lose their place: a Container Apps stream re-sends its last
`tail_lines`, a file probe skips what was written in between. The
entrypoint says so on startup when the directory is not a mounted
volume, naming only what `SENHUB_HOST_ID` and `SENHUB_AGENT_KEY` do not
already carry.

Two identities live there, and they answer different questions.

`machine-id` is the host identity. It is what a container has none of:
on a real machine the operating system provides it, and the agent reads
it to decide which host its measurements belong to. Without one the
underlying library falls back to the kernel's boot identifier, which it
documents as not stable, so each container is seen as a different
machine. Measured on a real deployment: three runs of the same image,
three hosts in the graph.

`agent.key` is the agent identity. It is generated on first start and it
is what the receiving side reads to tell two agents apart, so two agents
carrying the same key are one agent to everything downstream. Keeping it
in the volume means one agent per volume, not one per container.

If your platform already knows what this host is, `SENHUB_HOST_ID`
settles the first without a volume, and `SENHUB_AGENT_KEY` the second.
With both set, a container without a volume comes back as the same host
and the same agent. Give each instance its own values: an identity
shared between several running agents is worse than one that changes,
because nothing signals it, and an example or blank value is refused at
start.

What survives a new container without a volume: the host identity and
the agent identity when those two variables are set, the configuration
the variables describe. What does not: the log bookmarks, so a
Container Apps stream re-sends its recent lines and a file probe skips
what was written in between, and anything written to the configuration
from the console.

The host's *name* is another matter: it is the container's host name,
which Docker sets to the container id unless told otherwise, so a new
container shows up under a new name while keeping the same identity.
Give it one that means something with `--hostname` (`hostname:` in a
Compose file).

Nothing else needs a mount. The configuration lives inside the container
unless you choose otherwise, and the log file is written to
`/var/log/senhub-agent` inside it.

## Variables

None is strictly required. `OTLP_BEARER_TOKEN` is what a first run
needs to export to SenHub; the rest have defaults or are only read when
the feature they configure is wanted. These variables are read only when
the entrypoint writes the configuration, that is when no `agent.yaml` is
present (see [Bringing your own configuration](#bringing-your-own-configuration)).
The `SENHUB_PROBE_<NAME>_*` variables are the exception: the agent itself
reads them, so they apply with a configuration of your own too (see
[Probes from variables](#probes-from-variables)).

| Variable | Required | Default | What it does |
|---|---|---|---|
| `OTLP_BEARER_TOKEN` | No (needed to export to SenHub) | - | Authenticates the export to SenHub, sent as `Authorization: Bearer` and resolved from the environment at every start, never written to a file. Without it and without `SENHUB_OTLP_ENDPOINT`, no OTLP output is written: the agent collects and serves its local HTTP endpoints but exports nothing, and the entrypoint says so on startup. With `SENHUB_OTLP_ENDPOINT` set, the export goes out without an `Authorization` header |
| `SENHUB_OTLP_ENDPOINT` | No | `eu-west-1.intake.senhub.io:443` when `OTLP_BEARER_TOKEN` is set | Another collector: your own OpenTelemetry collector, VictoriaMetrics, Grafana Alloy |
| `SENHUB_OTLP_PROTOCOL` | No | `grpc` | `grpc` or `http`, the latter for a backend that ingests OTLP over HTTP |
| `SENHUB_ENTITIES` | No | `true` | Sends the entities (this host, the agent, what its probes watch) a topology backend builds its map from. `false` exports measurements and logs only. Any value other than `true` or `false` stops the container |
| `SENHUB_OTLP_TLS` | No | `true` | `false` for a collector that listens in plain text, such as a sidecar on `localhost:4317`. The token then crosses the network unencrypted: keep it to the same host or a trusted network. Any value other than `true` or `false` stops the container |
| `SENHUB_LICENSE` | No | - | Licence token, for the probes that need one |
| `SENHUB_TAGS` | No | - | Tags on every metric, as `key=value,key2=value2` |
| `SENHUB_ZABBIX_SERVER` | No | - | Zabbix server or proxy, `host:port`: writes the Zabbix output, and the container registers in Zabbix at its first contact |
| `SENHUB_ZABBIX_HOST_METADATA` | No | `senhub-agent` | Host metadata the Zabbix autoregistration action matches |
| `SENHUB_HTTP_PORT` | No | `8080` | Port of the console and of the PRTG, Nagios and Prometheus endpoints |
| `SENHUB_HTTP_BIND` | No | `0.0.0.0` | Address the console and the PRTG, Nagios and Prometheus endpoints listen on. The container's own loopback is reachable by no one, so the image listens on every address and relies on the container network; `127.0.0.1` keeps them inside the container |
| `SENHUB_CONFIG_DIR` | No | `/etc/senhub-agent` | Where the configuration is read and written |
| `SENHUB_STATE_DIR` | No | `/var/lib/senhub-agent` | Where the identity, the key, the bookmarks and the logs queue (`otlp-queue/`) live |
| `SENHUB_LOG_QUEUE` | No | `true` | `false` stops the agent keeping failed log batches on disk during an OTLP outage |
| `SENHUB_LOG_QUEUE_RETENTION` | No | `24h` | Age after which a queued log batch is dropped; `0` = no limit |
| `SENHUB_LOG_QUEUE_MAX_BYTES` | No | `134217728` (128 MiB) | Disk cap of the logs queue, in bytes or with a suffix (`64MiB`). The queue lives in the state directory: put it on a persistent volume (a File Share on Container Apps) for logs to survive a restart; see [Logs survive an outage](otlp.md#logs-survive-an-outage) |
| `SENHUB_HOST_ID` | No | kept in the state directory | Host identity, 32 hexadecimal characters, dashes optional. One value per instance: an example or blank value (all zeros, `01234567-89ab-cdef-…`) is refused at start, and the host entity is marked `senhub.host.id.source=configuration` |
| `SENHUB_AGENT_KEY` | No | kept in the state directory | Agent identity, a UUID. One value per instance; with `SENHUB_HOST_ID` it lets a container without a volume keep one identity |
| `SENHUB_PROBES` | No | - | YAML of the probes to run, as a `probes.d` file would hold it. For one probe at a time, the `SENHUB_PROBE_<NAME>_*` variables are shorter, see [Probes from variables](#probes-from-variables) |
| `SENHUB_OUTPUT` | No | - | YAML of one more output, as a `strategies.d` file would hold it |

Without `SENHUB_AGENT_KEY` the agent generates its own key on first
start, and the entrypoint keeps it in the state directory so the next
container reuses it. That is why the mount matters.

Never bake either identity into an image. An image is deployed in
several copies by construction, so an identity that belongs to the image
belongs to all of its instances at once.

## Starting other probes

The variables above configure the agent. The probes are configuration of
their own, and there are sixty-six types of them, so they are not each
given a variable each. Three ways in, from the least to the most work, and a fourth that adds to any of them:

### A variable carrying the fragment

`SENHUB_PROBES` holds the same YAML a file in `probes.d` would. It is
the way in on a platform where placing a file is harder than setting a
variable, which is the usual case for a managed container service:

```yaml
SENHUB_PROBES: |
  - name: db
    type: mysql
    params:
      host: "${env:DB_HOST}"
      username: monitor
      password: "${env:DB_PASSWORD}"
  - name: web
    type: http_check
    params:
      targets: ["https://example.test/health"]
```

A `${env:...}` reference inside it is resolved by the agent at every
start, so a password is set as its own variable and never appears in
this one. `SENHUB_OUTPUT` does the same for one output.

### A mounted drop-in directory

Mount `/etc/senhub-agent/probes.d` and drop one file per probe. The
entrypoint still writes `agent.yaml` and the output from the variables,
so you keep the one-variable start and add only what the platform can
mount.

### A whole mounted configuration

Mount `/etc/senhub-agent` whole. Nothing is written and every variable
is ignored, which the entrypoint says on startup. This door has a trap
of its own when the same directory serves several instances: see
[Bringing your own configuration](#bringing-your-own-configuration).

Whichever you choose, the configuration is checked before the agent
starts: a container whose variables produce a file the agent would
refuse stops with the reason rather than restarting in a loop.

### Probes from variables

`SENHUB_PROBE_<NAME>_TYPE` declares a probe and `SENHUB_PROBE_<NAME>_<PARAM>` sets
its parameters, one variable each, typed from the probe's schema. It is a
rule of the agent, not of the image: it applies the same way under systemd,
Helm or Podman, and it applies **even when you mount your own
configuration**, where it adjusts the probes of your files instead of being
ignored. A secret is read from a file with the `_FILE` suffix, as Docker
and Kubernetes deliver them.

```yaml
SENHUB_PROBE_DB_TYPE: mysql
SENHUB_PROBE_DB_HOST: db.internal
SENHUB_PROBE_DB_USERNAME: monitor
SENHUB_PROBE_DB_PASSWORD_FILE: /run/secrets/db_password
```

The whole rule, with the precedence over `SENHUB_PROBES` and the files, is
on [Configuration](configuration.md#configuring-probes-from-environment-variables).
Reading Azure Container Apps takes the same form, see
[Azure Container Apps](probes/azure_container_apps.md#from-environment-variables).

## Bringing your own configuration

Mount a directory on `/etc/senhub-agent` and the entrypoint writes
nothing: your files win and every `SENHUB_*` variable is ignored. The
entrypoint says which of the two it did on startup, so a container that
ignores your variables tells you why.

```bash
docker run -d --name senhub-agent \
  -v /srv/senhub/conf:/etc/senhub-agent \
  -v senhub-state:/var/lib/senhub-agent \
  ghcr.io/senhub-io/senhub-agent:0.6.0
```

Your configuration wins completely, and that includes the agent key
under `agent: key:`. The entrypoint does not touch it, so a
configuration shared between several instances makes them one agent: the
receiving side reads that key to tell agents apart, and their health
metrics land on the same series with different values. Nothing signals
it, and the counters it hides are the ones read when looking for missing
data.

With a mounted configuration, each instance must therefore carry its own
key. Reference it from the environment, which works because substitution
is applied to the whole file before it is parsed, and give every
instance its own `SENHUB_AGENT_KEY`:

```yaml
agent:
  key: "${env:SENHUB_AGENT_KEY}"
```

The agent resolves that reference itself, even though the entrypoint
ignores `SENHUB_*` variables in this mode. Do not leave the key out or
empty: the loader refuses a configuration whose agent key is empty, and
the container stops at start.

## What the container does not do

- **No service commands.** `install`, `start`, `stop` and their kin
  manage a system service and have no meaning here. The agent is the
  container's main process.
- **No privileges.** The image runs as uid 10001. Nothing in the agent
  needs root to run, but a few things need access the container may not
  have: reading the host's journal needs that journal mounted, the
  dependency discovery cannot attribute sockets to processes without
  the host's process namespace, and the hardware serial is not readable
  from inside a container.
- **No auto-update.** Updating means pulling a new image tag.

## Health

The image declares a health check against the console's `/health`. It
answers as soon as the HTTP output is listening, which is what tells a
scheduler the agent is up.
