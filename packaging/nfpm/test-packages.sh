#!/usr/bin/env bash
# Install / upgrade / removal proof for the .deb and .rpm packages, on real
# distributions, with systemd as PID 1 in a privileged container.
#
# Usage: packaging/nfpm/test-packages.sh [distro...]
#   distro: debian12 ubuntu2204 ubuntu2404 rocky9 leap156 (default: all)
# Env:
#   ARCH  amd64 | arm64 (default: the docker host architecture, so that the
#         containers run natively)
#   V1/V2 versions of the first and the upgraded build (default 0.6.2 / 0.6.3)
#
# Builds both versions with `make packages` first (needs docker for nFPM).
set -u

cd "$(dirname "$0")/../.."
ROOT=$(pwd)

V1=${V1:-0.6.2}
V2=${V2:-0.6.3}
if [ -z "${ARCH:-}" ]; then
    case "$(docker info --format '{{.Architecture}}')" in
        aarch64|arm64) ARCH=arm64 ;;
        *) ARCH=amd64 ;;
    esac
fi
case "$ARCH" in
    amd64) RPMARCH=x86_64 ;;
    arm64) RPMARCH=aarch64 ;;
    *) echo "unsupported ARCH=$ARCH" >&2; exit 2 ;;
esac

PKGDIR="$ROOT/dist/packages"
LEGACY_BIN="$PKGDIR/stage/legacy-senhub-agent-$ARCH"
CFG=/etc/senhub-agent/agent.yaml
MARK="# edited-by-test-packages"

# name|image|format|prepare command (installs systemd and what the test needs)
ALL_DISTROS="debian12 ubuntu2204 ubuntu2404 rocky9 leap156"
spec() {
    case "$1" in
        debian12)   echo "debian:12|deb|apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd systemd-sysv ca-certificates" ;;
        ubuntu2204) echo "ubuntu:22.04|deb|apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd systemd-sysv ca-certificates" ;;
        ubuntu2404) echo "ubuntu:24.04|deb|apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd systemd-sysv ca-certificates" ;;
        rocky9)     echo "rockylinux:9|rpm|dnf install -y -q systemd shadow-utils" ;;
        leap156)    echo "opensuse/leap:15.6|rpm|zypper --non-interactive -q install systemd shadow" ;;
        *) return 1 ;;
    esac
}

OUT=$(mktemp)
FAILED=0
RESULTS=()
CID=""

cleanup() { [ -n "$CID" ] && docker rm -f "$CID" >/dev/null 2>&1; CID=""; }
trap 'cleanup; rm -f "$OUT"' EXIT

x() { docker exec "$CID" sh -c "$1"; }

# check <label> <command...>: runs inside the container, records pass/fail.
check() {
    local label=$1; shift
    if x "$*" >"$OUT" 2>&1; then
        echo "    ok   $label"
    else
        echo "    FAIL $label"
        sed 's/^/         | /' "$OUT" | tail -15
        DISTRO_FAIL=1
    fi
}

wait_active() {
    local i
    for i in $(seq 1 30); do
        x "systemctl is-active --quiet senhub-agent" && return 0
        sleep 1
    done
    return 1
}

pkg_file() { # version
    if [ "$FMT" = deb ]; then echo "senhub-agent_$1_${ARCH}.deb"
    else echo "senhub-agent-$1.${RPMARCH}.rpm"; fi
}

install_cmd() { # file
    case "$DISTRO" in
        debian12|ubuntu*) echo "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq -o Dpkg::Options::=--force-confold /pkgs/$1" ;;
        rocky9) echo "dnf install -y -q /pkgs/$1" ;;
        leap156) echo "zypper --non-interactive --no-gpg-checks -q install --allow-unsigned-rpm /pkgs/$1" ;;
    esac
}

upgrade_cmd() { # file
    case "$DISTRO" in
        leap156) echo "zypper --non-interactive --no-gpg-checks -q install --allow-unsigned-rpm /pkgs/$1" ;;
        rocky9) echo "dnf upgrade -y -q /pkgs/$1" ;;
        *) install_cmd "$1" ;;
    esac
}

remove_cmd() {
    case "$DISTRO" in
        debian12|ubuntu*) echo "dpkg -r senhub-agent" ;;
        rocky9) echo "dnf remove -y -q senhub-agent" ;;
        leap156) echo "zypper --non-interactive -q remove senhub-agent" ;;
    esac
}

