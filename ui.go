package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	if !m.ready {
		return "Getting ready..."
	}

	return zone.Scan(m.viewport.View() + "\n" + renderHints(m.keyHints(), m.width))
}

// headerBar is the top line: psq, the service and where it points, and the
// refresh status on the right, all on a shaded bar
func (m *Model) headerBar() string {
	bar := lipgloss.NewStyle().Background(colorBarBg)
	left := bar.Render(" ") + bar.Bold(true).Foreground(colorBlue).Render("psq") +
		bar.Render("  ") + bar.Bold(true).Foreground(colorFg).Render(m.service)
	if target := m.connTarget(); target != "" {
		left += bar.Render("  ") + bar.Foreground(colorDim).Render(target)
	}

	var right string
	switch {
	case m.paused:
		right = bar.Foreground(colorYellow).Render("⏸") + bar.Foreground(colorFg).Render(" paused")
	case m.currentInterval() == 0:
		right = bar.Foreground(colorDim).Render("↻") + bar.Foreground(colorFg).Render(" manual")
	case m.err != "":
		right = bar.Foreground(colorRed).Render("●") + bar.Foreground(colorFg).Render(" every "+formatWindow(m.currentInterval()))
	default:
		right = bar.Foreground(colorGreen).Render("●") + bar.Foreground(colorFg).Render(" every "+formatWindow(m.currentInterval()))
	}
	if m.lastQuery.RefreshSeconds != nil && !m.paused {
		right += bar.Foreground(colorDim).Render(" (this query)")
	}
	right += bar.Render(" ")

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return ansi.Truncate(left, m.width, "")
	}
	return left + bar.Render(strings.Repeat(" ", gap)) + right
}

// connTarget is "user@host:port/db" for the header, read once from the
// service file
func (m *Model) connTarget() string {
	if m.target == nil {
		target := ""
		if cfg, err := getDBConfig(m.service); err == nil && cfg.Host != "" {
			target = cfg.Host
			if cfg.User != "" {
				target = cfg.User + "@" + target
			}
			if cfg.Port != "" {
				target += ":" + cfg.Port
			}
			if cfg.Database != "" {
				target += "/" + cfg.Database
			}
		}
		m.target = &target
	}
	return *m.target
}

func (m *Model) updateContent() {
	content := m.headerBar() + "\n"

	// Show help if requested
	if m.showHelp {
		content += "\n" + m.customHelpView()
		m.viewport.SetContent(content)
		return
	}

	if m.searchMode {
		content += m.renderSearchMode()
	} else if m.editMode {
		content += m.renderEditMode()
	} else {
		content += m.renderNormalMode()
	}

	content += "\n" + styleDim.Render(strings.Repeat("─", m.width)) + "\n"

	// Results section
	onActive := m.activeView != nil && m.activeView.Loaded &&
		m.selected < len(m.queries) && IsActiveTab(m.queries[m.selected].Name)
	if m.err != "" {
		content += styleRed.Render("Error: " + m.err)
	} else if onActive {
		// Re-render active view from cached data so key presses take effect immediately
		switch m.activeView.Mode {
		case ActiveModeDetail:
			content += RenderActiveDetail(m.activeView, m.width)
		case ActiveModeConfirmTerminate:
			list := RenderActiveList(m.activeView, m.width, m.height)
			content += overlay(list, RenderTerminateConfirm(m.activeView, m.width), m.width)
		default:
			content += RenderActiveList(m.activeView, m.width, m.height)
		}
	} else {
		content += m.results
	}

	m.viewport.SetContent(content)
}

