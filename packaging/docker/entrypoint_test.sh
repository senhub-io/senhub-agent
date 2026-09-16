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

kept_id=0123456789abcdef0123456789abcdef
kept_key=11111111-2222-3333-4444-555555555555

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

exit "$fail"
