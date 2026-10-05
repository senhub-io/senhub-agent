#!/bin/sh
# Runs before the files are unpacked, so the senhub user and group exist
# when the package manager applies the ownership recorded in the package.
# Mirrors ensureServiceUser in app/service_user_linux.go.
set -e

SENHUB_USER="senhub"
STATE_DIR="/var/lib/senhub-agent"
LOG_READER_GROUP="adm"

if ! getent group "${SENHUB_USER}" >/dev/null 2>&1; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system "${SENHUB_USER}"
    else
        addgroup --system "${SENHUB_USER}"
    fi
fi

if ! getent passwd "${SENHUB_USER}" >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid "${SENHUB_USER}" \
            --home-dir "${STATE_DIR}" --no-create-home \
            --shell /usr/sbin/nologin "${SENHUB_USER}"
    else
        adduser --system --ingroup "${SENHUB_USER}" \
            --home "${STATE_DIR}" --no-create-home \
            --shell /usr/sbin/nologin "${SENHUB_USER}"
    fi
fi

# System-log read access for the filetail probe (syslog:adm 0640 on
# Debian and Ubuntu). Not every distribution has the group, and the
# join is never worth failing an install for.
if getent group "${LOG_READER_GROUP}" >/dev/null 2>&1; then
    if command -v usermod >/dev/null 2>&1; then
        usermod -aG "${LOG_READER_GROUP}" "${SENHUB_USER}" || true
    elif command -v gpasswd >/dev/null 2>&1; then
        gpasswd -a "${SENHUB_USER}" "${LOG_READER_GROUP}" || true
    fi
fi

exit 0
