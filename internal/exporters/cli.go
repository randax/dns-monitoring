package exporters

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/randax/dns-monitoring/internal/metrics"
)

type CLIFormatter struct {
	ShowColors        bool
	DetailedView      bool
	CompactMode       bool
	ShowDistributions bool
	terminal          *Terminal
}

func NewCLIFormatter(showColors, detailedView, compactMode, showDistributions bool) *CLIFormatter {
	return &CLIFormatter{
		ShowColors:        showColors,
		DetailedView:      detailedView,
		CompactMode:       compactMode,
		ShowDistributions: showDistributions,
		terminal:          NewTerminal(showColors),
	}
}

func (f *CLIFormatter) FormatMetrics(m *metrics.Metrics) string {
	if m == nil {
		return f.terminal.Red("No metrics available")
	}

	var sb strings.Builder

	if !f.CompactMode {
		f.formatHeader(&sb, m)
	}

	f.formatLatencyMetrics(&sb, m)
	f.formatSuccessMetrics(&sb, m)
	f.formatThroughputMetrics(&sb, m)

	// Add cache and network metrics
	if m.Cache != nil {
		f.formatCacheMetrics(&sb, m)
	}
	if m.Network != nil {
		f.formatNetworkMetrics(&sb, m)
	}

	if f.DetailedView {
		f.formatResponseCodeDistribution(&sb, m)
		if f.ShowDistributions {
			f.formatQueryTypeDistribution(&sb, m)
		}
	}

	if !f.CompactMode {
		f.formatFooter(&sb, m)
	}

	return sb.String()
}

func (f *CLIFormatter) FormatSummary(m *metrics.Metrics) string {
	if m == nil {
		return f.terminal.Red("No metrics available for summary")
	}

	var sb strings.Builder

	sb.WriteString(f.terminal.Bold("\n=== DNS Monitoring Summary ===\n\n"))

	duration := m.Period.End.Sub(m.Period.Start)
	sb.WriteString(fmt.Sprintf("Monitoring Duration: %s\n", f.formatDuration(duration)))
	sb.WriteString(fmt.Sprintf("Total Queries: %s\n", f.formatNumber(m.Rates.TotalQueries)))
	sb.WriteString(fmt.Sprintf("Total Errors: %s\n", f.formatNumber(m.Rates.ErrorQueries)))
	sb.WriteString("\n")

	f.formatLatencyMetrics(&sb, m)
	f.formatSuccessMetrics(&sb, m)
	f.formatThroughputMetrics(&sb, m)
	f.formatResponseCodeDistribution(&sb, m)

	return sb.String()
}

func (f *CLIFormatter) formatHeader(sb *strings.Builder, m *metrics.Metrics) {
	if clearStr := f.safeTerminalOp(f.terminal.Clear); clearStr != "" {
		sb.WriteString(clearStr)
	}
	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("DNS Monitoring Dashboard\n") }))
	duration := m.Period.End.Sub(m.Period.Start)
	sb.WriteString(fmt.Sprintf("Updated: %s | Duration: %s\n",
		time.Now().Format("15:04:05"),
		f.formatDuration(duration)))
	sb.WriteString(strings.Repeat("-", 60) + "\n\n")
}

func (f *CLIFormatter) formatFooter(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString("\n" + strings.Repeat("-", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Total Queries: %s | Errors: %s | Success Rate: %.2f%%\n",
		f.formatNumber(m.Rates.TotalQueries),
		f.formatNumber(m.Rates.ErrorQueries),
		f.safePercentage(m.Rates.SuccessRate)))
}

