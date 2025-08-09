package dns

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/randax/dns-monitoring/internal/config"
)

// formatDNSError creates a consistent error message for all DNS protocol errors.
// Format: [Protocol] operation failed for domain X on server Y (address:port): error
func formatDNSError(protocol string, operation string, domain string, server config.DNSServer, err error) error {
	if err != nil {
		return fmt.Errorf("[%s] %s failed for domain %s on server %s (%s:%d): %w", 
			protocol, operation, domain, server.Name, server.Address, server.Port, err)
	}
	return fmt.Errorf("[%s] %s failed for domain %s on server %s (%s:%d)", 
		protocol, operation, domain, server.Name, server.Address, server.Port)
}

// formatDNSErrorWithResponse creates an error message including response context.
func formatDNSErrorWithResponse(protocol string, operation string, domain string, server config.DNSServer, responseCode string) error {
	return fmt.Errorf("[%s] %s failed for domain %s on server %s (%s:%d): response code %s", 
		protocol, operation, domain, server.Name, server.Address, server.Port, responseCode)
}

// formatDNSErrorWithStatus creates an error message including HTTP status or other status information.
func formatDNSErrorWithStatus(protocol string, operation string, domain string, server config.DNSServer, status interface{}) error {
	return fmt.Errorf("[%s] %s failed for domain %s on server %s (%s:%d): status %v", 
		protocol, operation, domain, server.Name, server.Address, server.Port, status)
}

// extractTimeoutFromContext extracts timeout from context deadline.
// If no deadline is set, returns the configured query timeout.
// This ensures consistent timeout handling across all DNS protocols.
func extractTimeoutFromContext(ctx context.Context, cfg *config.Config) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout > 0 {
			return timeout
		}
		// Context has already expired, return a minimal but reasonable timeout
		// to allow for proper error handling (100ms is more reasonable than 1ms)
		return 100 * time.Millisecond
	}
	
	// Fallback to configured query timeout
	if cfg != nil && cfg.DNS.Queries.Timeout > 0 {
		return cfg.DNS.Queries.Timeout
	}
	
	// Ultimate fallback if config is not available
	return 5 * time.Second
}

func queryUDP(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result, cfg *config.Config) error {
	return performDNSQuery(ctx, server, domain, qtype, "udp", result, cfg)
}

func queryTCP(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result, cfg *config.Config) error {
	return performDNSQuery(ctx, server, domain, qtype, "tcp", result, cfg)
}

func queryDoT(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result, cfg *config.Config) error {
	return performDNSQuery(ctx, server, domain, qtype, "tcp-tls", result, cfg)
}

func performDNSQuery(ctx context.Context, server config.DNSServer, domain, qtype, network string, result *Result, cfg *config.Config) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), StringToQueryType(qtype))
	m.RecursionDesired = true

	serverAddr := fmt.Sprintf("%s:%d", server.Address, server.Port)
	
	c := new(dns.Client)
	c.Net = network
	
	// Determine protocol name for error messages
	protocolName := strings.ToUpper(network)
	if network == "tcp-tls" {
		protocolName = "DoT"
		c.TLSConfig = &tls.Config{
			ServerName:         server.Address,
			InsecureSkipVerify: server.InsecureSkipVerify,
		}
	}

	// Use standardized timeout extraction
	c.Timeout = extractTimeoutFromContext(ctx, cfg)

	r, _, err := c.ExchangeContext(ctx, m, serverAddr)
	if err != nil {
		return formatDNSError(protocolName, "query", domain, server, err)
	}

	result.ResponseCode = r.Rcode
	
	if r.Rcode != dns.RcodeSuccess {
		return formatDNSErrorWithResponse(protocolName, "query", domain, server, dns.RcodeToString[r.Rcode])
	}

	result.Answers = extractAnswers(r)
	return nil
}

