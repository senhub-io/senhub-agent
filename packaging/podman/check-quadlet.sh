#!/bin/sh
# Check that senhub-agent.container is a unit Quadlet accepts, rootful and
# rootless, as shipped and with every optional line turned on.
#
# Runs on a Linux host with Podman 4.6 or later. It installs nothing and
# starts nothing: it hands copies of the files to the Quadlet generator in
# dry-run mode, through QUADLET_UNIT_DIRS, and reads the service it would
# write. An unknown key, a bad value or a missing option fails the check.
#
#   sh packaging/podman/check-quadlet.sh
#
# QUADLET=/path/to/quadlet overrides the generator lookup.
set -eu

here=$(cd "$(dirname "$0")" && pwd)

find_quadlet() {
  if [ -n "${QUADLET:-}" ]; then
    printf '%s\n' "$QUADLET"
    return 0
  fi
  for candidate in \
    /usr/libexec/podman/quadlet \
    /usr/lib/podman/quadlet \
    /usr/lib/systemd/system-generators/podman-system-generator; do
    if [ -x "$candidate" ]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

if ! quadlet=$(find_quadlet); then
  echo "no Quadlet generator found: install Podman 4.6 or later, or set QUADLET" >&2
  exit 2
fi
echo "generator: $quadlet"
if command -v podman >/dev/null 2>&1; then
  podman --version
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

failures=0

fail() {
  echo "FAIL: $*" >&2
  failures=$((failures + 1))
}

# expect NAME PATTERN: the generated ExecStart must carry PATTERN.
expect() {
  if ! grep -q -- "$2" "$work/out"; then
    fail "$1: generated service lacks '$2'"
  fi
}

# run_case NAME MODE: generate the service from $work/units and check it.
run_case() {
  name=$1
  mode=$2
  set -- -dryrun
  if [ "$mode" = rootless ]; then
    set -- "$@" -user
  fi
  if ! QUADLET_UNIT_DIRS="$work/units" "$quadlet" "$@" > "$work/out" 2> "$work/err"; then
    fail "$name ($mode): generator exited non-zero"
    cat "$work/err" >&2
    return 0
  fi
  # Older generators report a conversion error on stderr and still exit 0.
  if grep -qi 'error\|unsupported' "$work/err"; then
    fail "$name ($mode): generator reported a problem"
    cat "$work/err" >&2
    return 0
  fi
  if ! grep -q '^ExecStart=.*podman run' "$work/out"; then
    fail "$name ($mode): no senhub-agent.service in the output"
    cat "$work/out" "$work/err" >&2
    return 0
  fi

  expect "$name" '--name[= ]senhub-agent'
  expect "$name" 'io.containers.autoupdate=registry'
  expect "$name" '--env-file'
  expect "$name" 'senhub-agent-state:/var/lib/senhub-agent'
  expect "$name" 'senhub-agent-config:/etc/senhub-agent'
  expect "$name" '--stop-timeout[= ]30'
  expect "$name" '--health-cmd'
  expect "$name" '--health-on-failure kill'
  expect "$name" 'ghcr.io/senhub-io/senhub-agent:'
  expect "$name" '^Restart=always'
  expect "$name" '^TimeoutStartSec=900'
  case "$name" in
    container-scope)
      expect "$name" '8080:8080'
      if grep -q -- '--network host\|--net=host\|--network=host\|--pid' "$work/out"; then
        fail "$name: host namespaces are still there"
      fi
      ;;
    *)
      expect "$name" '--network[= ]host'
      expect "$name" '--pid[= ]host'
      expect "$name" ':/host:ro,rslave\|/:/host'
      expect "$name" 'SENHUB_HOST_ROOT=/host'
      expect "$name" 'senhub-otlp-token,type=mount,target=/run/secrets/senhub-otlp-token'
      expect "$name" 'OTLP_BEARER_TOKEN_FILE=/run/secrets/senhub-otlp-token'
      if grep -q -- 'type=env,target=OTLP_BEARER_TOKEN' "$work/out"; then
        fail "$name: the OTLP token is an environment variable again"
      fi
      ;;
  esac
  if [ "$name" = optional ]; then
    expect "$name" 'senhub-license,type=mount,target=/run/secrets/senhub-license'
    expect "$name" 'SENHUB_LICENSE_FILE=/run/secrets/senhub-license'
    expect "$name" 'type=env,target=SENHUB_AZURE_CLIENT_SECRET'
    expect "$name" '/etc/senhub-agent/probes.d:Z,U'
    expect "$name" 'label[=: ]disable'
  fi
  echo "ok: $name ($mode)"
}

mkdir -p "$work/units"
cp "$here/senhub-agent.env" "$work/units/"

cp "$here/senhub-agent.container" "$work/units/"
for mode in rootful rootless; do
  run_case shipped "$mode"
done

# Every commented Secret=, Volume= and HostName= line turned on.
sed -e 's/^#\(Secret=\)/\1/' \
    -e 's/^#\(Volume=\)/\1/' \
    -e 's/^#\(SecurityLabelDisable=\)/\1/' \
    -e 's/^#\(Environment=SENHUB_LICENSE_FILE\)/\1/' \
    "$here/senhub-agent.container" > "$work/units/senhub-agent.container"
for mode in rootful rootless; do
  run_case optional "$mode"
done

# Container scope: the host block removed, the port published, the host
# name set.
sed -e '/^# >>> host scope/,/^# <<< host scope/d' \
    -e 's/^#\(PublishPort=\)/\1/' \
    -e 's/^#\(HostName=\)/\1/' \
    "$here/senhub-agent.container" > "$work/units/senhub-agent.container"
for mode in rootful rootless; do
  run_case container-scope "$mode"
done

# Control: the same generator must refuse a key it does not know, or the
# passes above prove nothing about the keys used.
sed -e 's/^HealthRetries=3$/HealthRetries=3\
SenHubNoSuchKey=1/' \
    "$here/senhub-agent.container" > "$work/units/senhub-agent.container"
if QUADLET_UNIT_DIRS="$work/units" "$quadlet" -dryrun > "$work/out" 2> "$work/err" \
   && ! grep -qi 'error\|unsupported' "$work/err" \
   && grep -q '^ExecStart=.*podman run' "$work/out"; then
  fail "control: the generator accepted an unknown key"
else
  echo "ok: control (an unknown key is refused)"
fi

if [ "$failures" -ne 0 ]; then
  echo "$failures check(s) failed" >&2
  exit 1
fi
echo "senhub-agent.container: all checks passed"
