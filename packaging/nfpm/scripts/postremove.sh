#!/bin/sh
# deb: $1 = remove | purge | upgrade | ...
# rpm: $1 = 0 (erase) | 1 (upgrade)
set -e

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload || true
fi

case "$1" in
    purge)
        # dpkg purge only: removes what the operator and the agent created.
        rm -rf /etc/senhub-agent /var/lib/senhub-agent /var/log/senhub-agent
        if getent passwd senhub >/dev/null 2>&1; then
            userdel senhub >/dev/null 2>&1 || true
        fi
        if getent group senhub >/dev/null 2>&1; then
            groupdel senhub >/dev/null 2>&1 || true
        fi
        ;;
    0)
        # rpm erase moves an edited config aside as agent.yaml.rpmsave.
        # Put it back: a removal keeps the operator's configuration.
        cfg=/etc/senhub-agent/agent.yaml
        if [ -f "${cfg}.rpmsave" ] && [ ! -e "${cfg}" ]; then
            mv "${cfg}.rpmsave" "${cfg}" || true
        fi
        ;;
esac

exit 0
