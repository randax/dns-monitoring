package exporters

import (
	"fmt"
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
	sb.WriteString(f.terminal.Clear())
	sb.WriteString(f.terminal.Bold("DNS Monitoring Dashboard\n"))
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
		m.Rates.SuccessRate))
}

func (f *CLIFormatter) formatLatencyMetrics(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString(f.terminal.Bold("Latency Metrics:\n"))

	latencyData := []struct {
		label string
		value time.Duration
		warn  time.Duration
		crit  time.Duration
	}{
		{"  P50 ", time.Duration(m.Latency.P50 * float64(time.Millisecond)), 50 * time.Millisecond, 100 * time.Millisecond},
		{"  P95 ", time.Duration(m.Latency.P95 * float64(time.Millisecond)), 100 * time.Millisecond, 200 * time.Millisecond},
		{"  P99 ", time.Duration(m.Latency.P99 * float64(time.Millisecond)), 200 * time.Millisecond, 500 * time.Millisecond},
		{"  P999", time.Duration(m.Latency.P999 * float64(time.Millisecond)), 500 * time.Millisecond, 1000 * time.Millisecond},
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
		sb.WriteString(fmt.Sprintf("  Min : %s\n", f.formatDuration(time.Duration(m.Latency.Min * float64(time.Millisecond)))))
		sb.WriteString(fmt.Sprintf("  Max : %s\n", f.formatDuration(time.Duration(m.Latency.Max * float64(time.Millisecond)))))
		sb.WriteString(fmt.Sprintf("  Avg : %s\n", f.formatDuration(time.Duration(m.Latency.Mean * float64(time.Millisecond)))))
	}
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatSuccessMetrics(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString(f.terminal.Bold("Success Metrics:\n"))

	successRate := m.Rates.SuccessRate
	errorRate := m.Rates.ErrorRate

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
	sb.WriteString(f.terminal.Bold("Throughput:\n"))
	sb.WriteString(fmt.Sprintf("  Current QPS: %s\n", f.formatQPS(m.Throughput.CurrentQPS)))
	sb.WriteString(fmt.Sprintf("  Average QPS: %s\n", f.formatQPS(m.Throughput.AverageQPS)))
	sb.WriteString(fmt.Sprintf("  Peak QPS   : %s\n", f.formatQPS(m.Throughput.PeakQPS)))
	sb.WriteString("\n")
}

func (f *CLIFormatter) formatResponseCodeDistribution(sb *strings.Builder, m *metrics.Metrics) {
	if m.ResponseCode.Total == 0 {
		return
	}

	sb.WriteString(f.terminal.Bold("Response Codes:\n"))
	
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
		percentage := float64(count) / float64(m.ResponseCode.Total) * 100
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

	sb.WriteString(f.terminal.Bold("Query Types:\n"))
	
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
		percentage := float64(count) / float64(m.QueryType.Total) * 100
		sb.WriteString(fmt.Sprintf("  %s: %s (%.1f%%)\n", qtype, f.formatNumber(int64(count)), percentage))
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
	return f.terminal.Clear()
}

func (f *CLIFormatter) MoveCursorToTop() string {
	return f.terminal.MoveCursor(0, 0)
}

func (f *CLIFormatter) Terminal() *Terminal {
	return f.terminal
}