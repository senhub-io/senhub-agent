#!/bin/sh
# deb: $1 = remove | upgrade | deconfigure | failed-upgrade
# rpm: $1 = 0 (erase) | 1 (upgrade)
set -e

case "$1" in
    remove|0) ;;
    *) exit 0 ;;
esac

# rpm erases the replaced edition after unpacking the new one: stopping and
# disabling the service here would undo the new package's postinstall.
if [ "$1" = 0 ] && command -v rpm >/dev/null 2>&1; then
    editions=$(rpm -q senhub-agent senhub-agent-oss 2>/dev/null | grep -vc 'not installed')
    [ "${editions}" -gt 1 ] && exit 0
fi

if [ -d /run/systemd/system ]; then
    systemctl stop senhub-agent-update.path senhub-agent-update-reset.timer || true
    systemctl disable senhub-agent-update.path senhub-agent-update-reset.timer || true
    systemctl stop senhub-agent.service || true
    systemctl disable senhub-agent.service || true
    systemctl stop senhub-ebpf.service || true
    systemctl disable senhub-ebpf.service || true
fi

# Files an update keeps beside the binary; the package does not own them.
rm -f /usr/bin/senhub-agent.previous /usr/bin/.senhub-agent.failed /usr/bin/.senhub-agent.new /usr/bin/.senhub-agent.old

exit 0
