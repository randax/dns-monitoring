#!/bin/bash
# Build script for creating DEB packages

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "${SCRIPT_DIR}/../.." && pwd )"

# Parse arguments
VERSION="${1:-dev}"
GIT_COMMIT="${2:-$(git rev-parse HEAD 2>/dev/null || echo 'unknown')}"
BUILD_NUMBER="${3:-1}"
ARCH="${4:-$(dpkg --print-architecture 2>/dev/null || echo 'amd64')}"

echo -e "${GREEN}Building DNS Monitor DEB package${NC}"
echo "Version: ${VERSION}"
echo "Commit: ${GIT_COMMIT}"
echo "Build: ${BUILD_NUMBER}"
echo "Architecture: ${ARCH}"

# Check for required tools
for tool in dpkg-deb go git fakeroot; do
    if ! command -v $tool &> /dev/null; then
        echo -e "${RED}Error: $tool is not installed${NC}"
        echo "Install with: apt-get install build-essential golang git fakeroot"
        exit 1
    fi
done

# Clean version string (remove 'v' prefix if present)
DEB_VERSION="${VERSION#v}"

# Create package directory structure
PACKAGE_NAME="dns-monitor"
PACKAGE_DIR="${PROJECT_ROOT}/dist/deb/${PACKAGE_NAME}_${DEB_VERSION}_${ARCH}"
rm -rf "${PACKAGE_DIR}"
mkdir -p "${PACKAGE_DIR}"

# Create directory structure
mkdir -p "${PACKAGE_DIR}/DEBIAN"
mkdir -p "${PACKAGE_DIR}/usr/bin"
mkdir -p "${PACKAGE_DIR}/etc/dns-monitor"
mkdir -p "${PACKAGE_DIR}/usr/share/dns-monitor/examples"
mkdir -p "${PACKAGE_DIR}/usr/share/dns-monitor/docs"
mkdir -p "${PACKAGE_DIR}/usr/share/doc/dns-monitor"
mkdir -p "${PACKAGE_DIR}/usr/share/man/man1"
mkdir -p "${PACKAGE_DIR}/lib/systemd/system"
mkdir -p "${PACKAGE_DIR}/var/log/dns-monitor"
mkdir -p "${PACKAGE_DIR}/var/lib/dns-monitor"

# Build the binary
echo -e "${YELLOW}Building binary...${NC}"
cd "${PROJECT_ROOT}"
CGO_ENABLED=1 go build -v \
    -ldflags="-s -w \
        -X 'main.Version=${VERSION}' \
        -X 'main.BuildTime=$(date -u +'%Y-%m-%dT%H:%M:%SZ')' \
        -X 'main.GitCommit=${GIT_COMMIT}' \
        -X 'main.Platform=linux/${ARCH}'" \
    -o "${PACKAGE_DIR}/usr/bin/dns-monitor" \
    ./cmd/dns-monitoring

# Strip binary to reduce size
strip "${PACKAGE_DIR}/usr/bin/dns-monitor" 2>/dev/null || true

# Copy configuration files
echo -e "${YELLOW}Copying configuration files...${NC}"
cp "${PROJECT_ROOT}/config.yaml" "${PACKAGE_DIR}/etc/dns-monitor/"
cp "${PROJECT_ROOT}/config-minimal.yaml" "${PACKAGE_DIR}/etc/dns-monitor/"

