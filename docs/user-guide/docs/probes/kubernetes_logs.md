
# Kubernetes Logs Probe

The `kubernetes_logs` probe discovers and follows the stdout/stderr log
files of every pod on the node it runs on, and sends each line as a log
record through the [OTLP output](../otlp.md), with the pod and container
attributes listed below.

It is designed to run as a node `DaemonSet` with the node's
`/var/log/pods` directory mounted read-only. Auto-discovery is the
primary mode: there is no static target list. Inclusion and exclusion
globs are available to narrow the set of pods and namespaces.

The probe runs on Linux nodes only: it follows a rotated log file by its
inode, which Windows does not expose.

## Quick start

Mount the node pod log directory and provide the agent's identity
through the Kubernetes downward API. A minimal `DaemonSet` probe entry
looks like this:

```yaml
# probes.d/kubernetes-logs.yaml
- name: pods
  type: kubernetes_logs
  params:
    # root defaults to /var/log/pods
    scan_interval: 5s
    from_beginning: false
```

The chart sets the following environment variables:

| Variable | Purpose |
|---|---|
| `SENHUB_K8S_NODE_NAME` | Value of the `k8s.node.name` attribute on every emitted record |
| `SENHUB_K8S_POD_NAMESPACE` | Namespace of the agent's own pod |
| `SENHUB_K8S_POD_NAME` | Name of the agent's own pod |
| `SENHUB_K8S_POD_UID` | UID of the agent's own pod |

The agent's own pod is excluded from collection.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:0fbd23f5507de9a583a58b91962f1fe0c1b8d8b4193b0621b268b6834944f21b -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `root` | No | `/var/log/pods` | Root directory containing the pod log directory structure |
| `include_namespaces` | No | - | Follow only pods in namespaces matching one of these glob patterns |
| `exclude_namespaces` | No | - | Never follow pods in namespaces matching one of these glob patterns |
| `include_pods` | No | - | Follow only pods whose name matches one of these glob patterns |
| `exclude_pods` | No | - | Never follow pods whose name matches one of these glob patterns |
| `scan_interval` | No | `5s` | How often the node log directory is rescanned |
| `from_beginning` | No | `false` | Read existing content the first time a file is seen |
| `max_line_bytes` | No | `262144` | Cap on one CRI message after partial reassembly, in bytes |
| `bookmark_path` | No | - | File persisting read offsets across restarts; defaults to a probe-name bookmark file in the agent state directory |
| `multiline` | No | - | Fold continuation lines (stack traces) into one record, per container stream |
| `multiline.pattern` | No | - | Regular expression tested against each message. Example: `^\d{4}-\d{2}-\d{2}` |
| `multiline.negate` | No | `false` | Invert the pattern match |
| `multiline.match` | No | `after` | Whether a matching line starts a record or flushes the previous one. One of `after`, `before` |
| `parser` | No | - | Structured parsing of each CRI message |
| `parser.type` | No | `raw` | Message format. One of `raw`, `regex`, `json`, `logfmt` |
| `parser.pattern` | No | - | Regular expression with named groups; required for the regex type |
| `parser.timestamp_field` | No | - | Field carrying the message timestamp |
| `parser.timestamp_format` | No | - | Go reference-time layout of that field. Example: `2006-01-02 15:04:05` |

<!-- schema:params:end -->

## How files are followed

- **Discovery.** The probe rescans `root` every `scan_interval` and
  follows every container log file of every pod on the node, so a new pod
  is picked up without a restart. The `include_*` and `exclude_*` lists
  are glob patterns; the agent's own pod is always left out.
- **CRI partial lines.** The runtime splits a long line into chunks.
  The probe joins them back into one message before it is sent, up to
  `max_line_bytes`. A longer message is cut there and counted in
  `senhub.kubernetes_logs.records_truncated`.
- **Rotation.** Offsets are kept per file and tied to its inode. When the
  runtime rotates a file, the probe finishes the old one and starts the
  new one, and after a restart it resumes from the saved offset
  (`bookmark_path`) instead of re-reading or skipping lines. Compressed
  rotated files are not read.
- **Multiline.** With `multiline` set, continuation lines such as a stack
  trace are folded into the record that opened them. Folding is done
  separately for the stdout and stderr stream of each container, so the
  two never mix.
- **Severity.** Derived from the message as described in
  [File tail](filetail.md#severity).

## Attributes on records

Every emitted log record carries these attributes:

| Attribute | Description |
|---|---|
| `k8s.namespace.name` | Pod namespace |
| `k8s.pod.name` | Pod name |
| `k8s.pod.uid` | Pod UID |
| `k8s.container.name` | Container name |
| `k8s.container.restart_count` | Container restart count |
| `k8s.node.name` | Node name from `SENHUB_K8S_NODE_NAME` |
| `log.iostream` | `stdout` or `stderr` |
| `log.file.path` | Full path of the pod log file |

The record body is the container message. Its timestamp is the CRI
timestamp. Severity is derived by the configured parser, including level
sniffing for the default `raw` parser.

## Probe metrics

The probe reports its own state, without any per-pod or per-file label.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.kubernetes_logs.files_followed` | `senhub.kubernetes_logs.files_followed` | Kubernetes Logs Files Followed | # | Number of pod log files currently followed |
| `senhub.kubernetes_logs.pods_followed` | `senhub.kubernetes_logs.pods_followed` | Kubernetes Logs Pods Followed | # | Number of unique pods currently followed |
| `senhub.kubernetes_logs.records_emitted` | `senhub.kubernetes_logs.records_emitted` | Kubernetes Logs Records Emitted | # | Cumulative count of log records this probe has published to the log rail |
| `senhub.kubernetes_logs.records_truncated` | `senhub.kubernetes_logs.records_truncated` | Kubernetes Logs Records Truncated | # | Cumulative count of log records truncated by the max_line_bytes cap |
| `senhub.kubernetes_logs.read_errors` | `senhub.kubernetes_logs.read_errors` | Kubernetes Logs Read Errors | # | Cumulative count of pod log file read errors |

<!-- schema:metrics:end -->

## What it does not do

The probe reads regular pod log files only. It does not read compressed
rotated files such as `*.gz`. It does not use Kubernetes API streaming or
the container runtime API: collection is entirely file-based from the
node's `/var/log/pods` tree.
