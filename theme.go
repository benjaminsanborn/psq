package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The palette. Each colour has a light-terminal and a dark-terminal value so
// psq reads well on either background.
var (
	colorFg      = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#C9D1D9"}
	colorDim     = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#6E7681"}
	colorBlue    = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#79A8FF"}
	colorGreen   = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#56D364"}
	colorYellow  = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#E3B341"}
	colorOrange  = lipgloss.AdaptiveColor{Light: "#BC4C00", Dark: "#F0883E"}
	colorRed     = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#FF7B72"}
	colorBorder  = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	colorBarBg   = lipgloss.AdaptiveColor{Light: "#EAEEF2", Dark: "#1C2330"}
	colorSelBg   = lipgloss.AdaptiveColor{Light: "#DDF4FF", Dark: "#1F3354"}
	colorModalBg = lipgloss.AdaptiveColor{Light: "#F6F8FA", Dark: "#1C2128"}
	colorTabFg   = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0D1117"}
)

var (
	styleFg     = lipgloss.NewStyle().Foreground(colorFg)
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleDim    = lipgloss.NewStyle().Foreground(colorDim)
	styleBlue   = lipgloss.NewStyle().Foreground(colorBlue)
	styleGreen  = lipgloss.NewStyle().Foreground(colorGreen)
	styleYellow = lipgloss.NewStyle().Foreground(colorYellow)
	styleRed    = lipgloss.NewStyle().Foreground(colorRed)
	styleKey    = lipgloss.NewStyle().Bold(true).Foreground(colorFg)
)

// stateStyle colours a pg_stat_activity state
func stateStyle(state string) lipgloss.Style {
	switch {
	case state == "active":
		return styleGreen
	case state == "idle in transaction (aborted)":
		return styleRed
	case strings.HasPrefix(state, "idle in transaction"):
		return styleYellow
	case state == "idle":
		return styleDim
	}
	return styleBlue
}

// keyHint is one entry in the key-hint bar: a key and what it does
type keyHint struct{ key, label string }

// renderHints renders the bottom key-hint bar, dropping hints from the end
// when they don't fit
func renderHints(hints []keyHint, width int) string {
	var b strings.Builder
	used := 1
	b.WriteString(" ")
	for i, h := range hints {
		w := ansi.StringWidth(h.key) + 1 + ansi.StringWidth(h.label)
		if i > 0 {
			w += 2
		}
		if used+w > width {
			break
		}
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(styleKey.Render(h.key) + " " + styleDim.Render(h.label))
		used += w
	}
	return b.String()
}

// titledBox draws a rounded box of the given outer width with the title set
// into the top border, like "╭─ Title ───╮". Body lines are clipped or padded
// to fit; height counts the borders (0 means fit the body).
func titledBox(title, body string, width, height int, border lipgloss.Style) string {
	inner := max(1, width-4) // "│ " + content + " │"
	lines := strings.Split(body, "\n")
	if height > 0 {
		rows := max(0, height-2)
		if len(lines) > rows {
			lines = lines[:rows]
		}
		for len(lines) < rows {
			lines = append(lines, "")
		}
	}

	var b strings.Builder
	label := ""
	if title != "" {
		label = " " + title + " "
	}
	fill := max(0, width-3-ansi.StringWidth(label))
	b.WriteString(border.Render("╭─") + border.Bold(true).Render(label) + border.Render(strings.Repeat("─", fill)+"╮"))
	for _, line := range lines {
		b.WriteString("\n" + border.Render("│") + " " + fitWidth(line, inner) + " " + border.Render("│"))
	}
	b.WriteString("\n" + border.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
	return b.String()
}

// gauge draws a horizontal bar: filled cells in the fill style, the rest as
// a dim track. frac is clamped to 0..1.
func gauge(frac float64, width int, fill lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	frac = max(0, min(1, frac))
	n := int(frac*float64(width) + 0.5)
	if frac > 0 && n == 0 {
		n = 1
	}
	return fill.Render(strings.Repeat("█", n)) + styleDim.Render(strings.Repeat("░", width-n))
}

var sparkBlocks = []rune(" ▁▂▃▄▅▆▇█")

// sparkline draws values as a two-row bar chart, the newest value on the
// right. The vertical scale runs from lo to hi.
func sparkline(values []float64, width int, lo, hi float64, style lipgloss.Style) (top, bottom string) {
	if width <= 0 {
		return "", ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	span := hi - lo
	var t, b strings.Builder
	pad := width - len(values)
	t.WriteString(strings.Repeat(" ", pad))
	b.WriteString(strings.Repeat(" ", pad))
	for _, v := range values {
		level := 0
		if span > 0 {
			level = int((v-lo)/span*16 + 0.5)
		}
		level = max(1, min(16, level))
		if level > 8 {
			t.WriteRune(sparkBlocks[level-8])
			b.WriteRune(sparkBlocks[8])
		} else {
			t.WriteRune(' ')
			b.WriteRune(sparkBlocks[level])
		}
	}
	return style.Render(t.String()), style.Render(b.String())
}

// overlay draws box over the middle of base, line by line, keeping whatever
// of base shows on either side
func overlay(base, box string, width int) string {
	baseLines := strings.Split(base, "\n")
	boxLines := strings.Split(box, "\n")
	boxW := 0
	for _, l := range boxLines {
		boxW = max(boxW, ansi.StringWidth(l))
	}
	x := max(0, (width-boxW)/2)
	y := max(2, (len(baseLines)-len(boxLines))/3)
	for len(baseLines) < y+len(boxLines) {
		baseLines = append(baseLines, "")
	}
	for i, l := range boxLines {
		row := padRight(baseLines[y+i], x+boxW)
		left := ansi.Truncate(row, x, "")
		right := ansi.TruncateLeft(row, x+boxW, "")
		baseLines[y+i] = left + "\x1b[0m" + l + "\x1b[0m" + right
	}
	return strings.Join(baseLines, "\n")
}

// modalBox draws a dialog: a bordered box with a solid background so it
// stands clear of whatever it's drawn over
func modalBox(title string, lines []string, width int, accent lipgloss.Style) string {
	bg := lipgloss.NewStyle().Background(colorModalBg)
	border := accent.Background(colorModalBg)
	inner := width - 4
	var b strings.Builder
	label := " " + title + " "
	b.WriteString(border.Render("╭─") + border.Bold(true).Render(label) +
		border.Render(strings.Repeat("─", max(0, width-3-ansi.StringWidth(label)))+"╮"))
	for _, l := range lines {
		// Re-apply the background after every reset inside the line
		body := strings.ReplaceAll(fitWidth(l, inner), "\x1b[0m", "\x1b[0m"+bgSeq())
		b.WriteString("\n" + border.Render("│") + bg.Render(" ") + bgSeq() + body + "\x1b[0m" + bg.Render(" ") + border.Render("│"))
	}
	b.WriteString("\n" + border.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
	return b.String()
}

// bgSeq is the escape sequence that turns on the modal background, taken from
// lipgloss so it follows the terminal's colour profile
func bgSeq() string {
	s := lipgloss.NewStyle().Background(colorModalBg).Render("x")
	if i := strings.Index(s, "x"); i > 0 {
		return s[:i]
	}
	return ""
}

// onSelection gives a style the selected-row background
func onSelection(s lipgloss.Style, selected bool) lipgloss.Style {
	if selected {
		return s.Background(colorSelBg)
	}
	return s
}
