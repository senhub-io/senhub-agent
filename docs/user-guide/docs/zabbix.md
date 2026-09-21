# Zabbix output (preview)

!!! warning "Preview"
    The Zabbix output is being built during the 0.6.0 cycle and is not
    supported yet. This page documents the parameters the agent reads;
    the templates and the discovery rules are still to come.

The `zabbix` output makes the agent a native **Zabbix active agent**: it
connects out to a Zabbix server or proxy on port 10051, registers the host
through Zabbix autoregistration, asks which items the server wants for it,
and pushes their latest values in batches. Nothing listens on the agent
side; PRTG, Nagios and Prometheus keep working next to it.

## Configuration

```yaml
# strategies.d/20-zabbix.yaml
zabbix:
  server: "zabbix.example.com:10051"
  hostname: "web-01"            # default: the machine's host name
  host_metadata: "senhub-agent" # matched by the autoregistration action
  interval: 60s                 # push cadence
  refresh_interval: 120s        # item list refresh
  heartbeat_interval: 60s
  timeout: 10s
  key_prefix: senhub
  tls:
    enabled: false
    ca_file: ""
    cert_file: ""
    key_file: ""
    server_name: ""
    insecure_skip_verify: false
```

| Parameter | Default | Description |
|---|---|---|
| `server` | required | Zabbix server or proxy, `host:port`; `10051` when the port is omitted. Several addresses separated by commas name a proxy group (see below). |
| `hostname` | machine host name | Name this host registers under. |
| `host_metadata` | `senhub-agent` | Sent with every check-list request; the autoregistration action matches on it to choose host groups and templates. The agent appends its operating system, so `senhub-agent linux`, which is how the per-platform actions tell hosts apart. Limited to 2034 bytes by Zabbix. |
| `interval` | `60s` | Push cadence of the collected values. |
| `refresh_interval` | `120s` | How often the item list is asked again. |
| `heartbeat_interval` | `60s` | Heartbeat cadence; the server declares the host unavailable after twice that. |
| `timeout` | `10s` | Bound on one connection, request and reply. |
| `key_prefix` | `senhub` | First segment of every item key. |
| `passive.enabled` | `false` | Answer the server's polls on the passive port (see below). |
| `passive.bind_address` | `0.0.0.0` | Address the passive listener binds to. |
| `passive.port` | `10050` | Port the passive listener binds to; sent to the server so autoregistration creates the agent interface on it. |
| `passive.allow` | server addresses | Addresses or CIDR ranges allowed to poll the passive port. |
| `tls.enabled` | `false` | Encrypt the connection with TLS (certificate). |
| `tls.ca_file` | | CA certificate that signed the server's certificate. |
| `tls.cert_file`, `tls.key_file` | | Client certificate and key, both or none. |
| `tls.server_name` | server host | Name expected in the server's certificate. |
| `tls.insecure_skip_verify` | `false` | Skip the server certificate check. |

Zabbix pre-shared keys (PSK) are not supported: Go's TLS library has no
PSK cipher suites. Encrypted autoregistration, which Zabbix only offers
with PSK, is therefore not available; register in clear or through a
local proxy, then encrypt the data connection with a certificate.

## Item keys

Every series is sent under a key built from the probe's definition:

```
<key_prefix>.<metric>[<probe name>,<dimension>,...,<static attribute>,...]
```

The metric is the OTel name of the series (`system.cpu.utilization`,
`senhub.veeam.job.status`), so it is called the same thing here, on the
Prometheus endpoint and on the OTLP output. The dimensions are the
metric's `multi_instance_labels`, in the order the definition lists them;
a metric that names its own replaces the probe's rather than adding to
them, so a Windows drive metric carries the drive letter alone and not
the device and mount point it does not have.

The static attributes are the values of the metric's `otel.attributes`,
in attribute-key order: they tell apart the internal metrics that share
one OTel name, so on a probe named `memory` the used memory is
`senhub.system.memory.usage[memory,used]` and the free memory
`senhub.system.memory.usage[memory,free]`, while a filesystem series
carries its device and mount point first:
`senhub.system.filesystem.usage[logicaldisk,/dev/sda1,/,used]`. A metric
whose OTel name already starts with the prefix is not prefixed twice. Values follow the OTel unit (a
percentage is a ratio, a duration is in seconds); an enum metric is sent
as its raw code under one key.

The server only receives the keys it asked for. Until the host exists on
the server and a template gives it items, the log says so at start and
nothing is pushed.

## Templates and discovery

The templates are generated from the same definitions the keys come from,
one per probe type:

```bash
senhub-agent zabbix template --out ./templates            # every probe type
senhub-agent zabbix template --probe memory --probe veeam # a selection
senhub-agent zabbix template --probe veeam --version 6.0  # to standard output
```

Options: `--version 6.0|7.0` (export format, `7.0` by default), `--prefix`
(must match the output's `key_prefix`), `--delay` (update interval of the
items, `1m` by default), `--out` (directory; without it a single template
goes to standard output).

Every item of a template is a prototype under a low-level discovery rule,
because the probe instance name is discovered too: the rule
`senhub.discovery[memory]` returns `[{"{#PROBE}":"memory"}]`, and a
metric with dimensions hangs under the rule of its dimension set,
`senhub.discovery[logicaldisk,device,mount_point]`, with one macro per
dimension. The agent serves these discovery keys like any other item, so
a host gets its items within one discovery interval (1 hour by default,
`--delay` does not change it; edit the rule in Zabbix if you want faster
discovery on a lab). An enum metric with a lookup gets a value map.

Import the files through **Data collection > Templates > Import**, or
`configuration.import` on the API. Re-importing a regenerated template
updates the same objects: the identifiers are derived from the keys.

## Passive polling

With `passive.enabled: true` the agent also listens on `passive.port`
(10050 by default) and answers the server's polls the way a classic
agent does: `agent.ping`, so the host's availability icon turns green,
`agent.version`, `agent.hostname`, and every item key the active push
sends, for an operator who prefers passive items. Both wire dialects are
served, the bare key of servers before 7.0 and the JSON batch of 7.0 and
later.

Only the addresses in `passive.allow` may poll; when the list is empty,
the addresses the configured `server` resolves to. The port is sent with
the registration request so the autoregistration action creates the
agent interface on it. The passive port is not encrypted.

```yaml
zabbix:
  server: "zabbix.example.com:10051"
  passive:
    enabled: true
    port: 10050
    allow: ["10.20.0.0/24"]
```

## One template set per platform

A definition declares every metric its probe can produce, and a probe
does not produce the same ones everywhere: a processor's deferred
procedure calls exist on Windows and nowhere else. Declaring them all on
every host leaves items that can never receive a value, which an
operator reads as a defect rather than as an absence.

So `zabbix template` writes one set per platform and names the files for
it, `senhub-cpu-linux-7.0.yaml` beside `senhub-cpu-windows-7.0.yaml`.
Use `--platform linux` or `--platform windows`; without it the template
carries every metric of the definition, which is what you want when you
generate for reading rather than for import.

Nothing has to be chosen per host. The agent appends its operating
system to the host metadata it registers with, and `zabbix setup`
creates one autoregistration action per platform matching on it, so a
Linux host is linked to the Linux templates and a Windows host to the
Windows ones by itself. A setup run on a server prepared by an earlier
version disables the single action that version created, because Zabbix
runs every matching action and leaving it would link both sets.

## The agent's own items

Beside the probe templates, `zabbix template` writes a small **SenHub
Agent** template carrying `agent.ping`, `agent.version` and
`agent.hostname`. Link it on every host: `agent.ping` is what turns the
host's availability green, and the other two say which agent is running
there. `zabbix setup` imports it and adds it to the autoregistration
action by itself.

It is a template of its own because Zabbix refuses two linked templates
that declare the same key, and every probe template is linked beside the
others. The three items are served on both rails, so they arrive whether
the host is monitored actively or polled on the passive port.

## Through a proxy

Point `server` at the proxy instead of the server and nothing else
changes: the agent registers through it, the server attaches the host to
the proxy that relayed the registration, and the values travel the same
way.

```yaml
zabbix:
  server: "zabbix-proxy-paris.example.com:10051"
```

A **proxy group** needs every member listed, separated by commas, the way
a classic agent takes several `ServerActive` entries:

```yaml
zabbix:
  server: "proxy-a.example.com,proxy-b.example.com,proxy-c.example.com"
```

The agent talks to the first member that answers. When the host is held
by another member, that member replies with a redirection and the agent
moves to it for every request, the check list, the values and the
heartbeat alike. If the member holding the host goes down, the agent
forgets the redirection and asks the configured addresses again, which is
how it learns where the group moved the host.

With `passive.enabled` and no explicit `passive.allow`, every configured
address is allowed to poll the agent, because the member polling today is
not necessarily the one that polled yesterday.

## Autoregistration

Create an action under **Alerts > Actions > Autoregistration actions**
with a condition on the host metadata (`contains senhub-agent`, or
whatever you set in `host_metadata`) and three operations: add host, add
to a host group, link the generated templates. Every agent whose
metadata matches then appears by itself at its first check-list request,
with its items created by discovery within the discovery interval.
