#!/bin/bash
#
# Cronlock Installation Script
# Installs cronlock binary, config, and systemd service on Linux systems
#

set -euo pipefail

REPO="eugenetaranov/cronlock"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/cronlock"
CONFIG_FILE="${CONFIG_DIR}/cronlock.yaml"
SERVICE_FILE="/etc/systemd/system/cronlock.service"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Check if systemd is available
has_systemd() {
    command -v systemctl &> /dev/null && [ -d /run/systemd/system ]
}

# Track if service was installed
SERVICE_INSTALLED=false

# Pre-flight checks
preflight_checks() {
    info "Running pre-flight checks..."

    # Check if running on Linux
    OS=$(uname -s)
    if [ "$OS" != "Linux" ]; then
        error "This script only supports Linux. Detected OS: $OS"
    fi

    # Check if running as root
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root. Please use sudo."
    fi

    # Detect architecture
    MACHINE=$(uname -m)
    case "$MACHINE" in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $MACHINE"
            ;;
    esac
    info "Detected architecture: $ARCH"

    # Check for required tools
    for tool in curl tar; do
        if ! command -v "$tool" &> /dev/null; then
            error "Required tool not found: $tool"
        fi
    done

    info "Pre-flight checks passed"
}

# Fetch latest release version from GitHub
get_latest_version() {
    info "Fetching latest release version..."

    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
        grep '"tag_name":' | \
        sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$VERSION" ]; then
        error "Failed to fetch latest version from GitHub"
    fi

    info "Latest version: $VERSION"
}

# Download and extract release
download_release() {
    info "Downloading cronlock ${VERSION} for linux/${ARCH}..."

    # Remove leading 'v' from version for tarball name if present
    VERSION_NUM="${VERSION#v}"

    TARBALL="cronlock_${VERSION_NUM}_linux_${ARCH}.tar.gz"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"

    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TEMP_DIR"' EXIT

    # Download tarball
    if ! curl -fsSL "$DOWNLOAD_URL" -o "${TEMP_DIR}/${TARBALL}"; then
        error "Failed to download ${DOWNLOAD_URL}"
    fi

    # Extract tarball
    info "Extracting archive..."
    tar -xzf "${TEMP_DIR}/${TARBALL}" -C "$TEMP_DIR"

    BINARY_PATH="${TEMP_DIR}/cronlock"
    if [ ! -f "$BINARY_PATH" ]; then
        error "Binary not found in archive"
    fi
}

# Install binary
install_binary() {
    info "Installing binary to ${INSTALL_DIR}/cronlock..."

    cp "$BINARY_PATH" "${INSTALL_DIR}/cronlock"
    chmod +x "${INSTALL_DIR}/cronlock"

    info "Binary installed successfully"
}

# Create config directory and file
create_config() {
    if [ -f "$CONFIG_FILE" ]; then
        warn "Config file already exists at ${CONFIG_FILE}, skipping..."
        return
    fi

    info "Creating config directory and file..."

    mkdir -p "$CONFIG_DIR"

    # Get hostname for node ID
    NODE_ID=$(hostname)

    cat > "$CONFIG_FILE" << EOF
node:
  id: "${NODE_ID}"
  grace_period: 5s

redis:
  address: "localhost:6379"

jobs:
  - name: "example"
    schedule: "* * * * *"
    command: "echo 'Hello from cronlock'"
    timeout: 30s
EOF

    chmod 644 "$CONFIG_FILE"

    info "Config created at ${CONFIG_FILE}"
}

# Install systemd service
install_service() {
    if ! has_systemd; then
        info "Systemd not available, skipping service installation"
        return
    fi

    info "Installing systemd service..."

    cat > "$SERVICE_FILE" << 'EOF'
[Unit]
Description=Cronlock - Distributed Cron Scheduler
Documentation=https://github.com/eugenetaranov/cronlock
After=network.target redis.service
Wants=redis.service

[Service]
Type=notify
ExecStart=/usr/local/bin/cronlock --config /etc/cronlock/cronlock.yaml
ExecReload=/bin/kill -HUP $MAINPID

# Graceful shutdown
TimeoutStopSec=30
KillMode=mixed
KillSignal=SIGTERM

# Restart policy
Restart=on-failure
RestartSec=5

# Watchdog
WatchdogSec=30

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
RestrictNamespaces=yes

# Allow read access to config
ReadOnlyPaths=/etc/cronlock

# Resource limits
LimitNOFILE=65536
MemoryMax=256M

# Environment
Environment=GOMAXPROCS=2

[Install]
WantedBy=multi-user.target
EOF

    # Reload systemd
    systemctl daemon-reload

    SERVICE_INSTALLED=true
    info "Systemd service installed"
}

# Print summary and next steps
print_summary() {
    echo ""
    echo "=========================================="
    echo -e "${GREEN}Cronlock installation complete!${NC}"
    echo "=========================================="
    echo ""
    echo "Installed components:"
    echo "  - Binary:  ${INSTALL_DIR}/cronlock"
    echo "  - Config:  ${CONFIG_FILE}"
    if [ "$SERVICE_INSTALLED" = true ]; then
        echo "  - Service: ${SERVICE_FILE}"
    fi
    echo ""
    echo "Next steps:"
    echo "  1. Edit config:        sudo vi ${CONFIG_FILE}"
    echo "  2. Test manually:      cronlock --config ${CONFIG_FILE}"
    if [ "$SERVICE_INSTALLED" = true ]; then
        echo "  3. Enable and start:   sudo systemctl enable --now cronlock"
        echo "  4. Check status:       sudo systemctl status cronlock"
    else
        echo ""
        echo "Note: Systemd not available. Run cronlock manually or configure"
        echo "      your init system to start it at boot."
    fi
    echo ""
}

# Main
main() {
    echo ""
    echo "Cronlock Installer"
    echo "=================="
    echo ""

    preflight_checks
    get_latest_version
    download_release
    install_binary
    create_config
    install_service
    print_summary
}

main "$@"
