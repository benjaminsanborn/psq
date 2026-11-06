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

	m.editErr = ""
	m.confirmDelete = false

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

	// Deleting takes two presses of ctrl+d; any other key disarms it
	if msg.String() == "ctrl+d" {
		if m.editQuery.Name == "" {
			return m, nil
		}
		if m.confirmDelete {
			m.confirmDelete = false
			return m.handleDeleteQuery()
		}
		m.confirmDelete = true
		m.editErr = fmt.Sprintf("Press ctrl+d again to delete %q", m.editQuery.Name)
		m.updateContent()
		return m, nil
	}
	if m.confirmDelete {
		m.confirmDelete = false
		m.editErr = ""
	}

	switch msg.String() {
	case "ctrl+s":
		return m.handleSaveQuery()
	case "tab", "shift+tab":
		return m.handleTabNavigation(msg.String())
	default:
		return m.handleEditInput(msg)
	}
}

func (m *Model) handleDeleteQuery() (tea.Model, tea.Cmd) {
	// Only existing queries can be deleted, not unsaved new ones
	if m.editQuery.Name == "" || globalQueryDB == nil {
		m.updateContent()
		return m, nil
	}
	if err := globalQueryDB.DeleteQuery(m.editQuery.Name); err != nil {
		m.editErr = fmt.Sprintf("Failed to delete query: %v", err)
		m.updateContent()
		return m, nil
	}
	delete(m.tempQueries, m.editQuery.Name)

	if err := m.reloadQueries(); err != nil {
		m.editErr = fmt.Sprintf("Failed to reload queries: %v", err)
		m.updateContent()
		return m, nil
	}

	m.editMode = false
	m.err = ""
	m.selected = m.previousSelected
	m.ensureValidSelection()
	m.syncActiveView()
	m.loading = true
	m.lastQuery = m.queries[m.selected]
	m.updateContent()
	return m, m.runQuery(m.lastQuery)
}

// validateQueryName rejects names the tab bar can't represent
func validateQueryName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if IsHomeTab(name) || IsActiveTab(name) || IsStatementsTab(name) {
		return fmt.Errorf("%q is reserved for a built-in tab", name)
	}
	return nil
}

func (m *Model) handleSaveQuery() (tea.Model, tea.Cmd) {
	if globalQueryDB == nil {
		m.updateContent()
		return m, nil
	}

	newQuery := Query{
		Name:        strings.TrimSpace(m.nameInput.Value()),
		Description: m.descInput.Value(),
		SQL:         m.sqlTextarea.Value(),
	}
	if err := validateQueryName(newQuery.Name); err != nil {
		m.editErr = fmt.Sprintf("Cannot save query: %v", err)
		m.updateContent()
		return m, nil
	}

	// Parse order position, but don't persist a temporary tab position
	orderStr := strings.TrimSpace(m.orderInput.Value())
	oldName := m.editQuery.Name
	isNewQuery := oldName == ""

	if orderStr != "" {
		var pos int
		if n, err := fmt.Sscanf(orderStr, "%d", &pos); err == nil && n == 1 {
			if !m.isTemporaryQuery(oldName) || orderStr != fmt.Sprintf("%d", m.tempQueries[oldName]) {
				newQuery.OrderPosition = &pos
			}
		}
	}

	hadPosition := m.editQuery.OrderPosition != nil
	willHavePosition := newQuery.OrderPosition != nil

	var err error
	if isNewQuery {
		err = globalQueryDB.CreateQuery(newQuery)
	} else {
		err = globalQueryDB.UpdateQuery(oldName, newQuery)
	}
	if err != nil {
		m.editErr = fmt.Sprintf("Failed to save query: %v", err)
		m.updateContent()
		return m, nil
	}

	// A renamed or newly positioned query is no longer the old temporary tab
	if !isNewQuery && (newQuery.Name != oldName || willHavePosition) {
		delete(m.tempQueries, oldName)
	}

	if err := m.reloadQueries(); err != nil {
		m.editErr = fmt.Sprintf("Failed to reload queries: %v", err)
		m.updateContent()
		return m, nil
	}

	// Keep a hidden query visible as a temporary tab after saving it
	if !willHavePosition && (isNewQuery || hadPosition) {
		m.addTemporaryQuery(newQuery)
	}

	for i, q := range m.queries {
		if q.Name == newQuery.Name {
			m.selected = i
			break
		}
	}
	m.ensureValidSelection()
	m.syncActiveView()

	m.editMode = false
	m.loading = true
	m.err = ""
	m.lastQuery = newQuery
	m.updateContent()
	return m, m.runQuery(newQuery)
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
