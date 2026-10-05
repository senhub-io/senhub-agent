# Zabbix output

The `zabbix` output makes the agent a native **Zabbix active agent**: it
connects out to a Zabbix server or proxy on port 10051, registers the host
through Zabbix autoregistration, asks which items the server wants for it,
and pushes their latest values in batches. Nothing listens on the agent
side; PRTG, Nagios and Prometheus keep working next to it.

It is proven against Zabbix 7.0 and 8.0 on a Linux and a Windows host:
every generated template imports into both lines, and encryption works
both ways with a certificate or a pre-shared key. Zabbix 6.0 LTS is
supported too: `zabbix setup` was measured on 6.0.48 and adapts to it on
its own (see [Versions](#versions)).

## Deploying

Prepare the Zabbix server once with [`zabbix setup`](#zabbix-setup-options):
it imports the templates and creates the autoregistration actions. Then
give every agent the server's address when you install it; nothing else
is set, on the agent or in the Zabbix interface:

| Where | How |
|---|---|
| Windows | `msiexec /i senhub-agent-<version>-amd64.msi /qn ZABBIX_SERVER=zabbix.example.com:10051` |
| Linux | after `install`: `sudo /usr/local/bin/senhub-agent config init --zabbix-server zabbix.example.com:10051` |
| Container | `SENHUB_ZABBIX_SERVER=zabbix.example.com:10051` |

The port is the server's or proxy's trapper port (`ListenPort` in its
configuration), 10051 by default; an address without a port gets 10051.
`zabbix setup` ends by printing these lines with the frontend's host and
no port, since it only talks to the frontend: add the port when the
server listens elsewhere, and replace the host when the server does not
run on the frontend's machine.

The agent asks the server for its items; the server does not know the
host and runs its autoregistration action, which creates it, puts it in
the SenHub group and links the templates of its platform. Discovery then
creates the items: on a Zabbix 8.0 server prepared by `zabbix setup`, a
new host appeared within a minute and had its items within five, with no
step on the Zabbix side. A machine already configured gets the output
the same way: `config init` adds the Zabbix file to an existing
configuration and the running agent picks it up.

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
| `interval` | `60s` | Push cadence of the collected values. Each push sends the latest value of every item, including the value of a probe that runs less often, until that probe's next run is due. |
| `refresh_interval` | `120s` | How often the item list is asked again. |
| `heartbeat_interval` | `60s` | Heartbeat cadence; the server declares the host unavailable after twice that. A server before 6.2 answers that it does not know the request; the agent then stops sending it. A heartbeat that fails because the connection failed is sent again at the next beat. |
| `timeout` | `10s` | Bound on one connection, request and reply. |
| `key_prefix` | `senhub` | First segment of every item key. |
| `passive.enabled` | `false` | Answer the server's polls on the passive port (see below). |
| `passive.bind_address` | `0.0.0.0` | Address the passive listener binds to. |
| `passive.port` | `10050` | Port the passive listener binds to; sent to the server so autoregistration creates the agent interface on it. |
| `passive.allow` | server addresses | Addresses or CIDR ranges allowed to poll the passive port. |
| `passive.advertise` | source address | Address or name the server should poll, sent with the registration. |
| `passive.tls.enabled` | `false` | Encrypt what the server polls. Needs `cert_file` and `key_file`, or `psk_identity` and `psk_file`. |
| `passive.tls.cert_file`, `passive.tls.key_file` | | Certificate the agent presents to whoever polls it, and its key. |
| `passive.tls.ca_file` | | Authority that signed the server's certificate. When set, a poller must present one it signed. |
| `tls.enabled` | `false` | Encrypt the connection with TLS (certificate). |
| `tls.ca_file` | | CA certificate that signed the server's certificate. |
| `tls.cert_file`, `tls.key_file` | | Client certificate and key, both or none. |
| `tls.server_name` | server host | Name expected in the server's certificate. |
| `tls.insecure_skip_verify` | `false` | Skip the server certificate check. |
| `tls.psk_identity` | | Identity sent with the pre-shared key; must match what the server or the autoregistration setting holds. |
| `tls.psk_file` | | File holding the pre-shared key, hex-encoded as Zabbix writes it. Excludes `cert_file`. |
| `passive.tls.psk_identity`, `passive.tls.psk_file` | | The same pair for the polled port, configured apart because the roles are opposite. |

A block takes a certificate **or** a pre-shared key, never both. See
[Encryption](#encryption) for both forms and for what each one buys.

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
as its raw code under one key. The generated templates multiply a
utilization by 100 on the server side, so it is stored and shown as a
percentage (`95.31 %`) as the native agent shows it; write a trigger or
a calculated item against the percentage.

A metric that is a cumulative counter (network bytes and packets, disk
I/O, CPU time, request totals) is sent as its ever-growing total, which
a graph cannot read. The generated templates therefore add the
**Change per second** preprocessing step to its item, so the server
stores and shows a rate. The unit follows: bytes become `Bps`, a bare
count becomes `/s`, and any other unit gets `/s` appended (CPU time,
in seconds, is shown as `s/s`: the share of a second spent per second).
The key and what the agent sends do not change. Gauges, non-monotonic
sums (current connections) and the count and sum of a distribution are
stored as sent. Write a trigger or a calculated item against the rate.

The server only receives the keys it asked for. Until the host exists on
the server and a template gives it items, the log says so at start and
nothing is pushed.

### Metrics relayed from another sender

An agent running the `otlp_receiver` probe relays what applications send
it, under the names they chose. The agent describes none of those names,
so their key is built from the metric name alone, and the emitter is
carried beside the probe:

```
senhub.http.server.request.duration[relay,checkout]
```

where `checkout` is the sender's `service.name`. Without it two
applications reporting the same metric name through one receiver would
build the same key, and the second value would overwrite the first on an
item that goes on looking healthy. `service.name` is used rather than
the host name because a replaced container keeps the first and changes
the second, which would create an item on every deployment. A sender
that names itself neither keeps the shorter key.

The matching discovery rule is `senhub.discovery[otlp_receiver,service.name]`,
whose instances carry `{#PROBE}` and `{#SERVICE_NAME}`.

#### The standard conventions arrive with a template

Zabbix asks for nothing it was not told about, so a relayed metric had
to be declared by hand. The usual application metrics no longer do: the
agent ships a template for the OpenTelemetry semantic conventions —
HTTP server, JVM and database client — generated from the same
definitions as every other template.

```bash
senhub-agent zabbix setup --url https://zabbix.example.com --probe otlp_receiver
```

A host carrying it discovers the applications that relay through it, and
their series become items keyed on the sender and on the attributes the
convention defines:

```
senhub.http.server.request.duration.count[relay,checkout,GET,/cart,200]
senhub.jvm.memory.used[relay,checkout,heap,G1 Eden Space]
senhub.db.client.operation.duration.sum[relay,catalog,postgresql,SELECT]
```

Nothing is rewritten on the way. A relayed metric reaches every output
through a pass-through that is consulted before any definition, so the
name, the unit and the value stay the application's. The template only
tells the server what to ask for.

**A duration arrives as a distribution**, not as a number: a count, a
sum and a bucket ladder. A sink that holds one value per item cannot
hold that, so the agent sends the two facts it can, under keys that say
which is which — `.count` and `.sum`. Sending the bare name would put a
number of requests under a key that reads as a latency. The average over
a period is the change in the sum divided by the change in the count,
which is a calculated item in Zabbix.

**A metric outside those conventions is not lost.** It keeps the shorter
key its own name gives it, and an operator who wants it declares its
item once. Adding a convention to the shipped set is a definition file,
not code.

An application that exports only part of a set, one of the three JVM
memory metrics of a pool for instance, gets items for what it sends and
none for the rest: discovery lists, per sender, the metrics it feeds.

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
`senhub.discovery[memory]` returns
`[{"{#PROBE}":"memory","{#PROBE_MEMORY}":"memory"}]`, and a metric with
dimensions hangs under the rule of its dimension set,
`senhub.discovery[logicaldisk,device,mount_point]`, with one macro per
dimension.

Both macros carry the same instance name. The item keys use the one
named after the probe type, `senhub.db.up[{#PROBE_POSTGRESQL}]`, because
Zabbix refuses to link two templates that declare the same key, and a
plain `{#PROBE}` made the PostgreSQL and MySQL templates both declare
`senhub.db.up[{#PROBE}]`. The key the agent sends is the same either
way, `senhub.db.up[production-postgres]`; item names keep `{#PROBE}`. The agent serves these discovery keys like any other item, so
a host gets its items within one discovery interval. The generated
templates set the discovery rules to 1 hour, and `--delay` does not
change it. `zabbix setup` sets the rules of the templates it imports to
5 minutes, so a new host fills within minutes; `--discovery-delay`
chooses another interval, and `--no-discovery-delay` leaves the rules at
the template's hour. An enum metric with a lookup gets a value map.

An instance only gets the items of the metrics it sends. Each discovery
row carries `{#SENHUB.FED}`, the list of metrics that instance feeds,
and the rule holds every prototype to it: a virtual network card whose
kernel reports no speed gets no speed item, while a bridge on the same
host that reports one does. An agent too old to send the list keeps
every item, as before.

Import the files through **Data collection > Templates > Import**, or
`configuration.import` on the API. Re-importing a regenerated template
updates the same objects: the identifiers are derived from the keys.

Every generated template is imported into a live Zabbix 7.0 and 8.0
before a release, in both export formats and for both platforms. Two
probes get no file at all: `syslog` and `event` relay records rather
than measuring anything, so they declare no item, and the command says
so on standard error rather than writing a template that could only
ever stay empty.

Two constraints of Zabbix's own are worth knowing if you write a
definition. A template's technical name is validated as a host name, so
the generator strips what that forbids — a friendly name like
"IBM i / Power Systems" becomes `SenHub IBM i Power Systems`, while the
name an operator reads keeps the slash. And the 6.0 export spells the
root template-group tag `groups` where 7.0 renamed it to
`template_groups`; `--version` picks the right one.

### Triggers

The templates carry triggers, so a host linked to them raises problems
without anyone writing an expression.

- **States.** A metric that reports a state (a virtual server that is
  down, a backup job that failed, a cluster in red) raises a problem
  from the same classification PRTG and Nagios read: the codes its
  lookup calls an error raise a *High* problem, the ones it calls a
  warning raise a *Warning*. The problem names the state, through the
  item's value map.
- **Thresholds.** Processor, memory and disk usage raise a *Warning*
  and a *High* problem when every value over the last five minutes is
  above a threshold: 80 and 90 % for the processor and the disks, 85
  and 95 % for the memory, the values the shipped Nagios checks use.
  The warning depends on the high one, so a value past both raises one
  problem. Each threshold is a template macro, overridden on a host or
  a host group without editing the template (the table below also lists
  the clock tolerance, which is not a usage threshold):

| Macro | Default |
|---|---|
| `{$SENHUB.CPU_USAGE_TOTAL.WARN}` / `.CRIT}` | 80 / 90 |
| `{$SENHUB.MEMORY_USED_PERCENT.WARN}` / `.CRIT}` | 85 / 95 |
| `{$SENHUB.FS_USED_PERCENT.WARN}` / `.CRIT}` (Linux) | 80 / 90 |
| `{$SENHUB.DISK_USED_PERCENT.WARN}` / `.CRIT}` (Windows) | 80 / 90 |
| `{$SENHUB.CLOCK.DRIFT.MAX}` | 60s |

- **Clock drift.** The CPU template carries the host's clock as an item
  shown as a date, and a *Warning* trigger, `fuzzytime()`, that fires when
  the host's time is further from the server's than `{$SENHUB.CLOCK.DRIFT.MAX}`.
  It works the same on Zabbix 6.0 and 7.0.

Items and triggers are tagged the way the native templates are: every
item carries `component` (the probe type, or `agent` and `inventory` on
the agent's own items), and every trigger carries `scope`:
`availability` for a state, `performance` for the processor, `capacity`
for memory and disks. Filter problem views and actions on them.

## Encryption

The agent encrypts with **certificates** or with a **pre-shared key**,
in both directions, which is the same choice a native Zabbix agent
offers.

### With a pre-shared key

```yaml
zabbix:
  server: "zabbix.example.com:10051"
  tls:
    enabled: true
    psk_identity: "senhub-paris"
    psk_file: /etc/senhub-agent/zabbix.psk
```

`psk_file` holds the key hex-encoded, exactly as Zabbix writes it, and
is the only place it appears: nothing carries it in the configuration,
so `config show` has nothing to redact and the file's permissions are
the protection. Generate one the way Zabbix documents:

```bash
openssl rand -hex 32 > /etc/senhub-agent/zabbix.psk
chmod 600 /etc/senhub-agent/zabbix.psk
chown senhub:senhub /etc/senhub-agent/zabbix.psk   # the service account reads it
```

On the Zabbix side, set the same identity and key on the host, or in
**Administration > General > Autoregistration** for agents that register
themselves.

The polled port takes the same two settings under `passive.tls`, and the
two are configured apart because the roles are opposite. A block takes a
certificate **or** a pre-shared key, never both: Zabbix picks one
encryption per connection, and configuring both would leave the agent
choosing silently which secret proves it.

### With a certificate

```yaml
zabbix:
  tls:
    enabled: true
    ca_file: /etc/senhub-agent/zabbix-ca.crt
    cert_file: /etc/senhub-agent/agent.crt
    key_file: /etc/senhub-agent/agent.key
```

See [Encrypting the polled port](#encrypting-the-polled-port) for the
listener's own block.

### Which to choose

A pre-shared key needs no certificate authority, which is why most
Zabbix sites use it, and it is the **only** encryption Zabbix offers for
autoregistration — that setting takes "none", "PSK" or both, and refuses
a certificate. So an agent that registers itself on a site which
encrypts that step needs PSK. A certificate carries an identity a
authority vouches for, and is the better answer where a PKI already
exists.

### What is implemented

TLS 1.2 with `TLS_PSK_WITH_AES_128_GCM_SHA256` or
`TLS_PSK_WITH_AES_256_GCM_SHA384`, which is what Zabbix's own default
PSK cipher list offers. Go's standard TLS has no external pre-shared
keys, so this profile is implemented in the agent: one suite family, no
certificate, no resumption, no renegotiation, no 0-RTT.

Verified against real servers on both lines — the outbound connection,
autoregistration (the host is created with PSK set on both directions by
Zabbix itself), collection, and the polled port answering `zabbix_get
--tls-connect psk`. A wrong key, a wrong identity and an unencrypted
poll are all refused.

Not implemented: the TLS 1.3 external-PSK profile. Both Zabbix lines
accept the 1.2 one, so this is not a limitation in practice; a server
configured to refuse TLS 1.2 outright would not be served.

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
agent interface on it.

Zabbix records the address the packets came from, which behind NAT is
the translation and not somewhere it can poll. `passive.advertise` names
the address instead: a name creates a DNS interface, an address creates
an IP one.

```yaml
zabbix:
  passive:
    enabled: true
    port: 10050
    advertise: "web-01.example.com"
```

### Encrypting the polled port

The listener takes its own `tls` block, apart from the one on the
outbound connection, because the two are opposite roles: there the agent
checks a server, here it presents itself to one.

```yaml
zabbix:
  server: "zabbix.example.com:10051"
  passive:
    enabled: true
    port: 10050
    tls:
      enabled: true
      cert_file: /etc/senhub/agent.crt
      key_file: /etc/senhub/agent.key
      ca_file: /etc/senhub/zabbix-ca.crt
```

With `cert_file` and `key_file` alone, the agent encrypts what it serves
and anyone the allow list admits may read it. Add `ca_file` and the
agent also demands a certificate from whoever polls, signed by that
authority: the allow list says which addresses may connect, a
certificate says who they are.

On the Zabbix side, set the host's "Connections to host" to
*Certificate*. A certificate that cannot be read stops the agent at
start rather than leaving a listener that serves in clear.

The polled port also takes a pre-shared key, in place of the
certificate:

```yaml
zabbix:
  server: "zabbix.example.com:10051"
  passive:
    enabled: true
    port: 10050
    tls:
      enabled: true
      psk_identity: "senhub-paris"
      psk_file: /etc/senhub-agent/zabbix.psk
```

On the Zabbix side, set "Connections to host" to *PSK* with the same
identity and key. A block takes a certificate or a pre-shared key, not
both, and a key file that cannot be read or does not hold at least 16
bytes in hexadecimal stops the agent at start.

To restrict who may poll, without encryption:

```yaml
zabbix:
  server: "zabbix.example.com:10051"
  passive:
    enabled: true
    port: 10050
    allow: ["10.20.0.0/24"]
```

## The host's inventory fills itself

Beside the measurements, the agent knows what the machine *is*: its
operating system, its hardware, its serial number. Zabbix keeps those
facts in host inventory, and the **SenHub Agent** template carries them
as text items linked to the matching inventory fields.

| Inventory field | What the agent reports |
|---|---|
| Name | The name the machine reports for itself |
| OS, OS (Full), OS (Short) | Operating system, its full description, and its family |
| Type | What the machine is: a server, a virtual machine, a laptop |
| Hardware | Processor model |
| Vendor, Model | What the firmware names |
| Serial number A | Serial number from the firmware, which ties the host to an asset record |

`zabbix setup` sets the autoregistration action to put new hosts in
**automatic** inventory mode, without which the values arrive and fill
nothing. On a host created by hand, set the mode yourself under
**Inventory**.

A fact the agent did not find is not sent at all, so its field keeps
whatever it held rather than being blanked. On one Linux host, eight of
the nine fill by themselves at the first collection.

The relationships the agent also discovers, which machine a container
runs on and which card is the same machine seen twice, have no home
here: Zabbix has hosts, groups and tags, not a graph. They stay on the
topology rail rather than being flattened into a text field.

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
Windows ones by itself.

### Which probes are linked without being named

A probe is linked by default when its definition marks it `universal`
and it has an item on the platform: the processor, memory, network,
disks and processes everywhere, and on Windows the services
(`winservices`) and the event log counter (`windows_eventlog`). The
other probes depend on what the host runs (`hyperv` on a Hyper-V host,
`mssql` beside an SQL Server, any vendor probe), so you name them with
`--probe`, or link everything with `--all-probes`. A template with
nothing to send on a platform, such as `winservices` on Linux, is not
imported for it.

`windows_eventlog` sends its records on the log rail, which the Zabbix
output does not carry, so what a Zabbix host gets from it is its
throughput counter, `senhub.windows_eventlog.records_emitted`, as a
rate. The records themselves go to an OTLP output.

Only the published platforms have an action. An agent built for macOS,
which is a development target and not a release, registers as
`... darwin`, matches nothing and waits for an autoregistration that
never comes; the server logs `host [...] not found` and the agent says
it is waiting. Add a condition for it by hand if you monitor one. A setup run on a server prepared by an earlier
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

## Versions

Measured on a server of each line, not inferred from a changelog.

| | Zabbix 7.0.30 | Zabbix 8.0.0 |
|---|---|---|
| `zabbix setup` (templates, host group, autoregistration action) | yes | yes, unchanged |
| autoregistration | yes | yes |
| items by low-level discovery | yes | yes |
| host inventory filled from what the agent knows | yes | yes |
| polled port read by `zabbix_get` | yes | yes |
| proxy group redirection | yes | yes |

`zabbix setup` also prepares a 6.0 LTS server (measured on 6.0.48). A
server before 6.4 reads the API token only inside the request, and one
before 6.2 has no template groups and cannot read the 7.0 export format;
the command reads the server's version and adapts to both, so
`--version 6.0` is chosen for you there.

Two things are worth knowing about the 8.0 line specifically.

**The API no longer accepts the session token in the request body.**
Where 7.0 took `"auth": "<token>"` inside the JSON-RPC envelope, 8.0
answers `unexpected parameter "auth"` and wants an
`Authorization: Bearer` header. `senhub-agent zabbix setup` reads the
server's version first and sends the header to 6.4 and later, the
`auth` field inside the request to older servers, so it works on every
line without a flag; a script of your own that drives the API may need
changing.

**The proxy group redirection is unchanged.** A host assigned to a group
makes the server answer the check-list request with:

```json
{"response": "failed", "redirect": {"revision": 1, "address": "proxy:10051"}}
```

which is the same shape 7.0 emits, and the agent follows it to the
member holding the host. Two conditions have to be met for a group to
redirect at all, and missing either one looks like the feature not
working: the group needs a member that is **online**, and the host has
to be assigned with `monitored_by` set to the proxy group rather than
left on the server. While the assignment propagates, the server answers
once with `host ... is monitored by a proxy` and no redirect; the agent
logs it and the next request is redirected normally.

The templates are exported in the 7.0 format and import into 8.0 as they
are; `--version 8.0` is not needed and does not exist.

## `zabbix setup` options

```
senhub-agent zabbix setup --url <frontend> [--token-file <path>]
                          [--group <name>] [--metadata <string>]
                          [--action-name <name>] [--probe <type> ...] [--all-probes]
                          [--discovery-delay <interval> | --no-discovery-delay]
                          [--prefix <key prefix>] [--version 6.0|7.0] [--dry-run]
```

| Option | Default | Effect |
|---|---|---|
| `--url` | required | The Zabbix frontend, for example `https://zabbix.example.com`; `/api_jsonrpc.php` is appended |
| `--token-file`, `--token` | | API token. Read from `--token-file`, then `--token`, then the environment variable `SENHUB_ZABBIX_TOKEN`. Prefer a file: a token on the command line is visible to every process on the machine |
| `--group` | `SenHub Agents` | Host group new hosts are put in |
| `--metadata` | `senhub-agent` | Host metadata the autoregistration action matches; must match the output's `host_metadata` |
| `--action-name` | `Autoregistration — SenHub Agent` | Name of the autoregistration action |
| `--probe` | the probes every machine runs | Also import and link the template of this probe type; repeat for several. `hyperv` and `mssql` are linked this way, since only some hosts run them |
| `--all-probes` | | Import and link every template that carries an item on the platform |
| `--discovery-delay` | `5m` | Interval set on the discovery rules of the imported templates |
| `--no-discovery-delay` | | Leave the discovery rules at the template's interval (1 hour) |
| `--prefix` | `senhub` | Key prefix; must match the output's `key_prefix` |
| `--version` | `7.0`, or `6.0` on a server before 6.2 | Template export format, `6.0` or `7.0` |
| `--dry-run` | | Print what would be done without changing the server; without a token it describes the steps without reading the server |

Run it once, as a Zabbix administrator; a deployed agent never holds an
API token. Every step is idempotent, so re-running it is safe and is
how templates are refreshed after an upgrade.

## Autoregistration

Create an action under **Alerts > Actions > Autoregistration actions**
with a condition on the host metadata (`contains senhub-agent`, or
whatever you set in `host_metadata`) and three operations: add host, add
to a host group, link the generated templates. Every agent whose
metadata matches then appears by itself at its first check-list request,
with its items created by discovery within the discovery interval.

`zabbix setup` creates these actions for you, one per platform.

### A host already registered keeps the templates it was given

Zabbix runs an autoregistration action when a host registers, not
afterwards. Re-running `zabbix setup` after an upgrade refreshes the
content of every template, and hosts already linked to them receive the
change. A template the action links for the first time, because you
named a new probe with `--probe`, reaches only the hosts that register
from then on. For the hosts already there, link it yourself: select them
under **Data collection > Hosts**, then **Mass update > Templates >
Link**.
