package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	if !m.ready {
		return "Getting ready..."
	}

	return zone.Scan(m.viewport.View())
}

func (m *Model) updateContent() {
	var content string

	// Header section
	content += " " + lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("psq@") +
		lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("201")).
			Render(m.service) +
		"  " + lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(m.refreshStatus())

	// Show help if requested
	if m.showHelp {
		content += "\n\n" + m.customHelpView()
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

	content += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(strings.Repeat("─", m.width)) + "\n"

	// Results section
	if m.err != "" {
		content += "Error: " + m.err
	} else if m.activeView != nil && m.activeView.Loaded &&
		m.selected < len(m.queries) && IsActiveTab(m.queries[m.selected].Name) {
		// Re-render active view from cached data so key presses take effect immediately
		switch m.activeView.Mode {
		case ActiveModeDetail:
			content += RenderActiveDetail(m.activeView, m.width)
		case ActiveModeConfirmTerminate:
			content += RenderTerminateConfirm(m.activeView)
		default:
			content += RenderActiveList(m.activeView, m.width, m.height)
		}
	} else {
		content += m.results
	}

	m.viewport.SetContent(content)
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

func (m *Model) renderNormalMode() string {
	content := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(": Press ? for help\n")

	// Query list
	content += "\n "
	for i, query := range m.queries {
		var queryText string
		if i == m.selected {
			style := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Background(lipgloss.Color("235"))

			// Add italics for temporary queries
			if m.isTemporaryQuery(query.Name) {
				style = style.Italic(true)
			}

			queryText = style.Render(query.Name)
		} else {
			// Non-selected queries: subtle background and padding to show they're clickable
			baseStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("238")).
				Foreground(lipgloss.Color("252"))

			if m.isTemporaryQuery(query.Name) {
				baseStyle = baseStyle.Italic(true)
			}

			queryText = baseStyle.Render(query.Name)
		}

		// Wrap in bubblezone mark for clickability
		content += zone.Mark(fmt.Sprintf("query_%d", i), queryText)

		if i < len(m.queries)-1 {
			content += " "
		}
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
