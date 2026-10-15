#!/bin/sh
# deb: $1 = configure, $2 = previously configured version (empty on a fresh install)
# rpm: $1 = number of installed instances after this one (1 fresh, 2 upgrade)
set -e

fresh=0
case "$1" in
    configure) [ -z "$2" ] && fresh=1 ;;
    1) fresh=1 ;;
esac

for f in agent.yaml probes.d/00-host.yaml strategies.d/00-http.yaml; do
    if [ -f "/etc/senhub-agent/$f.pre-package" ]; then
        mv -f "/etc/senhub-agent/$f.pre-package" "/etc/senhub-agent/$f"
    fi
done

# Take over from `senhub-agent install`. Its unit in /etc/systemd/system
# shadows the packaged one and keeps running /usr/local/bin/senhub-agent,
# so both go. Configuration, agent key, secrets and data are not touched.
legacy_unit=/etc/systemd/system/senhub-agent.service
legacy_bin=/usr/local/bin/senhub-agent
migrated=0
if [ -f "${legacy_unit}" ]; then
    migrated=1
    if [ -d /run/systemd/system ]; then
        systemctl stop senhub-agent.service || true
        systemctl disable senhub-agent.service || true
    fi
    if [ -f "${legacy_bin}" ]; then
        mkdir -p /var/lib/senhub-agent
        mv -f "${legacy_bin}" /var/lib/senhub-agent/senhub-agent.pre-package
    fi
    rm -f "${legacy_unit}"
    # A root install left root-owned files the packaged (senhub) unit
    # could not read.
    chown -R senhub:senhub /etc/senhub-agent /var/lib/senhub-agent /var/log/senhub-agent || true
    # A monolithic config already carries probes and outputs; the packaged
    # fragments would add a second set.
    if grep -Eq '^(probes|storage):' /etc/senhub-agent/agent.yaml 2>/dev/null; then
        rm -f /etc/senhub-agent/probes.d/00-host.yaml /etc/senhub-agent/strategies.d/00-http.yaml
    fi
fi

# Default fragments the agent adds on a multi-file install. The packaged
# unit runs with /etc read-only, so the agent cannot write them itself:
# they are written here, under the same rules (no file, no *.disabled
# twin, no probe of that type configured elsewhere).
add_default_fragment() {
    file=/etc/senhub-agent/probes.d/$1
    [ -e "${file}" ] || [ -e "${file}.disabled" ] && return 0
    grep -Eqs "^[[:space:]]*type:[[:space:]]*\"?$2\"?[[:space:]]*$" /etc/senhub-agent/probes.d/*.yaml && return 0
    cat > "${file}" <<FRAGMENT
# $3 Rename to *.disabled to
# turn it off; deleting it brings it back at the next start or upgrade.

- name: $4
  type: $2
  params:
    interval: 60
FRAGMENT
    chown senhub:senhub "${file}" 2>/dev/null || true
    chmod 0600 "${file}"
}
if [ -d /etc/senhub-agent/probes.d ] && ! grep -Eq '^(probes|storage):' /etc/senhub-agent/agent.yaml 2>/dev/null; then
    add_default_fragment 05-services.yaml systemd "Default OS service supervisor probe." services
    if [ -S /var/run/docker.sock ]; then
        add_default_fragment 06-swarm.yaml swarm "Default Docker Swarm probe (declares overlay networks on a manager)." swarm
    fi
fi

# The shipped config carries an empty agent key; the agent refuses to start
# without one. Each host gets its own, and an operator-set key is never
# touched. Written in place (cat >) so owner and mode survive.
cfg=/etc/senhub-agent/agent.yaml
if [ -f "${cfg}" ] && grep -q '^  key: ""' "${cfg}"; then
    key=$(cat /proc/sys/kernel/random/uuid)
    tmp=$(mktemp)
    sed "s/^  key: \"\"/  key: \"${key}\"/" "${cfg}" > "${tmp}"
    cat "${tmp}" > "${cfg}"
    rm -f "${tmp}"
fi

# No running systemd (image build, chroot): nothing to register or start.
[ -d /run/systemd/system ] || exit 0

systemctl daemon-reload || true

if [ "${fresh}" = 1 ] || [ "${migrated}" = 1 ]; then
    systemctl enable senhub-agent.service || true
    # restart, not start: an edition switch on rpm leaves the replaced
    # edition running (its stop is skipped, see preremove.sh).
    systemctl restart senhub-agent.service || true
else
    # Upgrade: pick up the new binary. A stopped service stays as the
    # operator left it.
    systemctl try-restart senhub-agent.service || true
fi

exit 0
