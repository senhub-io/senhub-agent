#!/bin/sh
# postinstall hook for the senhub-agent .deb / .rpm packages.
#
# Shared by both formats; nFPM runs it after files are unpacked. It
# must be idempotent: on an upgrade it runs again with the new files
# already in place and an existing service possibly running.
set -e

SENHUB_USER="senhub"
SENHUB_GROUP="senhub"
CONFIG_DIR="/etc/senhub-agent"
STATE_DIR="/var/lib/senhub-agent"
LOG_DIR="/var/log/senhub-agent"
CONFIG_FILE="${CONFIG_DIR}/agent.yaml"
CONFIG_EXAMPLE="${CONFIG_DIR}/agent.yaml.example"

# 1. System user/group. The agent runs as a dedicated unprivileged
#    account; it only needs read access to host metrics plus write
#    access to its own state and log directories.
if ! getent group "${SENHUB_GROUP}" >/dev/null 2>&1; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system "${SENHUB_GROUP}"
    elif command -v addgroup >/dev/null 2>&1; then
        addgroup --system "${SENHUB_GROUP}"
    fi
fi

if ! getent passwd "${SENHUB_USER}" >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid "${SENHUB_GROUP}" \
            --home-dir "${STATE_DIR}" --no-create-home \
            --shell /usr/sbin/nologin "${SENHUB_USER}"
    elif command -v adduser >/dev/null 2>&1; then
        adduser --system --ingroup "${SENHUB_GROUP}" \
            --home "${STATE_DIR}" --no-create-home \
            --shell /usr/sbin/nologin "${SENHUB_USER}"
    fi
fi

# 2. Directories + ownership. ConfigurationDirectory/StateDirectory in
#    the unit cover the systemd-managed runtime, but the package owns
#    the on-disk paths so they exist before the first start.
mkdir -p "${CONFIG_DIR}" "${STATE_DIR}" "${LOG_DIR}"
chown "${SENHUB_USER}:${SENHUB_GROUP}" "${STATE_DIR}" "${LOG_DIR}"
chmod 0750 "${STATE_DIR}" "${LOG_DIR}"

# 3. Seed the live config from the shipped example on a fresh install.
#    Never overwrite an operator-edited config on upgrade.
if [ ! -f "${CONFIG_FILE}" ] && [ -f "${CONFIG_EXAMPLE}" ]; then
    cp "${CONFIG_EXAMPLE}" "${CONFIG_FILE}"
    chown "${SENHUB_USER}:${SENHUB_GROUP}" "${CONFIG_FILE}"
    chmod 0640 "${CONFIG_FILE}"
fi

# 4. Register and (re)start the service. Guarded so chroot/container
#    builds without a running systemd don't fail the install.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl enable senhub-agent.service >/dev/null 2>&1 || true
    if systemctl is-active --quiet senhub-agent.service; then
        systemctl restart senhub-agent.service >/dev/null 2>&1 || true
    else
        systemctl start senhub-agent.service >/dev/null 2>&1 || true
    fi
fi

exit 0
