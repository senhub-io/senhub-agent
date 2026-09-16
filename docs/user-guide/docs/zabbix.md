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
| `server` | required | Zabbix server or proxy, `host:port`; `10051` when the port is omitted. |
| `hostname` | machine host name | Name this host registers under. |
| `host_metadata` | `senhub-agent` | Sent with every check-list request; the autoregistration action matches on it to choose host groups and templates. Limited to 2034 bytes by Zabbix. |
| `interval` | `60s` | Push cadence of the collected values. |
| `refresh_interval` | `120s` | How often the item list is asked again. |
| `heartbeat_interval` | `60s` | Heartbeat cadence; the server declares the host unavailable after twice that. |
| `timeout` | `10s` | Bound on one connection, request and reply. |
| `key_prefix` | `senhub` | First segment of every item key. |
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
metric's `multi_instance_labels`, in the order the definition lists them.
The static attributes are the values of the metric's `otel.attributes`,
in attribute-key order: they tell apart the internal metrics that share
one OTel name, so on a probe named `memory` the used memory is
`senhub.system.memory.usage[memory,used]` and the free memory
`senhub.system.memory.usage[memory,free]`, while a filesystem series
carries its device and mount point first:
`senhub.system.filesystem.usage[logicaldisk,/dev/sda1,/,,used]` (an
empty dimension stays empty). A metric whose OTel name already starts
with the prefix is not prefixed twice. Values follow the OTel unit (a
percentage is a ratio, a duration is in seconds); an enum metric is sent
as its raw code under one key.

The server only receives the keys it asked for. Until the host exists on
the server and a template gives it items, the log says so at start and
nothing is pushed.
