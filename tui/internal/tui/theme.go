package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The palette follows the default OpenCode dark theme: almost black surfaces,
// a warm peach accent and muted greys.
var (
	cBg        = lipgloss.Color("#0a0a0a")
	cPanel     = lipgloss.Color("#141414")
	cPanelHigh = lipgloss.Color("#1e1e1e")
	cBorder    = lipgloss.Color("#2e2e2e")
	cText      = lipgloss.Color("#eeeeee")
	cMuted     = lipgloss.Color("#808080")
	cFaint     = lipgloss.Color("#4c4c4c")
	cAccent    = lipgloss.Color("#fab283")
	cAccent2   = lipgloss.Color("#ffc09f")
	cBlue      = lipgloss.Color("#7aa2f7")
	cGreen     = lipgloss.Color("#7fd88f")
	cYellow    = lipgloss.Color("#e5c07b")
	cRed       = lipgloss.Color("#e06c75")
	cPurple    = lipgloss.Color("#9d7cd8")
)

var (
	styBase       = lipgloss.NewStyle().Foreground(cText)
	styMuted      = lipgloss.NewStyle().Foreground(cMuted)
	styFaint      = lipgloss.NewStyle().Foreground(cFaint)
	styText       = lipgloss.NewStyle().Foreground(cText)
	styAccent     = lipgloss.NewStyle().Foreground(cAccent)
	styAccentBold = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styBlue       = lipgloss.NewStyle().Foreground(cBlue)
	styGreen      = lipgloss.NewStyle().Foreground(cGreen)
	styYellow     = lipgloss.NewStyle().Foreground(cYellow)
	styRed        = lipgloss.NewStyle().Foreground(cRed)
	styPurple     = lipgloss.NewStyle().Foreground(cPurple)
	styBold       = lipgloss.NewStyle().Foreground(cText).Bold(true)
	styPanel      = lipgloss.NewStyle().Background(cPanel)
	stySelected   = lipgloss.NewStyle().Background(cPanelHigh).Foreground(cText)
)

func keycap(key string) string {
	return lipgloss.NewStyle().Foreground(cAccent).Render(key)
}

func hint(key, label string) string {
	return keycap(key) + " " + styMuted.Render(label)
}

func pill(text string, fg lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(fg).
		Background(cPanelHigh).
		Padding(0, 1).
		Render(text)
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return ansi.Truncate(s, width-1, "…")
}

func truncateMiddle(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width < 8 {
		return truncate(s, width)
	}
	left := (width - 1) / 2
	right := width - 1 - left
	runes := []rune(s)
	if len(runes) < width {
		return truncate(s, width)
	}
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return truncate(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}

func humanBytes(n int64) string {
	if n < 0 {
		return "—"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(n)
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func humanSpeed(n int64) string {
	if n <= 0 {
		return ""
	}
	return humanBytes(n) + "/s"
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		return "—"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func interpolateHex(a, b lipgloss.Color, t float64) lipgloss.Color {
	ar, ag, ab := hexRGB(string(a))
	br, bg, bb := hexRGB(string(b))
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	r := int(math.Round(float64(ar) + (float64(br-ar) * t)))
	g := int(math.Round(float64(ag) + (float64(bg-ag) * t)))
	bl := int(math.Round(float64(ab) + (float64(bb-ab) * t)))
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, bl))
}

func hexRGB(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 255, 255, 255
	}
	var r, g, b int
	fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

// gradientBar renders a progress bar with a blue-to-peach gradient.
func gradientBar(percent float64, width int, indeterminate bool, frame int) string {
	if width <= 0 {
		return ""
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(math.Round(percent / 100 * float64(width)))
	if indeterminate {
		filled = width / 4
		if filled < 2 {
			filled = 2
		}
		offset := 0
		if width > filled {
			offset = frame % (width - filled + 1)
		}
		var b strings.Builder
		for i := 0; i < width; i++ {
			if i >= offset && i < offset+filled {
				t := float64(i) / float64(max(width-1, 1))
				b.WriteString(lipgloss.NewStyle().Foreground(interpolateHex(cBlue, cAccent, t)).Render("█"))
			} else {
				b.WriteString(styFaint.Render("░"))
			}
		}
		return b.String()
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			t := float64(i) / float64(max(width-1, 1))
			b.WriteString(lipgloss.NewStyle().Foreground(interpolateHex(cBlue, cAccent, t)).Render("█"))
		} else {
			b.WriteString(styFaint.Render("░"))
		}
	}
	return b.String()
}

var sparkChars = []rune("▁▂▃▄▅▆▇█")

func sparkline(values []float64, width int) string {
	if width <= 0 || len(values) == 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	peak := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		return styFaint.Render(strings.Repeat("▁", len(values)))
	}
	var b strings.Builder
	for i, v := range values {
		idx := int(math.Round(v / peak * float64(len(sparkChars)-1)))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkChars) {
			idx = len(sparkChars) - 1
		}
		t := float64(i) / float64(max(len(values)-1, 1))
		b.WriteString(lipgloss.NewStyle().Foreground(interpolateHex(cBlue, cAccent, t)).Render(string(sparkChars[idx])))
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