func queryDoH(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result, cfg *config.Config) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), StringToQueryType(qtype))
	m.RecursionDesired = true

	wireFormat, err := m.Pack()
	if err != nil {
		return formatDNSError("DoH", "pack DNS message", domain, server, err)
	}

	endpoint := server.DoHEndpoint
	if endpoint == "" {
		endpoint = "/dns-query"
	}
	
	baseURL := server.Address
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = fmt.Sprintf("https://%s", baseURL)
	}
	
	// Parse the base URL
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return formatDNSError("DoH", "parse URL", domain, server, err)
	}
	
	// Join the endpoint path properly
	parsedURL.Path = strings.TrimSuffix(parsedURL.Path, "/") + endpoint
	finalURL := parsedURL.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, finalURL, bytes.NewReader(wireFormat))
	if err != nil {
		return formatDNSError("DoH", "create request", domain, server, err)
	}

	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	hostname := extractHostname(server.Address)
	
	// Parse the hostname to check if it's an IP address
	ip := net.ParseIP(hostname)
	isIPAddress := ip != nil
	
	// Determine if we need to skip certificate verification
	// Skip only for private IPs when not explicitly configured, or when explicitly configured
	shouldSkipVerify := server.InsecureSkipVerify
	if isIPAddress && !server.InsecureSkipVerify {
		// For private IPs, automatically skip verification (common for internal DNS servers)
		// For public IPs, attempt normal verification first
		if isPrivateIP(ip) {
			shouldSkipVerify = true
		}
	}
	
	tlsConfig := &tls.Config{
		InsecureSkipVerify: shouldSkipVerify,
	}
	
	// Set ServerName for certificate validation (even for IPs, in case they have proper certs)
	if !shouldSkipVerify {
		tlsConfig.ServerName = hostname
	}
	
	// Create HTTP client without explicit timeout - let context handle it
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return formatDNSError("DoH", "request", domain, server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return formatDNSErrorWithStatus("DoH", "request", domain, server, resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/dns-message") {
		return formatDNSErrorWithStatus("DoH", "response validation", domain, server, fmt.Sprintf("unexpected Content-Type: %s", contentType))
	}

	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return formatDNSError("DoH", "read response", domain, server, err)
	}

	if len(responseData) == 0 {
		return formatDNSError("DoH", "response validation", domain, server, fmt.Errorf("empty response body"))
	}

	r := new(dns.Msg)
	if err := r.Unpack(responseData); err != nil {
		return formatDNSError("DoH", "unpack response", domain, server, err)
	}

	result.ResponseCode = r.Rcode
	
	if r.Rcode != dns.RcodeSuccess {
		return formatDNSErrorWithResponse("DoH", "query", domain, server, dns.RcodeToString[r.Rcode])
	}

	result.Answers = extractAnswers(r)
	return nil
}

