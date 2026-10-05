#!/bin/sh
# Exercises entrypoint.sh without a container: the identity resolution,
# whose answer decides whether a redeployed agent keeps being the same host
# and the same agent and stays invisible until weeks later in the graph, and
# the Container Apps shorthand, which turns a variable into the probe list.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail=0
check() {
  if [ "$2" = "$3" ]; then
    printf 'ok   %s\n' "$1"
  else
    printf 'FAIL %s\n     expected: %s\n     got:      %s\n' "$1" "$3" "$2"
    fail=1
  fi
}

# Load the functions without running the body.
sed '/^resolve_machine_id$/,$d' "$here/entrypoint.sh" > "$work/lib.sh"

SENHUB_STATE_DIR="$work/state"
SENHUB_CONFIG_DIR="$work/conf"
SENHUB_MACHINE_ID_PATH="$work/machine-id"
export SENHUB_STATE_DIR SENHUB_CONFIG_DIR SENHUB_MACHINE_ID_PATH
mkdir -p "$SENHUB_STATE_DIR" "$SENHUB_CONFIG_DIR"
: > "$SENHUB_MACHINE_ID_PATH"

# shellcheck source=/dev/null
. "$work/lib.sh"

# 0. SENHUB_ZABBIX_SERVER reaches config init, with its metadata; nothing
#    Zabbix is passed when the variable is unset.
# 0b. The HTTP output listens beyond the container's loopback by default,
#    and SENHUB_HTTP_BIND narrows it.
mkdir -p "$work/bin"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" > "%s/init-args"\n' "$work" > "$work/bin/senhub-agent"
chmod +x "$work/bin/senhub-agent"
saved_path=$PATH
PATH="$work/bin:$PATH"
SENHUB_ZABBIX_SERVER=zbx.example.com:10051
SENHUB_ZABBIX_HOST_METADATA=senhub-agent
init_config >/dev/null 2>&1 || true
unset SENHUB_ZABBIX_SERVER SENHUB_ZABBIX_HOST_METADATA
case "$(cat "$work/init-args")" in
  *"--zabbix-server zbx.example.com:10051 --zabbix-host-metadata senhub-agent"*) check "SENHUB_ZABBIX_SERVER reaches config init" "yes" "yes" ;;
  *) check "SENHUB_ZABBIX_SERVER reaches config init" "$(cat "$work/init-args")" "--zabbix-server zbx.example.com:10051 --zabbix-host-metadata senhub-agent" ;;
esac
init_config >/dev/null 2>&1 || true
case "$(cat "$work/init-args")" in
  *zabbix*) check "no Zabbix flag without SENHUB_ZABBIX_SERVER" "$(cat "$work/init-args")" "no zabbix flag" ;;
  *) check "no Zabbix flag without SENHUB_ZABBIX_SERVER" "ok" "ok" ;;
esac
case "$(cat "$work/init-args")" in
  *"--http-bind 0.0.0.0"*) check "the HTTP output listens on every address by default" "yes" "yes" ;;
  *) check "the HTTP output listens on every address by default" "$(cat "$work/init-args")" "--http-bind 0.0.0.0" ;;
esac
SENHUB_HTTP_BIND=127.0.0.1
init_config >/dev/null 2>&1 || true
unset SENHUB_HTTP_BIND
case "$(cat "$work/init-args")" in
  *"--http-bind 127.0.0.1"*) check "SENHUB_HTTP_BIND sets the address" "yes" "yes" ;;
  *) check "SENHUB_HTTP_BIND sets the address" "$(cat "$work/init-args")" "--http-bind 127.0.0.1" ;;
esac
PATH=$saved_path
rm -rf "$SENHUB_CONFIG_DIR" && mkdir -p "$SENHUB_CONFIG_DIR"

kept_id=0123456789abcdef0123456789abcdef
kept_key=11111111-2222-3333-4444-555555555555

# 0c. With HOST_ETC pointing at a host's /etc, the host's machine-id is
#    the identity: nothing is written, and an explicit SENHUB_HOST_ID
#    still wins.
mkdir -p "$work/hostetc"
printf '%s\n' "fedcba9876543210fedcba9876543210" > "$work/hostetc/machine-id"
printf '%s\n' "keepme" > "$MACHINE_ID_PATH"
(HOST_ETC="$work/hostetc" resolve_machine_id) 2>/dev/null
check "the host's own machine-id is left alone" "$(cat "$MACHINE_ID_PATH")" "keepme"
: > "$MACHINE_ID_PATH"

