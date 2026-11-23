#!/usr/bin/env bash
# Abuse tests of the root update unit, on a real systemd (Debian 12 container).
#
# Usage: packaging/nfpm/test-root-update.sh [path/to/senhub-agent-oss_<v>_<arch>.deb]
# Needs docker and a .deb built by `make packages EDITION=oss PACKAGE_ARCHES=<arch>`.
# Every case writes a request that must be refused and journaled, and checks
# that nothing was installed: the binary hash is unchanged, no .previous, no
# download. Prints one line per case: "<verdict> <case>".
set -u

cd "$(dirname "$0")/../.."
case "$(docker info --format '{{.Architecture}}')" in
    aarch64|arm64) ARCH=arm64 ;;
    *) ARCH=amd64 ;;
esac
PKG=${1:-$(ls dist/packages/senhub-agent-oss_*_"$ARCH".deb | head -1)}
IMG=rootu-img-debian12
NAME=rootu-abuse
REQ=/var/lib/senhub-agent/update-request/request
UNIT=senhub-agent-update.service
FAILED=0

x() { docker exec "$NAME" bash -c "$1"; }
cleanup() { docker rm -f "$NAME" >/dev/null 2>&1; }
trap cleanup EXIT
verdict() { # ok|FAIL case detail
    printf '%s %s %s\n' "$1" "$2" "${3:-}"
    [ "$1" = ok ] || FAILED=1
}

if ! docker image inspect "$IMG" >/dev/null 2>&1; then
    printf 'FROM debian:12\nRUN apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd systemd-sysv ca-certificates sudo\n' | docker build -q -t "$IMG" - >/dev/null || { echo "FAIL image"; exit 1; }
fi
cleanup
docker run -d --name "$NAME" --cpus=1 --privileged --cgroupns=host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw -v "$(cd "$(dirname "$PKG")" && pwd)":/pkgs:ro \
    --tmpfs /run --tmpfs /run/lock "$IMG" /usr/lib/systemd/systemd >/dev/null || exit 1
for _ in $(seq 1 30); do
    x "systemctl is-system-running" 2>/dev/null | grep -qE 'running|degraded' && break
    sleep 1
done
x "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq /pkgs/$(basename "$PKG")" >/dev/null 2>&1 || { echo "FAIL package install"; exit 1; }
x "useradd -m eve" >/dev/null

x "systemctl is-enabled senhub-agent-update.path && systemctl is-active senhub-agent-update.path" >/dev/null \
    && verdict ok "path unit enabled and waiting" || verdict FAIL "path unit enabled and waiting"

BIN_SHA=$(x "sha256sum /usr/bin/senhub-agent | cut -d' ' -f1")
SHADOW_SHA=$(x "sha256sum /etc/shadow | cut -d' ' -f1")

# write_as <user> <content-printf> <mode>: atomic write, like the agent does.
write_as() {
    x "su -s /bin/sh $1 -c \"printf %b '$2' > /var/lib/senhub-agent/update-request/.t && chmod $3 /var/lib/senhub-agent/update-request/.t && mv /var/lib/senhub-agent/update-request/.t $REQ\""
}

# expect_refused <case> <journal pattern>: waits for the unit, then checks.
expect_refused() {
    local name=$1 pattern=$2 i out
    for i in $(seq 1 40); do
        out=$(x "journalctl -u $UNIT --no-pager -o cat 2>/dev/null")
        echo "$out" | grep -q "$pattern" && break
        sleep 0.5
    done
    local ok=1 why=""
    echo "$out" | grep -q "$pattern" || { ok=0; why="no journal line '$pattern'"; }
    x "[ ! -e $REQ ] && [ ! -L $REQ ]" || { ok=0; why="$why; request not deleted"; }
    [ "$(x "sha256sum /usr/bin/senhub-agent | cut -d' ' -f1")" = "$BIN_SHA" ] || { ok=0; why="$why; binary changed"; }
    x "[ ! -e /usr/bin/senhub-agent.previous ] && ! ls /usr/bin/.senhub-agent-update-* >/dev/null 2>&1" || { ok=0; why="$why; leftovers in /usr/bin"; }
    echo "$out" | grep -q "Downloading update archive" && { ok=0; why="$why; an archive was downloaded"; }
    [ "$(x "sha256sum /etc/shadow | cut -d' ' -f1")" = "$SHADOW_SHA" ] || { ok=0; why="$why; /etc/shadow changed"; }
    [ $ok = 1 ] && verdict ok "$name" "$(echo "$out" | grep "$pattern" | tail -1 | cut -c1-160)" || verdict FAIL "$name" "$why"
    x "systemctl reset-failed $UNIT senhub-agent-update.path; journalctl --rotate; journalctl --vacuum-time=1s" >/dev/null 2>&1
    sleep 1
}

