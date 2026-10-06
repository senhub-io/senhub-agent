<!-- Publish with feat/podman-quadlet: this page describes the unit that branch adds (packaging/podman) and links to its full page, podman.md. -->

# Podman

On a Linux host that runs containers under systemd without Docker, a
Quadlet unit makes the agent a normal service: `systemctl start`, the
journal, restart on failure, boot start. The unit and its environment file
live in `packaging/podman/` of the repository. This page is the deployment
runbook; the full description of the unit, rootless mode and the SELinux
notes is [Running the agent with Podman](../podman.md).

## Install

The unit monitors **the host** (host scope), which makes it a rootful
setup, and it needs Podman 4.6 or later.

```bash
# The unit and its environment file, from the release that carries them.
TAG=<tag that carries packaging/podman>
BASE=https://raw.githubusercontent.com/senhub-io/senhub-agent/$TAG/packaging/podman
curl -fsSLO "$BASE/senhub-agent.container"
curl -fsSLO "$BASE/senhub-agent.env"
curl -fsSLO "$BASE/check-quadlet.sh"

# The bearer token, as a Podman secret read from standard input.
printf '%s' "$OTLP_TOKEN" | sudo podman secret create senhub-otlp-token -

# Your settings, in the environment file (one KEY=value per line, no quotes).
cat >> senhub-agent.env <<'EOF'
SENHUB_OTLP_ENDPOINT=collector.example.com:4317
SENHUB_TAGS=site=paris,env=prod
EOF

sudo install -m 0644 senhub-agent.container senhub-agent.env /etc/containers/systemd/
sudo systemctl daemon-reload
sudo systemctl start senhub-agent
```

A Quadlet unit has no `enable` step: its `[Install]` section starts it at
boot once the file is in place.

`SENHUB_OTLP_ENDPOINT` points the export at your own collector. The
unit's `Secret=senhub-otlp-token` line supplies the bearer token sent to it;
delete that line, and the `OTLP_BEARER_TOKEN_FILE` line under it, if the
collector takes none.