# 1. A machine-id kept in the state directory is restored verbatim.
printf '%s\n' "$kept_id" > "$STATE_DIR/machine-id"
resolve_machine_id 2>/dev/null
check "machine-id restored from the state directory" "$(cat "$MACHINE_ID_PATH")" "$kept_id"

# 2. SENHUB_HOST_ID wins over the kept one, dashes and case absorbed.
SENHUB_HOST_ID="AABBCCDD-EEFF-0011-2233-445566778899"
export SENHUB_HOST_ID
resolve_machine_id 2>/dev/null
check "SENHUB_HOST_ID wins and is normalised" "$(cat "$MACHINE_ID_PATH")" "aabbccddeeff00112233445566778899"
unset SENHUB_HOST_ID

# 2b. An example or blank SENHUB_HOST_ID is refused rather than written:
#     every container given it would be one host on the graph.
for bad in 01234567-89ab-cdef-0123-456789abcdef 00000000000000000000000000000000; do
  SENHUB_HOST_ID=$bad
  export SENHUB_HOST_ID
  if (resolve_machine_id 2>/dev/null); then
    check "SENHUB_HOST_ID=$bad is refused" "accepted" "refused"
  else
    check "SENHUB_HOST_ID=$bad is refused" "refused" "refused"
  fi
done
unset SENHUB_HOST_ID
for good in 6a6d1121-4a85-4e64-a222-746f7bc9c04c aabbccddeeff00112233445566778899; do
  if degenerate_machine_id "$(printf '%s' "$good" | tr -d '-')"; then
    check "$good is accepted" "refused" "accepted"
  else
    check "$good is accepted" "accepted" "accepted"
  fi
done

# 3. A corrupt kept machine-id is ignored rather than written through: the
#    file ends up with a fresh identity (Linux, where /proc provides one) or
#    empty (macOS, where it does not), never with the corrupt value.
printf 'not-a-machine-id\n' > "$STATE_DIR/machine-id"
: > "$MACHINE_ID_PATH"
resolve_machine_id 2>/dev/null || true
check "a corrupt kept machine-id is not written through" \
  "$(printf '%s\n' "$(cat "$MACHINE_ID_PATH")" | grep -Ec '^([0-9a-f]{32})?$')" "1"

# 4. The agent key kept in the state directory is put back into agent.yaml.
printf 'config_version: 3\n\nagent:\n  key: "e313cd19-45d9-4711-8b09-3f58ac6e7595"\n  # a trailing comment\n\ncache:\n  retention_minutes: 5\n' > "$CONFIG"
printf '%s\n' "$kept_key" > "$STATE_DIR/agent.key"
keep_agent_key 2>/dev/null
check "agent key restored into agent.yaml" "$(sed -n 's/^  key: "\(.*\)"$/\1/p' "$CONFIG")" "$kept_key"
check "the rest of agent.yaml is untouched" "$(grep -c 'retention_minutes\|trailing comment' "$CONFIG")" "2"

# 5. With no kept key, the generated one is saved for the next container.
rm -f "$STATE_DIR/agent.key"
printf 'config_version: 3\n\nagent:\n  key: "e313cd19-45d9-4711-8b09-3f58ac6e7595"\n' > "$CONFIG"
keep_agent_key 2>/dev/null
check "a fresh agent key is kept for the next container" "$(cat "$STATE_DIR/agent.key")" "e313cd19-45d9-4711-8b09-3f58ac6e7595"

# 5b. SENHUB_AGENT_KEY names the agent without a volume: two containers
#     with no shared state, each with a freshly generated agent.yaml,
#     come up as the same agent. It wins over a kept key.
for run in 1 2; do
  rm -rf "$STATE_DIR"; mkdir -p "$STATE_DIR"
  printf 'config_version: 3\n\nagent:\n  key: "%s"\n' "$(cat /proc/sys/kernel/random/uuid 2>/dev/null || echo 9f1c2e3d-4b5a-4c6d-8e7f-a1b2c3d4e5f$run)" > "$CONFIG"
  SENHUB_AGENT_KEY="7E1D2C3B-4A59-4687-9A0B-1C2D3E4F5A6B"
  export SENHUB_AGENT_KEY
  keep_agent_key 2>/dev/null
  check "SENHUB_AGENT_KEY names the agent, container $run" "$(sed -n 's/^  key: "\(.*\)"$/\1/p' "$CONFIG")" "7e1d2c3b-4a59-4687-9a0b-1c2d3e4f5a6b"
