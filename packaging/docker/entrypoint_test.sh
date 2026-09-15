#!/bin/sh
# Exercises the identity resolution of entrypoint.sh without a container.
# The two functions decide whether a redeployed agent keeps being the same
# host and the same agent, and that answer is invisible until weeks later
# in the graph, so it is worth a test that does not need a daemon.
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

exit "$fail"