func queryDoHGet(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result, cfg *config.Config) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), StringToQueryType(qtype))
	m.RecursionDesired = true

	wireFormat, err := m.Pack()
	if err != nil {
		return formatDNSError("DoH-GET", "pack DNS message", domain, server, err)
	}

	dnsParam := base64.RawURLEncoding.EncodeToString(wireFormat)
	
	endpoint := server.DoHEndpoint
	if endpoint == "" {
		endpoint = "/dns-query"
	}
	
	baseURL := server.Address
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = fmt.Sprintf("https://%s", baseURL)
	}
	
	// Parse the base URL
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return formatDNSError("DoH-GET", "parse URL", domain, server, err)
	}
	
	// Join the endpoint path properly
	parsedURL.Path = strings.TrimSuffix(parsedURL.Path, "/") + endpoint
	
	// Add query parameters
	q := parsedURL.Query()
	q.Set("dns", dnsParam)
	parsedURL.RawQuery = q.Encode()
	
	finalURL := parsedURL.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, finalURL, nil)
	if err != nil {
		return formatDNSError("DoH-GET", "create request", domain, server, err)
	}

	req.Header.Set("Accept", "application/dns-message")

	hostname := extractHostname(server.Address)
	
	// Parse the hostname to check if it's an IP address
	ip := net.ParseIP(hostname)
	isIPAddress := ip != nil
	
	// Determine if we need to skip certificate verification
	// Skip only for private IPs when not explicitly configured, or when explicitly configured
	shouldSkipVerify := server.InsecureSkipVerify
	if isIPAddress && !server.InsecureSkipVerify {
		// For private IPs, automatically skip verification (common for internal DNS servers)
		// For public IPs, attempt normal verification first
		if isPrivateIP(ip) {
			shouldSkipVerify = true
		}
	}
	
	tlsConfig := &tls.Config{
		InsecureSkipVerify: shouldSkipVerify,
	}
	
	// Set ServerName for certificate validation (even for IPs, in case they have proper certs)
	if !shouldSkipVerify {
		tlsConfig.ServerName = hostname
	}
	
	// Create HTTP client without explicit timeout - let context handle it
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return formatDNSError("DoH-GET", "request", domain, server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return formatDNSErrorWithStatus("DoH-GET", "request", domain, server, resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/dns-message") {
		return formatDNSErrorWithStatus("DoH-GET", "response validation", domain, server, fmt.Sprintf("unexpected Content-Type: %s", contentType))
	}

	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return formatDNSError("DoH-GET", "read response", domain, server, err)
	}

	if len(responseData) == 0 {
		return formatDNSError("DoH-GET", "response validation", domain, server, fmt.Errorf("empty response body"))
	}

	r := new(dns.Msg)
	if err := r.Unpack(responseData); err != nil {
		return formatDNSError("DoH-GET", "unpack response", domain, server, err)
	}

	result.ResponseCode = r.Rcode
	
	if r.Rcode != dns.RcodeSuccess {
		return formatDNSErrorWithResponse("DoH-GET", "query", domain, server, dns.RcodeToString[r.Rcode])
	}

	result.Answers = extractAnswers(r)
	return nil
}

func extractAnswers(r *dns.Msg) []string {
	var answers []string
	for _, a := range r.Answer {
		switch record := a.(type) {
		case *dns.A:
			answers = append(answers, record.A.String())
		case *dns.AAAA:
			answers = append(answers, record.AAAA.String())
		case *dns.CNAME:
			answers = append(answers, record.Target)
		case *dns.MX:
			answers = append(answers, fmt.Sprintf("%d %s", record.Preference, record.Mx))
		case *dns.NS:
			answers = append(answers, record.Ns)
		case *dns.TXT:
			answers = append(answers, strings.Join(record.Txt, " "))
		case *dns.SOA:
			answers = append(answers, fmt.Sprintf("%s %s", record.Ns, record.Mbox))
		case *dns.PTR:
			answers = append(answers, record.Ptr)
		case *dns.SRV:
			answers = append(answers, fmt.Sprintf("%d %d %d %s", record.Priority, record.Weight, record.Port, record.Target))
		case *dns.CAA:
			answers = append(answers, fmt.Sprintf("%d %s %s", record.Flag, record.Tag, record.Value))
		default:
			answers = append(answers, a.String())
		}
	}
	return answers
}

func extractHostname(address string) string {
	address = strings.TrimPrefix(address, "https://")
	address = strings.TrimPrefix(address, "http://")
	
	if idx := strings.Index(address, ":"); idx != -1 {
		address = address[:idx]
	}
	if idx := strings.Index(address, "/"); idx != -1 {
		address = address[:idx]
	}
	
	return address
}

// isPrivateIP checks if an IP address is in a private range (RFC 1918, RFC 4193, loopback, link-local)
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	
	// Check for IPv4 private ranges
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 127.0.0.0/8 (loopback)
		if ip4[0] == 127 {
			return true
		}
		// 169.254.0.0/16 (link-local)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		return false
	}
	
	// Check for IPv6 private ranges
	// Loopback (::1)
	if ip.IsLoopback() {
		return true
	}
	// Link-local (fe80::/10)
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// Unique local (fc00::/7)
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	
	return false
}