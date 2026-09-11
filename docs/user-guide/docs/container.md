# Running the agent in a container

The image carries one static binary, an unprivileged user, and an
entrypoint that writes a configuration when none is mounted. Everything
else is a parameter.

```bash
docker run -d --name senhub-agent \
  -e OTLP_BEARER_TOKEN=<your token> \
  -v senhub-state:/var/lib/senhub-agent \
  ghcr.io/senhub-io/senhub-agent:0.5.5-beta
```

That is the whole of it for a first run: one variable, one mount.

## The one mount that matters

`/var/lib/senhub-agent` holds the agent's own key and the bookmarks its
log probes keep. **Mount it, or every restart gives this agent a new
identity** and re-reads the tail of every log it follows. The agent says
so on startup when the directory is not writable.

Nothing else needs a mount. The configuration lives inside the container
unless you choose otherwise, and the log file is written to
`/var/log/senhub-agent` inside it.

## Variables

One is required. The rest have defaults or are only read when the
feature they configure is wanted.

| Variable | Required | Default | What it does |
|---|---|---|---|
| `OTLP_BEARER_TOKEN` | Yes | - | Authenticates the export to SenHub. Without it the agent collects and exports nothing |
| `SENHUB_OTLP_ENDPOINT` | No | `eu-west-1.intake.senhub.io:443` | Another collector: your own OpenTelemetry collector, VictoriaMetrics, Grafana Alloy |
| `SENHUB_OTLP_PROTOCOL` | No | `grpc` | `grpc` or `http`, the latter for a backend that ingests OTLP over HTTP |
| `SENHUB_LICENSE` | No | - | Licence token, for the probes that need one |
| `SENHUB_TAGS` | No | - | Tags on every metric, as `key=value,key2=value2` |
| `SENHUB_HTTP_PORT` | No | `8080` | Port of the console and of the PRTG, Nagios and Prometheus endpoints |
| `SENHUB_CONFIG_DIR` | No | `/etc/senhub-agent` | Where the configuration is read and written |
| `SENHUB_STATE_DIR` | No | `/var/lib/senhub-agent` | Where the key and the bookmarks live |
| `SENHUB_PROBES` | No | - | YAML of the probes to run, as a `probes.d` file would hold it |
| `SENHUB_OUTPUT` | No | - | YAML of one more output, as a `strategies.d` file would hold it |

The agent key is **not** a variable: the agent generates its own on
first start and keeps it in the state directory. That is why the mount
matters.

## Starting other probes

The variables above configure the agent. The probes are configuration of
their own, and there are sixty-six types of them, so they are not each
given a variable. Three ways in, from the least to the most work:

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

### Your own configuration

Mount `/etc/senhub-agent` whole. Nothing is written and every variable
is ignored, which the entrypoint says on startup.

Whichever you choose, the configuration is checked before the agent
starts: a container whose variables produce a file the agent would
refuse stops with the reason rather than restarting in a loop.

### Reading an Azure Container App

This is a shorthand for the fragment `SENHUB_PROBES` would carry, kept
because it is the probe this image is most often asked for. Set the
application and its credentials, and the agent reads the console log
stream of that Container App. Setting `SENHUB_AZURE_APP` without the
rest stops the container with the list of what is missing, rather than
starting half configured.

| Variable | What it does |
|---|---|
| `SENHUB_AZURE_APP` | Name of the Container App to read |
| `SENHUB_AZURE_TENANT_ID` | Entra tenant of the app registration |
| `SENHUB_AZURE_CLIENT_ID` | Application (client) ID |
| `SENHUB_AZURE_CLIENT_SECRET` | Client secret |
| `SENHUB_AZURE_SUBSCRIPTION_ID` | Subscription holding the Container App |
| `SENHUB_AZURE_RESOURCE_GROUP` | Its resource group |

None of these credentials is written to a file: the configuration holds
a reference and the agent reads the value from the environment at every
start.

The role behind the app registration needs four actions, and `Reader`
is not enough. See [Azure Container Apps](probes/azure_container_apps.md).

## Bringing your own configuration

Mount a directory on `/etc/senhub-agent` and the entrypoint writes
nothing: your files win and every `SENHUB_*` variable is ignored. The
entrypoint says which of the two it did on startup, so a container that
ignores your variables tells you why.

```bash
docker run -d --name senhub-agent \
  -v /srv/senhub/conf:/etc/senhub-agent \
  -v senhub-state:/var/lib/senhub-agent \
  ghcr.io/senhub-io/senhub-agent:0.5.5-beta
```

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