// keyHints are the keys shown in the bottom bar for whatever is on screen
func (m *Model) keyHints() []keyHint {
	switch {
	case m.showHelp:
		return []keyHint{{"esc", "close help"}, {"?", "close help"}, {"q", "quit"}}
	case m.editMode:
		return []keyHint{{"tab", "next field"}, {"ctrl+s", "save"}, {"ctrl+d", "delete"}, {"esc", "cancel"}}
	case m.searchMode:
		return []keyHint{{"type", "to search"}, {"↑/↓", "select"}, {"enter", "open"}, {"esc", "cancel"}}
	}

	tab := ""
	if m.selected < len(m.queries) {
		tab = m.queries[m.selected].Name
	}
	switch {
	case IsActiveTab(tab) && m.activeView != nil:
		return activeHints(m.activeView)
	case IsHomeTab(tab):
		return []keyHint{{"?", "help"}, {"←/→", "tabs"}, {"b", "lock tree"}, {"p", "pause"}, {"+/-", "interval"}, {"s", "search"}, {"c", "services"}, {"q", "quit"}}
	case IsStatementsTab(tab):
		return []keyHint{{"?", "help"}, {"←/→", "tabs"}, {"w", "window"}, {"o", "sort"}, {"p", "pause"}, {"q", "quit"}}
	}
	return []keyHint{{"?", "help"}, {"←/→", "tabs"}, {"↑/↓", "scroll"}, {"r", "refresh"}, {"s", "search"}, {"e", "edit"}, {"n", "new"}, {"x", "psql"}, {"p", "pause"}, {"q", "quit"}}
}

func (m *Model) renderSearchMode() string {
	content := "\n Search: " + m.searchQuery + "█\n\n"

	// Display filtered queries
	if len(m.filteredQueries) == 0 {
		content += "No queries match your search"
	} else {
		for i, query := range m.filteredQueries {
			if i == m.selected {
				content += lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("86")).
					Render("▶ " + query.Name + " - " + query.Description)
			} else {
				content += "  " + query.Name + " - " + query.Description
			}
			content += "\n"
		}
	}
	return content
}

func (m *Model) renderEditMode() string {
	content := ": Tab to switch fields, Ctrl+S to save, Ctrl+D to delete, Esc to cancel\n\n"

	// Query editor
	editorTitle := "Edit Query"
	if m.editQuery.Name == "" {
		editorTitle = "Create New Query"
	}
	content += lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render(editorTitle) + "\n\n"

	if m.editErr != "" {
		content += lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.editErr) + "\n\n"
	}

	// Name input
	nameStyle := lipgloss.NewStyle()
	if m.editFocus == 0 {
		nameStyle = nameStyle.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86"))
	}
	content += "Name:\n" + nameStyle.Render(m.nameInput.View()) + "\n\n"

	// Description input
	descStyle := lipgloss.NewStyle()
	if m.editFocus == 1 {
		descStyle = descStyle.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86"))
	}
	content += "Description:\n" + descStyle.Render(m.descInput.View()) + "\n\n"

	// Order input
	orderStyle := lipgloss.NewStyle()
	if m.editFocus == 2 {
		orderStyle = orderStyle.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86"))
	}
	content += "Order Position (empty to hide from tabs):\n" + orderStyle.Render(m.orderInput.View()) + "\n\n"

	// Refresh input
	refreshStyle := lipgloss.NewStyle()
	if m.editFocus == 3 {
		refreshStyle = refreshStyle.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86"))
	}
	content += "Refresh Every (seconds; empty = global interval, 0 = manual):\n" + refreshStyle.Render(m.refreshInput.View()) + "\n\n"

	// SQL textarea
	sqlStyle := lipgloss.NewStyle()
	if m.editFocus == 4 {
		sqlStyle = sqlStyle.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86"))
	}
	content += "SQL:\n" + sqlStyle.Render(m.sqlTextarea.View()) + "\n"

	return content
}

