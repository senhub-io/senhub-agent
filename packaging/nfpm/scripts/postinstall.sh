#!/bin/sh
# deb: $1 = configure, $2 = previously configured version (empty on a fresh install)
# rpm: $1 = number of installed instances after this one (1 fresh, 2 upgrade)
set -e

fresh=0
case "$1" in
    configure) [ -z "$2" ] && fresh=1 ;;
    1) fresh=1 ;;
esac

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

if [ "${fresh}" = 1 ]; then
    systemctl enable senhub-agent.service || true
    systemctl start senhub-agent.service || true
else
    # Upgrade: pick up the new binary. A stopped service stays as the
    # operator left it.
    systemctl try-restart senhub-agent.service || true
fi

exit 0
