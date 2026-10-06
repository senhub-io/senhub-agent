#!/usr/bin/env bash
#
# Runs the senhub.agent role against systemd containers with plain
# ansible-playbook: converge, a second converge that must change nothing,
# then verify. It uses the same converge.yml and verify.yml as the Molecule
# scenario, so the two cannot drift apart. Containers it starts are removed;
# an image it had to pull is removed too, an image that was already there is
# left alone.
#
#   usage: test.sh [debian12] [ubuntu2404] [rockylinux9]     (default: all three)
#
# Environment:
#   SENHUB_TEST_CHANNEL   stable or beta           (default beta)
#   SENHUB_TEST_EDITION   oss or full              (default oss)
#   SENHUB_TEST_VERSION   version to pin, optional (default: newest)
#   SENHUB_TEST_KEEP      1 keeps containers and pulled images for inspection
#
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COLLECTION="$HERE/senhub/agent"
SCENARIO="$COLLECTION/extensions/molecule/default"

export SENHUB_TEST_CHANNEL="${SENHUB_TEST_CHANNEL:-beta}"
export SENHUB_TEST_EDITION="${SENHUB_TEST_EDITION:-oss}"
export SENHUB_TEST_VERSION="${SENHUB_TEST_VERSION:-}"
KEEP="${SENHUB_TEST_KEEP:-0}"
KEEP_LOGS=0

image_for() {
  case "$1" in
    debian12) echo geerlingguy/docker-debian12-ansible:latest ;;
    ubuntu2404) echo geerlingguy/docker-ubuntu2404-ansible:latest ;;
    rockylinux9) echo geerlingguy/docker-rockylinux9-ansible:latest ;;
  esac
}

for tool in docker ansible-playbook; do
  command -v "$tool" >/dev/null || { echo "ERROR: $tool is required" >&2; exit 2; }
done

WORK="$(mktemp -d -t senhub-ansible-test.XXXXXX)"
mkdir -p "$WORK/collections/ansible_collections/senhub"
ln -s "$COLLECTION" "$WORK/collections/ansible_collections/senhub/agent"
export ANSIBLE_COLLECTIONS_PATH="$WORK/collections${ANSIBLE_COLLECTIONS_PATH:+:$ANSIBLE_COLLECTIONS_PATH}"
export ANSIBLE_HOST_KEY_CHECKING=False
export ANSIBLE_RETRY_FILES_ENABLED=False
export ANSIBLE_NOCOLOR=1

CONTAINERS=()
PULLED=()

cleanup() {
  if [ "$KEEP" = 1 ]; then
    echo "kept: containers ${CONTAINERS[*]:-none}, images ${PULLED[*]:-none}, work dir $WORK"
    return
  fi
  for c in "${CONTAINERS[@]:-}"; do [ -n "$c" ] && docker rm -f "$c" >/dev/null 2>&1; done
  for i in "${PULLED[@]:-}"; do [ -n "$i" ] && docker rmi "$i" >/dev/null 2>&1; done
  [ "$KEEP_LOGS" = 1 ] && echo "logs kept in $WORK" || rm -rf "$WORK"
}
trap cleanup EXIT

run_distro() {
  local distro="$1" image="$(image_for "$1")" name="senhub-ansible-test-$1"
  [ -n "$image" ] || { echo "ERROR: unknown distro $1 (debian12 ubuntu2404 rockylinux9)" >&2; return 2; }

  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "[$distro] pulling $image"
    docker pull -q "$image" >/dev/null || return 2
    PULLED+=("$image")
  fi

  docker rm -f "$name" >/dev/null 2>&1
  docker run -d --name "$name" --privileged --cgroupns=host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw "$image" >/dev/null || return 2
  CONTAINERS+=("$name")

  local i state
  for i in $(seq 1 30); do
    state="$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)"
    case "$state" in running|degraded) break ;; esac
    sleep 1
  done
  [ "$state" = running ] || [ "$state" = degraded ] || { echo "[$distro] systemd did not start ($state)" >&2; return 2; }

  local inv="$WORK/inventory-$distro.yml"
  cat > "$inv" <<EOINV
all:
  hosts:
    $distro:
      ansible_connection: community.docker.docker
      ansible_host: $name
      ansible_python_interpreter: auto_silent
EOINV

  local log="$WORK/$distro"
  echo "[$distro] converge"
  ansible-playbook -i "$inv" "$SCENARIO/converge.yml" >"$log.converge.log" 2>&1
  local rc=$?
  tail -n 6 "$log.converge.log"
  if [ $rc -ne 0 ]; then echo "[$distro] CONVERGE FAILED, log: $log.converge.log"; KEEP_LOGS=1; return 1; fi

  echo "[$distro] idempotence"
  ansible-playbook -i "$inv" "$SCENARIO/converge.yml" >"$log.idem.log" 2>&1
  rc=$?
  local recap
  recap="$(grep -E "^$distro +:" "$log.idem.log")"
  echo "  $recap"
  if [ $rc -ne 0 ] || ! grep -qE "changed=0 +unreachable=0 +failed=0" <<<"$recap"; then
    echo "[$distro] NOT IDEMPOTENT, log: $log.idem.log"; KEEP_LOGS=1
    grep -B1 -E "^changed:" "$log.idem.log" | head -20
    return 1
  fi

  echo "[$distro] verify"
  ansible-playbook -i "$inv" "$SCENARIO/verify.yml" >"$log.verify.log" 2>&1
  rc=$?
  grep -E "msg|\"msg\"" "$log.verify.log" | tail -n 2
  grep -E "^$distro +:" "$log.verify.log"
  if [ $rc -ne 0 ]; then echo "[$distro] VERIFY FAILED, log: $log.verify.log"; KEEP_LOGS=1; return 1; fi
  echo "[$distro] PASS"
}

DISTROS=("$@")
[ ${#DISTROS[@]} -gt 0 ] || DISTROS=(debian12 ubuntu2404 rockylinux9)

FAILED=0
for d in "${DISTROS[@]}"; do
  run_distro "$d" || FAILED=1
  if [ "$KEEP" != 1 ]; then
    docker rm -f "senhub-ansible-test-$d" >/dev/null 2>&1
  fi
done
exit $FAILED