# 1 the other user cannot even write there; a file he owns is refused if one lands there
x "su -s /bin/sh eve -c 'echo 0.7.3 > $REQ'" 2>/dev/null && verdict FAIL "other user cannot write the request directory" || verdict ok "other user cannot write the request directory"
x "printf '0.7.3\n' > /tmp/r && install -o eve -g eve -m 0640 /tmp/r $REQ"
expect_refused "request owned by another user" "not service user"

# 2 symlink to /etc/shadow
x "su -s /bin/sh senhub -c 'ln -s /etc/shadow $REQ'"
expect_refused "symlink to /etc/shadow" "symlink"

# 3 malformed contents, among them a command injection attempt
write_as senhub '0.7.3; touch /tmp/pwned\n' 640
expect_refused "malformed content (shell injection)" "malformed request content"
x "[ ! -e /tmp/pwned ]" && verdict ok "injected command not executed" || verdict FAIL "injected command not executed"
write_as senhub '--registry-url=https://evil.example\n' 640
expect_refused "option-like content" "malformed request content"
write_as senhub '../../../etc/shadow\n' 640
expect_refused "path-like content" "malformed request content"
write_as senhub '0.7.3' 640
expect_refused "no trailing newline" "missing trailing newline"

# 4 too long, group writable, other writable
write_as senhub "0.7.3\n$(printf 'a%.0s' $(seq 1 100))" 640
expect_refused "content too long" "too long"
write_as senhub '0.7.3\n' 660
expect_refused "group-writable request" "too permissive"
write_as senhub '0.7.3\n' 666
expect_refused "world-writable request" "too permissive"

# 5 a FIFO must not hang the unit
x "su -s /bin/sh senhub -c 'mkfifo $REQ'"
expect_refused "FIFO as request" "not a regular file"

# 6 a version nobody published (the signed list is fetched and checked first)
write_as senhub '99.9.9\n' 640
expect_refused "unknown version" "Root update refused"

# 7 six refusals within the hour have used the start limit: a seventh start is not made
sleep 1
x "systemctl reset-failed $UNIT senhub-agent-update.path" >/dev/null 2>&1
for n in 1 2 3 4 5 6 7 8 9 10; do
    write_as senhub 'x\n' 640; sleep 1.5
done
starts=$(x "journalctl -u $UNIT --no-pager -o cat | grep -c 'Root update refused'")
limit=$(x "systemctl show -p Result --value $UNIT; systemctl show -p Result --value senhub-agent-update.path" | tr '\n' ' ')
if [ "$starts" -le 6 ] && echo "$limit" | grep -q limit; then
    verdict ok "start limit stops a flood of requests" "10 requests, $starts runs, result: $limit"
else
    verdict FAIL "start limit stops a flood of requests" "10 requests, $starts runs, result: $limit"
fi
[ "$(x "sha256sum /usr/bin/senhub-agent | cut -d' ' -f1")" = "$BIN_SHA" ] && verdict ok "binary never changed" || verdict FAIL "binary never changed"

# 8 the unit is sandboxed as designed
x "systemd-analyze security $UNIT --no-pager 2>/dev/null | tail -1" || true

# 9 disabled unit: the service cannot ask
x "systemctl disable --now senhub-agent-update.path" >/dev/null 2>&1
x "systemctl is-enabled senhub-agent-update.path" >/dev/null 2>&1 && verdict FAIL "disable turns the unit off" || verdict ok "disable turns the unit off"

[ $FAILED = 0 ] && echo "ALL OK" || echo "SOME FAILED"
exit $FAILED