start_container() {
    CID=$(docker run -d --privileged --cgroupns=host \
        -v /sys/fs/cgroup:/sys/fs/cgroup:rw -v "$PKGDIR":/pkgs:ro \
        --tmpfs /run --tmpfs /run/lock "$1" /usr/lib/systemd/systemd)
    local i
    for i in $(seq 1 30); do
        x "systemctl is-system-running" 2>/dev/null | grep -qE 'running|degraded' && break
        sleep 1
    done
}

# A host set up with `senhub-agent install` (old binary in /usr/local/bin,
# unit in /etc/systemd/system) must be taken over by the package.
run_migration() {
    local f2=$1 tag=$2
    echo "  migration from 'senhub-agent install'"
    if [ ! -f "$LEGACY_BIN" ]; then
        echo "    FAIL no legacy binary at $LEGACY_BIN (run without SKIP_BUILD once)"
        DISTRO_FAIL=1; return
    fi
    start_container "$tag"
    docker cp "$LEGACY_BIN" "$CID:/root/senhub-agent" >/dev/null
    check "legacy install" "chmod +x /root/senhub-agent && /root/senhub-agent install && systemctl start senhub-agent"
    wait_active; check "legacy service active" "systemctl is-active senhub-agent"
    check "legacy layout in place" "[ -x /usr/local/bin/senhub-agent ] && [ -f /etc/systemd/system/senhub-agent.service ]"
    x "echo '$MARK' >> $CFG"
    local key1 key2
    key1=$(x "/usr/local/bin/senhub-agent license key" 2>/dev/null | tail -1)
    check "agent key readable before" "[ -n '$key1' ]"
    # No -o force-confold here: the existing config must survive a plain install.
    case "$FMT" in
        deb) check "package installs over it" "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq /pkgs/$f2" ;;
        *) check "package installs over it" "$(install_cmd "$f2")" ;;
    esac
    wait_active; check "service active" "systemctl is-active senhub-agent"
    check "runs /usr/bin/senhub-agent" "[ \"\$(readlink /proc/\$(systemctl show -p MainPID --value senhub-agent)/exe)\" = /usr/bin/senhub-agent ]"
    check "systemd loads the packaged unit" "[ \"\$(systemctl show -p FragmentPath --value senhub-agent)\" != /etc/systemd/system/senhub-agent.service ] && [ ! -e /etc/systemd/system/senhub-agent.service ]"
    check "legacy binary removed, backup kept" "[ ! -e /usr/local/bin/senhub-agent ] && [ -f /var/lib/senhub-agent/senhub-agent.pre-package ]"
    check "version is $V2" "[ \"\$(senhub-agent version | grep -o '$V2' | head -1)\" = '$V2' ]"
    check "configuration kept" "grep -qx '$MARK' $CFG"
    key2=$(x "/usr/bin/senhub-agent license key" 2>/dev/null | tail -1)
    check "same agent key" "[ '$key1' = '$key2' ]"
    cleanup
}

