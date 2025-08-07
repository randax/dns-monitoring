package exporters

import (
	"fmt"
	"strings"
	"time"

	"github.com/randax/dns-monitoring/internal/metrics"
)

type LayoutMode int

const (
	LayoutCompact LayoutMode = iota
	LayoutDetailed
	LayoutDashboard
)

type Layout struct {
	mode      LayoutMode
	terminal  *Terminal
	width     int
	height    int
	showTrend bool
}

func NewLayout(mode LayoutMode, terminal *Terminal) *Layout {
	width, height := GetTerminalSize()
	return &Layout{
		mode:      mode,
		terminal:  terminal,
		width:     width,
		height:    height,
		showTrend: true,
	}
}

func (l *Layout) Render(m *metrics.Metrics, prevMetrics *metrics.Metrics) string {
	switch l.mode {
	case LayoutDashboard:
		return l.renderDashboard(m, prevMetrics)
	case LayoutDetailed:
		return l.renderDetailed(m)
	default:
		return l.renderCompact(m)
	}
}

func (l *Layout) renderDashboard(m *metrics.Metrics, prev *metrics.Metrics) string {
	var sb strings.Builder

	sb.WriteString(l.terminal.Clear())
	sb.WriteString(l.terminal.MoveCursor(0, 0))

	l.renderDashboardHeader(&sb, m)

	sections := l.createDashboardSections(m, prev)
	l.renderSections(&sb, sections)

	l.renderDashboardFooter(&sb, m)

	return sb.String()
}

func (l *Layout) renderDetailed(m *metrics.Metrics) string {
	var sb strings.Builder

	l.renderDetailedHeader(&sb, m)

	sections := l.createDetailedSections(m)
	for _, section := range sections {
		sb.WriteString(section.Render())
		sb.WriteString("\n")
	}

	return sb.String()
}

func (l *Layout) renderCompact(m *metrics.Metrics) string {
	var sb strings.Builder

	sections := l.createCompactSections(m)
	for i, section := range sections {
		sb.WriteString(section.Render())
		if i < len(sections)-1 {
			sb.WriteString(" | ")
		}
	}
	sb.WriteString("\n")

	return sb.String()
}

func (l *Layout) renderDashboardHeader(sb *strings.Builder, m *metrics.Metrics) {
	title := "DNS Monitoring Dashboard"
	timestamp := time.Now().Format("15:04:05")
	duration := formatDuration(m.Period.End.Sub(m.Period.Start))

	headerLine := fmt.Sprintf("%s - %s (Duration: %s)",
		l.terminal.Bold(title),
		timestamp,
		duration)

	sb.WriteString(centerText(headerLine, l.width))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("═", l.width))
	sb.WriteString("\n\n")
}

func (l *Layout) renderDashboardFooter(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("═", l.width))
	sb.WriteString("\n")

	status := l.getSystemStatus(m)
	statusColor := l.terminal.Green
	if status == "WARNING" {
		statusColor = l.terminal.Yellow
	} else if status == "CRITICAL" {
		statusColor = l.terminal.Red
	}

	footerText := fmt.Sprintf("Status: %s | Queries: %d | Errors: %d | Success: %.2f%%",
		statusColor(status),
		m.Rates.TotalQueries,
		m.Rates.ErrorQueries,
		m.Rates.SuccessRate)

	sb.WriteString(centerText(footerText, l.width))
	sb.WriteString("\n")
	sb.WriteString(l.terminal.Dim("Press Ctrl+C to exit"))
}

func (l *Layout) renderDetailedHeader(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString(l.terminal.Bold("\n=== DNS Monitoring Detailed Report ===\n"))
	sb.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", formatDuration(m.Period.End.Sub(m.Period.Start))))
	sb.WriteString(strings.Repeat("-", 60) + "\n\n")
}

func (l *Layout) createDashboardSections(m *metrics.Metrics, prev *metrics.Metrics) []*Section {
	sections := []*Section{
		l.createLatencySection(m, prev),
		l.createThroughputSection(m, prev),
		l.createSuccessSection(m, prev),
	}

	if m.ResponseCode.Total > 0 {
		sections = append(sections, l.createResponseCodeSection(m))
	}

	if m.Throughput.ByServer != nil && len(m.Throughput.ByServer) > 0 {
		sections = append(sections, l.createServerSection(m))
	}

	return sections
}

func (l *Layout) createDetailedSections(m *metrics.Metrics) []*Section {
	return []*Section{
		l.createLatencySection(m, nil),
		l.createThroughputSection(m, nil),
		l.createSuccessSection(m, nil),
		l.createResponseCodeSection(m),
		l.createQueryTypeSection(m),
		l.createServerSection(m),
	}
}

