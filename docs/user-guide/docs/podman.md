# Running the agent with Podman

The image is the one described in [Running the agent in a container](container.md),
and everything on that page holds under Podman: the variables, the state
volume, the identities. This page covers what differs, and the Quadlet
unit that runs the agent as a systemd service.

## One command

The `docker run` lines work as they are with `podman run`:

```bash
podman run -d --name senhub-agent \
  -e OTLP_BEARER_TOKEN=<your token> \
  -v senhub-state:/var/lib/senhub-agent \
  -p 8080:8080 \
  ghcr.io/senhub-io/senhub-agent:0.6.1
```

Use the full image name. A short name such as `senhub-agent:0.6.1`
makes Podman search its configured registries, or ask which one to use.

Three things behave differently from Docker.

**Rootless ports.** Run as an ordinary user, Podman cannot publish a
host port below 1024. Keep the default 8080, or lower the limit on the
host with the `net.ipv4.ip_unprivileged_port_start` sysctl.

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
| `senhub-agent.container` | The unit: image, volumes, published port, health check, restart policy, secrets |
| `senhub-agent.env` | The `SENHUB_*` variables, all commented out. No secret goes there |

### Install

Rootful, the files go in `/etc/containers/systemd/`:

```bash
sudo cp senhub-agent.container senhub-agent.env /etc/containers/systemd/
sudo systemctl daemon-reload
sudo systemctl start senhub-agent
```

Rootless, in `~/.config/containers/systemd/`, and every `systemctl`
takes `--user`:

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

The bearer token, the licence and the Azure client secret belong in
Podman's secret store rather than in the environment file. Create each
one as the user that runs the unit (root for a rootful unit), reading
the value from standard input so that it appears in no command line:

```bash
printf '%s' "$TOKEN" | podman secret create senhub-otlp-token -
printf '%s' "$LICENSE" | podman secret create senhub-license -
```

Then uncomment the matching lines of the unit:

```ini
Secret=senhub-otlp-token,type=env,target=OTLP_BEARER_TOKEN
Secret=senhub-license,type=env,target=SENHUB_LICENSE
```

and run `systemctl daemon-reload` before the next start. A unit naming a
secret that does not exist fails to start, which is why these lines ship
commented out.

The container sees each secret as the variable it targets, and the
entrypoint treats it as any other. What becomes of it differs:

- `OTLP_BEARER_TOKEN` and `SENHUB_AZURE_CLIENT_SECRET` stay references.
  The configuration holds `${env:...}` and the agent reads the value at
  every start, so replacing the secret and restarting is enough:

  ```bash
  podman secret rm senhub-otlp-token
  printf '%s' "$NEW_TOKEN" | podman secret create senhub-otlp-token -
  systemctl restart senhub-agent
  ```

- `SENHUB_LICENSE` is read once. On first start the agent keeps the
  licence in `license.jwt`, in the configuration volume, like any
  installation does. A later licence is activated inside the running
  container, then the unit restarted, or the configuration volume is
  rebuilt as shown above:

  ```bash
  printf '%s' "$NEW_LICENSE" | podman exec -i senhub-agent \
    senhub-agent license activate --config-path /etc/senhub-agent/agent.yaml -
  systemctl restart senhub-agent
  ```

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

The image tags are versions, and there is no moving tag such as
`latest`. Moving to a new version therefore means editing the `Image=`
line of the unit, then:

```bash
sudo systemctl daemon-reload
sudo systemctl restart senhub-agent
```

The unit also carries `AutoUpdate=registry`, which lets
`podman auto-update` pull the same tag again and restart the unit when
the registry serves a different image under it, for instance a
republished build of the same version. It never moves to another
version. To run it daily:

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