// renderNormalMode draws the tab bar: the current tab highlighted, the rest
// plain. Tabs opened from search (hidden queries) are in italics.
//
// When the tabs don't fit, the bar scrolls to keep the current tab in view
// and shows ‹ › where tabs are hidden.
func (m *Model) renderNormalMode() string {
	widths := make([]int, len(m.queries))
	for i, q := range m.queries {
		widths[i] = ansi.StringWidth(q.Name) + 3 // " name " plus the gap after it
	}
	avail := m.width - 5 // leading space and room for ‹ ›

	// First tab shown: the earliest one that still leaves the current tab in view
	first, used := 0, 0
	for i := 0; i <= m.selected && i < len(widths); i++ {
		used += widths[i]
	}
	for used > avail && first < m.selected {
		used -= widths[first]
		first++
	}

	content := "\n "
	if first > 0 {
		content += styleDim.Render("‹ ")
	}
	used = 0
	for i := first; i < len(m.queries); i++ {
		if used+widths[i] > avail && i > m.selected {
			content += styleDim.Render("›")
			break
		}
		used += widths[i]
		query := m.queries[i]
		style := lipgloss.NewStyle().Foreground(colorDim)
		if i == m.selected {
			style = lipgloss.NewStyle().Bold(true).Foreground(colorTabFg).Background(colorBlue)
		}
		if m.isTemporaryQuery(query.Name) {
			style = style.Italic(true)
		}

		// Wrap in bubblezone mark for clickability
		content += zone.Mark(fmt.Sprintf("query_%d", i), style.Render(" "+query.Name+" ")) + " "
	}
	return content
}

func (m *Model) customHelpView() string {
	var helpText strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	section := func(title string, lines ...[2]string) {
		helpText.WriteString(sectionStyle.Render(title) + "\n")
		for _, l := range lines {
			helpText.WriteString(keyStyle.Render(l[0]) + " " + descStyle.Render(l[1]) + "\n")
		}
		helpText.WriteString("\n")
	}

	helpText.WriteString(titleStyle.Render("Help") + "\n\n")

	section("Tabs:",
		[2]string{"←/h →/l", "previous / next tab"},
		[2]string{"click", "select tab"},
		[2]string{"enter/space/r", "refresh now"},
	)
	section("Refresh:",
		[2]string{"p", "pause / resume auto-refresh"},
		[2]string{"+ / -", "refresh less / more often (1s to 1m)"},
	)
	section("Scrolling:",
		[2]string{"↑/k ↓/j", "scroll results"},
		[2]string{"pgup/pgdn", "page up / down"},
		[2]string{"home/end", "top / bottom"},
	)
	section("Saved Queries:",
		[2]string{"s", "fuzzy search all queries, including hidden ones"},
		[2]string{"e", "edit query"},
		[2]string{"n", "new query"},
		[2]string{"ctrl+s", "save (in editor)"},
		[2]string{"ctrl+d ctrl+d", "delete (in editor; press twice)"},
		[2]string{"x", "open psql"},
	)
	section("Home:",
		[2]string{"b / click", "open the lock tree from the blocked-queries widget"},
	)
	section("Active:",
		[2]string{"↑/k ↓/j", "select session"},
		[2]string{"enter", "session details"},
		[2]string{"t / c", "terminate backend / cancel query"},
		[2]string{"b", "lock tree: only blocking chains, nested under root blockers"},
		[2]string{"o", "cycle sort (duration, blocking, state, user, database)"},
		[2]string{"/", "filter by pid, user, database, state, app, client or query"},
		[2]string{"y", "copy query (in details)"},
		[2]string{"esc", "back (details, filter, lock tree)"},
	)
	section("Statements:",
		[2]string{"w", "window: last 10s / 1m / 5m"},
		[2]string{"o", "cycle sort (total time, calls, rows, mean, disk reads)"},
	)
	section("System:",
		[2]string{"?", "toggle help"},
		[2]string{"c", "back to the service picker"},
		[2]string{"q/ctrl+c", "quit"},
	)

	return helpText.String()
}

// refreshStatus describes the current tab's auto-refresh for the header
func (m *Model) refreshStatus() string {
	switch {
	case m.paused:
		return "⏸ paused (p to resume)"
	case m.currentInterval() == 0:
		return "↻ manual (r to refresh)"
	}
	status := "↻ every " + formatWindow(m.currentInterval())
	if m.lastQuery.RefreshSeconds != nil {
		status += " (set on this query)"
	}
	return status
}
