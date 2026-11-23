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
    [ "$(echo "$out" | grep -c 'Starting senhub-agent-update.service')" = 1 ] || { ok=0; why="$why; not exactly one start of the unit"; }
    x "systemctl is-active senhub-agent-update.path" >/dev/null || { ok=0; why="$why; path unit not active"; }
    echo "$out" | grep -q "Downloading update archive" && { ok=0; why="$why; an archive was downloaded"; }
    [ "$(x "sha256sum /etc/shadow | cut -d' ' -f1")" = "$SHADOW_SHA" ] || { ok=0; why="$why; /etc/shadow changed"; }
    [ $ok = 1 ] && verdict ok "$name" "$(echo "$out" | grep "$pattern" | tail -1 | cut -c1-160)" || verdict FAIL "$name" "$why"
    x "rm -f /var/lib/senhub-agent-update/attempts.json; journalctl --rotate; journalctl --vacuum-time=1s" >/dev/null 2>&1
    sleep 3
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

# 6b a rollback the root unit never made, and one forged through the version request
x "su -s /bin/sh senhub -c 'printf \"rollback\\n\" > /var/lib/senhub-agent/update-request/.t && mv /var/lib/senhub-agent/update-request/.t /var/lib/senhub-agent/update-request/rollback'"
expect_refused "rollback with no install by the unit" "step=rollback"
write_as senhub 'rollback\n' 640
expect_refused "rollback forged through the version request" "malformed request content"

# 7 a burst: six runs are handled, the rest are rate_limited, every request is one
# start of the unit, and the path unit never dies (no systemd start limit)
x "rm -f /var/lib/senhub-agent-update/attempts.json; journalctl --rotate; journalctl --vacuum-time=1s" >/dev/null 2>&1
sleep 2
for n in 1 2 3 4 5 6 7 8 9 10; do
    write_as senhub 'x\n' 640; sleep 3.5
done
out=$(x "journalctl -u $UNIT --no-pager -o cat")
starts=$(echo "$out" | grep -c 'Starting senhub-agent-update.service')
handled=$(echo "$out" | grep -c 'step=request')
limited=$(echo "$out" | grep -c 'reason=rate_limited')
if [ "$starts" = 10 ] && [ "$handled" = 6 ] && [ "$limited" = 4 ] && x "systemctl is-active senhub-agent-update.path" >/dev/null; then
    verdict ok "burst of 10: 6 handled, 4 rate_limited, one start each, path unit active" "starts=$starts handled=$handled limited=$limited"
else
    verdict FAIL "burst of 10" "starts=$starts handled=$handled limited=$limited path=$(x 'systemctl is-active senhub-agent-update.path')"
fi
x "[ ! -e $REQ ]" && verdict ok "no request left after the burst" || verdict FAIL "no request left after the burst"
res=$(x "cat /var/lib/senhub-agent/update-result.json; stat -c ' %a %U' /var/lib/senhub-agent/update-result.json")
echo "$res" | grep -q 'rate_limited' && echo "$res" | grep -q ' 644 root' \
    && verdict ok "result file for the service: refused/rate_limited, 0644" "$res" || verdict FAIL "result file for the service" "$res"
write_as senhub '99.9.9\n' 640; sleep 4
[ "$(x "journalctl -u $UNIT --no-pager -o cat | grep -c 'reason=rate_limited'")" = 5 ] && verdict ok "a later request inside the window is refused" || verdict FAIL "a later request inside the window is refused"
x "systemctl is-active senhub-agent-update.path" >/dev/null && verdict ok "path unit still active" || verdict FAIL "path unit still active"
x "systemctl restart senhub-agent; sleep 3; journalctl -u senhub-agent --no-pager -o cat | grep -ci 'mode is different'" | grep -qx 0 \
    && verdict ok "no ConfigurationDirectory mode warning at service start" || verdict FAIL "mode warning at service start"
[ "$(x "sha256sum /usr/bin/senhub-agent | cut -d' ' -f1")" = "$BIN_SHA" ] && verdict ok "binary never changed" || verdict FAIL "binary never changed"

# 7b symlinks planted by the service user before a (re)install: root follows none
x "echo KEEP > /root/victim1; echo KEEP > /root/victim2; echo KEEP > /root/victim3; chmod 600 /root/victim1 /root/victim2 /root/victim3"
VICTIMS=$(x "stat -c '%U:%a' /root/victim1 /root/victim2 /root/victim3; sha256sum /root/victim1 /root/victim2 /root/victim3" | sha256sum)
x "rm -f /var/lib/senhub-agent/.default-probes-0.6.4 /var/lib/senhub-agent-update/.root-update-0.7.2 /etc/senhub-agent/probes.d/05-services.yaml /etc/senhub-agent/probes.d/06-swarm.yaml"
x "su -s /bin/sh senhub -c 'ln -s /root/victim1 /var/lib/senhub-agent/.default-probes-0.6.4; ln -s /root/victim2 /var/lib/senhub-agent/.root-update-0.7.2; ln -s /root/victim3 /etc/senhub-agent/probes.d/05-services.yaml'"
x "DEBIAN_FRONTEND=noninteractive apt-get install --reinstall -y -qq /pkgs/$(basename "$PKG")" >/dev/null 2>&1
[ "$(x "stat -c '%U:%a' /root/victim1 /root/victim2 /root/victim3; sha256sum /root/victim1 /root/victim2 /root/victim3" | sha256sum)" = "$VICTIMS" ] \
    && verdict ok "planted symlinks (marker, default fragment) not followed by postinst" || verdict FAIL "planted symlinks not followed by postinst"
x "[ -L /var/lib/senhub-agent/.default-probes-0.6.4 ] && [ -L /etc/senhub-agent/probes.d/05-services.yaml ]" \
    && verdict ok "planted links left as they were" || verdict FAIL "planted links left as they were"
x "[ -f /var/lib/senhub-agent-update/.root-update-0.7.2 ] && [ ! -L /var/lib/senhub-agent-update/.root-update-0.7.2 ] && [ \"\$(stat -c %U:%a /var/lib/senhub-agent-update)\" = root:700 ]" \
    && verdict ok "root marker written in the root-only directory" || verdict FAIL "root marker written in the root-only directory"

# 8 the unit is sandboxed as designed
x "systemd-analyze security $UNIT --no-pager 2>/dev/null | tail -1" || true

# 9 disabled unit: the service cannot ask
x "systemctl disable --now senhub-agent-update.path" >/dev/null 2>&1
x "systemctl is-enabled senhub-agent-update.path" >/dev/null 2>&1 && verdict FAIL "disable turns the unit off" || verdict ok "disable turns the unit off"

[ $FAILED = 0 ] && echo "ALL OK" || echo "SOME FAILED"
exit $FAILED
