# Docker Compose

A Compose file describes the agent once: the image and its version, the
variables, the secrets, the state volume. Applying it twice changes
nothing. This page is the Compose form of
[Running the agent in a container](../container.md), which holds the
reference for every variable.

## The file

`compose.yaml`:

```yaml
services:
  senhub-agent:
    image: ghcr.io/senhub-io/senhub-agent-oss:0.6.1
    container_name: senhub-agent
    hostname: docker01
    restart: unless-stopped
    environment:
      SENHUB_OTLP_ENDPOINT: collector.example.com:4317
      OTLP_BEARER_TOKEN_FILE: /run/secrets/otlp_token
      SENHUB_TAGS: site=paris,env=prod
      SENHUB_PROBE_ORDERSDB_TYPE: postgresql
      SENHUB_PROBE_ORDERSDB_HOST: db.example.com
      SENHUB_PROBE_ORDERSDB_USERNAME: monitor
      SENHUB_PROBE_ORDERSDB_PASSWORD_FILE: /run/secrets/pg_password
    secrets:
      - otlp_token
      - pg_password
    volumes:
      - senhub-state:/var/lib/senhub-agent
    ports:
      - "127.0.0.1:8080:8080"

secrets:
  otlp_token:
    file: ./secrets/otlp_token
  pg_password:
    file: ./secrets/pg_password

volumes:
  senhub-state:
```

Create the secret files before the first start, readable by the image's user
(uid 10001) and by nobody else:

```bash
mkdir -p secrets
printf '%s' "$OTLP_TOKEN" > secrets/otlp_token
printf '%s' "$PG_PASSWORD" > secrets/pg_password
sudo chown 10001 secrets/otlp_token secrets/pg_password
sudo chmod 0400 secrets/otlp_token secrets/pg_password
```

Then:

```bash
docker compose up -d
```

What each part is for:

- `hostname` is the name the host shows under. Docker sets it to the
  container id unless told otherwise, so a recreated container would show up
  under a new name.
- `senhub-state` is the one mount that matters: the host identity, the
  agent key, the log bookmarks and the on-disk log queue
  (`otlp-queue/`) live there. Without it every recreation is a new host.
  See [the one mount that matters](../container.md#the-one-mount-that-matters).
- `127.0.0.1:8080:8080` publishes the console and the PRTG, Nagios and
  Prometheus endpoints on the Docker host only. Use `8080:8080` to reach them
  from the network, and then read [HTTP / HTTPS](../http-https.md) first: the
  agent key travels in every request.
- `SENHUB_PROBE_<NAME>_*` declares a probe from the environment, one variable
  per parameter, typed from the probe's schema. A secret is read from a file
  with the `_FILE` suffix. The rule is the agent's, see
  [Probes from variables](../container.md#probes-from-variables).
- The variables are read when the container is created, to write the
  configuration. A changed variable means a recreated container, which
  `docker compose up -d` does by itself.

!!! note "Secrets as files for the token and the licence"
    `OTLP_BEARER_TOKEN_FILE` and `SENHUB_LICENSE_FILE` are read by the
    entrypoint of the image that ships with the Podman unit. On an image
    that predates it, use `OTLP_BEARER_TOKEN` and `SENHUB_LICENSE` instead,
    and keep them out of the Compose file by reading them from an
    environment file that is not committed (`env_file: .env`). The probe
    parameters with `_FILE` are read by the agent itself and work on every
    image. This note goes when the image is published.

## Pinning

The tag is the pin: `0.6.1`. Tags are exact versions; there is no `latest`
and no minor tag. For a pin that survives a re-publication of the tag, use
the digest:

```bash
docker buildx imagetools inspect ghcr.io/senhub-io/senhub-agent-oss:0.6.1 | grep Digest
```

```yaml
    image: ghcr.io/senhub-io/senhub-agent-oss@sha256:<digest>
```

`senhub-agent-oss` is the open source image, which holds the free probes
only; `senhub-agent` is the full one. Switching from one to the other is an
edit of the `image` line.

## Licence

The free tier needs none. For the paid probes, add the licence as a secret
file, the same way as the token:

```yaml
    environment:
      SENHUB_LICENSE_FILE: /run/secrets/senhub_license
    secrets:
      - senhub_license
```

```yaml
secrets:
  senhub_license:
    file: ./secrets/senhub_license
```

The agent keeps the licence in its configuration for the life of the
container. A new licence is a recreated container: `docker compose up -d
--force-recreate`. A licence token never goes in an `environment:` value of
a file that is committed: `docker inspect` shows every variable in clear.

## Secrets

Everything secret in the file above is a path, not a value: the token, the
licence and the database password are files mounted under `/run/secrets`.
`docker inspect` shows the mounts, not their contents. Rotating a secret is
writing the new file and restarting:

```bash
printf '%s' "$NEW_TOKEN" > secrets/otlp_token
docker compose restart senhub-agent
```

The agent reads each file again at every start and trims a trailing newline.

## Validation

Compose checks its own syntax and the agent checks its configuration, in
that order, before anything runs.

```bash
docker compose config -q && echo "compose file valid"
docker compose run --rm --no-deps senhub-agent config check
echo "exit $?"
```

The second command builds the configuration from the variables in a throwaway
container, with the same mounts, and runs `config check` on it: exit `0`
clean, `1` warnings only, `2` an error, with the findings on screen. `--json`
gives them as a document.

The entrypoint runs the same check at every container start and refuses an
error: the container stops with the reason in `docker compose logs`. With
`restart: unless-stopped` it is restarted and stops again, so run the
validation above before `up -d` on a change.

## Upgrade

Edit the tag (or the digest), then:

```bash
docker compose pull
docker compose up -d
```

The container is recreated, the volume stays, so the agent keeps its
identity, its key and its bookmarks. The agent's own auto-update is off in a
container: it would replace a binary in a layer the next container
discards. Updating means running a new image.

## Removal

```bash
docker compose down            # removes the container, keeps the state volume
docker compose down --volumes  # also removes senhub-state: the identity is lost
```

Keep the volume if the host may come back: a reinstall then finds its key
and its bookmarks. Delete `./secrets` yourself; Compose does not.

## Verify

Run from the directory of `compose.yaml`, after `docker compose up -d`.

```bash
docker compose ps
docker compose logs senhub-agent | grep "configuration written and checked"
docker compose exec senhub-agent senhub-agent --version
docker inspect senhub-agent --format '{{.Config.Image}}'
docker inspect senhub-agent --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -E 'TOKEN|PASSWORD|LICENSE'
curl -fsS http://127.0.0.1:8080/health
docker compose exec senhub-agent senhub-agent key show > /tmp/key.before
docker compose down && docker compose up -d
docker compose exec senhub-agent senhub-agent key show | diff - /tmp/key.before && echo "same agent key"
```

Expected: the service `running` and `healthy`, the `checked` line in the
log, the pinned image, and only paths under `/run/secrets` in the lines that
mention a token, a password or a licence (never a value). `/health` answers,
and after the recreation the agent key is the same.
