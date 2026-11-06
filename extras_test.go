package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFuzzyScore(t *testing.T) {
	if _, ok := fuzzyScore("pdb", "prod-db"); !ok {
		t.Error("pdb should match prod-db")
	}
	if _, ok := fuzzyScore("bdp", "prod-db"); ok {
		t.Error("characters must appear in order")
	}
	if _, ok := fuzzyScore("", "anything"); !ok {
		t.Error("empty pattern matches everything")
	}
	exact, _ := fuzzyScore("prod", "prod")
	scattered, _ := fuzzyScore("prod", "p-r-o-d")
	if exact <= scattered {
		t.Errorf("contiguous match (%d) should beat a scattered one (%d)", exact, scattered)
	}
}

func TestFuzzyRankOrdersBestFirst(t *testing.T) {
	services := []string{"app-replica", "staging", "prod", "prod-replica", "dev"}
	got := fuzzyRank("prod", len(services), func(i int) string { return services[i] })
	var names []string
	for _, i := range got {
		names = append(names, services[i])
	}
	if strings.Join(names, ",") != "prod,prod-replica" {
		t.Errorf("rank = %v", names)
	}
	if got := fuzzyRank("rep", len(services), func(i int) string { return services[i] }); len(got) != 2 {
		t.Errorf("rep should match both replicas, got %v", got)
	}
}

func pickerModel(services ...string) *PickerModel {
	m := &PickerModel{ready: true, width: 80, height: 24, viewport: viewport.New(80, 24)}
	m.setServices(services)
	return m
}

func TestPickerFilter(t *testing.T) {
	m := pickerModel("dev", "prod", "prod-replica", "staging")
	m.Update(runes("/"))
	for _, r := range "prd" {
		m.Update(runes(string(r)))
	}
	if strings.Join(m.visible, ",") != "prod,prod-replica" {
		t.Fatalf("visible = %v", m.visible)
	}
	// While filtering, j and k are typed rather than used to navigate
	m.Update(runes("k"))
	if m.filter != "prdk" || len(m.visible) != 0 {
		t.Errorf("filter=%q visible=%v", m.filter, m.visible)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || m.selectedService != "prod-replica" {
		t.Errorf("enter should pick the highlighted match, got %q", m.selectedService)
	}
}

func TestPickerEscClearsFilterBeforeQuitting(t *testing.T) {
	m := pickerModel("dev", "prod")
	m.filter = "p"
	m.applyFilter()
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape}); cmd != nil {
		t.Error("esc with a filter set should clear it, not quit")
	}
	if m.filter != "" || len(m.visible) != 2 {
		t.Errorf("filter=%q visible=%v", m.filter, m.visible)
	}
}

func TestPickerReload(t *testing.T) {
	m := pickerModel("dev")
	m.Update(pickerReloadMsg{services: []string{"dev", "new"}})
	if len(m.visible) != 2 {
		t.Errorf("visible after reload = %v", m.visible)
	}
	m.Update(pickerReloadMsg{err: errors.New("boom")})
	if !strings.Contains(m.err, "boom") {
		t.Errorf("err = %q", m.err)
	}
}

func TestDefaultPackSeedsNewDatabase(t *testing.T) {
	pack, err := defaultQueries()
	if err != nil || len(pack) == 0 {
		t.Fatalf("embedded pack: %v (%d queries)", err, len(pack))
	}
	for _, q := range pack {
		if validateQueryName(q.Name) != nil || strings.TrimSpace(q.SQL) == "" {
			t.Errorf("bad pack entry %+v", q)
		}
	}

	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()
	if err := qdb.createDefaultQueries(); err != nil {
		t.Fatal(err)
	}
	all, _ := qdb.LoadAllQueries()
	if len(all) != len(pack) {
		t.Errorf("seeded %d queries, pack has %d", len(all), len(pack))
	}
	tabs, _ := qdb.LoadQueries()
	if len(tabs) == 0 || tabs[0].Name != "Connection States" {
		t.Errorf("first tab = %v", tabs)
	}
	cfg, _ := qdb.GetQuery("Configuration Settings")
	if cfg.RefreshSeconds == nil || *cfg.RefreshSeconds != 0 {
		t.Error("Configuration Settings should be manual-refresh")
	}
}

func TestImportDefaultsAddsMissingAsHidden(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	qdb.CreateQuery(Query{Name: "Lock Information", Description: "mine", SQL: "SELECT 'mine'", OrderPosition: intPtr(1)})

	added, skipped, err := qdb.ImportDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "Lock Information" {
		t.Errorf("skipped = %v", skipped)
	}
	mine, _ := qdb.GetQuery("Lock Information")
	if mine.SQL != "SELECT 'mine'" {
		t.Error("import must not overwrite an existing query")
	}
	tabs, _ := qdb.LoadQueries()
	if len(tabs) != 1 {
		t.Errorf("imported queries should be hidden, tabs = %d", len(tabs))
	}

	again, _, _ := qdb.ImportDefaults()
	if len(again) != 0 || len(added) == 0 {
		t.Errorf("first import added %d, second added %d", len(added), len(again))
	}
}

func TestPageKeysScrollResults(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 10)
	m.results = strings.Repeat("row\n", 200)
	m.updateContent()

	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.viewport.YOffset == 0 {
		t.Error("pgdown should scroll the results")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.viewport.YOffset != 0 {
		t.Errorf("pgup should scroll back, offset = %d", m.viewport.YOffset)
	}
}
