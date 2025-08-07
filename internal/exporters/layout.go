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
	duration := formatDuration(m.Duration)

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
		m.TotalQueries,
		m.TotalErrors,
		m.SuccessRate*100)

	sb.WriteString(centerText(footerText, l.width))
	sb.WriteString("\n")
	sb.WriteString(l.terminal.Dim("Press Ctrl+C to exit"))
}

func (l *Layout) renderDetailedHeader(sb *strings.Builder, m *metrics.Metrics) {
	sb.WriteString(l.terminal.Bold("\n=== DNS Monitoring Detailed Report ===\n"))
	sb.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", formatDuration(m.Duration)))
	sb.WriteString(strings.Repeat("-", 60) + "\n\n")
}

func (l *Layout) createDashboardSections(m *metrics.Metrics, prev *metrics.Metrics) []*Section {
	sections := []*Section{
		l.createLatencySection(m, prev),
		l.createThroughputSection(m, prev),
		l.createSuccessSection(m, prev),
	}

	if len(m.ResponseCodeDist) > 0 {
		sections = append(sections, l.createResponseCodeSection(m))
	}

	if len(m.ServerMetrics) > 0 {
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
		NewSection("P95", fmt.Sprintf("%s", formatDuration(m.LatencyP95)), l.terminal),
		NewSection("QPS", fmt.Sprintf("%.1f", m.CurrentQPS), l.terminal),
		NewSection("Success", fmt.Sprintf("%.1f%%", m.SuccessRate*100), l.terminal),
		NewSection("Errors", fmt.Sprintf("%d", m.TotalErrors), l.terminal),
	}
}

func (l *Layout) createLatencySection(m *metrics.Metrics, prev *metrics.Metrics) *Section {
	s := NewSection("Latency", "", l.terminal)

	latencyData := []struct {
		label string
		value time.Duration
		prev  time.Duration
	}{
		{"P50", m.LatencyP50, 0},
		{"P95", m.LatencyP95, 0},
		{"P99", m.LatencyP99, 0},
		{"P999", m.LatencyP999, 0},
	}

	if prev != nil {
		latencyData[0].prev = prev.LatencyP50
		latencyData[1].prev = prev.LatencyP95
		latencyData[2].prev = prev.LatencyP99
		latencyData[3].prev = prev.LatencyP999
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
		currentTrend = l.getTrend(m.CurrentQPS, prev.CurrentQPS, false)
	}

	s.AddLine(fmt.Sprintf("Current: %.1f q/s %s", m.CurrentQPS, currentTrend))
	s.AddLine(fmt.Sprintf("Average: %.1f q/s", m.AvgQPS))
	s.AddLine(fmt.Sprintf("Peak   : %.1f q/s", m.PeakQPS))

	return s
}

func (l *Layout) createSuccessSection(m *metrics.Metrics, prev *metrics.Metrics) *Section {
	s := NewSection("Success Rate", "", l.terminal)

	successTrend := ""
	if l.showTrend && prev != nil {
		successTrend = l.getTrend(m.SuccessRate, prev.SuccessRate, false)
	}

	successColor := l.getSuccessColor(m.SuccessRate)
	s.AddLine(fmt.Sprintf("Success: %s %s", successColor(fmt.Sprintf("%.2f%%", m.SuccessRate*100)), successTrend))
	s.AddLine(fmt.Sprintf("Errors : %s", l.terminal.Red(fmt.Sprintf("%.2f%%", m.ErrorRate*100))))
	s.AddLine(fmt.Sprintf("Total  : %d queries", m.TotalQueries))

	return s
}

func (l *Layout) createResponseCodeSection(m *metrics.Metrics) *Section {
	s := NewSection("Response Codes", "", l.terminal)

	for code, count := range m.ResponseCodeDist {
		percentage := float64(count) / float64(m.TotalQueries) * 100
		codeStr := getResponseCodeString(code)
		color := l.getResponseCodeColor(code)
		s.AddLine(fmt.Sprintf("%-10s: %6d (%.1f%%)", color(codeStr), count, percentage))
	}

	return s
}

func (l *Layout) createQueryTypeSection(m *metrics.Metrics) *Section {
	s := NewSection("Query Types", "", l.terminal)

	for qtype, count := range m.QueryTypeDist {
		percentage := float64(count) / float64(m.TotalQueries) * 100
		s.AddLine(fmt.Sprintf("%-10s: %6d (%.1f%%)", qtype, count, percentage))
	}

	return s
}

func (l *Layout) createServerSection(m *metrics.Metrics) *Section {
	s := NewSection("Server Status", "", l.terminal)

	for server, sm := range m.ServerMetrics {
		status := "UP"
		statusColor := l.terminal.Green
		if sm.ErrorRate > 0.1 {
			status = "DEGRADED"
			statusColor = l.terminal.Yellow
		}
		if sm.ErrorRate > 0.5 {
			status = "DOWN"
			statusColor = l.terminal.Red
		}

		s.AddLine(fmt.Sprintf("%-20s: %s (%.1f%% errors)",
			TruncateString(server, 20),
			statusColor(status),
			sm.ErrorRate*100))
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

func (l *Layout) getSystemStatus(m *metrics.Metrics) string {
	if m.SuccessRate < 0.9 || m.LatencyP95 > 500*time.Millisecond {
		return "CRITICAL"
	}
	if m.SuccessRate < 0.95 || m.LatencyP95 > 200*time.Millisecond {
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