func (l *Layout) createCompactSections(m *metrics.Metrics) []*Section {
	return []*Section{
		NewSection("P95", fmt.Sprintf("%s", formatDuration(time.Duration(m.Latency.P95 * float64(time.Millisecond)))), l.terminal),
		NewSection("QPS", fmt.Sprintf("%.1f", m.Throughput.CurrentQPS), l.terminal),
		NewSection("Success", fmt.Sprintf("%.1f%%", m.Rates.SuccessRate), l.terminal),
		NewSection("Errors", fmt.Sprintf("%d", m.Rates.ErrorQueries), l.terminal),
	}
}

func (l *Layout) createLatencySection(m *metrics.Metrics, prev *metrics.Metrics) *Section {
	s := NewSection("Latency", "", l.terminal)

	latencyData := []struct {
		label string
		value time.Duration
		prev  time.Duration
	}{
		{"P50", time.Duration(m.Latency.P50 * float64(time.Millisecond)), 0},
		{"P95", time.Duration(m.Latency.P95 * float64(time.Millisecond)), 0},
		{"P99", time.Duration(m.Latency.P99 * float64(time.Millisecond)), 0},
		{"P999", time.Duration(m.Latency.P999 * float64(time.Millisecond)), 0},
	}

	if prev != nil {
		latencyData[0].prev = time.Duration(prev.Latency.P50 * float64(time.Millisecond))
		latencyData[1].prev = time.Duration(prev.Latency.P95 * float64(time.Millisecond))
		latencyData[2].prev = time.Duration(prev.Latency.P99 * float64(time.Millisecond))
		latencyData[3].prev = time.Duration(prev.Latency.P999 * float64(time.Millisecond))
	}

	for _, ld := range latencyData {
		trend := ""
		if l.showTrend && prev != nil {
			trend = l.getTrend(float64(ld.value), float64(ld.prev), true)
		}

		color := l.getLatencyColor(ld.value)
		s.AddLine(fmt.Sprintf("%-6s: %s %s", ld.label, color(formatDuration(ld.value)), trend))
	}

	return s
}

func (l *Layout) createThroughputSection(m *metrics.Metrics, prev *metrics.Metrics) *Section {
	s := NewSection("Throughput", "", l.terminal)

	currentTrend := ""
	if l.showTrend && prev != nil {
		currentTrend = l.getTrend(m.Throughput.CurrentQPS, prev.Throughput.CurrentQPS, false)
	}

	s.AddLine(fmt.Sprintf("Current: %.1f q/s %s", m.Throughput.CurrentQPS, currentTrend))
	s.AddLine(fmt.Sprintf("Average: %.1f q/s", m.Throughput.AverageQPS))
	s.AddLine(fmt.Sprintf("Peak   : %.1f q/s", m.Throughput.PeakQPS))

	return s
}

func (l *Layout) createSuccessSection(m *metrics.Metrics, prev *metrics.Metrics) *Section {
	s := NewSection("Success Rate", "", l.terminal)

	successTrend := ""
	if l.showTrend && prev != nil {
		successTrend = l.getTrend(m.Rates.SuccessRate, prev.Rates.SuccessRate, false)
	}

	successColor := l.getSuccessColor(m.Rates.SuccessRate)
	s.AddLine(fmt.Sprintf("Success: %s %s", successColor(fmt.Sprintf("%.2f%%", m.Rates.SuccessRate)), successTrend))
	s.AddLine(fmt.Sprintf("Errors : %s", l.terminal.Red(fmt.Sprintf("%.2f%%", m.Rates.ErrorRate))))
	s.AddLine(fmt.Sprintf("Total  : %d queries", m.Rates.TotalQueries))

	return s
}

func (l *Layout) createResponseCodeSection(m *metrics.Metrics) *Section {
	s := NewSection("Response Codes", "", l.terminal)

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
		color := l.getResponseCodeColorByName(code)
		s.AddLine(fmt.Sprintf("%-10s: %6d (%.1f%%)", color(code), count, percentage))
	}

	return s
}

func (l *Layout) createQueryTypeSection(m *metrics.Metrics) *Section {
	s := NewSection("Query Types", "", l.terminal)

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
		s.AddLine(fmt.Sprintf("%-10s: %6d (%.1f%%)", qtype, count, percentage))
	}

	return s
}

