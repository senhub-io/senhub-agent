#!/usr/bin/env bash
# Install / upgrade / removal proof for the .deb and .rpm packages, on real
# distributions, with systemd as PID 1 in a privileged container.
#
# Usage: packaging/nfpm/test-packages.sh [step...]
#   step: lint versions debian12 ubuntu2204 ubuntu2404 rocky9 leap156
#         (default: all). `lint` runs lintian and rpmlint, `versions` proves
#         the prerelease ordering with dpkg and rpm.
# Env:
#   ARCH  amd64 | arm64 (default: the docker host architecture, so that the
#         containers run natively)
#   V1/V2 versions of the first and the upgraded build (default 0.6.2 / 0.6.3)
#
# Builds with `make packages` first (needs docker for nFPM): the oss edition
# at V1 and V2, then the full edition at V2. The full edition is packaged
# from the same binary as the oss one here (what the enterprise build
# supplies in production): it is the switch logic that is under test.
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
EDIT_VALUE=7

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

# The agent seals its key into agent.yaml ~50 ms after its first start, with a
# read-modify-write that does not lock: an edit landing inside that window is
# lost (config_seal.go sealAgentKeyInFile). Wait for it to settle, as an
# operator editing a running host would.
wait_sealed() {
    local i
    for i in $(seq 1 50); do
        x "grep -q 'secret:agent.key' $CFG" && break
        sleep 0.2
    done
    check "first-start sealing settled" "grep -q 'secret:agent.key' $CFG"
}

# Package form of a version: the first hyphen becomes a tilde.
pkg_version() { echo "$1" | sed 's/-/~/'; }

pkg_file() { # version [name]
    local pv; pv=$(pkg_version "$1")
    if [ "$FMT" = deb ]; then echo "${2:-senhub-agent-oss}_${pv}-1_${ARCH}.deb"
    else echo "${2:-senhub-agent-oss}-${pv}-1.${RPMARCH}.rpm"; fi
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

# Edition switch: one install command on deb; an explicit swap on rpm, where the
# editions only Conflict (no Obsoletes, so asking for one name never installs the other).
switch_cmd() { # file old-package
    case "$DISTRO" in
        rocky9) echo "dnf swap -y -q $2 /pkgs/$1" ;;
        leap156) echo "zypper --non-interactive --no-gpg-checks -q install --allow-unsigned-rpm --force-resolution /pkgs/$1" ;;
        *) install_cmd "$1" ;;
    esac
}

remove_cmd() { # [package]
    case "$DISTRO" in
        debian12|ubuntu*) echo "dpkg -r ${1:-senhub-agent-oss}" ;;
        rocky9) echo "dnf remove -y -q ${1:-senhub-agent-oss}" ;;
        leap156) echo "zypper --non-interactive -q remove ${1:-senhub-agent-oss}" ;;
    esac
}

