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
MACHINE_ID_PATH="${SENHUB_MACHINE_ID_PATH:-/etc/machine-id}"

log() { printf '%s entrypoint: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }

valid_machine_id() {
  case "$1" in
    *[!0-9a-f]*) return 1 ;;
  esac
  [ ${#1} -eq 32 ]
}

# degenerate_machine_id succeeds for an identity no machine derives by
# chance: fewer than four distinct hexadecimal digits, or a run of at
# least sixteen ascending steps, which is the shape of an example value
# such as 01234567-89ab-cdef-0123-456789abcdef. Every host carrying one
# merges with every other host carrying it, silently, so it is refused
# rather than written. Judged on its shape, since the next copied example
# will not be on any list. Mirrors common.DegenerateHostID in the agent.
degenerate_machine_id() {
  printf '%s' "$1" | awk '{
    hex = "0123456789abcdef"; distinct = 0; asc = 0
    for (i = 1; i <= length($0); i++) {
      c = substr($0, i, 1)
      if (!(c in seen)) { seen[c] = 1; distinct++ }
      if (i > 1 && (index(hex, prev) % 16) == index(hex, c) - 1) asc++
      prev = c
    }
    exit !(distinct < 4 || asc >= 16)
  }'
}

# host.id is not the agent's own key: it is the operating system's
# machine-id, read by gopsutil from /etc/machine-id. An image carries no
# machine-id, so without one gopsutil falls back to the kernel boot id,
# which it documents as "not stable between reboot". Measured in a real
# Container App: three runs of the same image reported three different
# host.id, so the fleet saw three hosts where there is one service.
#
# The identity is resolved here, in this order:
#   1. SENHUB_HOST_ID, when the deployment already knows this host;
#   2. the machine-id kept in the state directory, so a mounted volume
#      carries one identity across restarts and image upgrades;
#   3. a fresh one, kept in the state directory when that is possible.
resolve_machine_id() {
  kept="$STATE_DIR/machine-id"

  if [ -n "${SENHUB_HOST_ID:-}" ]; then
    wanted=$(printf '%s' "$SENHUB_HOST_ID" | tr -d '-' | tr 'ABCDEF' 'abcdef')
    if ! valid_machine_id "$wanted"; then
      log "SENHUB_HOST_ID is not a machine id: 32 hexadecimal characters, dashes optional"
      exit 1
    fi
    if degenerate_machine_id "$wanted"; then
      log "SENHUB_HOST_ID looks like an example or a blank value ($SENHUB_HOST_ID): every host carrying it would merge into one. Give each instance its own"
      exit 1
    fi
    log "host identity taken from SENHUB_HOST_ID"
  elif [ -r "$kept" ] && valid_machine_id "$(cat "$kept")"; then
    wanted=$(cat "$kept")
    log "host identity restored from $kept"
  else
    wanted=$(tr -d '-' < /proc/sys/kernel/random/uuid)
    if (umask 077; printf '%s\n' "$wanted" > "$kept") 2>/dev/null; then
      log "host identity generated and kept in $kept"
    else
      log "host identity generated for this container only: $kept is not writable"
    fi
  fi

  if [ -w "$MACHINE_ID_PATH" ]; then
    printf '%s\n' "$wanted" > "$MACHINE_ID_PATH"
  else
    log "$MACHINE_ID_PATH is not writable: this host reports the identity the platform gives it"
  fi
}

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

# The agent key is the agent's own identity, distinct from host.id: it is
# what service.instance.id carries, and what tells two agents apart on the
# receiving side. `config init` mints a fresh one, and the configuration
# lives in the container layer, so without this every container would
# introduce a brand new agent while claiming to be the same host. Keeping
# it beside the machine-id gives one agent identity per volume.
keep_agent_key() {
  kept="$STATE_DIR/agent.key"

  if [ ! -r "$kept" ]; then
    key=$(sed -n 's/^  key: "\(.*\)"$/\1/p' "$CONFIG" | head -1)
    if [ -z "$key" ]; then
      log "cannot read the agent key from $CONFIG; leaving it as generated"
      return 0
    fi
    if (umask 077; printf '%s\n' "$key" > "$kept") 2>/dev/null; then
      log "agent key kept in $kept"
    else
      log "agent key generated for this container only: $kept is not writable"
    fi
    return 0
  fi

  key=$(cat "$kept")
  case "$key" in
    "" | *[!0-9a-fA-F-]*)
      log "the agent key kept in $kept is not usable; leaving the generated one"
      return 0
      ;;
  esac
  tmp="$CONFIG.new"
  if sed "s|^  key: \".*\"$|  key: \"$key\"|" "$CONFIG" > "$tmp" 2>/dev/null && mv "$tmp" "$CONFIG"; then
    log "agent key restored from $kept"
  else
    rm -f "$tmp"
    log "could not restore the agent key from $kept; leaving the generated one"
  fi
}

