#!/bin/sh
# postremove hook for the senhub-agent .deb / .rpm packages.
#
# Reloads systemd after the unit file has been removed. Deliberately
# does NOT delete /etc/senhub-agent, /var/lib/senhub-agent or the
# senhub user: per the acceptance criteria a plain uninstall keeps the
# operator's configuration and state in place. Full purge of those
# paths is left to the operator.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
