# Nagios output

The agent answers Nagios the way a plugin does: one line,
`STATUS - message | perfdata`, fetched over HTTP from the agent's REST
API. It works with Nagios Core, Nagios XI and any tool that runs Nagios
plugins. Nothing is installed on the Nagios side beyond a short command.

Two kinds of answer are available:

- **The probe summary**, one per probe: is the probe collecting, is its
  target reachable, and every value it holds as performance data.
- **Checks**, which hold chosen metrics against thresholds and return
  OK, WARNING, CRITICAL or UNKNOWN. The agent ships a set of checks, and
  you can write your own.

## Enable the endpoint

The `nagios` endpoint of the HTTP output must be listed. The installer
lists it by default:

```yaml
http:
  port: 8080
  endpoints: ["prtg", "web", "nagios"]
```

Every URL below carries the agent key, printed by `senhub-agent key show`.
See [HTTP / HTTPS](http-https.md) for the port, TLS and the firewall.

## Call the agent from Nagios

The agent sets the status in the first word of its answer. A four-line
plugin turns that word into the exit code Nagios reads:

```sh
#!/bin/sh
# /usr/local/nagios/libexec/check_senhub URL
out=$(curl -s --max-time 10 "$1") || { echo "UNKNOWN - agent unreachable"; exit 3; }
echo "$out"
case "$out" in OK*) exit 0 ;; WARNING*) exit 1 ;; CRITICAL*) exit 2 ;; *) exit 3 ;; esac
```

With HTTPS and a certificate your Nagios host does not trust, add
`--cacert /path/to/ca.pem` to the `curl` line rather than `-k`.

Then declare the command once, and one service per check:

```
define command {
    command_name  check_senhub
    command_line  $USER1$/check_senhub "http://$HOSTADDRESS$:8080/api/$USER3$/nagios/$ARG1$"
}

define service {
    use                  generic-service
    host_name            web-01
    service_description  CPU
    check_command        check_senhub!check/cpu_detailed
}

define service {
    use                  generic-service
    host_name            web-01
    service_description  SenHub memory probe
    check_command        check_senhub!metrics/memory
}
```

`$USER3$` holds the agent key; set it in `resource.cfg` so the key stays
out of the object files.

## The probe summary

```bash
curl http://localhost:8080/api/{key}/nagios/metrics/cpu
```

```
OK - Probe cpu healthy - 3 metrics collected | CPU_Total_Usage=12.00 CPU_System=5.00 CPU_User=10.00
```

The status is **OK** while the probe holds values, and **CRITICAL** when
it holds none or when every availability metric it reports (a metric
whose name ends in `.up`) is 0, meaning its target cannot be reached.
The summary never returns WARNING: thresholds belong to checks.

Performance data labels are the PRTG channel labels of the probe's
metrics, with spaces turned into underscores. The last part of the URL
is the probe's `name` as configured, whatever its case; the probes the
installer writes are named after their type (`cpu`, `memory`, ...).

The HTTP status is 500 when the answer is CRITICAL, 200 otherwise.

## Checks

### The shipped checks

```bash
curl http://localhost:8080/api/{key}/nagios/checks
```

lists every check with its metrics and thresholds, as JSON. The agent
ships checks for the host (`system_health`, `cpu_detailed`, `cpu_cores`,
`memory_detailed`, `network_detailed`, `network_health`), for the
`ping_gateway`, `ping_webapp` and `load_webapp` probes, and for Veeam.

### Run one check

```bash
curl http://localhost:8080/api/{key}/nagios/check/cpu_detailed
```

```
CRITICAL - cpu_usage_total: CRITICAL 97.00%, cpu_system: OK 5.00%, cpu_user: OK 10.00% | cpu_usage_total=97.00%;80;90;0;100 cpu_system=5.00%;30;50;0;100 cpu_user=10.00%;70;85;0;100
```

The worst metric sets the status of the check. The HTTP status is 500
from CRITICAL upward, 404 when no check has that name, 200 otherwise.

Right after the agent starts, a check can answer UNKNOWN for one
collection interval: rates and CPU time shares need two readings before
they have a value.

A request can narrow or override a check:

| Parameter | Effect |
|---|---|
| `warning=`, `critical=` | Replace the thresholds of every metric in the check |
| `tag_<name>=<value>` | Keep only the series whose tag `<name>` equals the value, for example `tag_interface=eth0` |
| `tags=<name>:<v1>,<v2>` | Keep only the series whose tag takes one of the values |
| `exclude_tags=<name>:<v1>,<v2>` | Drop the series whose tag takes one of the values |
| `metrics=<name>,<name>` | Keep only these metric names |

### All checks at once, as JSON

