#!/bin/bash

# Check for root privileges
if [ "$EUID" -ne 0 ]; then
    echo "This script must be run as root"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        SOURCE_BINARY="senhub-agent_linux_amd64"
        ;;
    aarch64)
        SOURCE_BINARY="senhub-agent_linux_arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Configuration
AGENT_USER="senhub-agent"
AGENT_GROUP="senhub-agent"
AGENT_BIN="/usr/local/bin/senhubagent"
SERVICE_FILE="/etc/systemd/system/senhub-agent.service"

# Create group and user
echo "Creating service group and user..."
groupadd -r $AGENT_GROUP
useradd -r -s /bin/false -g $AGENT_GROUP $AGENT_USER

# Set required permissions
echo "Setting up permissions..."
# Access to system metrics
usermod -aG systemd-journal $AGENT_USER
# Network statistics access
setcap cap_net_raw,cap_net_admin=eip $AGENT_BIN

# Copy the agent
echo "Installing SenhubAgent..."
if [ ! -f "./$SOURCE_BINARY" ]; then
    echo "Error: Source binary ./$SOURCE_BINARY not found!"
    exit 1
fi

cp "./$SOURCE_BINARY" $AGENT_BIN
chown $AGENT_USER:$AGENT_GROUP $AGENT_BIN
chmod 755 $AGENT_BIN

# Create systemd service file
echo "Creating systemd service..."
cat > $SERVICE_FILE << EOF
[Unit]
Description=Senhub Monitoring Agent
After=network.target

[Service]
Type=simple
User=$AGENT_USER
Group=$AGENT_GROUP
ExecStart=$AGENT_BIN
Restart=always
RestartSec=10

# Additional permissions for metrics collection
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
NoNewPrivileges=true

# Security measures
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true

[Install]
WantedBy=multi-user.target
EOF

# Set service file permissions
chmod 644 $SERVICE_FILE

# Enable and start service
echo "Enabling and starting service..."
systemctl daemon-reload
systemctl enable senhub-agent
systemctl start senhub-agent

# Check status
echo "Checking service status..."
systemctl status senhub-agent

echo "Installation completed successfully!"