start_container() {
    CID=$(docker run -d --platform "linux/$ARCH" --privileged --cgroupns=host \
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
    wait_sealed
    # A value the agent never rewrites, plus a comment: both must survive.
    x "sed -i 's/retention_minutes: 5/retention_minutes: $EDIT_VALUE/' $CFG; echo '$MARK' >> $CFG"
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
    check "configuration value edit kept" "grep -q 'retention_minutes: $EDIT_VALUE' $CFG"
    check "configuration comment kept" "grep -qx '$MARK' $CFG"
    key2=$(x "/usr/bin/senhub-agent license key" 2>/dev/null | tail -1)
    check "same agent key" "[ '$key1' = '$key2' ]"
    cleanup
}

installed_cmd() { # package
    if [ "$FMT" = deb ]; then echo "dpkg-query -W -f='\${db:Status-Abbrev}' $1 2>/dev/null | grep -q '^ii'"
    else echo "rpm -q $1 >/dev/null 2>&1"; fi
}

# oss -> full -> oss, in place: configuration, agent key and data stay, the
# service comes back on the new package.
run_switch() {
    local tag=$1 oss1 oss2 full2 key1 key2 pid1 pid2
    oss1=$(pkg_file "$V1"); oss2=$(pkg_file "$V2"); full2=$(pkg_file "$V2" senhub-agent)
    echo "  edition switch oss -> full -> oss"
    start_container "$tag"
    check "oss installs" "$(install_cmd "$oss1")"
    wait_active; wait_sealed
    x "echo '$MARK' >> $CFG; echo data > /var/lib/senhub-agent/marker; chown senhub /var/lib/senhub-agent/marker"
    key1=$(x "/usr/bin/senhub-agent license key" 2>/dev/null | tail -1)
    check "agent key readable before" "[ -n '$key1' ]"
    pid1=$(x "systemctl show -p MainPID --value senhub-agent")

    check "full replaces oss" "$(switch_cmd "$full2" senhub-agent-oss)"
    wait_active; check "service active" "systemctl is-active senhub-agent"
    check "full is installed" "$(installed_cmd senhub-agent)"
    check "oss is gone" "! $(installed_cmd senhub-agent-oss)"
    check "service enabled" "systemctl is-enabled senhub-agent"
    pid2=$(x "systemctl show -p MainPID --value senhub-agent")
    check "service restarted (new PID)" "[ '$pid1' != '$pid2' ] && [ '$pid2' != 0 ]"
    check "version is $V2" "[ \"\$(senhub-agent version | grep -o '$V2' | head -1)\" = '$V2' ]"
    check "configuration edit kept" "grep -qx '$MARK' $CFG"
    key2=$(x "/usr/bin/senhub-agent license key" 2>/dev/null | tail -1)
    check "same agent key" "[ '$key1' = '$key2' ]"
    check "data kept" "[ -f /var/lib/senhub-agent/marker ]"
    check "config senhub:senhub 0600" "[ \"\$(stat -c '%U:%G:%a' $CFG)\" = senhub:senhub:600 ]"
    if [ "$FMT" = deb ]; then
        check "purging the replaced oss keeps config, data and user" "dpkg -P senhub-agent-oss; grep -qx '$MARK' $CFG && [ -f /var/lib/senhub-agent/marker ] && getent passwd senhub >/dev/null"
    fi

    pid1=$pid2
    check "oss replaces full" "$(switch_cmd "$oss2" senhub-agent)"
    wait_active; check "service active" "systemctl is-active senhub-agent"
    check "oss is installed" "$(installed_cmd senhub-agent-oss)"
    check "full is gone" "! $(installed_cmd senhub-agent)"
    pid2=$(x "systemctl show -p MainPID --value senhub-agent")
    check "service restarted (new PID)" "[ '$pid1' != '$pid2' ] && [ '$pid2' != 0 ]"
    check "configuration edit kept" "grep -qx '$MARK' $CFG"
    key2=$(x "/usr/bin/senhub-agent license key" 2>/dev/null | tail -1)
    check "same agent key" "[ '$key1' = '$key2' ]"
    check "data kept" "[ -f /var/lib/senhub-agent/marker ]"
    cleanup
}

# lintian on the .deb and rpmlint on the .rpm of both editions. Any warning
# or error fails; the exceptions are the reasoned overrides in
# packaging/nfpm/lintian-overrides and packaging/nfpm/rpmlint.toml.
run_lint() {
    echo "== lint"
    local rc=0 f out pv
    pv=$(pkg_version "$V2")
    if ! docker image inspect senhub-pkglint-deb >/dev/null 2>&1; then
        printf 'FROM debian:12\nRUN apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq lintian\n' | docker build -q -t senhub-pkglint-deb - >/dev/null || rc=1
    fi
    if ! docker image inspect senhub-pkglint-rpm >/dev/null 2>&1; then
        printf 'FROM fedora:41\nRUN dnf install -y -q rpmlint\n' | docker build -q -t senhub-pkglint-rpm - >/dev/null || rc=1
    fi
    for f in "senhub-agent-oss_${pv}-1_${ARCH}.deb" "senhub-agent_${pv}-1_${ARCH}.deb"; do
        out=$(docker run --rm -v "$PKGDIR":/pkgs:ro senhub-pkglint-deb lintian --pedantic --tag-display-limit 0 --fail-on error,warning "/pkgs/$f" 2>&1 | grep -v 'setlocale\|running with root')
        if [ -z "$out" ]; then echo "    ok   lintian $f"; else echo "    FAIL lintian $f"; echo "$out" | sed 's/^/         | /'; rc=1; fi
    done
    for f in "senhub-agent-oss-${pv}-1.${RPMARCH}.rpm" "senhub-agent-${pv}-1.${RPMARCH}.rpm"; do
        out=$(docker run --rm -v "$PKGDIR":/pkgs:ro -v "$ROOT/packaging/nfpm/rpmlint.toml":/rpmlint.toml:ro senhub-pkglint-rpm rpmlint -c /rpmlint.toml "/pkgs/$f" 2>&1 | grep ': [EW]: ')
        if [ -z "$out" ]; then echo "    ok   rpmlint $f"; else echo "    FAIL rpmlint $f"; echo "$out" | sed 's/^/         | /'; rc=1; fi
    done
    if [ $rc = 0 ]; then RESULTS+=("lint PASS"); else RESULTS+=("lint FAIL"); FAILED=1; fi
}

# Prerelease ordering as the package managers see it. The versions go through
# the Makefile's own conversion, then dpkg and rpm compare them.
run_versions() {
    echo "== versions"
    local v list="" pv rc=0 img dev devlist
    for v in 0.6.2-beta.1 0.6.2-beta.2 0.6.2-beta.10 0.6.2 0.6.3-beta.1; do
        pv=$(make --no-print-directory package-version VERSION="$v")
        list="$list $pv-1"
    done
    dev=$(make --no-print-directory package-version VERSION=0.6.2-dev.57.g1a2b3c4d)
    devlist="0.6.1-1 $dev-1 $(make --no-print-directory package-version VERSION=0.6.2)-1"
    for img in debian:12 fedora:41; do
        echo "  $img"
        docker run --rm -v "$ROOT/packaging/nfpm/vercmp-check.sh":/vercmp-check.sh:ro "$img" sh /vercmp-check.sh $list || rc=1
        docker run --rm -v "$ROOT/packaging/nfpm/vercmp-check.sh":/vercmp-check.sh:ro "$img" sh /vercmp-check.sh $devlist || rc=1
    done
    if [ $rc = 0 ]; then RESULTS+=("versions PASS"); else RESULTS+=("versions FAIL"); FAILED=1; fi
}

run_distro() {
    DISTRO=$1
    local s image prep
    s=$(spec "$DISTRO") || { echo "unknown distro $DISTRO"; FAILED=1; return; }
    image=${s%%|*}; s=${s#*|}; FMT=${s%%|*}; prep=${s#*|}
    DISTRO_FAIL=0

    echo "== $DISTRO ($image, $FMT, $ARCH)"

    local tag="senhub-pkgtest-$DISTRO-$ARCH"
    if ! docker image inspect "$tag" >/dev/null 2>&1; then
        echo "    building test image (systemd)"
        if ! printf 'FROM %s\nRUN %s\n' "$image" "$prep" | docker build -q --platform "linux/$ARCH" -t "$tag" - >/dev/null; then
            echo "    FAIL test image build"; DISTRO_FAIL=1
            RESULTS+=("$DISTRO $ARCH FAIL(image)"); FAILED=1; return
        fi
    fi

    CID=$(docker run -d --platform "linux/$ARCH" --privileged --cgroupns=host \
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
    check "doctor reads the packaged unit" "senhub-agent doctor --json | tr -d '\\n ' | grep -q '\"id\":\"install.unit\",\"level\":\"ok\"'"
    check "refresh-unit finds the packaged unit" "senhub-agent refresh-unit --yes 2>&1 | grep -q 'up to date'"
    check "doctor --json exit code is not 2" "senhub-agent doctor --json >/dev/null; rc=\$?; echo exit=\$rc; [ \$rc -ne 2 ]"

    # --- upgrade
    wait_sealed
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
        check "purge" "dpkg -P senhub-agent-oss"
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
    run_switch "$tag"
    if [ $DISTRO_FAIL = 0 ]; then RESULTS+=("$DISTRO $ARCH PASS"); else RESULTS+=("$DISTRO $ARCH FAIL"); FAILED=1; fi
}

if [ "${SKIP_BUILD:-0}" != 1 ]; then
    for v in "$V1" "$V2"; do
        make packages EDITION=oss VERSION="$v" PACKAGE_ARCHES="$ARCH" || { echo "package build failed" >&2; exit 2; }
        # The first build plays the binary an operator installed earlier.
        [ "$v" = "$V1" ] && cp "dist/linux-$ARCH/senhub-agent" "$LEGACY_BIN"
    done
    # The full edition comes from the enterprise build in production; here it
    # is the same binary as the oss edition, so what is tested is the switch.
    make packages EDITION=full BINARY_DIR="$ROOT/dist" VERSION="$V2" PACKAGE_ARCHES="$ARCH" || { echo "package build failed" >&2; exit 2; }
fi

STEPS=${*:-"lint versions $ALL_DISTROS"}
for d in $STEPS; do
    case "$d" in
        versions) run_versions ;;
        lint) run_lint ;;
        *) run_distro "$d" ;;
    esac
done

echo
echo "== summary"
printf '%s\n' "${RESULTS[@]}"
exit $FAILED
