Name:           dns-monitor
Version:        %{_version}
Release:        1%{?dist}
Summary:        Comprehensive DNS monitoring solution with real-time metrics

License:        Apache-2.0
URL:            https://github.com/randax/dns-monitoring
Source0:        %{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.21
BuildRequires:  git
BuildRequires:  make
BuildRequires:  libpcap-devel
BuildRequires:  systemd-rpm-macros

Requires:       libpcap
Requires:       systemd

%description
DNS Monitoring Tool is an enterprise-grade DNS monitoring solution that provides
deep insights into DNS performance, cache behavior, and network quality. It
supports both active and passive monitoring modes, multiple DNS protocols
(UDP, TCP, DoT, DoH), and exports metrics to various monitoring platforms.

Features:
- Active and passive DNS monitoring
- Multi-protocol support (UDP, TCP, DNS-over-TLS, DNS-over-HTTPS)
- Cache performance analysis
- Network metrics and quality scoring
- Prometheus and Zabbix integration
- Real-time CLI dashboard

%prep
%setup -q

%build
# Set build variables
export VERSION=%{version}
export BUILD_TIME=$(date -u +'%%Y-%%m-%%dT%%H:%%M:%%SZ')
export GIT_COMMIT=%{_commit}
export CGO_ENABLED=1

# Build the binary
go build -v \
    -ldflags="-s -w \
        -X 'main.Version=${VERSION}' \
        -X 'main.BuildTime=${BUILD_TIME}' \
        -X 'main.GitCommit=${GIT_COMMIT}' \
        -X 'main.Platform=linux/%{_arch}'" \
    -o dns-monitor \
    ./cmd/dns-monitoring

%install
# Create directories
install -d %{buildroot}%{_bindir}
install -d %{buildroot}%{_sysconfdir}/dns-monitor
install -d %{buildroot}%{_unitdir}
install -d %{buildroot}%{_datadir}/dns-monitor/examples
install -d %{buildroot}%{_datadir}/dns-monitor/docs
install -d %{buildroot}%{_localstatedir}/log/dns-monitor
install -d %{buildroot}%{_localstatedir}/lib/dns-monitor
install -d %{buildroot}%{_mandir}/man1
install -d %{buildroot}%{_docdir}/%{name}

# Install binary
install -p -m 0755 dns-monitor %{buildroot}%{_bindir}/dns-monitor

# Install configuration files
install -p -m 0644 config.yaml %{buildroot}%{_sysconfdir}/dns-monitor/config.yaml
install -p -m 0644 config-minimal.yaml %{buildroot}%{_sysconfdir}/dns-monitor/config-minimal.yaml

# Install systemd service file
cat > %{buildroot}%{_unitdir}/dns-monitor.service <<EOF
[Unit]
Description=DNS Monitoring Service
Documentation=https://github.com/randax/dns-monitoring
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=dns-monitor
Group=dns-monitor
ExecStart=%{_bindir}/dns-monitor monitor -c %{_sysconfdir}/dns-monitor/config.yaml
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
ReadWritePaths=%{_localstatedir}/log/dns-monitor %{_localstatedir}/lib/dns-monitor

# Resource limits
LimitNOFILE=65536
LimitNPROC=512

# For passive monitoring (packet capture)
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN

[Install]
WantedBy=multi-user.target
EOF

# Install examples
cp -r examples/* %{buildroot}%{_datadir}/dns-monitor/examples/ 2>/dev/null || true

# Install documentation
cp -r docs/* %{buildroot}%{_datadir}/dns-monitor/docs/ 2>/dev/null || true
install -p -m 0644 README.md %{buildroot}%{_docdir}/%{name}/
install -p -m 0644 LICENSE %{buildroot}%{_docdir}/%{name}/ 2>/dev/null || true

# Create man page
cat > %{buildroot}%{_mandir}/man1/dns-monitor.1 <<EOF
.TH DNS-MONITOR 1 "%{version}" "DNS Monitor" "User Commands"
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
.I %{_sysconfdir}/dns-monitor/config.yaml
Main configuration file
.TP
.I %{_localstatedir}/log/dns-monitor/
Log directory
.TP
.I %{_localstatedir}/lib/dns-monitor/
Data directory
.SH SEE ALSO
Full documentation at: https://github.com/randax/dns-monitoring
.SH AUTHOR
DNS Monitor Contributors
EOF

# Create default config for rpm
cat > %{buildroot}%{_sysconfdir}/dns-monitor/dns-monitor.conf <<EOF
# DNS Monitor RPM Configuration
# This file is sourced by the systemd service

# Additional environment variables
# DNS_MONITOR_LOG_LEVEL=info
# DNS_MONITOR_PROMETHEUS_PORT=9090
EOF

%pre
# Create service user and group
getent group dns-monitor >/dev/null || groupadd -r dns-monitor
getent passwd dns-monitor >/dev/null || \
    useradd -r -g dns-monitor -d %{_localstatedir}/lib/dns-monitor \
    -s /sbin/nologin -c "DNS Monitor Service" dns-monitor
exit 0

%post
%systemd_post dns-monitor.service

# Set proper permissions
chown -R dns-monitor:dns-monitor %{_localstatedir}/log/dns-monitor
chown -R dns-monitor:dns-monitor %{_localstatedir}/lib/dns-monitor
chmod 750 %{_localstatedir}/log/dns-monitor
chmod 750 %{_localstatedir}/lib/dns-monitor

# Set capabilities for packet capture
setcap cap_net_raw,cap_net_admin+eip %{_bindir}/dns-monitor 2>/dev/null || true

%preun
%systemd_preun dns-monitor.service

%postun
%systemd_postun_with_restart dns-monitor.service

# Remove capabilities
if [ $1 -eq 0 ]; then
    setcap -r %{_bindir}/dns-monitor 2>/dev/null || true
fi

%files
%license %{_docdir}/%{name}/LICENSE
%doc %{_docdir}/%{name}/README.md
%{_bindir}/dns-monitor
%{_unitdir}/dns-monitor.service
%{_mandir}/man1/dns-monitor.1*
%config(noreplace) %{_sysconfdir}/dns-monitor/config.yaml
%config(noreplace) %{_sysconfdir}/dns-monitor/config-minimal.yaml
%config(noreplace) %{_sysconfdir}/dns-monitor/dns-monitor.conf
%{_datadir}/dns-monitor/
%dir %{_sysconfdir}/dns-monitor
%dir %attr(0750, dns-monitor, dns-monitor) %{_localstatedir}/log/dns-monitor
%dir %attr(0750, dns-monitor, dns-monitor) %{_localstatedir}/lib/dns-monitor

%changelog
* Thu Jan 09 2025 DNS Monitor Contributors <noreply@example.com> - %{version}-1
- Initial RPM package release
- Support for active and passive DNS monitoring
- Multi-protocol support (UDP, TCP, DoT, DoH)
- Prometheus and Zabbix integration
- Systemd service integration