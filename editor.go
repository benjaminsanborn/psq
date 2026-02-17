package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) initEditor(query Query) {
	// Initialize name input
	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "Query name"
	m.nameInput.SetValue(query.Name)
	m.nameInput.CharLimit = 50
	m.nameInput.Width = 50

	// Initialize description input
	m.descInput = textinput.New()
	m.descInput.Placeholder = "Query description"
	m.descInput.SetValue(query.Description)
	m.descInput.CharLimit = 100
	m.descInput.Width = 50

	// Initialize order input
	m.orderInput = textinput.New()
	m.orderInput.Placeholder = "Order position (empty to hide)"
	if query.OrderPosition != nil {
		m.orderInput.SetValue(fmt.Sprintf("%d", *query.OrderPosition))
	}
	m.orderInput.CharLimit = 10
	m.orderInput.Width = 30

	// Initialize SQL textarea
	m.sqlTextarea = textarea.New()
	m.sqlTextarea.Placeholder = "Enter your SQL query here..."
	m.sqlTextarea.SetValue(query.SQL)
	m.sqlTextarea.SetWidth(80)
	m.sqlTextarea.SetHeight(10)

	// Focus on the first input
	m.editFocus = 0
	m.nameInput.Focus()
}

func (m *Model) handleEditModeKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Check for escape key by type as well as string
	if msg.Type == tea.KeyEscape || msg.String() == "escape" || msg.String() == "esc" || msg.String() == "ctrl+c" || msg.String() == "ctrl+[" {
		m.editMode = false
		// Restore previous selection
		if m.previousSelected < len(m.queries) {
			m.selected = m.previousSelected
		}
		m.ensureValidSelection()
		m.updateContent()
		return m, nil
	}

	switch msg.String() {
	case "ctrl+d":
		return m.handleDeleteQuery()
	case "ctrl+s":
		return m.handleSaveQuery()
	case "tab", "shift+tab":
		return m.handleTabNavigation(msg.String())
	default:
		return m.handleEditInput(msg)
	}
}

func (m *Model) handleDeleteQuery() (tea.Model, tea.Cmd) {
	// Delete the query (only for existing queries, not new ones)
	if m.editQuery.Name != "" {
		if globalQueryDB != nil {
			if err := globalQueryDB.DeleteQuery(m.editQuery.Name); err != nil {
				m.err = fmt.Sprintf("Failed to delete query: %v", err)
				m.updateContent()
				return m, nil
			} else {
				// Remove from temporary queries if it was temporary
				if m.isTemporaryQuery(m.editQuery.Name) {
					delete(m.tempQueries, m.editQuery.Name)
				}

				// Reload queries
				queries, err := loadQueries()
				if err != nil {
					m.err = fmt.Sprintf("Failed to reload queries: %v", err)
					m.updateContent()
					return m, nil
				} else {
					m.queries = queries
					// Also reload all queries for search
					if allQueries, err := globalQueryDB.LoadAllQueries(); err == nil {
						m.allQueries = allQueries
					}

					// Adjust selection if needed
					if m.previousSelected >= len(m.queries) && len(m.queries) > 0 {
						m.previousSelected = len(m.queries) - 1
					}

					m.editMode = false
					// Restore previous selection
					if m.previousSelected < len(m.queries) {
						m.selected = m.previousSelected
					}
					m.ensureValidSelection()
					m.updateContent()
					return m, nil
				}
			}
		}
	}
	m.updateContent()
	return m, nil
}

func (m *Model) handleSaveQuery() (tea.Model, tea.Cmd) {
	// Save the query
	newQuery := Query{
		Name: func() string {
			if m.nameInput.Value() == "" {
				return "New Query"
			}
			return m.nameInput.Value()
		}(),
		Description: m.descInput.Value(),
		SQL:         m.sqlTextarea.Value(),
	}

	// Parse order position (but don't save temporary ones)
	orderStr := strings.TrimSpace(m.orderInput.Value())
	isNewQuery := m.editQuery.Name == ""

	if orderStr != "" {
		var pos int
		if n, err := fmt.Sscanf(orderStr, "%d", &pos); err == nil && n == 1 {
			// Only set if it's not a temporary position or user changed it
			if !m.isTemporaryQuery(m.editQuery.Name) || orderStr != fmt.Sprintf("%d", m.tempQueries[m.editQuery.Name]) {
				newQuery.OrderPosition = &pos
			}
		}
	}

	// Check if we're removing position from an existing query
	hadPosition := m.editQuery.OrderPosition != nil
	willHavePosition := newQuery.OrderPosition != nil

	if globalQueryDB != nil {
		if err := globalQueryDB.SaveQuery(newQuery); err != nil {
			m.err = fmt.Sprintf("Failed to save query: %v", err)
			m.updateContent()
			return m, nil
		} else {
			// Reload queries
			queries, err := loadQueries()
			if err != nil {
				m.err = fmt.Sprintf("Failed to reload queries: %v", err)
				m.updateContent()
				return m, nil
			} else {
				m.queries = queries
				// Also reload all queries for search
				if allQueries, err := globalQueryDB.LoadAllQueries(); err == nil {
					m.allQueries = allQueries
				}

				// If this is a new query without a position, or if we removed position from existing query, add it as temporary
				if (isNewQuery && newQuery.OrderPosition == nil) || (!isNewQuery && hadPosition && !willHavePosition) {
					m.addTemporaryQuery(newQuery)
				}

				// Find the updated/new query in the list
				for i, q := range m.queries {
					if q.Name == newQuery.Name {
						m.selected = i
						break
					}
				}
				m.ensureValidSelection()
			}
			m.editMode = false
			m.loading = true
			m.err = ""
			m.lastQuery = newQuery
			m.updateContent()
			// Execute the updated query
			return m, m.runQuery(newQuery)
		}
	}
	m.updateContent()
	return m, nil
}

func (m *Model) handleTabNavigation(key string) (tea.Model, tea.Cmd) {
	// Cycle through inputs (4 total: name, description, order, sql)
	if key == "tab" {
		m.editFocus = (m.editFocus + 1) % 4
	} else {
		m.editFocus = (m.editFocus + 3) % 4
	}

	// Update focus
	m.nameInput.Blur()
	m.descInput.Blur()
	m.orderInput.Blur()
	m.sqlTextarea.Blur()

	switch m.editFocus {
	case 0:
		m.nameInput.Focus()
	case 1:
		m.descInput.Focus()
	case 2:
		m.orderInput.Focus()
	case 3:
		m.sqlTextarea.Focus()
	}
	m.updateContent()
	return m, nil
}

func (m *Model) handleEditInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Update the focused input
	var cmd tea.Cmd
	switch m.editFocus {
	case 0:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case 1:
		m.descInput, cmd = m.descInput.Update(msg)
	case 2:
		m.orderInput, cmd = m.orderInput.Update(msg)
	case 3:
		m.sqlTextarea, cmd = m.sqlTextarea.Update(msg)
	}
	m.updateContent()
	return m, cmd
}