func (f *CLIFormatter) formatLatencyMetrics(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("Latency Metrics:\n") }))

	latencyData := []struct {
		label string
		value time.Duration
		warn  time.Duration
		crit  time.Duration
	}{
		{"  P50 ", f.safeLatencyDuration(m.Latency.P50), 50 * time.Millisecond, 100 * time.Millisecond},
		{"  P95 ", f.safeLatencyDuration(m.Latency.P95), 100 * time.Millisecond, 200 * time.Millisecond},
		{"  P99 ", f.safeLatencyDuration(m.Latency.P99), 200 * time.Millisecond, 500 * time.Millisecond},
		{"  P999", f.safeLatencyDuration(m.Latency.P999), 500 * time.Millisecond, 1000 * time.Millisecond},
	}

	for _, l := range latencyData {
		formattedValue := f.formatDuration(l.value)
		coloredValue := formattedValue

		if f.ShowColors {
			if l.value >= l.crit {
				coloredValue = f.terminal.Red(formattedValue)
			} else if l.value >= l.warn {
				coloredValue = f.terminal.Yellow(formattedValue)
			} else {
				coloredValue = f.terminal.Green(formattedValue)
			}
		}

		sb.WriteString(fmt.Sprintf("%s: %s\n", l.label, coloredValue))
	}

	if f.DetailedView {
		sb.WriteString(fmt.Sprintf("  Min : %s\n", f.formatDuration(f.safeLatencyDuration(m.Latency.Min))))
		sb.WriteString(fmt.Sprintf("  Max : %s\n", f.formatDuration(f.safeLatencyDuration(m.Latency.Max))))
		sb.WriteString(fmt.Sprintf("  Avg : %s\n", f.formatDuration(f.safeLatencyDuration(m.Latency.Mean))))
	}
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatSuccessMetrics(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("Success Metrics:\n") }))

	successRate := f.safePercentage(m.Rates.SuccessRate)
	errorRate := f.safePercentage(m.Rates.ErrorRate)

	successColor := f.terminal.Green
	if successRate < 95 {
		successColor = f.terminal.Yellow
	}
	if successRate < 90 {
		successColor = f.terminal.Red
	}

	if f.ShowColors {
		sb.WriteString(fmt.Sprintf("  Success Rate: %s\n", successColor(fmt.Sprintf("%.2f%%", successRate))))
		sb.WriteString(fmt.Sprintf("  Error Rate  : %s\n", f.terminal.Red(fmt.Sprintf("%.2f%%", errorRate))))
	} else {
		sb.WriteString(fmt.Sprintf("  Success Rate: %.2f%%\n", successRate))
		sb.WriteString(fmt.Sprintf("  Error Rate  : %.2f%%\n", errorRate))
	}

	if f.DetailedView {
		sb.WriteString(fmt.Sprintf("  Total Success: %s\n", f.formatNumber(m.Rates.SuccessQueries)))
		sb.WriteString(fmt.Sprintf("  Total Errors : %s\n", f.formatNumber(m.Rates.ErrorQueries)))
	}
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatThroughputMetrics(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("Throughput:\n") }))
	sb.WriteString(fmt.Sprintf("  Current QPS: %s\n", f.formatQPS(f.safeQPS(m.Throughput.CurrentQPS))))
	sb.WriteString(fmt.Sprintf("  Average QPS: %s\n", f.formatQPS(f.safeQPS(m.Throughput.AverageQPS))))
	sb.WriteString(fmt.Sprintf("  Peak QPS   : %s\n", f.formatQPS(f.safeQPS(m.Throughput.PeakQPS))))
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatResponseCodeDistribution(sb *strings.Builder, m *metrics.Metrics) {
	if m.ResponseCode.Total == 0 {
		return
	}

	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("Response Codes:\n") }))
	
	responseCodes := map[string]int64{
		"NoError":  m.ResponseCode.NoError,
		"NXDomain": m.ResponseCode.NXDomain,
		"ServFail": m.ResponseCode.ServFail,
		"FormErr":  m.ResponseCode.FormErr,
		"NotImpl":  m.ResponseCode.NotImpl,
		"Refused":  m.ResponseCode.Refused,
		"Other":    m.ResponseCode.Other,
	}
	
	for code, count := range responseCodes {
		if count == 0 {
			continue
		}
		percentage := f.safeDivide(float64(count), float64(m.ResponseCode.Total)) * 100
		codeStr := code
		
		if f.ShowColors {
			if code == "NoError" {
				codeStr = f.terminal.Green(codeStr)
			} else if code == "ServFail" {
				codeStr = f.terminal.Red(codeStr)
			} else if code == "NXDomain" {
				codeStr = f.terminal.Yellow(codeStr)
			}
		}
		
		sb.WriteString(fmt.Sprintf("  %s: %s (%.1f%%)\n", codeStr, f.formatNumber(int64(count)), percentage))
	}
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatQueryTypeDistribution(sb *strings.Builder, m *metrics.Metrics) {
	if m.QueryType.Total == 0 {
		return
	}

	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("Query Types:\n") }))
	
	queryTypes := map[string]int64{
		"A":     m.QueryType.A,
		"AAAA":  m.QueryType.AAAA,
		"CNAME": m.QueryType.CNAME,
		"MX":    m.QueryType.MX,
		"TXT":   m.QueryType.TXT,
		"NS":    m.QueryType.NS,
		"SOA":   m.QueryType.SOA,
		"PTR":   m.QueryType.PTR,
		"Other": m.QueryType.Other,
	}
	
	for qtype, count := range queryTypes {
		if count == 0 {
			continue
		}
		percentage := f.safeDivide(float64(count), float64(m.QueryType.Total)) * 100
		sb.WriteString(fmt.Sprintf("  %s: %s (%.1f%%)\n", qtype, f.formatNumber(int64(count)), percentage))
	}
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatCacheMetrics(sb *strings.Builder, m *metrics.Metrics) {
	if m.Cache == nil {
		return
	}

	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("Cache Performance:\n") }))
	
	// Cache hit rate with color coding
	hitRate := f.safePercentage(m.Cache.HitRate * 100)
	hitRateStr := fmt.Sprintf("%.1f%%", hitRate)
	if f.ShowColors {
		if hitRate >= 80 {
			hitRateStr = f.terminal.Green(hitRateStr)
		} else if hitRate >= 50 {
			hitRateStr = f.terminal.Yellow(hitRateStr)
		} else {
			hitRateStr = f.terminal.Red(hitRateStr)
		}
	}
	sb.WriteString(fmt.Sprintf("  Hit Rate   : %s\n", hitRateStr))
	
	// Cache efficiency with color coding
	efficiency := f.safePercentage(m.Cache.Efficiency * 100)
	efficiencyStr := fmt.Sprintf("%.1f%%", efficiency)
	if f.ShowColors {
		if efficiency >= 70 {
			efficiencyStr = f.terminal.Green(efficiencyStr)
		} else if efficiency >= 40 {
			efficiencyStr = f.terminal.Yellow(efficiencyStr)
		} else {
			efficiencyStr = f.terminal.Red(efficiencyStr)
		}
	}
	sb.WriteString(fmt.Sprintf("  Efficiency : %s\n", efficiencyStr))
	
	// TTL stats
	if f.DetailedView {
		sb.WriteString(fmt.Sprintf("  Avg TTL    : %s\n", f.formatDuration(time.Duration(m.Cache.AvgTTL * float64(time.Second)))))
		sb.WriteString(fmt.Sprintf("  Min TTL    : %s\n", f.formatDuration(time.Duration(m.Cache.MinTTL * float64(time.Second)))))
		sb.WriteString(fmt.Sprintf("  Max TTL    : %s\n", f.formatDuration(time.Duration(m.Cache.MaxTTL * float64(time.Second)))))
		sb.WriteString(fmt.Sprintf("  Hits/Misses: %s/%s\n", 
			f.formatNumber(m.Cache.Hits), 
			f.formatNumber(m.Cache.Misses)))
	}
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatNetworkMetrics(sb *strings.Builder, m *metrics.Metrics) {
	if m.Network == nil {
		return
	}

	sb.WriteString(f.safeTerminalOp(func() string { return f.terminal.Bold("Network Performance:\n") }))
	
	// Packet loss with color coding
	packetLoss := f.safePercentage(m.Network.PacketLoss * 100)
	packetLossStr := fmt.Sprintf("%.2f%%", packetLoss)
	if f.ShowColors {
		if packetLoss < 1 {
			packetLossStr = f.terminal.Green(packetLossStr)
		} else if packetLoss < 5 {
			packetLossStr = f.terminal.Yellow(packetLossStr)
		} else {
			packetLossStr = f.terminal.Red(packetLossStr)
		}
	}
	sb.WriteString(fmt.Sprintf("  Packet Loss: %s\n", packetLossStr))
	
	// Network quality score with color coding
	qualityScore := m.Network.QualityScore
	qualityStr := fmt.Sprintf("%.0f/100", qualityScore)
	if f.ShowColors {
		if qualityScore >= 90 {
			qualityStr = f.terminal.Green(qualityStr)
		} else if qualityScore >= 70 {
			qualityStr = f.terminal.Yellow(qualityStr)
		} else {
			qualityStr = f.terminal.Red(qualityStr)
		}
	}
	sb.WriteString(fmt.Sprintf("  Quality    : %s\n", qualityStr))
	
	// Jitter with color coding
	jitter := m.Network.AvgJitter
	jitterStr := f.formatDuration(time.Duration(jitter * float64(time.Millisecond)))
	if f.ShowColors {
		if jitter < 20 {
			jitterStr = f.terminal.Green(jitterStr)
		} else if jitter < 50 {
			jitterStr = f.terminal.Yellow(jitterStr)
		} else {
			jitterStr = f.terminal.Red(jitterStr)
		}
	}
	sb.WriteString(fmt.Sprintf("  Jitter     : %s\n", jitterStr))
	
	if f.DetailedView {
		// Network latency percentiles
		sb.WriteString(fmt.Sprintf("  Net Lat P50: %s\n", f.formatDuration(time.Duration(m.Network.LatencyP50 * float64(time.Millisecond)))))
		sb.WriteString(fmt.Sprintf("  Net Lat P95: %s\n", f.formatDuration(time.Duration(m.Network.LatencyP95 * float64(time.Millisecond)))))
		sb.WriteString(fmt.Sprintf("  Net Lat P99: %s\n", f.formatDuration(time.Duration(m.Network.LatencyP99 * float64(time.Millisecond)))))
		
		// Hop count
		if m.Network.AvgHopCount > 0 {
			sb.WriteString(fmt.Sprintf("  Avg Hops   : %.1f\n", m.Network.AvgHopCount))
		}
		
		// Packet statistics
		sb.WriteString(fmt.Sprintf("  Packets    : Sent %s, Recv %s\n", 
			f.formatNumber(m.Network.PacketsSent),
			f.formatNumber(m.Network.PacketsReceived)))
	}
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func (f *CLIFormatter) formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

func (f *CLIFormatter) formatQPS(qps float64) string {
	if qps < 1 {
		return fmt.Sprintf("%.2f", qps)
	}
	if qps < 1000 {
		return fmt.Sprintf("%.1f", qps)
	}
	return f.formatNumber(int64(qps))
}

func (f *CLIFormatter) getResponseCodeString(code int) string {
	switch code {
	case 0:
		return "NOERROR"
	case 1:
		return "FORMERR"
	case 2:
		return "SERVFAIL"
	case 3:
		return "NXDOMAIN"
	case 4:
		return "NOTIMP"
	case 5:
		return "REFUSED"
	default:
		return fmt.Sprintf("CODE_%d", code)
	}
}

func (f *CLIFormatter) ClearScreen() string {
	return f.safeTerminalOp(f.terminal.Clear)
}

func (f *CLIFormatter) MoveCursorToTop() string {
	return f.safeTerminalOp(func() string { return f.terminal.MoveCursor(0, 0) })
}

func (f *CLIFormatter) Terminal() *Terminal {
	return f.terminal
}

// Helper functions for safe operations

func (f *CLIFormatter) safeDivide(numerator, denominator float64) float64 {
	if denominator == 0 || math.IsNaN(denominator) || math.IsInf(denominator, 0) {
		return 0
	}
	result := numerator / denominator
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0
	}
	return result
}

func (f *CLIFormatter) safePercentage(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func (f *CLIFormatter) safeLatencyDuration(milliseconds float64) time.Duration {
	if math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || milliseconds < 0 {
		return 0
	}
	// Cap at 1 hour to prevent overflow
	if milliseconds > 3600000 {
		milliseconds = 3600000
	}
	return time.Duration(milliseconds * float64(time.Millisecond))
}

func (f *CLIFormatter) safeQPS(qps float64) float64 {
	if math.IsNaN(qps) || math.IsInf(qps, 0) || qps < 0 {
		return 0
	}
	return qps
}

func (f *CLIFormatter) safeTerminalOp(op func() string) string {
	defer func() {
		if r := recover(); r != nil {
			// Terminal operation failed, return empty string
		}
	}()
	
	if f.terminal == nil {
		return ""
	}
	
	return op()
}