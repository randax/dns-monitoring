package dns

import (
	"time"

	"github.com/miekg/dns"
)

type Protocol string

const (
	ProtocolUDP Protocol = "udp"
	ProtocolTCP Protocol = "tcp"
	ProtocolDoT Protocol = "dot"
	ProtocolDoH Protocol = "doh"
)

type Result struct {
	Server       string           `json:"server"`
	Domain       string           `json:"domain"`
	QueryType    string           `json:"query_type"`
	Protocol     Protocol         `json:"protocol"`
	Duration     time.Duration    `json:"duration"`
	ResponseCode int              `json:"response_code"`
	Error        error            `json:"error,omitempty"`
	Timestamp    time.Time        `json:"timestamp"`
	Answers      []string         `json:"answers,omitempty"`
	Retries      int              `json:"retries"`
	Extra        interface{}      `json:"-"`
	
	// Cache performance fields
	CacheHit         bool          `json:"cache_hit,omitempty"`
	CacheAge         time.Duration `json:"cache_age,omitempty"`
	TTL              time.Duration `json:"ttl,omitempty"`
	CacheEfficiency  float64       `json:"cache_efficiency,omitempty"`
	RecursiveServer  bool          `json:"recursive_server,omitempty"`
	
	// Network-level metrics fields
	PacketLoss      float64       `json:"packet_loss,omitempty"`
	Jitter          time.Duration `json:"jitter,omitempty"`
	NetworkLatency  time.Duration `json:"network_latency,omitempty"`
	HopCount        int           `json:"hop_count,omitempty"`
	NetworkQuality  float64       `json:"network_quality,omitempty"`
}

func StringToQueryType(qtype string) uint16 {
	switch qtype {
	case "A":
		return dns.TypeA
	case "AAAA":
		return dns.TypeAAAA
	case "CNAME":
		return dns.TypeCNAME
	case "MX":
		return dns.TypeMX
	case "NS":
		return dns.TypeNS
	case "PTR":
		return dns.TypePTR
	case "SOA":
		return dns.TypeSOA
	case "TXT":
		return dns.TypeTXT
	case "SRV":
		return dns.TypeSRV
	case "CAA":
		return dns.TypeCAA
	default:
		return dns.TypeA
	}
}

func QueryTypeToString(qtype uint16) string {
	switch qtype {
	case dns.TypeA:
		return "A"
	case dns.TypeAAAA:
		return "AAAA"
	case dns.TypeCNAME:
		return "CNAME"
	case dns.TypeMX:
		return "MX"
	case dns.TypeNS:
		return "NS"
	case dns.TypePTR:
		return "PTR"
	case dns.TypeSOA:
		return "SOA"
	case dns.TypeTXT:
		return "TXT"
	case dns.TypeSRV:
		return "SRV"
	case dns.TypeCAA:
		return "CAA"
	default:
		return "UNKNOWN"
	}
}