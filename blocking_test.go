package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A lock chain: 10 blocks 20, which blocks 30; 10 also blocks 40 directly.
// 50 is an unrelated long-running query.
func lockChain() []ActiveProcess {
	return []ActiveProcess{
		{PID: 10, Username: "app", State: "idle in transaction", DurationSec: 5, StateSec: 300},
		{PID: 20, Username: "app", State: "active", DurationSec: 60, BlockedBy: []int{10}, WaitEventType: "Lock"},
		{PID: 30, Username: "batch", State: "active", DurationSec: 30, BlockedBy: []int{20}, WaitEventType: "Lock"},
		{PID: 40, Username: "web", State: "active", DurationSec: 90, BlockedBy: []int{10}, WaitEventType: "Lock"},
		{PID: 50, Username: "report", State: "active", DurationSec: 900},
	}
}

func rowPIDs(av *ActiveView) []int {
	var pids []int
	for _, r := range av.Rows {
		pids = append(pids, r.proc.PID)
	}
	return pids
}

func TestLinkBlocking(t *testing.T) {
	procs := lockChain()
	linkBlocking(procs)

	byPID := map[int]ActiveProcess{}
	for _, p := range procs {
		byPID[p.PID] = p
	}
	if got := byPID[10]; !got.IsRootBlocker() || got.BlockingTotal != 3 || len(got.Blocking) != 2 {
		t.Errorf("PID 10: root=%v total=%d direct=%v", got.IsRootBlocker(), got.BlockingTotal, got.Blocking)
	}
	if got := byPID[20]; got.IsRootBlocker() || got.BlockingTotal != 1 {
		t.Errorf("PID 20 is blocked, so not a root; total=%d", got.BlockingTotal)
	}
	if got := byPID[50]; got.IsRootBlocker() || got.IsBlocked() {
		t.Error("PID 50 is unrelated to the chain")
	}
}

func TestLinkBlockingSurvivesCycle(t *testing.T) {
	procs := []ActiveProcess{
		{PID: 1, BlockedBy: []int{2}},
		{PID: 2, BlockedBy: []int{1}},
	}
	linkBlocking(procs) // must terminate
	if procs[0].BlockingTotal != 1 || procs[1].BlockingTotal != 1 {
		t.Errorf("totals = %d, %d", procs[0].BlockingTotal, procs[1].BlockingTotal)
	}
}

func TestDefaultSortIsLongestFirst(t *testing.T) {
	av := NewActiveView()
	av.UpdateSelection(lockChain())
	if got := rowPIDs(av); got[0] != 50 || got[len(got)-1] != 10 {
		t.Errorf("rows = %v, want longest-running (50) first", got)
	}

	av.CycleSort() // blocking
	if got := rowPIDs(av); got[0] != 10 {
		t.Errorf("sort by blocking: rows = %v, want root blocker 10 first", got)
	}
}

func TestLockTreeOrdersChainsDepthFirst(t *testing.T) {
	av := NewActiveView()
	av.UpdateSelection(lockChain())
	av.SetTreeMode(true)

	got := rowPIDs(av)
	want := []int{10, 40, 20, 30} // waiters ordered longest first; unrelated 50 hidden
	if len(got) != len(want) {
		t.Fatalf("tree rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tree rows = %v, want %v", got, want)
		}
	}
	depths := []int{0, 1, 1, 2}
	for i, r := range av.Rows {
		if r.depth != depths[i] {
			t.Errorf("row %d (PID %d) depth = %d, want %d", i, r.proc.PID, r.depth, depths[i])
		}
	}
}

func TestFilterKeepsWholeChainInTreeMode(t *testing.T) {
	av := NewActiveView()
	av.UpdateSelection(lockChain())

	av.SetFilter("batch")
	if got := rowPIDs(av); len(got) != 1 || got[0] != 30 {
		t.Errorf("list filter rows = %v, want [30]", got)
	}

	av.SetTreeMode(true)
	if got := rowPIDs(av); len(got) != 4 {
		t.Errorf("tree filter should keep the chain containing 30, got %v", got)
	}

	av.SetFilter("nomatch")
	if len(av.Rows) != 0 {
		t.Errorf("rows = %v, want none", rowPIDs(av))
	}
}

func TestSelectionFollowsPIDAcrossViewChanges(t *testing.T) {
	av := NewActiveView()
	av.UpdateSelection(lockChain())
	for av.SelectedProcess().PID != 30 {
		av.MoveSelection(1)
	}
	av.CycleSort()
	if p := av.SelectedProcess(); p == nil || p.PID != 30 {
		t.Errorf("selection after re-sort = %v, want PID 30", p)
	}
	av.SetTreeMode(true)
	if p := av.SelectedProcess(); p == nil || p.PID != 30 {
		t.Errorf("selection in tree mode = %v, want PID 30", p)
	}
}

func TestRenderActiveListShowsLockColumn(t *testing.T) {
	av := NewActiveView()
	av.UpdateSelection(lockChain())
	out := RenderActiveList(av, 200, 40)
	for _, want := range []string{"blocks 3", "by 10", "3 blocked by 1 root blocker", "idle in xact", "5m"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q", want)
		}
	}
}

func activeTabModel() *Model {
	m := newTestModel()
	m.selected = 1 // Active
	m.syncActiveView()
	m.activeView.UpdateSelection(lockChain())
	return m
}

func typeKeys(m *Model, keys ...tea.KeyMsg) {
	for _, k := range keys {
		m.Update(k)
	}
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestActiveFilterKeys(t *testing.T) {
	m := activeTabModel()
	typeKeys(m, runes("/"), runes("w"), runes("e"), runes("b"))
	if m.activeView.Filter != "web" || !m.activeView.Filtering {
		t.Fatalf("filter=%q filtering=%v", m.activeView.Filter, m.activeView.Filtering)
	}
	// While typing, tab-switch keys go into the filter instead of switching tabs
	typeKeys(m, runes("l"))
	if m.selected != 1 || m.activeView.Filter != "webl" {
		t.Errorf("selected=%d filter=%q", m.selected, m.activeView.Filter)
	}
	typeKeys(m, tea.KeyMsg{Type: tea.KeyBackspace}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.activeView.Filtering || m.activeView.Filter != "web" || len(m.activeView.Rows) != 1 {
		t.Errorf("after enter: filtering=%v filter=%q rows=%v", m.activeView.Filtering, m.activeView.Filter, rowPIDs(m.activeView))
	}
	typeKeys(m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.activeView.Filter != "" {
		t.Error("esc should clear the filter")
	}
}

func TestActiveTreeKeys(t *testing.T) {
	m := activeTabModel()
	typeKeys(m, runes("b"))
	if !m.activeView.TreeMode {
		t.Fatal("b should open the lock tree")
	}
	typeKeys(m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.activeView.TreeMode {
		t.Error("esc should leave the lock tree")
	}
}

func TestHomeBOpensLockTree(t *testing.T) {
	m := newTestModel()
	m.selected = 0 // Home
	_, cmd := m.Update(runes("b"))
	if !IsActiveTab(m.queries[m.selected].Name) || m.activeView == nil || !m.activeView.TreeMode {
		t.Fatal("b on Home should switch to the Active tab's lock tree")
	}
	if cmd == nil {
		t.Error("switching tabs should start a refresh")
	}
}
