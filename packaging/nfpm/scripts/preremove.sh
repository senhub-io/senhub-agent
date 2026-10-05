#!/bin/sh
# deb: $1 = remove | upgrade | deconfigure | failed-upgrade
# rpm: $1 = 0 (erase) | 1 (upgrade)
set -e

case "$1" in
    remove|0) ;;
    *) exit 0 ;;
esac

if [ -d /run/systemd/system ]; then
    systemctl stop senhub-agent.service || true
    systemctl disable senhub-agent.service || true
fi

exit 0
