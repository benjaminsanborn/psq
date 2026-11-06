package main

import (
	"strings"
	"testing"
	"time"
)

func snap(at time.Time, entries map[int64]stmtCounters) *stmtSnapshot {
	s := &stmtSnapshot{At: at, Stats: map[stmtKey]stmtCounters{}}
	for id, c := range entries {
		s.Stats[stmtKey{userID: 10, dbID: 5, queryID: id}] = c
	}
	return s
}

func TestDiffStatements(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := snap(t0, map[int64]stmtCounters{
		1: {Calls: 100, TotalMs: 1000, Rows: 100},
		2: {Calls: 50, TotalMs: 5000, Rows: 50},
		3: {Calls: 900, TotalMs: 9000}, // will be reset
		4: {Calls: 7, TotalMs: 70},     // no new activity
	})
	cur := snap(t0.Add(time.Minute), map[int64]stmtCounters{
		1: {Calls: 160, TotalMs: 1600, Rows: 160}, // +60 calls, +600ms
		2: {Calls: 52, TotalMs: 7000, Rows: 52},   // +2 calls, +2000ms
		3: {Calls: 5, TotalMs: 40},                // reset: counters went backwards
		4: {Calls: 7, TotalMs: 70},
		5: {Calls: 3, TotalMs: 30}, // new since baseline
	})

	rows := diffStatements(cur, base, StmtSortTime)
	got := map[int64]stmtCounters{}
	var order []int64
	for _, r := range rows {
		got[r.key.queryID] = r.delta
		order = append(order, r.key.queryID)
	}
	if _, ok := got[4]; ok {
		t.Error("a statement with no new calls should be left out")
	}
	if got[1].Calls != 60 || got[1].TotalMs != 600 {
		t.Errorf("query 1 delta = %+v", got[1])
	}
	if got[3].Calls != 5 {
		t.Errorf("a reset entry should count its current value, got %+v", got[3])
	}
	if got[5].Calls != 3 {
		t.Errorf("a new entry should count its current value, got %+v", got[5])
	}
	if order[0] != 2 {
		t.Errorf("by total time, query 2 should lead: %v", order)
	}

	if rows := diffStatements(cur, base, StmtSortCalls); rows[0].key.queryID != 1 {
		t.Errorf("by calls, query 1 should lead, got %d", rows[0].key.queryID)
	}
	if rows := diffStatements(cur, base, StmtSortMean); rows[0].key.queryID != 2 {
		t.Errorf("by mean time, query 2 (1000ms/call) should lead, got %d", rows[0].key.queryID)
	}

	if rows := diffStatements(cur, nil, StmtSortTime); len(rows) != 5 {
		t.Errorf("without a baseline every entry is shown as cumulative, got %d rows", len(rows))
	}
}

func TestStmtBaselineAndPrune(t *testing.T) {
	v := NewStmtView()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if v.Baseline(t0) != nil {
		t.Error("no baseline before the first sample")
	}

	// One sample per second for 10 minutes
	for i := 0; i <= 600; i++ {
		v.Add(&stmtSnapshot{At: t0.Add(time.Duration(i) * time.Second)})
	}
	now := t0.Add(600 * time.Second)

	if n := len(v.history); n > 60 {
		t.Errorf("history should be downsampled, has %d samples", n)
	}
	if oldest := now.Sub(v.history[0].At); oldest > 5*time.Minute+20*time.Second {
		t.Errorf("samples older than the longest window should be dropped, oldest is %v", oldest)
	}

	for i, w := range stmtWindows {
		v.Window = i
		b := v.Baseline(now)
		age := now.Sub(b.At)
		if age < w || age > w+10*time.Second {
			t.Errorf("window %v: baseline is %v old", w, age)
		}
	}

	// A fresh view with a little data uses the oldest sample as a partial window
	v = NewStmtView()
	v.Add(&stmtSnapshot{At: t0})
	v.Add(&stmtSnapshot{At: t0.Add(3 * time.Second)})
	if b := v.Baseline(t0.Add(3 * time.Second)); b == nil || !b.At.Equal(t0) {
		t.Errorf("partial window baseline = %v", b)
	}
}

func TestStmtTextCache(t *testing.T) {
	c := &stmtTextCache{}
	if _, ok := c.get(1); ok {
		t.Error("empty cache returned a value")
	}
	c.put(1, "SELECT 1")
	if text, ok := c.get(1); !ok || text != "SELECT 1" {
		t.Errorf("get = %q, %v", text, ok)
	}
}

func TestRenderStatements(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := snap(t0, map[int64]stmtCounters{1: {Calls: 0}})
	cur := snap(t0.Add(30*time.Second), map[int64]stmtCounters{1: {Calls: 60, TotalMs: 1500, Rows: 60}})
	rows := diffStatements(cur, base, StmtSortTime)
	rows[0].query = "SELECT * FROM orders WHERE id = $1"

	out := renderStatements(rows, cur, stmtRequest{baseline: base, window: time.Minute, width: 200, height: 40})
	for _, want := range []string{"last 1m", "only 30s of data", "1.5s", "100.0%", "2.0", "SELECT * FROM orders"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestStatementsKeysCycleAndRefresh(t *testing.T) {
	m := newTestModel()
	m.selected = 2 // Statements
	m.lastQuery = m.queries[2]
	window := m.statements().Window

	_, cmd := m.Update(runes("w"))
	if m.statements().Window == window {
		t.Error("w should change the window")
	}
	if cmd == nil {
		t.Error("changing the window should re-run the query")
	}
	m.Update(runes("o"))
	if m.statements().Sort != StmtSortCalls {
		t.Errorf("o should cycle the sort, got %v", m.statements().Sort)
	}
}

func TestReservedBuiltinNames(t *testing.T) {
	if validateQueryName("Statements") == nil {
		t.Error("Statements is a built-in tab name")
	}
}