`GET /api/{key}/nagios/metrics` runs every check and answers
`{"checks": [...], "count": n}`, each entry carrying `status` (0 to 3),
`status_text`, `message` and `perfdata`. A `POST` with a JSON body runs
one check, the query string winning over the body where both set a value:

```json
{
  "check_name": "cpu_cores",
  "overrides": {
    "warning": "70",
    "critical": "95",
    "tag_filters": { "core": "1" }
  }
}
```

## Write your own checks

Put your checks in `nagios.yaml`, in the directory that holds the agent
configuration, then restart the agent:

| System | File |
|---|---|
| Linux | `/etc/senhub-agent/nagios.yaml` |
| Windows | `C:\ProgramData\SenHub\nagios.yaml` |

The example is written for a Linux host. A metric can be specific to
one operating system: on Windows the same check reads `disk_used_percent`
with the tag `drive`. The metric reference of each probe page lists the
names; the agent log says at start when a check names a metric this
system does not produce.

The file **replaces** the shipped checks; copy the ones you want to keep
from the output of `/api/{key}/nagios/checks`. The agent reads the file
once, at start. A file it cannot use is reported in the agent log and
the shipped checks are served instead, so read the log after a restart.

```yaml
version: "1"
description: "Checks for web-01"
checks:
  - name: disk_space
    description: "Space used on every file system"
    probe_filter: logicaldisk
    tag_filters:
      - key: mount_point
        operator: not_in
        values: ["/boot"]
    metrics:
      - channel: fs_used_percent
        aggregation: none
        tag_context: mount_point
        warning: "80"
        critical: "90"
        unit: "%"
        tag_specific_thresholds:
          - tags: { mount_point: "/var" }
            warning: "70"
            critical: "85"
```

### `channel` is the metric name

In a check, `channel:` takes the metric's **name**, the second column of
the metric reference at the bottom of every probe page. It is not the
PRTG label, and not the OpenTelemetry name. On the CPU page, that is
`cpu_user`, not `CPU User` and not `system.cpu.utilization`.

When a check names a metric no probe emits on this system, the agent
logs it at start, names the metric to use when what was given is a PRTG
label, and says which systems emit it when it belongs to another one. Probes
whose metric names are set by their configuration (`exec`,
`prometheus_scrape`, `snmp_poll`, `otlp_receiver`) are not in the
reference, so the log mentions them without proving them wrong.

### Check fields

| Field | Required | Description |
|---|---|---|
| `name` | yes | Name used in `/nagios/check/{name}` |
| `description` | no | Shown when no metric returns a message |
| `probe_filter` | no | Only look at the probes of this type or with this name, whatever the case. Empty: every probe |
| `tag_filters` | no | Keep only the series that pass every filter (below) |
| `metrics` | yes | One entry or more (below) |

A tag filter has a `key`, an `operator` and, except for `exists`,
`values`:

| Operator | Keeps the series whose tag |
|---|---|
| `in` | takes one of the values |
| `not_in` | takes none of the values |
| `equals` | equals the first value |
| `not_equals` | differs from the first value |
| `exists` | is present |

### Metric fields

| Field | Required | Description |
|---|---|---|
| `channel` | yes | The metric name, see above |
| `warning` | yes | A number, the last acceptable value |
| `critical` | no | A number, the last value before CRITICAL. Empty: the metric never goes past WARNING |
| `invert` | no | `true` when low values are the problem, such as free space or days left |
| `aggregation` | no | `none` (the default) evaluates each series; `average`, `max`, `min`, `sum` or `count` reduce them to one value first |
| `tag_context` | no | Tag whose value names each series in the message and the perfdata, for example `core` or `mount_point` |
| `tag_specific_thresholds` | no | Thresholds for the series that carry all the listed tags. The first matching entry wins; `warning=` and `critical=` in the request still win over it. Needs `aggregation: none` |
| `unit` | no | Unit printed in the message and the perfdata |
| `description` | no | Free text |

The agent refuses the file when a field is misspelt, a threshold is not
a number, an aggregation or an operator is unknown, or
`tag_specific_thresholds` is set on an aggregated metric. The log names
the check and the metric at fault.

### How a value is judged

A threshold is the last acceptable value, as in the Nagios plugin
convention. With `warning: "80"` and `critical: "90"`, 80 is OK, 80.5 is
WARNING and 90.5 is CRITICAL. With `invert: true`, the value alerts when
it drops below the threshold. `warning: "0"` on a count of failures
alerts on the first failure.

Some metrics report a state rather than a quantity, such as a Veeam job
status or a Redfish health. Their values are named states, listed in the
PRTG lookups the agent ships, and each state carries its own severity:
for those metrics the thresholds are not used, and the message shows the
state's name (`veeam_job_status: SUCCESS`).