run_distro() {
    DISTRO=$1
    local s image prep
    s=$(spec "$DISTRO") || { echo "unknown distro $DISTRO"; FAILED=1; return; }
    image=${s%%|*}; s=${s#*|}; FMT=${s%%|*}; prep=${s#*|}
    DISTRO_FAIL=0

    echo "== $DISTRO ($image, $FMT, $ARCH)"

    local tag="senhub-pkgtest-$DISTRO"
    if ! docker image inspect "$tag" >/dev/null 2>&1; then
        echo "    building test image (systemd)"
        if ! printf 'FROM %s\nRUN %s\n' "$image" "$prep" | docker build -q -t "$tag" - >/dev/null; then
            echo "    FAIL test image build"; DISTRO_FAIL=1
            RESULTS+=("$DISTRO $ARCH FAIL(image)"); FAILED=1; return
        fi
    fi

    CID=$(docker run -d --privileged --cgroupns=host \
        -v /sys/fs/cgroup:/sys/fs/cgroup:rw -v "$PKGDIR":/pkgs:ro \
        --tmpfs /run --tmpfs /run/lock "$tag" /usr/lib/systemd/systemd)
    local i
    for i in $(seq 1 30); do
        x "systemctl is-system-running" 2>/dev/null | grep -qE 'running|degraded' && break
        sleep 1
    done
    check "systemd is PID 1" "[ \"\$(cat /proc/1/comm)\" = systemd ]"
    if [ $DISTRO_FAIL != 0 ]; then
        cleanup; RESULTS+=("$DISTRO $ARCH FAIL(no systemd)"); FAILED=1; return
    fi

    local f1 f2
    f1=$(pkg_file "$V1"); f2=$(pkg_file "$V2")

    # --- install
    echo "  install $V1"
    check "package installs" "$(install_cmd "$f1")"
    wait_active; check "service active" "systemctl is-active senhub-agent"
    check "service enabled" "systemctl is-enabled senhub-agent"
    check "version is $V1" "[ \"\$(senhub-agent version | grep -o '$V1' | head -1)\" = '$V1' ]"
    check "binary is root-owned in /usr/bin" "[ \"\$(stat -c '%U:%a' /usr/bin/senhub-agent)\" = root:755 ]"
    check "service runs as senhub" "[ \"\$(stat -c %U /proc/\$(systemctl show -p MainPID --value senhub-agent))\" = senhub ]"
    check "state, log and config dirs owned by senhub" "[ \"\$(stat -c '%U:%G' /var/lib/senhub-agent /var/log/senhub-agent /etc/senhub-agent | sort -u)\" = senhub:senhub ]"
    check "config senhub:senhub 0600" "[ \"\$(stat -c '%U:%G:%a' $CFG)\" = senhub:senhub:600 ]"
    check "host agent key set (UUID, or sealed by the agent)" "grep -Eq '^  key: \"([0-9a-f-]{36}|\\\${secret:agent.key})\"' $CFG"
    check "auto_update disabled in packaged config" "grep -A1 '^auto_update:' $CFG | grep -q 'enabled: false'"
    check "senhub in adm (where the group exists)" "! getent group adm >/dev/null || id -nG senhub | tr ' ' '\\n' | grep -qx adm"
    check "doctor --json exit code is not 2" "senhub-agent doctor --json >/dev/null; rc=\$?; echo exit=\$rc; [ \$rc -ne 2 ]"

    # --- upgrade
    x "echo '$MARK' >> $CFG; echo data > /var/lib/senhub-agent/marker; chown senhub /var/lib/senhub-agent/marker"
    local pid1
    pid1=$(x "systemctl show -p MainPID --value senhub-agent")
    echo "  upgrade to $V2"
    check "package upgrades" "$(upgrade_cmd "$f2")"
    wait_active; check "service active after upgrade" "systemctl is-active senhub-agent"
    check "version is $V2" "[ \"\$(senhub-agent version | grep -o '$V2' | head -1)\" = '$V2' ]"
    check "operator edit kept" "grep -qx '$MARK' $CFG"
    local pid2
    pid2=$(x "systemctl show -p MainPID --value senhub-agent")
    check "service restarted (new PID)" "[ '$pid1' != '$pid2' ] && [ '$pid2' != 0 ]"

    # --- removal
    echo "  remove"
    check "package removes" "$(remove_cmd)"
    check "service gone" "! systemctl is-active --quiet senhub-agent && [ ! -e /usr/lib/systemd/system/senhub-agent.service ] && ! systemctl is-enabled --quiet senhub-agent"
    check "binary gone" "[ ! -e /usr/bin/senhub-agent ]"
    check "config kept with the edit" "grep -qx '$MARK' $CFG"
    check "data and logs kept" "[ -f /var/lib/senhub-agent/marker ] && [ -d /var/log/senhub-agent ]"

    if [ "$FMT" = deb ]; then
        echo "  purge"
        check "purge" "dpkg -P senhub-agent"
        check "config, data and logs gone" "[ ! -e /etc/senhub-agent ] && [ ! -e /var/lib/senhub-agent ] && [ ! -e /var/log/senhub-agent ]"
        check "service user removed" "! getent passwd senhub >/dev/null"
    else
        # Reinstall over the kept config: the edit must still be there.
        echo "  reinstall over kept config"
        check "package installs again" "$(install_cmd "$f1")"
        check "operator edit still there" "grep -qx '$MARK' $CFG"
    fi

    cleanup
    run_migration "$f2" "$tag"
    cleanup
    if [ $DISTRO_FAIL = 0 ]; then RESULTS+=("$DISTRO $ARCH PASS"); else RESULTS+=("$DISTRO $ARCH FAIL"); FAILED=1; fi
}

if [ "${SKIP_BUILD:-0}" != 1 ]; then
    for v in "$V1" "$V2"; do
        make packages VERSION="$v" PACKAGE_ARCHES="$ARCH" || { echo "package build failed" >&2; exit 2; }
        # The first build plays the binary an operator installed earlier.
        [ "$v" = "$V1" ] && cp "dist/linux-$ARCH/senhub-agent" "$LEGACY_BIN"
    done
fi

DISTROS=${*:-$ALL_DISTROS}
for d in $DISTROS; do run_distro "$d"; done

echo
echo "== summary"
printf '%s\n' "${RESULTS[@]}"
exit $FAILED
