#!/bin/sh
# preremove hook for the senhub-agent .deb / .rpm packages.
#
# Stops and disables the service before the binary and unit are
# removed. On a .deb upgrade nFPM passes "upgrade" as $1 and dpkg
# passes the new version as $2; on an .rpm upgrade $1 is "1". In both
# upgrade cases we must NOT stop the service — postinstall will
# restart it with the new binary. Only tear down on a real removal.
set -e

# dpkg (deb):  $1 = "remove" | "upgrade" | "deconfigure"
# rpm:         $1 = "0" (erase) | "1" (upgrade)
case "$1" in
    upgrade|1)
        # Upgrade in progress — leave the running service alone.
        exit 0
        ;;
esac

if command -v systemctl >/dev/null 2>&1; then
    systemctl stop senhub-agent.service >/dev/null 2>&1 || true
    systemctl disable senhub-agent.service >/dev/null 2>&1 || true
fi

exit 0
