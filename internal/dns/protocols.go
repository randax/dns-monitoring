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
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/randax/dns-monitoring/internal/config"
)

func queryUDP(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result) error {
	return performDNSQuery(ctx, server, domain, qtype, "udp", result)
}

func queryTCP(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result) error {
	return performDNSQuery(ctx, server, domain, qtype, "tcp", result)
}

func queryDoT(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result) error {
	return performDNSQuery(ctx, server, domain, qtype, "tcp-tls", result)
}

func performDNSQuery(ctx context.Context, server config.DNSServer, domain, qtype, network string, result *Result) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), StringToQueryType(qtype))
	m.RecursionDesired = true

	serverAddr := fmt.Sprintf("%s:%d", server.Address, server.Port)
	
	c := new(dns.Client)
	c.Net = network
	
	if network == "tcp-tls" {
		c.TLSConfig = &tls.Config{
			ServerName:         server.Address,
			InsecureSkipVerify: server.InsecureSkipVerify,
		}
	}

	deadline, ok := ctx.Deadline()
	if ok {
		c.Timeout = time.Until(deadline)
	}

	r, _, err := c.ExchangeContext(ctx, m, serverAddr)
	if err != nil {
		return fmt.Errorf("[%s] query failed for domain %s on server %s:%d: %w", network, domain, server.Address, server.Port, err)
	}

	result.ResponseCode = r.Rcode
	
	if r.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("[%s] query returned error code %s for domain %s on server %s:%d", network, dns.RcodeToString[r.Rcode], domain, server.Address, server.Port)
	}

	result.Answers = extractAnswers(r)
	return nil
}

func queryDoH(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), StringToQueryType(qtype))
	m.RecursionDesired = true

	wireFormat, err := m.Pack()
	if err != nil {
		return fmt.Errorf("[DoH] failed to pack DNS message for domain %s: %w", domain, err)
	}

	endpoint := server.DoHEndpoint
	if endpoint == "" {
		endpoint = "/dns-query"
	}
	
	url := server.Address
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = fmt.Sprintf("https://%s", url)
	}
	url = strings.TrimSuffix(url, "/") + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(wireFormat))
	if err != nil {
		return fmt.Errorf("[DoH] failed to create request for domain %s on server %s: %w", domain, server.Address, err)
	}

	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	hostname := extractHostname(server.Address)
	tlsConfig := &tls.Config{
		InsecureSkipVerify: server.InsecureSkipVerify,
	}
	
	if !server.InsecureSkipVerify {
		if net.ParseIP(hostname) != nil {
			tlsConfig.InsecureSkipVerify = true
		} else {
			tlsConfig.ServerName = hostname
		}
	}
	
	// Extract timeout from context deadline
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	} else {
		// Fallback to a default timeout if no deadline is set
		timeout = 10 * time.Second
	}
	
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("[DoH] request failed for domain %s on server %s: %w", domain, server.Address, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("[DoH] request returned status %d for domain %s on server %s", resp.StatusCode, domain, server.Address)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/dns-message") {
		return fmt.Errorf("[DoH] unexpected Content-Type %s for domain %s on server %s", contentType, domain, server.Address)
	}

	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("[DoH] failed to read response for domain %s on server %s: %w", domain, server.Address, err)
	}

	if len(responseData) == 0 {
		return fmt.Errorf("[DoH] empty response body received for domain %s on server %s", domain, server.Address)
	}

	r := new(dns.Msg)
	if err := r.Unpack(responseData); err != nil {
		return fmt.Errorf("[DoH] failed to unpack response for domain %s on server %s: %w", domain, server.Address, err)
	}

	result.ResponseCode = r.Rcode
	
	if r.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("[DoH] query returned error code %s for domain %s on server %s", dns.RcodeToString[r.Rcode], domain, server.Address)
	}

	result.Answers = extractAnswers(r)
	return nil
}

func queryDoHGet(ctx context.Context, server config.DNSServer, domain, qtype string, result *Result) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), StringToQueryType(qtype))
	m.RecursionDesired = true

	wireFormat, err := m.Pack()
	if err != nil {
		return fmt.Errorf("[DoH-GET] failed to pack DNS message for domain %s: %w", domain, err)
	}

	dnsParam := base64.RawURLEncoding.EncodeToString(wireFormat)
	
	endpoint := server.DoHEndpoint
	if endpoint == "" {
		endpoint = "/dns-query"
	}
	
	url := server.Address
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = fmt.Sprintf("https://%s", url)
	}
	url = fmt.Sprintf("%s%s?dns=%s", strings.TrimSuffix(url, "/"), endpoint, dnsParam)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("[DoH-GET] failed to create request for domain %s on server %s: %w", domain, server.Address, err)
	}

	req.Header.Set("Accept", "application/dns-message")

	hostname := extractHostname(server.Address)
	tlsConfig := &tls.Config{
		InsecureSkipVerify: server.InsecureSkipVerify,
	}
	
	if !server.InsecureSkipVerify {
		if net.ParseIP(hostname) != nil {
			tlsConfig.InsecureSkipVerify = true
		} else {
			tlsConfig.ServerName = hostname
		}
	}
	
	// Extract timeout from context deadline
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	} else {
		// Fallback to a default timeout if no deadline is set
		timeout = 10 * time.Second
	}
	
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("[DoH-GET] request failed for domain %s on server %s: %w", domain, server.Address, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("[DoH-GET] request returned status %d for domain %s on server %s", resp.StatusCode, domain, server.Address)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/dns-message") {
		return fmt.Errorf("[DoH-GET] unexpected Content-Type %s for domain %s on server %s", contentType, domain, server.Address)
	}

	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("[DoH-GET] failed to read response for domain %s on server %s: %w", domain, server.Address, err)
	}

	if len(responseData) == 0 {
		return fmt.Errorf("[DoH-GET] empty response body received for domain %s on server %s", domain, server.Address)
	}

	r := new(dns.Msg)
	if err := r.Unpack(responseData); err != nil {
		return fmt.Errorf("[DoH-GET] failed to unpack response for domain %s on server %s: %w", domain, server.Address, err)
	}

	result.ResponseCode = r.Rcode
	
	if r.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("[DoH-GET] query returned error code %s for domain %s on server %s", dns.RcodeToString[r.Rcode], domain, server.Address)
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