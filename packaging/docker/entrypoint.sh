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
      printf '  headers:\n    Authorization: "Bearer ${env:OTLP_BEARER_TOKEN}"\n' >> "$fragment"
      log "OTLP export authenticates with OTLP_BEARER_TOKEN"
    fi
  else
    log "OTLP_BEARER_TOKEN is not set: the agent collects, and exports nothing to SenHub"
  fi

  if [ -n "${SENHUB_AZURE_APP:-}" ]; then
    write_azure_probe
  fi
fi

if [ ! -w "$STATE_DIR" ]; then
  log "$STATE_DIR is not writable: the agent key and the log bookmarks will not survive a restart"
  log "mount a volume there, or every restart gives this agent a new identity"
fi

exec senhub-agent "$@" --config-path "$CONFIG"
