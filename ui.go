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
			Render(m.service)

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
	} else {
		content += m.results
	}

	m.viewport.SetContent(content)
}

func (m *Model) renderSearchMode() string {
	content := "\nSearch: " + m.searchQuery + "█\n\n"

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

	// SQL textarea
	sqlStyle := lipgloss.NewStyle()
	if m.editFocus == 3 {
		sqlStyle = sqlStyle.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86"))
	}
	content += "SQL:\n" + sqlStyle.Render(m.sqlTextarea.View()) + "\n\n"

	// AI Query Generator input
	aiStyle := lipgloss.NewStyle()
	if m.editFocus == 4 {
		aiStyle = aiStyle.BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("86"))
	}
	content += "AI Query Generator (Enter to generate/modify SQL):\n" + aiStyle.Render(m.aiPromptInput.View()) + "\n"

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

	helpText.WriteString(titleStyle.Render("Help") + "\n\n")

	// Query Navigation
	helpText.WriteString(titleStyle.Render("Query Navigation:") + "\n")
	helpText.WriteString(keyStyle.Render("←/h") + " " + descStyle.Render("previous query") + "\n")
	helpText.WriteString(keyStyle.Render("→/l") + " " + descStyle.Render("next query") + "\n")
	helpText.WriteString(keyStyle.Render("click") + " " + descStyle.Render("select query") + "\n")
	helpText.WriteString(keyStyle.Render("enter/space/r") + " " + descStyle.Render("execute query") + "\n\n")

	// Viewport Navigation
	helpText.WriteString(titleStyle.Render("Viewport Navigation:") + "\n")
	helpText.WriteString(keyStyle.Render("↑/k") + " " + descStyle.Render("scroll up") + "\n")
	helpText.WriteString(keyStyle.Render("↓/j") + " " + descStyle.Render("scroll down") + "\n")
	helpText.WriteString(keyStyle.Render("pgup") + " " + descStyle.Render("page up") + "\n")
	helpText.WriteString(keyStyle.Render("pgdn") + " " + descStyle.Render("page down") + "\n")
	helpText.WriteString(keyStyle.Render("home") + " " + descStyle.Render("go to top") + "\n")
	helpText.WriteString(keyStyle.Render("end") + " " + descStyle.Render("go to bottom") + "\n\n")

	// Query Operations
	helpText.WriteString(titleStyle.Render("Query Operations:") + "\n")
	helpText.WriteString(keyStyle.Render("s") + " " + descStyle.Render("search queries (type to filter, ↑/↓ navigate, enter select, esc cancel)") + "\n")
	helpText.WriteString(keyStyle.Render("e") + " " + descStyle.Render("edit query") + "\n")
	helpText.WriteString(keyStyle.Render("n") + " " + descStyle.Render("new query") + "\n")
	helpText.WriteString(keyStyle.Render("ctrl+d") + " " + descStyle.Render("delete query (in edit mode)") + "\n")
	helpText.WriteString(keyStyle.Render("a") + " " + descStyle.Render("chatgpt prompt (in new/edit mode)") + "\n")
	helpText.WriteString(keyStyle.Render("c") + " " + descStyle.Render("confirm chatgpt response") + "\n")
	helpText.WriteString(keyStyle.Render("d") + " " + descStyle.Render("dump queries") + "\n")
	helpText.WriteString(keyStyle.Render("x") + " " + descStyle.Render("psql prompt") + "\n\n")

	// System
	helpText.WriteString(titleStyle.Render("System:") + "\n")
	helpText.WriteString(keyStyle.Render("?") + " " + descStyle.Render("toggle help") + "\n")
	helpText.WriteString(keyStyle.Render("esc") + " " + descStyle.Render("quit") + "\n")

	return helpText.String()
}