To watch the container instead of the host (also the setup for a rootless
user), delete the lines between `# >>> host scope` and `# <<< host scope`
in the unit; see [Container scope](../podman.md#container-scope).

## A probe from the environment

A probe can be declared by variables, one per parameter, in the same
environment file. A password is read from a Podman secret mounted as a file:

```bash
printf '%s' "$PG_PASSWORD" | sudo podman secret create senhub-pg-password -
cat >> senhub-agent.env <<'EOF'
SENHUB_PROBE_ORDERSDB_TYPE=postgresql
SENHUB_PROBE_ORDERSDB_HOST=db.example.com
SENHUB_PROBE_ORDERSDB_USERNAME=monitor
SENHUB_PROBE_ORDERSDB_PASSWORD_FILE=/run/secrets/senhub-pg-password
EOF
```

and, in the `[Container]` section of the unit, next to the token's line:

```ini
Secret=senhub-pg-password,type=mount,target=/run/secrets/senhub-pg-password,mode=0400,uid=10001
```

The environment file is read **when the configuration volume holds no
`agent.yaml`**, that is at the first start. To rebuild the configuration
from a changed file, stop the unit and remove the configuration volume (the
identity lives in the other volume and survives):

```bash
sudo systemctl stop senhub-agent
sudo podman volume rm senhub-agent-config
sudo systemctl start senhub-agent
```

The `SENHUB_PROBE_<NAME>_*` variables are the exception: the agent itself
reads them at every start, so a change to them needs only a restart. See
[Probes from environment variables](../configuration.md#configuring-probes-from-environment-variables).

## Pinning

The `Image=` line is the pin:

```ini
Image=ghcr.io/senhub-io/senhub-agent:0.6.1
```

Only exact version tags are published, there is no `latest`. The shipped
unit carries `AutoUpdate=registry`, which pulls the same tag again and
restarts the unit only if the registry serves a different image under it;
it never moves to a new version. To pin what is run to a build you have
reviewed, use the digest and delete the `AutoUpdate=` line:

```bash
sudo podman pull ghcr.io/senhub-io/senhub-agent:0.6.1
sudo podman image inspect ghcr.io/senhub-io/senhub-agent:0.6.1 --format '{{.Digest}}'
```

```ini
Image=ghcr.io/senhub-io/senhub-agent@sha256:<digest>
```

The `-oss` image carries only the free probes.

## Licence

The free tier needs none. For the paid probes, create a Podman secret and
uncomment the two lines the unit ships for it:

```bash
printf '%s' "$LICENSE" | sudo podman secret create senhub-license -
```

```ini
Secret=senhub-license,type=mount,target=/run/secrets/senhub-license,mode=0400,uid=10001
Environment=SENHUB_LICENSE_FILE=/run/secrets/senhub-license
```

then `sudo systemctl daemon-reload && sudo systemctl restart senhub-agent`.
The agent keeps the licence in the configuration volume. To replace it
later, activate the new one inside the running container, then restart:

```bash
printf '%s' "$NEW_LICENSE" | sudo podman exec -i senhub-agent \
  senhub-agent license activate --config-path /etc/senhub-agent/agent.yaml -
sudo systemctl restart senhub-agent
```

## Secrets

Secrets are mounted as **files**, never given as environment values:
`podman inspect` prints every environment variable in clear, and that
includes a secret given with `type=env`. The mounted file is owned by the
agent's uid 10001 and readable by it alone, and `podman inspect` shows the
mount, not its content.

Rotating one is replacing the secret and restarting the unit:

```bash
sudo podman secret rm senhub-otlp-token
printf '%s' "$NEW_TOKEN" | sudo podman secret create senhub-otlp-token -
sudo systemctl restart senhub-agent
```

## Validation

Two checks before the unit runs, one after.

```bash
sh check-quadlet.sh      # from packaging/podman: asks the Quadlet generator, installs nothing
sudo /usr/lib/systemd/system-generators/podman-system-generator --dryrun
```

`check-quadlet.sh` hands the unit to the generator in dry-run mode, rootful
and rootless, as shipped and with every optional line enabled, and fails on
any key or value the installed Podman does not accept. If `systemctl start`
later answers that the unit does not exist, the generator refused the file,
and the second command says why.

After the start, the entrypoint has checked the configuration it wrote and
stops the container on an error, with the reason in the journal. By hand:

```bash
sudo podman exec senhub-agent senhub-agent config check --json
echo "exit $?"
```

Exit `0` is clean, `1` warnings only, `2` an error.

## Upgrade

Edit the `Image=` line, then:

```bash
sudo systemctl daemon-reload
sudo systemctl restart senhub-agent
```

The first start of a new image pulls it, and the unit allows it fifteen
minutes. Both volumes survive, so the agent keeps its identity and its
configuration. The agent's own auto-update is off in a container.

## Removal

```bash
sudo systemctl stop senhub-agent
sudo rm /etc/containers/systemd/senhub-agent.container /etc/containers/systemd/senhub-agent.env
sudo systemctl daemon-reload
sudo podman secret rm senhub-otlp-token senhub-pg-password senhub-license
sudo podman volume rm senhub-agent-config senhub-agent-state   # the identity goes with the state
```

Leave the volumes if the host may come back: the agent then returns as the
same agent. The two secrets that were never created make `secret rm` print
an error you can ignore.

## Verify

```bash
sudo systemctl is-active senhub-agent
sudo podman ps --filter name=senhub-agent --format '{{.Image}} {{.Status}}'
sudo podman healthcheck run senhub-agent && echo healthy
sudo journalctl -u senhub-agent -n 20 --no-pager | grep -E "configuration|checked"
sudo podman exec senhub-agent senhub-agent --version
sudo podman exec senhub-agent senhub-agent config check; echo "exit $?"
sudo podman exec senhub-agent senhub-agent license show
curl -fsS http://127.0.0.1:8080/health
sudo podman inspect senhub-agent --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -E 'TOKEN|PASSWORD|LICENSE'
sudo podman exec senhub-agent senhub-agent key show
sudo systemctl restart senhub-agent && sleep 20 && sudo podman exec senhub-agent senhub-agent key show
```

Expected: the unit `active`, the pinned image, `healthy`, the `checked`
line, `config check` exit `0`, the licence tier you provisioned, `/health`
answering, only paths under `/run/secrets` in the last environment filter
(never a value), and the same agent key before and after a restart.
