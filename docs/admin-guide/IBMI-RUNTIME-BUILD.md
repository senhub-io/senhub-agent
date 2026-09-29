# IBM i native runner

The `ibmi` probe is part of the full edition. It talks to the IBM i
through JT400, which needs either a Java runtime on the agent's host or
`jt400runner`, a self-contained native binary that speaks the same
protocol without one. This page covers where the runner comes from and
how an agent finds it.

## Supported platforms

The `ibmi` probe runs on **Linux only**, whatever IBM i it connects to:
the agent refuses to start it on Windows and macOS. Run it from a Linux
host that can reach the IBM i partition.

| Agent host | Runner |
|---|---|
| Linux amd64 | `jt400runner-linux-amd64` |
| Linux arm64 | `jt400runner-linux-arm64` |

## Getting the runner

Each release carries both binaries as assets, next to the agent:

```bash
VERSION=0.5.6
curl -fsSLO "https://github.com/senhub-io/senhub-agent/releases/download/$VERSION/jt400runner-linux-amd64"
```

The runner is built by the release pipeline of the full edition, which
carries the probe; this repository holds no build for it.

## Installing it next to the agent

1. Place the binary matching the host in a `bridge/` directory next to
   the `senhub-agent` binary:

   | OS | Path |
   |---|---|
   | Linux | `<dir-of-senhub-agent>/bridge/jt400runner` |

2. Mark it executable: `chmod +x bridge/jt400runner`.
3. Confirm the agent finds it: `senhub-agent ibmi check`.

The agent picks the sibling binary by itself when the probe sets no
`native_runner`, so no configuration change is needed. To use another
location, set `native_runner: /custom/path` in the probe's
`probes.d/<probe>.yaml`.

## When the runner fails

A runner that stops at start with `ClassNotFoundException` was built
without a reflection path a newer JT400 needs. The agent then logs the
bridge failure, and from 0.6.0 the console shows the probe failing with
the reason and retries it every two minutes. Use the runner of the same
release as the agent, and report the failure with the output of
`senhub-agent ibmi check`.