# trim removes the spaces around a value, so a list written with them
# ("a, b") names the same applications as one written without.
trim() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
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
  fragment="$CONFIG_DIR/probes.d/50-azure-container-apps.yaml"
  : > "$fragment"

  # One probe instance follows one application, so a comma-separated
  # list writes one entry per name, each with its own bookmark. A
  # collector that watches an environment holding several applications
  # is the ordinary case, and it must not require hand-written YAML in
  # SENHUB_PROBES.
  old_ifs=$IFS
  IFS=,
  # The function takes no arguments, so the positional parameters are
  # free to carry the split.
  # shellcheck disable=SC2086
  set -- $SENHUB_AZURE_APP
  IFS=$old_ifs

  written=""
  for raw in "$@"; do
    app=$(trim "$raw")
    if [ -z "$app" ]; then
      continue
    fi
    # The name becomes a probe name and a bookmark file name. A
    # separator or a space in it would place that file somewhere else,
    # and no Container App is named that way.
    case "$app" in
      */*|*' '*|*'	'*)
        log "SENHUB_AZURE_APP names \"$app\", which is not a Container App name"
        exit 1
        ;;
    esac
    for already in $written; do
      if [ "$already" = "$app" ]; then
        log "SENHUB_AZURE_APP names \"$app\" twice; the second one is ignored"
        app=""
        break
      fi
    done
    if [ -z "$app" ]; then
      continue
    fi
    written="$written $app"
    # Every credential stays a reference: the secret is read from the
    # environment at each start and never written to the file.
    cat >> "$fragment" <<YAML
- name: ${app}
  type: azure_container_apps
  params:
    tenant_id: "\${env:SENHUB_AZURE_TENANT_ID}"
    client_id: "\${env:SENHUB_AZURE_CLIENT_ID}"
    client_secret: "\${env:SENHUB_AZURE_CLIENT_SECRET}"
    subscription_id: "\${env:SENHUB_AZURE_SUBSCRIPTION_ID}"
    resource_group: "\${env:SENHUB_AZURE_RESOURCE_GROUP}"
    app: "${app}"
    bookmark_path: ${STATE_DIR}/${app}.bookmark
YAML
  done

  if [ -z "$written" ]; then
    log "SENHUB_AZURE_APP is set but names no application"
    exit 1
  fi
  log "reading the console log stream of the Container Apps:$written"
}

resolve_machine_id

if [ -f "$CONFIG" ]; then
  log "configuration already present at $CONFIG; every SENHUB_* variable is ignored"
else
  log "no configuration found; writing one from the environment"
  init_config
  keep_agent_key

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
    # The two doors write two different files, so an application named
    # in both is declared twice and its lines leave twice. Nothing in
    # the configuration says so, hence the warning.
    if [ -n "${SENHUB_PROBES:-}" ]; then
      log "SENHUB_AZURE_APP and SENHUB_PROBES are both set; they write separate files and are not merged"
      log "an application named in both is declared twice and its logs are collected twice — name each one in one place only"
    fi
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
# in the container's own layer and goes with the container, taking the
# host identity, the agent key and the log bookmarks with it. That is
# worth saying out loud, because it is invisible until someone reads the
# graph weeks later and finds one service spread over thirty hosts.
if [ ! -w "$STATE_DIR" ]; then
  log "$STATE_DIR is not writable: the agent cannot keep its identity, its key or its bookmarks"
  log "mount a volume there, or fix its ownership"
elif ! awk -v d="$STATE_DIR" '$2 == d { found = 1 } END { exit !found }' /proc/mounts 2>/dev/null; then
  log "$STATE_DIR is not a mounted volume: identity, agent key and log bookmarks live in this container only"
  log "every new container will arrive as a new host, and every log probe will re-read its tail"
  log "mount a volume on $STATE_DIR, or set SENHUB_HOST_ID, to keep one identity"
fi

exec senhub-agent "$@" --config-path "$CONFIG"