func (l *Layout) createServerSection(m *metrics.Metrics) *Section {
	s := NewSection("Server Status", "", l.terminal)

	// For now, we'll use the ByServer throughput data for basic server status
	if m.Throughput.ByServer != nil {
		for server, qps := range m.Throughput.ByServer {
			status := "UP"
			statusColor := l.terminal.Green
			if qps == 0 {
				status = "DOWN"
				statusColor = l.terminal.Red
			} else if qps < 10 {
				status = "DEGRADED"
				statusColor = l.terminal.Yellow
			}

			s.AddLine(fmt.Sprintf("%-20s: %s (%.1f q/s)",
				TruncateString(server, 20),
				statusColor(status),
				qps))
		}
	}

	return s
}

func (l *Layout) renderSections(sb *strings.Builder, sections []*Section) {
	cols := 2
	if l.width >= 120 {
		cols = 3
	}
	if l.width < 80 {
		cols = 1
	}

	colWidth := l.width / cols

	for i := 0; i < len(sections); i += cols {
		maxLines := 0
		for j := 0; j < cols && i+j < len(sections); j++ {
			lines := sections[i+j].LineCount()
			if lines > maxLines {
				maxLines = lines
			}
		}

		for line := 0; line < maxLines; line++ {
			for j := 0; j < cols && i+j < len(sections); j++ {
				content := sections[i+j].GetLine(line)
				if j > 0 {
					sb.WriteString(" │ ")
				}
				sb.WriteString(PadString(content, colWidth-3, "left"))
			}
			sb.WriteString("\n")
		}

		if i+cols < len(sections) {
			sb.WriteString("\n")
		}
	}
}

func (l *Layout) getTrend(current, previous float64, inverse bool) string {
	if previous == 0 {
		return ""
	}

	change := ((current - previous) / previous) * 100
	if inverse {
		change = -change
	}

	if change > 5 {
		return l.terminal.Red("↑")
	} else if change < -5 {
		return l.terminal.Green("↓")
	}
	return l.terminal.Yellow("→")
}

func (l *Layout) getLatencyColor(d time.Duration) func(string) string {
	if d > 500*time.Millisecond {
		return l.terminal.Red
	}
	if d > 100*time.Millisecond {
		return l.terminal.Yellow
	}
	return l.terminal.Green
}

func (l *Layout) getSuccessColor(rate float64) func(string) string {
	if rate < 0.9 {
		return l.terminal.Red
	}
	if rate < 0.95 {
		return l.terminal.Yellow
	}
	return l.terminal.Green
}

func (l *Layout) getResponseCodeColor(code int) func(string) string {
	switch code {
	case 0:
		return l.terminal.Green
	case 2:
		return l.terminal.Yellow
	case 3:
		return l.terminal.Red
	default:
		return l.terminal.Cyan
	}
}

func (l *Layout) getResponseCodeColorByName(code string) func(string) string {
	switch code {
	case "NoError":
		return l.terminal.Green
	case "NXDomain":
		return l.terminal.Yellow
	case "ServFail":
		return l.terminal.Red
	default:
		return l.terminal.Cyan
	}
}

func (l *Layout) getSystemStatus(m *metrics.Metrics) string {
	latencyP95 := time.Duration(m.Latency.P95 * float64(time.Millisecond))
	if m.Rates.SuccessRate < 90 || latencyP95 > 500*time.Millisecond {
		return "CRITICAL"
	}
	if m.Rates.SuccessRate < 95 || latencyP95 > 200*time.Millisecond {
		return "WARNING"
	}
	return "HEALTHY"
}

type Section struct {
	title    string
	lines    []string
	terminal *Terminal
}

func NewSection(title, content string, terminal *Terminal) *Section {
	s := &Section{
		title:    title,
		lines:    []string{},
		terminal: terminal,
	}
	if content != "" {
		s.lines = append(s.lines, content)
	}
	return s
}

func (s *Section) AddLine(line string) {
	s.lines = append(s.lines, line)
}

func (s *Section) Render() string {
	var sb strings.Builder
	sb.WriteString(s.terminal.Bold(s.title))
	sb.WriteString("\n")
	for _, line := range s.lines {
		sb.WriteString("  ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (s *Section) LineCount() int {
	return len(s.lines) + 1
}

func (s *Section) GetLine(index int) string {
	if index == 0 {
		return s.terminal.Bold(s.title)
	}
	if index-1 < len(s.lines) {
		return "  " + s.lines[index-1]
	}
	return ""
}

func formatDuration(d time.Duration) string {
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

func getResponseCodeString(code int) string {
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

func centerText(text string, width int) string {
	textLen := len(text)
	if textLen >= width {
		return text
	}
	padding := (width - textLen) / 2
	return strings.Repeat(" ", padding) + text
}