#!/bin/sh
# The agent in a container: build a configuration from the environment on
# first start, then hand the process over.
#
# Three ways to use the image, in order of how much you want to decide:
#
#   1. Set OTLP_BEARER_TOKEN and nothing else. The agent generates its own
#      key, watches the host it runs on, and pushes to the SenHub ingest.
#   2. Set the variables below for what your deployment needs: another
#      collector, a licence, tags, a Container Apps log stream.
#   3. Mount your own /etc/senhub-agent. Nothing here is written then,
#      and every variable is ignored: your files win.
#
# The agent must be the main process. Started in the background by a
# script it believes it runs as a service, writes to a syslog a container
# does not have, and stops on "Failed to create service logger" — hence
# the exec at the end, and no trailing command.
set -eu

CONFIG_DIR="${SENHUB_CONFIG_DIR:-/etc/senhub-agent}"
CONFIG="$CONFIG_DIR/agent.yaml"

log() { printf '%s entrypoint: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }

if [ -f "$CONFIG" ]; then
  log "using the configuration already present at $CONFIG; every SENHUB_* variable is ignored"
else
  endpoint="${SENHUB_OTLP_ENDPOINT:-eu-west-1.intake.senhub.io:443}"
  protocol="${SENHUB_OTLP_PROTOCOL:-grpc}"

  set -- --config-path "$CONFIG" --http-port "${SENHUB_HTTP_PORT:-8080}"
  [ -n "${SENHUB_OTLP_ENDPOINT:-}" ] || [ -n "${OTLP_BEARER_TOKEN:-}" ] && set -- "$@" --otlp-endpoint "$endpoint" --otlp-protocol "$protocol"
  [ -n "${SENHUB_LICENSE:-}" ] && set -- "$@" --license "$SENHUB_LICENSE"
  [ -n "${SENHUB_TAGS:-}" ] && set -- "$@" --tags "$SENHUB_TAGS"

  log "no configuration found; writing one from the environment"
  senhub-agent config init "$@"

  # The token never lands on disk: the fragment carries the reference and
  # the agent resolves it from the environment at every start.
  if [ -n "${OTLP_BEARER_TOKEN:-}" ] && [ -f "$CONFIG_DIR/strategies.d/10-otlp.yaml" ]; then
    if ! grep -q 'Authorization' "$CONFIG_DIR/strategies.d/10-otlp.yaml"; then
      printf '  headers:\n    Authorization: "Bearer ${env:OTLP_BEARER_TOKEN}"\n' \
        >> "$CONFIG_DIR/strategies.d/10-otlp.yaml"
      log "OTLP export will authenticate with OTLP_BEARER_TOKEN"
    fi
  elif [ -z "${OTLP_BEARER_TOKEN:-}" ]; then
    log "OTLP_BEARER_TOKEN is not set: the agent will collect but export nothing to SenHub"
  fi

  # A Container Apps log stream, when the deployment names one. Every
  # value stays a reference: the client secret is never written down.
  if [ -n "${SENHUB_AZURE_APP:-}" ]; then
    for required in SENHUB_AZURE_TENANT_ID SENHUB_AZURE_CLIENT_ID SENHUB_AZURE_CLIENT_SECRET SENHUB_AZURE_SUBSCRIPTION_ID SENHUB_AZURE_RESOURCE_GROUP; do
      eval "value=\${$required:-}"
      if [ -z "$value" ]; then
        log "SENHUB_AZURE_APP is set but $required is not; the Container Apps probe needs both"
        exit 1
      fi
    done
    mkdir -p "$CONFIG_DIR/probes.d"
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
    bookmark_path: ${SENHUB_STATE_DIR:-/var/lib/senhub-agent}/${SENHUB_AZURE_APP}.bookmark
YAML
    log "reading the console log stream of the Container App ${SENHUB_AZURE_APP}"
  fi
fi

exec senhub-agent "$@" --config-path "$CONFIG"