done
for bad in not-a-key 01234567-89ab-cdef-0123-456789abcdef; do
  SENHUB_AGENT_KEY=$bad
  if (keep_agent_key 2>/dev/null); then
    check "SENHUB_AGENT_KEY=$bad is refused" "accepted" "refused"
  else
    check "SENHUB_AGENT_KEY=$bad is refused" "refused" "refused"
  fi
done
unset SENHUB_AGENT_KEY

# 6. The Container Apps shorthand writes one entry per name of the list,
#    each with its own bookmark, so a collector follows several
#    applications without hand-written YAML.
SENHUB_AZURE_TENANT_ID=t
SENHUB_AZURE_CLIENT_ID=c
SENHUB_AZURE_CLIENT_SECRET=s
SENHUB_AZURE_SUBSCRIPTION_ID=sub
SENHUB_AZURE_RESOURCE_GROUP=rg
export SENHUB_AZURE_TENANT_ID SENHUB_AZURE_CLIENT_ID SENHUB_AZURE_CLIENT_SECRET \
       SENHUB_AZURE_SUBSCRIPTION_ID SENHUB_AZURE_RESOURCE_GROUP
fragment="$CONFIG_DIR/probes.d/50-azure-container-apps.yaml"

SENHUB_AZURE_APP="oltp, billing ,web"
export SENHUB_AZURE_APP
write_azure_probe 2>/dev/null
check "one entry per application of the list" "$(grep -c '^- name: ' "$fragment")" "3"
check "the spaces around a name are absorbed" "$(sed -n 's/^- name: //p' "$fragment" | tr '\n' ',')" "oltp,billing,web,"
check "each application gets its own bookmark" \
  "$(sed -n 's/.*bookmark_path: //p' "$fragment" | tr '\n' ',')" \
  "$STATE_DIR/oltp.bookmark,$STATE_DIR/billing.bookmark,$STATE_DIR/web.bookmark,"
check "the credentials stay references, never values" "$(grep -c 'env:SENHUB_AZURE_CLIENT_SECRET' "$fragment")" "3"
check "no secret is written into the fragment" "$(grep -c 'client_secret: "s"' "$fragment")" "0"

# 7. A single name keeps writing exactly what it wrote before the list.
SENHUB_AZURE_APP=oltp
write_azure_probe 2>/dev/null
check "a single name writes a single entry" "$(grep -c '^- name: ' "$fragment")" "1"
check "the fragment is rewritten, not appended to" "$(grep -c 'billing' "$fragment")" "0"

# 8. A name repeated in the list is declared once: two entries of the same
#    application would read the same stream twice into the same bookmark.
SENHUB_AZURE_APP="oltp,billing,oltp"
write_azure_probe 2>/dev/null
check "a repeated name is declared once" "$(grep -c '^- name: ' "$fragment")" "2"

# 9. A name that would place the bookmark elsewhere is refused rather than
#    written through.
SENHUB_AZURE_APP="oltp,../../etc/cron.d/x"
if (write_azure_probe >/dev/null 2>&1); then
  check "a name carrying a path separator is refused" "accepted" "refused"
else
  check "a name carrying a path separator is refused" "refused" "refused"
fi
unset SENHUB_AZURE_APP

# 10. Without a volume, the warning names only what is actually lost: with
#     SENHUB_HOST_ID and SENHUB_AGENT_KEY set, the identity and the key
#     survive a new container, and only the log bookmarks do not.
unset SENHUB_HOST_ID SENHUB_AGENT_KEY
bare=$(unmounted_state_warning 2>&1)
check "a bare container is told it loses identity, key and bookmarks" \
  "$(printf '%s' "$bare" | grep -c 'host identity, agent key, log bookmarks')" "1"
