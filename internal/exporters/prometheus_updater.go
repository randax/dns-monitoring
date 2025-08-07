package exporters

import (
	"strings"
	
	"github.com/prometheus/client_golang/prometheus"
)

// Helper function to copy labels
func copyLabels(labels prometheus.Labels) prometheus.Labels {
	newLabels := prometheus.Labels{}
	for k, v := range labels {
		newLabels[k] = v
	}
	return newLabels
}

// Normalize response code for consistent labeling
func normalizeResponseCode(code string) string {
	code = strings.ToUpper(code)
	switch code {
	case "NOERROR", "NO_ERROR", "SUCCESS":
		return "NoError"
	case "NXDOMAIN", "NX_DOMAIN":
		return "NXDomain"
	case "SERVFAIL", "SERV_FAIL", "SERVER_FAIL":
		return "ServFail"
	case "FORMERR", "FORM_ERR", "FORMAT_ERROR":
		return "FormErr"
	case "NOTIMP", "NOT_IMP", "NOT_IMPLEMENTED":
		return "NotImpl"
	case "REFUSED":
		return "Refused"
	default:
		return "Other"
	}
}

// Normalize query type for consistent labeling
func normalizeQueryType(qtype string) string {
	qtype = strings.ToUpper(qtype)
	switch qtype {
	case "A", "AAAA", "CNAME", "MX", "TXT", "NS", "SOA", "PTR", "SRV", "CAA":
		return qtype
	default:
		return "Other"
	}
}