# Copy examples and docs
if [ -d "${PROJECT_ROOT}/examples" ]; then
    cp -r "${PROJECT_ROOT}/examples"/* "${PACKAGE_DIR}/usr/share/dns-monitor/examples/" 2>/dev/null || true
fi
if [ -d "${PROJECT_ROOT}/docs" ]; then
    cp -r "${PROJECT_ROOT}/docs"/* "${PACKAGE_DIR}/usr/share/dns-monitor/docs/" 2>/dev/null || true
fi

# Copy documentation
cp "${PROJECT_ROOT}/README.md" "${PACKAGE_DIR}/usr/share/doc/dns-monitor/"
cp "${PROJECT_ROOT}/LICENSE" "${PACKAGE_DIR}/usr/share/doc/dns-monitor/" 2>/dev/null || true

# Create systemd service file
cat > "${PACKAGE_DIR}/lib/systemd/system/dns-monitor.service" <<EOF
[Unit]
Description=DNS Monitoring Service
Documentation=https://github.com/randax/dns-monitoring
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=dns-monitor
Group=dns-monitor
ExecStart=/usr/bin/dns-monitor monitor -c /etc/dns-monitor/config.yaml
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=dns-monitor

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/dns-monitor /var/lib/dns-monitor

# Resource limits
LimitNOFILE=65536
LimitNPROC=512

# For passive monitoring (packet capture)
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN

[Install]
WantedBy=multi-user.target
EOF

# Create man page
cat > "${PACKAGE_DIR}/usr/share/man/man1/dns-monitor.1" <<EOF
.TH DNS-MONITOR 1 "${VERSION}" "DNS Monitor" "User Commands"
.SH NAME
dns-monitor \- Comprehensive DNS monitoring solution
.SH SYNOPSIS
.B dns-monitor
[COMMAND] [OPTIONS]
.SH DESCRIPTION
DNS Monitor is an enterprise-grade DNS monitoring solution that provides
deep insights into DNS performance, cache behavior, and network quality.
.SH COMMANDS
.TP
.B monitor
Start DNS monitoring with specified configuration
.TP
.B version
Display version information
.TP
.B test
Test configuration and connectivity
.TP
.B validate
Validate configuration file
.SH OPTIONS
.TP
.B \-c, \-\-config FILE
Configuration file path (default: config.yaml)
.TP
.B \-v, \-\-verbose
Enable verbose logging
.TP
.B \-\-help
Show help message
.SH FILES
.TP
.I /etc/dns-monitor/config.yaml
Main configuration file
.TP
.I /var/log/dns-monitor/
Log directory
.TP
.I /var/lib/dns-monitor/
Data directory
.SH SEE ALSO
Full documentation at: https://github.com/randax/dns-monitoring
.SH AUTHOR
DNS Monitor Contributors
EOF

# Compress man page
gzip -9 "${PACKAGE_DIR}/usr/share/man/man1/dns-monitor.1"

# Calculate installed size
INSTALLED_SIZE=$(du -sk "${PACKAGE_DIR}" | cut -f1)

# Create control file
cat > "${PACKAGE_DIR}/DEBIAN/control" <<EOF
Package: dns-monitor
Version: ${DEB_VERSION}-${BUILD_NUMBER}
Section: net
Priority: optional
Architecture: ${ARCH}
Maintainer: DNS Monitor Contributors <noreply@example.com>
Installed-Size: ${INSTALLED_SIZE}
Depends: libpcap0.8, adduser, systemd
Recommends: prometheus, zabbix-agent
Homepage: https://github.com/randax/dns-monitoring
Description: Comprehensive DNS monitoring solution
 DNS Monitor is an enterprise-grade DNS monitoring solution that provides
 deep insights into DNS performance, cache behavior, and network quality.
 .
 Features:
  - Active and passive DNS monitoring
  - Multi-protocol support (UDP, TCP, DoT, DoH)
  - Cache performance analysis
  - Network metrics and quality scoring
  - Prometheus and Zabbix integration
  - Real-time CLI dashboard
EOF

# Create postinst script
cat > "${PACKAGE_DIR}/DEBIAN/postinst" <<'EOF'
#!/bin/bash
set -e

case "$1" in
    configure)
        # Create service user and group
        if ! getent group dns-monitor >/dev/null; then
            addgroup --system dns-monitor
        fi
        
        if ! getent passwd dns-monitor >/dev/null; then
            adduser --system --ingroup dns-monitor --home /var/lib/dns-monitor \
                --no-create-home --gecos "DNS Monitor Service" dns-monitor
        fi
        
        # Set proper permissions
        chown -R dns-monitor:dns-monitor /var/log/dns-monitor
        chown -R dns-monitor:dns-monitor /var/lib/dns-monitor
        chmod 750 /var/log/dns-monitor
        chmod 750 /var/lib/dns-monitor
        
        # Set capabilities for packet capture
        setcap cap_net_raw,cap_net_admin+eip /usr/bin/dns-monitor 2>/dev/null || true
        
        # Reload systemd
        systemctl daemon-reload
        
        # Enable service (but don't start)
        systemctl enable dns-monitor.service
        
        echo "DNS Monitor installed successfully!"
        echo "Edit /etc/dns-monitor/config.yaml to configure"
        echo "Start with: systemctl start dns-monitor"
        ;;
    
    abort-upgrade|abort-remove|abort-deconfigure)
        ;;
    
    *)
        echo "postinst called with unknown argument \`$1'" >&2
        exit 1
        ;;
esac

#DEBHELPER#
exit 0
EOF

# Create prerm script
cat > "${PACKAGE_DIR}/DEBIAN/prerm" <<'EOF'
#!/bin/bash
set -e

case "$1" in
    remove|upgrade|deconfigure)
        # Stop service if running
        if systemctl is-active --quiet dns-monitor; then
            systemctl stop dns-monitor
        fi
        
        # Disable service
        systemctl disable dns-monitor.service 2>/dev/null || true
        ;;
    
    failed-upgrade)
        ;;
    
    *)
        echo "prerm called with unknown argument \`$1'" >&2
        exit 1
        ;;
esac

#DEBHELPER#
exit 0
EOF

# Create postrm script
cat > "${PACKAGE_DIR}/DEBIAN/postrm" <<'EOF'
#!/bin/bash
set -e

case "$1" in
    purge)
        # Remove service user and group
        if getent passwd dns-monitor >/dev/null; then
            deluser --quiet dns-monitor
        fi
        
        if getent group dns-monitor >/dev/null; then
            delgroup --quiet dns-monitor
        fi
        
        # Remove directories
        rm -rf /var/log/dns-monitor
        rm -rf /var/lib/dns-monitor
        
        # Remove capabilities
        setcap -r /usr/bin/dns-monitor 2>/dev/null || true
        ;;
    
    remove|upgrade|failed-upgrade|abort-install|abort-upgrade|disappear)
        ;;
    
    *)
        echo "postrm called with unknown argument \`$1'" >&2
        exit 1
        ;;
esac

#DEBHELPER#
exit 0
EOF

# Create conffiles
cat > "${PACKAGE_DIR}/DEBIAN/conffiles" <<EOF
/etc/dns-monitor/config.yaml
/etc/dns-monitor/config-minimal.yaml
EOF

# Set correct permissions for DEBIAN scripts
chmod 755 "${PACKAGE_DIR}/DEBIAN/postinst"
chmod 755 "${PACKAGE_DIR}/DEBIAN/prerm"
chmod 755 "${PACKAGE_DIR}/DEBIAN/postrm"

# Build the package
echo -e "${YELLOW}Building DEB package...${NC}"
OUTPUT_DIR="${PROJECT_ROOT}/dist/deb"
mkdir -p "${OUTPUT_DIR}"

# Use fakeroot to build the package with correct permissions
fakeroot dpkg-deb --build "${PACKAGE_DIR}" "${OUTPUT_DIR}/"

# Create checksums
DEB_FILE="${OUTPUT_DIR}/${PACKAGE_NAME}_${DEB_VERSION}_${ARCH}.deb"
cd "${OUTPUT_DIR}"
sha256sum "$(basename ${DEB_FILE})" > "$(basename ${DEB_FILE}).sha256"
md5sum "$(basename ${DEB_FILE})" > "$(basename ${DEB_FILE}).md5"
cd -

# Clean up build directory
rm -rf "${PACKAGE_DIR}"

echo -e "${GREEN}Successfully built DEB package:${NC}"
ls -la "${DEB_FILE}"*

echo -e "${GREEN}Package information:${NC}"
dpkg-deb --info "${DEB_FILE}"

echo -e "${GREEN}DEB build complete!${NC}"
echo -e "Package is available at: ${DEB_FILE}"
echo -e "\nInstall with: sudo dpkg -i ${DEB_FILE}"
echo -e "Or: sudo apt install ${DEB_FILE}"