check "a bare container is told it arrives as a new host" "$(printf '%s' "$bare" | grep -c 'new host')" "1"
SENHUB_HOST_ID=aabbccddeeff00112233445566778899 SENHUB_AGENT_KEY=e313cd19-45d9-4711-8b09-3f58ac6e7595
export SENHUB_HOST_ID SENHUB_AGENT_KEY
named=$(unmounted_state_warning 2>&1)
check "a named container is told it loses only its bookmarks" \
  "$(printf '%s' "$named" | grep -c 'volume: log bookmarks live')" "1"
check "a named container is not told it arrives as a new host" "$(printf '%s' "$named" | grep -c 'new host')" "0"
check "a named container is told what losing its place costs" \
  "$(printf '%s' "$named" | grep -c 're-sends its recent lines, a file probe skips')" "1"
unset SENHUB_HOST_ID SENHUB_AGENT_KEY

# 11. A collector in plain text is reachable from the variables alone:
#     SENHUB_OTLP_TLS=false turns TLS off in the written output, once, and
#     the default leaves it on. A value that is neither is refused.
frag="$work/10-otlp.yaml"
printf 'otlp:\n  endpoint: localhost:4317\n  protocol: grpc\n' > "$frag"
OTLP_BEARER_TOKEN=t SENHUB_OTLP_TLS=false
export OTLP_BEARER_TOKEN SENHUB_OTLP_TLS
otlp_fragment_extras "$frag" 2>/dev/null
otlp_fragment_extras "$frag" 2>/dev/null
check "SENHUB_OTLP_TLS=false turns TLS off" "$(grep -c 'enabled: false' "$frag")" "1"
check "the bearer reference is written once" "$(grep -c 'Authorization' "$frag")" "1"
# SENHUB_LICENSE_FILE reaches config init as the licence.
printf 'lic.jwt.value\n' > "$work/licence"
PATH="$work/bin:$PATH" SENHUB_LICENSE_FILE="$work/licence" init_config >/dev/null 2>&1 || true
case "$(cat "$work/init-args" 2>/dev/null)" in
  *"--license lic.jwt.value"*) check "SENHUB_LICENSE_FILE reaches config init" "yes" "yes" ;;
  *) check "SENHUB_LICENSE_FILE reaches config init" "$(cat "$work/init-args" 2>/dev/null)" "--license lic.jwt.value" ;;
esac

# The token as a file is referenced, never copied, and read at every start.
unset OTLP_BEARER_TOKEN
printf 'abc\n' > "$work/otlp-token"
printf 'otlp:\n  endpoint: localhost:4317\n' > "$frag"
OTLP_BEARER_TOKEN_FILE="$work/otlp-token" otlp_fragment_extras "$frag" 2>/dev/null
check "OTLP_BEARER_TOKEN_FILE is written as a file reference" \
  "$(grep -c "Authorization: \"Bearer \${file:$work/otlp-token}\"" "$frag")" "1"
check "the file's content is not copied into the fragment" "$(grep -c abc "$frag")" "0"
printf 'otlp:\n  endpoint: collector:4317\n' > "$frag"
OTLP_BEARER_TOKEN=t
unset SENHUB_OTLP_TLS
otlp_fragment_extras "$frag" 2>/dev/null
check "TLS stays on by default" "$(grep -c 'tls:' "$frag")" "0"
SENHUB_OTLP_TLS=maybe
export SENHUB_OTLP_TLS
if (otlp_fragment_extras "$frag" >/dev/null 2>&1); then
  check "SENHUB_OTLP_TLS=maybe is refused" "accepted" "refused"
else
  check "SENHUB_OTLP_TLS=maybe is refused" "refused" "refused"
fi
unset SENHUB_OTLP_TLS OTLP_BEARER_TOKEN

# 12. Entities are on by default in the image, once, and SENHUB_ENTITIES
#     turns them off.
frag="$work/10-otlp-ent.yaml"
printf 'otlp:\n  endpoint: collector:4317\n' > "$frag"
otlp_fragment_extras "$frag" 2>/dev/null
otlp_fragment_extras "$frag" 2>/dev/null
check "entities are enabled by default" "$(grep -c 'entities:' "$frag")" "1"
printf 'otlp:\n  endpoint: collector:4317\n' > "$frag"
SENHUB_ENTITIES=false
export SENHUB_ENTITIES
otlp_fragment_extras "$frag" 2>/dev/null
check "SENHUB_ENTITIES=false leaves them off" "$(grep -c 'entities:' "$frag")" "0"
unset SENHUB_ENTITIES

exit "$fail"
