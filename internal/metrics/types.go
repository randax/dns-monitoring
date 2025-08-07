package metrics

import (
	"time"
)

type Metrics struct {
	Latency      LatencyMetrics
	Rates        RateMetrics
	ResponseCode ResponseCodeDistribution
	Throughput   ThroughputMetrics
	QueryType    QueryTypeDistribution
	Period       MetricsPeriod
}

type LatencyMetrics struct {
	P50  float64
	P95  float64
	P99  float64
	P999 float64
	Min  float64
	Max  float64
	Mean float64
}

type RateMetrics struct {
	SuccessRate    float64
	ErrorRate      float64
	TotalQueries   int64
	SuccessQueries int64
	ErrorQueries   int64
	NetworkErrors  int64
}

type ResponseCodeDistribution struct {
	NoError   int64
	NXDomain  int64
	ServFail  int64
	FormErr   int64
	NotImpl   int64
	Refused   int64
	Other     int64
	Total     int64
}

type ThroughputMetrics struct {
	CurrentQPS  float64
	AverageQPS  float64
	PeakQPS     float64
	TotalQueries int64
	Duration    time.Duration
	ByProtocol  map[string]float64
	ByServer    map[string]float64
}

type QueryTypeDistribution struct {
	A       int64
	AAAA    int64
	CNAME   int64
	MX      int64
	TXT     int64
	NS      int64
	SOA     int64
	PTR     int64
	Other   int64
	Total   int64
}

type MetricsPeriod struct {
	Start time.Time
	End   time.Time
}