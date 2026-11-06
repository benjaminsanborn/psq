package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBuildDSNSetsApplicationName(t *testing.T) {
	dsn := buildDSN(&DBConfig{Host: "h", Port: "5432", Database: "d", User: "u"}, "", "require")
	if !strings.Contains(dsn, "application_name='psq'") {
		t.Errorf("application_name missing: %s", dsn)
	}
}

func TestSessionSetupSetsStatementTimeout(t *testing.T) {
	prev := settings
	defer func() { settings = prev }()

	settings.StatementTimeout = 2500 * time.Millisecond
	c := newSessionConnector(nil)
	if len(c.setup) != 1 || c.setup[0] != "SET statement_timeout = 2500" {
		t.Errorf("setup = %v", c.setup)
	}

	settings.StatementTimeout = 0
	if c := newSessionConnector(nil); len(c.setup) != 0 {
		t.Errorf("timeout 0 should disable the setup statement, got %v", c.setup)
	}
}

func TestDeleteNeedsTwoPresses(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()
	prev := globalQueryDB
	globalQueryDB = qdb
	defer func() { globalQueryDB = prev }()

	pos := 1
	qdb.CreateQuery(Query{Name: "Doomed", Description: "d", SQL: "SELECT 1", OrderPosition: &pos})

	m := newTestModel()
	m.reloadQueries()
	m.editMode = true
	m.editQuery, _ = qdb.GetQuery("Doomed")
	m.initEditor(m.editQuery)

	ctrlD := tea.KeyMsg{Type: tea.KeyCtrlD}
	m.Update(ctrlD)
	if _, err := qdb.GetQuery("Doomed"); err != nil {
		t.Fatal("first ctrl+d must not delete")
	}
	if !strings.Contains(m.editErr, "again") {
		t.Errorf("expected a confirmation prompt, got %q", m.editErr)
	}

	// Any other key disarms the confirmation
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m.Update(ctrlD)
	if _, err := qdb.GetQuery("Doomed"); err != nil {
		t.Fatal("ctrl+d after another key must re-arm, not delete")
	}

	m.Update(ctrlD)
	if _, err := qdb.GetQuery("Doomed"); err == nil {
		t.Error("second consecutive ctrl+d should delete")
	}
	if m.editMode {
		t.Error("editor should close after deleting")
	}
}

func TestQuitKeys(t *testing.T) {
	m := newTestModel()
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape}); cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("esc must not quit from the main view")
		}
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("q should quit")
	}
}
