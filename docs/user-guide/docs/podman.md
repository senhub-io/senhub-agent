# Running the agent with Podman

The image is the one described in [Running the agent in a container](container.md),
and everything on that page holds under Podman: the variables, the state
volume, the identities. This page covers what differs, and the Quadlet
unit that runs the agent as a systemd service.

## What the agent monitors

An agent started in a plain container reports half a host. CPU and
memory are the host's, because `/proc` is not isolated for those
counters, but the disks are the container's overlay, the network is its
single `eth0` and the process list is its own few processes.

The unit shipped for Quadlet therefore monitors **the host** (host
scope): the host's network and PID namespaces, and the host's `/`
mounted read-only at `/host`. Disks, interfaces, processes, OS release
and `machine-id` are the host's. A [container scope](#container-scope)
is one edit away. The plain `podman run` below is a container scope.

## One command

The `docker run` lines work as they are with `podman run`:

```bash
podman run -d --name senhub-agent \
  -e OTLP_BEARER_TOKEN=<your token> \
  -v senhub-state:/var/lib/senhub-agent \
  -p 8080:8080 \
  ghcr.io/senhub-io/senhub-agent:0.6
```

Use the full image name. A short name such as `senhub-agent:0.6.1`
makes Podman search its configured registries, or ask which one to use.

Three things behave differently from Docker.

**Rootless ports and networking.** Run as an ordinary user, Podman
cannot publish a host port below 1024. Keep the default 8080, or lower
the limit on the host with the `net.ipv4.ip_unprivileged_port_start`
sysctl. A rootless container that publishes a port also needs a
user-space network helper: `slirp4netns` on Podman 4.x, `passt` (which
provides `pasta`) from Podman 5. Install the package before the first
start, or `podman run -p` fails. Host networking (`--network host`)
needs neither.

**SELinux.** On Fedora, RHEL and their derivatives, a host directory
mounted into the container is refused by SELinux until it carries a
container label. Add `:Z` to the mount to relabel it for this container
alone:

```bash
-v /srv/senhub-agent/probes.d:/etc/senhub-agent/probes.d:Z
```

Named volumes (`senhub-state` above) need no label.

**User namespaces.** The agent runs as the image's fixed uid 10001.
Rootful, that is uid 10001 on the host. Rootless, it is a uid taken from
your range in `/etc/subuid`, so the range must hold at least 10002
entries (the default 65536 does). A named volume is created with the
right owner on its own: Podman copies the image's directory into it,
ownership included. A host directory is not: add `U` to the mount and
Podman hands it to the container's uid, rootful or rootless.

```bash
-v /srv/senhub-agent/probes.d:/etc/senhub-agent/probes.d:Z,U
```

`--userns=keep-id` is of no help with this image. It maps your own uid
into the container, and the agent does not run as your uid. Prefer named
volumes, or `U` on a host directory.

## As a systemd service: Quadlet

