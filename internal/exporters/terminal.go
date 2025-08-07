package exporters

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

type Terminal struct {
	useColors bool
}

func NewTerminal(useColors bool) *Terminal {
	return &Terminal{
		useColors: useColors && isTerminal() && supportsColors(),
	}
}

func (t *Terminal) Clear() string {
	if runtime.GOOS == "windows" {
		return "\033[2J\033[H"
	}
	return "\033[2J\033[H"
}

func (t *Terminal) MoveCursor(row, col int) string {
	return fmt.Sprintf("\033[%d;%dH", row+1, col+1)
}

func (t *Terminal) Bold(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[1m%s\033[0m", text)
}

func (t *Terminal) Red(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[31m%s\033[0m", text)
}

func (t *Terminal) Green(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[32m%s\033[0m", text)
}

func (t *Terminal) Yellow(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[33m%s\033[0m", text)
}

func (t *Terminal) Blue(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[34m%s\033[0m", text)
}

func (t *Terminal) Cyan(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[36m%s\033[0m", text)
}

func (t *Terminal) Magenta(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[35m%s\033[0m", text)
}

func (t *Terminal) Dim(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[2m%s\033[0m", text)
}

func (t *Terminal) Underline(text string) string {
	if !t.useColors {
		return text
	}
	return fmt.Sprintf("\033[4m%s\033[0m", text)
}

func GetTerminalSize() (width, height int) {
	if !isTerminal() {
		return 80, 24
	}

	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80, 24
	}
	return width, height
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func supportsColors() bool {
	if runtime.GOOS == "windows" {
		return os.Getenv("ANSICON") != "" || 
			   os.Getenv("ConEmuANSI") == "ON" ||
			   os.Getenv("WT_SESSION") != ""
	}

	term := os.Getenv("TERM")
	colorTerm := os.Getenv("COLORTERM")

	if colorTerm == "truecolor" || colorTerm == "24bit" {
		return true
	}

	if term == "" {
		return false
	}

	colorTerms := []string{
		"xterm", "xterm-256", "xterm-256color",
		"screen", "screen-256", "screen-256color",
		"tmux", "tmux-256", "tmux-256color",
		"rxvt-unicode", "rxvt-unicode-256color",
		"linux", "cygwin",
	}

	for _, ct := range colorTerms {
		if strings.Contains(term, ct) {
			return true
		}
	}

	return false
}

func FormatTable(headers []string, rows [][]string) string {
	if len(headers) == 0 || len(rows) == 0 {
		return ""
	}

	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = len(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	var sb strings.Builder

	for i, h := range headers {
		sb.WriteString(fmt.Sprintf("%-*s", colWidths[i]+2, h))
	}
	sb.WriteString("\n")

	totalWidth := 0
	for _, w := range colWidths {
		totalWidth += w + 2
	}
	sb.WriteString(strings.Repeat("-", totalWidth) + "\n")

	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) {
				sb.WriteString(fmt.Sprintf("%-*s", colWidths[i]+2, cell))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func FormatProgressBar(current, total int64, width int) string {
	if total <= 0 {
		return ""
	}

	if width <= 0 {
		width = 50
	}

	percentage := float64(current) / float64(total)
	filled := int(percentage * float64(width))
	
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("[%s] %.1f%%", bar, percentage*100)
}

func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func PadString(s string, width int, align string) string {
	if len(s) >= width {
		return TruncateString(s, width)
	}

	padding := width - len(s)
	
	switch align {
	case "right":
		return strings.Repeat(" ", padding) + s
	case "center":
		leftPad := padding / 2
		rightPad := padding - leftPad
		return strings.Repeat(" ", leftPad) + s + strings.Repeat(" ", rightPad)
	default:
		return s + strings.Repeat(" ", padding)
	}
}

func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatPercentage(value float64, precision int) string {
	format := fmt.Sprintf("%%.%df%%%%", precision)
	return fmt.Sprintf(format, value*100)
}

func RepeatString(s string, count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(s, count)
}

func DrawBox(content string, width int) string {
	lines := strings.Split(content, "\n")
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}

	if width > 0 && maxLen < width {
		maxLen = width
	}

	var sb strings.Builder
	
	sb.WriteString("┌" + strings.Repeat("─", maxLen+2) + "┐\n")
	
	for _, line := range lines {
		sb.WriteString("│ " + PadString(line, maxLen, "left") + " │\n")
	}
	
	sb.WriteString("└" + strings.Repeat("─", maxLen+2) + "┘\n")
	
	return sb.String()
}