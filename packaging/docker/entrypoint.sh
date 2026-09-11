#!/bin/sh
# The agent in a container: build a configuration from the environment on
# first start, then hand the process over.
#
# Three ways to use the image, in order of how much you want to decide:
#
#   1. Set OTLP_BEARER_TOKEN and nothing else. The agent generates its
#      own key, watches the host it runs on, and pushes to SenHub.
#   2. Add the variables your deployment needs: another collector, a
#      licence, tags, a Container Apps log stream.
#   3. Mount your own /etc/senhub-agent. Nothing is written then and
#      every variable is ignored: your files win.
#
# The agent must be the main process. Started in the background by a
# script it believes it runs as a service, writes to a syslog a container
# does not have, and stops on "Failed to create service logger" — hence
# the exec at the end.
set -eu

CONFIG_DIR="${SENHUB_CONFIG_DIR:-/etc/senhub-agent}"
CONFIG="$CONFIG_DIR/agent.yaml"
STATE_DIR="${SENHUB_STATE_DIR:-/var/lib/senhub-agent}"

log() { printf '%s entrypoint: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }

# init_config writes agent.yaml and the output from the environment.
# The flags are assembled with `set --` inside this function on purpose:
# doing it in the body would overwrite the command the container was
# given, and the agent would then be started with the init flags.
init_config() {
  set -- --config-path "$CONFIG" --http-port "${SENHUB_HTTP_PORT:-8080}"

  endpoint="${SENHUB_OTLP_ENDPOINT:-}"
  if [ -z "$endpoint" ] && [ -n "${OTLP_BEARER_TOKEN:-}" ]; then
    endpoint="eu-west-1.intake.senhub.io:443"
  fi
  if [ -n "$endpoint" ]; then
    set -- "$@" --otlp-endpoint "$endpoint" --otlp-protocol "${SENHUB_OTLP_PROTOCOL:-grpc}"
  fi
  if [ -n "${SENHUB_LICENSE:-}" ]; then
    set -- "$@" --license "$SENHUB_LICENSE"
  fi
  if [ -n "${SENHUB_TAGS:-}" ]; then
    set -- "$@" --tags "$SENHUB_TAGS"
  fi

  senhub-agent config init "$@"

  if [ -n "${OTLP_BEARER_TOKEN:-}" ]; then
    fragment="$CONFIG_DIR/strategies.d/10-otlp.yaml"
    if [ -f "$fragment" ] && ! grep -q 'Authorization' "$fragment"; then
      # The token stays out of the file: the fragment carries the
      # reference and the agent resolves it at every start.
      # shellcheck disable=SC2016 # ${env:...} must reach the file literally
      printf '  headers:\n    Authorization: "Bearer ${env:OTLP_BEARER_TOKEN}"\n' >> "$fragment"
      log "OTLP export authenticates with OTLP_BEARER_TOKEN"
    fi
  else
    log "OTLP_BEARER_TOKEN is not set: the agent collects, and exports nothing to SenHub"
  fi
}

write_azure_probe() {
  missing=""
  for name in SENHUB_AZURE_TENANT_ID SENHUB_AZURE_CLIENT_ID SENHUB_AZURE_CLIENT_SECRET \
              SENHUB_AZURE_SUBSCRIPTION_ID SENHUB_AZURE_RESOURCE_GROUP; do
    eval "value=\${$name:-}"
    if [ -z "$value" ]; then
      missing="$missing $name"
    fi
  done
  if [ -n "$missing" ]; then
    log "SENHUB_AZURE_APP is set but these are not:$missing"
    log "the Container Apps probe needs all of them; nothing else is affected, the agent stops here rather than start half configured"
    exit 1
  fi
  mkdir -p "$CONFIG_DIR/probes.d"
  # Every credential stays a reference: the secret is read from the
  # environment at each start and never written to the file.
  cat > "$CONFIG_DIR/probes.d/50-azure-container-apps.yaml" <<YAML
- name: ${SENHUB_AZURE_APP}
  type: azure_container_apps
  params:
    tenant_id: "\${env:SENHUB_AZURE_TENANT_ID}"
    client_id: "\${env:SENHUB_AZURE_CLIENT_ID}"
    client_secret: "\${env:SENHUB_AZURE_CLIENT_SECRET}"
    subscription_id: "\${env:SENHUB_AZURE_SUBSCRIPTION_ID}"
    resource_group: "\${env:SENHUB_AZURE_RESOURCE_GROUP}"
    app: "${SENHUB_AZURE_APP}"
    bookmark_path: ${STATE_DIR}/${SENHUB_AZURE_APP}.bookmark
YAML
  log "reading the console log stream of the Container App ${SENHUB_AZURE_APP}"
}

if [ -f "$CONFIG" ]; then
  log "configuration already present at $CONFIG; every SENHUB_* variable is ignored"
else
  log "no configuration found; writing one from the environment"
  init_config

  # Any probe, without a mount: the variable carries the same YAML a
  # file in probes.d would. Container platforms make a file harder to
  # place than a variable, which is the whole reason this exists.
  if [ -n "${SENHUB_PROBES:-}" ]; then
    mkdir -p "$CONFIG_DIR/probes.d"
    printf '%s\n' "$SENHUB_PROBES" > "$CONFIG_DIR/probes.d/50-from-env.yaml"
    log "probes read from SENHUB_PROBES"
  fi

  # The same door for outputs: one file per output, as strategies.d
  # expects, so several outputs mean several variables or a mount.
  if [ -n "${SENHUB_OUTPUT:-}" ]; then
    mkdir -p "$CONFIG_DIR/strategies.d"
    printf '%s\n' "$SENHUB_OUTPUT" > "$CONFIG_DIR/strategies.d/50-from-env.yaml"
    log "output read from SENHUB_OUTPUT"
  fi

  # A shorthand for the one probe this image is most often asked for.
  # It writes the same kind of fragment SENHUB_PROBES would carry.
  if [ -n "${SENHUB_AZURE_APP:-}" ]; then
    write_azure_probe
  fi

  if senhub-agent config check --config-path "$CONFIG" >/dev/null 2>&1; then
    log "configuration written and checked"
  else
    log "the configuration that was written does not pass config check:"
    senhub-agent config check --config-path "$CONFIG" >&2 || true
    log "fix the variables, or mount a configuration of your own"
    exit 1
  fi
fi

# The state directory is always writable: it belongs to this user inside
# the image. What matters is whether it is a MOUNT. Without one it lives
# in the container's own layer and disappears with the container, so the
# agent generates a new key on every restart and the same host arrives
# under a new identity each time. That is worth saying out loud, because
# it is invisible until someone reads the graph weeks later.
if [ ! -w "$STATE_DIR" ]; then
  log "$STATE_DIR is not writable: the agent cannot keep its key or its bookmarks"
  log "mount a volume there, or fix its ownership"
elif ! awk -v d="$STATE_DIR" '$2 == d { found = 1 } END { exit !found }' /proc/mounts 2>/dev/null; then
  log "$STATE_DIR is not a mounted volume: the agent key and the log bookmarks live in this container only"
  log "every restart will give this agent a new identity, and every log probe will re-read its tail"
  log "mount a volume on $STATE_DIR to keep them"
fi

exec senhub-agent "$@" --config-path "$CONFIG"