Quadlet, part of Podman since 4.4, turns a `.container` file into a
systemd service. The repository ships one in
[`packaging/podman/`](https://github.com/senhub-io/senhub-agent/tree/master/packaging/podman),
with an environment file beside it. The unit needs Podman 4.6 or later;
on 4.4 and 4.5, replace its `AutoUpdate=registry` line with
`Label=io.containers.autoupdate=registry`.

| File | What it holds |
|---|---|
| `senhub-agent.container` | The unit: image, host scope, volumes, health check, restart policy, secrets |
| `senhub-agent.env` | The `SENHUB_*` variables, all commented out. No secret goes there |

### Install

Create the bearer token as a Podman secret first (the unit names it, and
refuses to start without it; to run without exporting to SenHub, delete
the `Secret=senhub-otlp-token` line instead):

```bash
printf '%s' "$TOKEN" | sudo podman secret create senhub-otlp-token -
```

No licence is needed: the free tier collects everything but the Pro
probes. Add one later, as shown under [Secrets](#secrets).

Host scope is a **rootful** setup. Rootful, the files go in `/etc/containers/systemd/`:

```bash
sudo cp senhub-agent.container senhub-agent.env /etc/containers/systemd/
sudo systemctl daemon-reload
sudo systemctl start senhub-agent
```

Rootless is the [container scope](#container-scope): the host block
(`--pid=host`, `Network=host`, the read of `/`) has been exercised
rootful only, and what an unprivileged user can see of the host through
it is not established, so it is not a supported rootless setup. Rootless,
delete the host block, then copy to
`~/.config/containers/systemd/`, and every `systemctl` takes `--user`:

```bash
mkdir -p ~/.config/containers/systemd
cp senhub-agent.container senhub-agent.env ~/.config/containers/systemd/
systemctl --user daemon-reload
systemctl --user start senhub-agent
sudo loginctl enable-linger "$USER"
```

The last line keeps your services running when you are not logged in,
and starts them at boot.

There is no `systemctl enable` step: a Quadlet unit is generated at
every `daemon-reload`, and its `[Install]` section is what starts it at
boot. The first start pulls the image, which is why the unit allows it
fifteen minutes.

If `systemctl start` answers that the unit does not exist, Quadlet
refused the file. It says why:

```bash
/usr/lib/systemd/system-generators/podman-system-generator --dryrun         # rootful
/usr/lib/systemd/system-generators/podman-system-generator --user --dryrun  # rootless
```

### Container scope

To monitor the container instead of the host, delete every line between
`# >>> host scope` and `# <<< host scope` in the unit, and uncomment
`PublishPort=8080:8080` (and `HostName=`, to keep a stable name). The
agent then reports the container's overlay, its own interface and its
own processes; CPU and memory are still the host's. This is what a
rootless unit runs, and it needs no host access at all.

What the host scope accepts, to know before choosing it:

- The container shares the host's PID and network namespaces and can
  read the whole host filesystem, read-only. It runs as the image's
  uid 10001 and drops nothing from Podman's default capabilities, so it
  reads what any user reads, not root-only files.
- The HTTP output opens `SENHUB_HTTP_PORT` (8080) on the host's own
  addresses. Free it, and close it in the firewall if it should not be
  reachable.
- The host identity is the host's own `machine-id`, read under
  `/host/etc`; the agent key stays in the state volume.
- On SELinux hosts, uncomment `SecurityLabelDisable=true`. Never put
  `:Z` on the `/` volume: it would relabel the host's root.

### Configure

Edit `senhub-agent.env` before the first start. Podman reads it
literally: one `KEY=value` per line, no quotes, no value on several
lines. `SENHUB_PROBES` and `SENHUB_OUTPUT` carry multi-line YAML and so
cannot go there; mount a `probes.d` directory instead, as the commented
`Volume=` line of the unit shows.

The unit keeps two named volumes:

| Volume | Mounted on | Holds |
|---|---|---|
| `senhub-agent-state` | `/var/lib/senhub-agent` | Host identity, agent key, log bookmarks |
| `senhub-agent-config` | `/etc/senhub-agent` | The configuration written on first start, and every change made from the console since |

Because the configuration is kept, **the variables are read on the
first start only**. The entrypoint writes `agent.yaml` from them, and at
every later start it finds that file and ignores them, which it says in
the log. To rebuild the configuration from a changed environment file:

```bash
sudo systemctl stop senhub-agent
sudo podman volume rm senhub-agent-config
sudo systemctl start senhub-agent
```

The identity lives in the other volume, so the agent comes back as the
same host and the same agent. Rootless, drop `sudo` and add `--user` to
`systemctl`.

To change the HTTP port, set `SENHUB_HTTP_PORT` and change the port in
both `PublishPort=` and `HealthCmd=` in the unit: the health command
names the port literally, since systemd would read a `${...}` variable
in it as one of its own.

### Secrets

The bearer token and the licence belong in Podman's secret store, and
**not** in `Environment=` or the environment file. `podman inspect`
prints the value of every environment variable in clear, to anyone who
can run it, and that includes a secret handed over as an environment
variable (`type=env`): it ends up in `Config.Env` like any other. The
unit therefore mounts each secret as a **file** under `/run/secrets`,
owned by the agent's uid 10001 and readable by it alone. `podman
inspect` shows the mount, not its content.

Create each secret as the user that runs the unit (root for a rootful
unit), reading the value from standard input so that it appears in no
command line:

```bash
printf '%s' "$TOKEN" | podman secret create senhub-otlp-token -
printf '%s' "$LICENSE" | podman secret create senhub-license -
```

The token's lines are active in the unit; the licence, which only the
Pro probes need, ships commented. Uncomment its two lines once the
secret exists:

```ini
Secret=senhub-otlp-token,type=mount,target=/run/secrets/senhub-otlp-token,mode=0400,uid=10001
Environment=OTLP_BEARER_TOKEN_FILE=/run/secrets/senhub-otlp-token
Secret=senhub-license,type=mount,target=/run/secrets/senhub-license,mode=0400,uid=10001
Environment=SENHUB_LICENSE_FILE=/run/secrets/senhub-license
```

and run `systemctl daemon-reload` before the next start. A unit naming a
secret that does not exist fails to start, which is why the licence
lines ship commented out. The variables name a path, not a value, so
`Environment=` is the right place for them.

What becomes of each:

- The token stays a reference. The OTLP output holds
  `Authorization: "Bearer ${file:/run/secrets/senhub-otlp-token}"` and
  the agent reads the file at every start (a trailing newline is
  trimmed), so replacing the secret and restarting is enough:

  ```bash
  podman secret rm senhub-otlp-token
  printf '%s' "$NEW_TOKEN" | podman secret create senhub-otlp-token -
  systemctl restart senhub-agent
  ```

- The licence is read once. On first start the agent keeps it in
  `license.jwt`, in the configuration volume, like any installation
  does. A later licence is activated inside the running container, then
  the unit restarted, or the configuration volume is rebuilt as shown
  above:

  ```bash
  printf '%s' "$NEW_LICENSE" | podman exec -i senhub-agent \
    senhub-agent license activate --config-path /etc/senhub-agent/agent.yaml -
  systemctl restart senhub-agent
  ```

- The Azure client secret of a Container Apps probe declared from
  environment variables (`SENHUB_PROBE_<NAME>_CLIENT_SECRET`) is the
  exception: it is read as an environment variable, so a `type=env` secret
  keeps it out of the unit but not out of `podman inspect`. Use a
  `probes.d` file with a `${file:...}` reference where that matters.

### Stopping

The unit gives the agent thirty seconds to stop (`--stop-timeout=30`;
Podman's default is ten, after which it sends SIGKILL and the unit ends
as failed with exit 137). The agent itself bounds its final flush to a
few seconds when the collector does not answer, and logs what it
dropped.

### Health

The unit restates the image's health check, a request to the console's
`/health`, so that it applies whatever format the image was pulled in.
It answers once the HTTP output is listening. After three failed checks
Podman stops the container and systemd starts it again
(`HealthOnFailure=kill` with `Restart=always`).

```bash
podman healthcheck run senhub-agent && echo healthy
```

### Logs

The agent writes to the container's output, which lands in the journal
under the unit:

```bash
journalctl -u senhub-agent -f            # rootful
journalctl --user -u senhub-agent -f     # rootless
```

`podman logs senhub-agent` shows the same lines.

## Updates

The agent's own auto-update is off in a container: it would replace a
binary in a layer the next container discards. Updating means running a
new image.

Three ways to hold the image; the unit uses the first:

- **The minor line tag with `AutoUpdate=registry`.** `:0.6` follows the
  stable patch releases of the 0.6 line (published from 0.6.2 on; betas
  never carry it, and there is no `latest`). `podman auto-update` pulls
  it again and restarts the unit when a new 0.6.x is published, with the
  agent key and data kept on the state volume. Moving to 0.7 is an edit
  of the `Image=` line.
- **An exact version with `AutoUpdate=registry`.** `:0.6.2` never moves
  to another version: auto-update only picks up a republished build of
  that same version. Moving on is an edit of the `Image=` line, then
  `systemctl daemon-reload` and `systemctl restart senhub-agent`.
- **A digest, without auto-update.** `Image=ghcr.io/senhub-io/senhub-agent@sha256:<digest>`
  runs exactly that image, whatever happens to the tags. Delete the
  `AutoUpdate=` line: there is nothing for it to follow. Choose this
  where the image must be reviewed before it runs.

To try a beta, use its exact tag (`:0.6.2-beta.1`): betas are not on the
minor line tag.

To run auto-update daily (first way):

```bash
sudo systemctl enable --now podman-auto-update.timer   # rootful
systemctl --user enable --now podman-auto-update.timer # rootless
podman auto-update --dry-run                           # what it would do now
```

Both volumes survive an update, so the agent keeps its identity and its
configuration.

## Checking the unit

`packaging/podman/check-quadlet.sh` hands the unit to the Quadlet
generator in dry-run mode, rootful and rootless, as shipped and with
every optional line enabled, and fails on any key or value the installed
Podman does not accept. It installs and starts nothing:

```bash
sh packaging/podman/check-quadlet.sh
```
