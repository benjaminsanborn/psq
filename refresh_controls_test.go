package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIntervalSteps(t *testing.T) {
	m := newTestModel()
	if m.globalInterval() != settings.RefreshInterval {
		t.Fatalf("default interval = %v", m.globalInterval())
	}
	m.Update(runes("+"))
	m.Update(runes("+"))
	if m.globalInterval() != 5*time.Second {
		t.Errorf("after ++ interval = %v, want 5s", m.globalInterval())
	}
	m.Update(runes("-"))
	if m.globalInterval() != 2*time.Second {
		t.Errorf("after - interval = %v, want 2s", m.globalInterval())
	}
	for i := 0; i < 10; i++ {
		m.Update(runes("-"))
	}
	if m.globalInterval() != time.Second {
		t.Errorf("interval should bottom out at 1s, got %v", m.globalInterval())
	}
	for i := 0; i < 10; i++ {
		m.Update(runes("="))
	}
	if m.globalInterval() != time.Minute {
		t.Errorf("interval should top out at 1m, got %v", m.globalInterval())
	}
}

func TestPerQueryInterval(t *testing.T) {
	m := newTestModel()
	m.lastQuery = Query{Name: "Sizes", RefreshSeconds: intPtr(30)}
	if m.currentInterval() != 30*time.Second {
		t.Errorf("per-query interval = %v", m.currentInterval())
	}
	if !strings.Contains(m.refreshStatus(), "30s (set on this query)") {
		t.Errorf("status = %q", m.refreshStatus())
	}
}

func TestManualQueryDoesNotAutoRefresh(t *testing.T) {
	m := newTestModel()
	m.querySeq = 1
	m.lastQuery = Query{Name: "Heavy", RefreshSeconds: intPtr(0)}

	if _, cmd := m.Update(queryResultMsg{seq: 1, result: "x"}); cmd != nil {
		t.Error("a manual-refresh query must not schedule a tick")
	}
	if _, cmd := m.Update(tickMsg(time.Now())); cmd != nil {
		t.Error("a stray tick must not run a manual-refresh query")
	}
	if !strings.Contains(m.refreshStatus(), "manual") {
		t.Errorf("status = %q", m.refreshStatus())
	}
}

func TestPauseStopsAndResumeRestarts(t *testing.T) {
	m := newTestModel()
	m.lastQuery = m.queries[0]
	m.querySeq = 1

	m.Update(runes("p"))
	if !m.paused || !strings.Contains(m.refreshStatus(), "paused") {
		t.Fatal("p should pause")
	}
	if _, cmd := m.Update(queryResultMsg{seq: 1, result: "x"}); cmd != nil {
		t.Error("no tick should be scheduled while paused")
	}
	if _, cmd := m.Update(tickMsg(time.Now())); cmd != nil {
		t.Error("a tick while paused must not run a query")
	}

	seq := m.querySeq
	_, cmd := m.Update(runes("p"))
	if m.paused || cmd == nil || m.querySeq != seq+1 {
		t.Error("resuming should run the query right away")
	}
}

func TestEditorSavesRefreshSeconds(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()
	prev := globalQueryDB
	globalQueryDB = qdb
	defer func() { globalQueryDB = prev }()

	m := newTestModel()
	m.editMode = true
	m.initEditor(Query{})
	m.nameInput.SetValue("Sizes")
	m.sqlTextarea.SetValue("SELECT 1")

	m.refreshInput.SetValue("soon")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.editMode || !strings.Contains(m.editErr, "whole number") {
		t.Fatalf("invalid refresh should keep the editor open with an error, got %q", m.editErr)
	}

	m.refreshInput.SetValue("30")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	got, err := qdb.GetQuery("Sizes")
	if err != nil || got.RefreshSeconds == nil || *got.RefreshSeconds != 30 {
		t.Fatalf("saved query = %+v, %v", got, err)
	}

	m.editMode = true
	m.editQuery = got
	m.initEditor(got)
	if m.refreshInput.Value() != "30" {
		t.Errorf("editor should load the saved interval, got %q", m.refreshInput.Value())
	}
	m.refreshInput.SetValue("")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if got, _ := qdb.GetQuery("Sizes"); got.RefreshSeconds != nil {
		t.Errorf("clearing the field should fall back to the global interval, got %v", *got.RefreshSeconds)
	}
}

func TestSchemaMigratesOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// A database from before order_position and refresh_seconds existed
	if _, err := db.Exec(`CREATE TABLE queries (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL, sql TEXT NOT NULL);
		INSERT INTO queries (name, description, sql) VALUES ('B', 'd', 'SELECT 2'), ('A', 'd', 'SELECT 1'), ('Statements', 'mine', 'SELECT 3');`); err != nil {
		t.Fatal(err)
	}
	qdb := &QueryDB{db: db}
	defer qdb.Close()
	if err := qdb.initSchema(); err != nil {
		t.Fatal(err)
	}
	queries, err := qdb.LoadQueries()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, q := range queries {
		names = append(names, q.Name)
	}
	if got := strings.Join(names, ","); got != "A,B,Statements (saved)" {
		t.Errorf("migrated tabs = %s", got)
	}
}
