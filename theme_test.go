package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTreePrefixes(t *testing.T) {
	// 10 ─┬─ 20 ── 30
	//     └─ 40
	rows := []activeRow{{depth: 0}, {depth: 1}, {depth: 2}, {depth: 1}}
	heads, conts := treePrefixes(rows)
	wantHeads := []string{"● ", "├─ ", "│  └─ ", "└─ "}
	wantConts := []string{"  ", "│    ", "│       ", "     "}
	for i := range rows {
		if heads[i] != wantHeads[i] || conts[i] != wantConts[i] {
			t.Errorf("row %d: head %q cont %q, want %q %q", i, heads[i], conts[i], wantHeads[i], wantConts[i])
		}
	}
}

func TestRenderLockTreeNestsWaiters(t *testing.T) {
	av := NewActiveView()
	av.UpdateSelection(lockChain())
	av.SetTreeMode(true)
	out := ansi.Strip(RenderActiveList(av, 120, 40))
	for _, want := range []string{"Lock tree", "● 10", "blocks 3", "├─ ", "└─ "} {
		if !strings.Contains(out, want) {
			t.Errorf("lock tree missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "report") {
		t.Error("the lock tree should only show blocking chains")
	}
}

func TestRenderHintsDropsWhatDoesntFit(t *testing.T) {
	hints := []keyHint{{"?", "help"}, {"q", "quit"}, {"b", "lock tree"}}
	out := ansi.Strip(renderHints(hints, 18))
	if !strings.Contains(out, "q quit") || strings.Contains(out, "lock tree") {
		t.Errorf("hints = %q", out)
	}
	if w := ansi.StringWidth(renderHints(hints, 18)); w > 18 {
		t.Errorf("hints are %d wide, want at most 18", w)
	}
}

func TestTabBarKeepsCurrentTabInView(t *testing.T) {
	m := newTestModel()
	m.width = 40
	for _, name := range []string{"Connection States", "Lock Information", "Replication Lag", "Vacuum Progress"} {
		m.queries = append(m.queries, Query{Name: name})
	}
	m.selected = len(m.queries) - 1
	out := ansi.Strip(m.renderNormalMode())
	if !strings.Contains(out, "Vacuum Progress") || !strings.Contains(out, "‹") {
		t.Errorf("tab bar = %q", out)
	}
	if strings.Contains(out, "Home") {
		t.Error("tabs scrolled out of view should be hidden")
	}
	if w := ansi.StringWidth(out); w > m.width {
		t.Errorf("tab bar is %d wide, want at most %d", w, m.width)
	}
}

func TestOverlayKeepsBothSides(t *testing.T) {
	base := strings.Repeat("abcdefghijklmnopqrst\n", 6)
	out := strings.Split(ansi.Strip(overlay(base, "XXXX\nXXXX", 20)), "\n")
	row := out[2]
	if row != "abcdefghXXXXmnopqrst" {
		t.Errorf("overlaid row = %q", row)
	}
}

func TestBlockedBody(t *testing.T) {
	if out := ansi.Strip(blockedBody(blockInfo{}, 34)); !strings.Contains(out, "all clear") {
		t.Errorf("no waiters: %q", out)
	}
	out := ansi.Strip(blockedBody(blockInfo{waiting: 3, longestWaitSec: 42, roots: []int{48213}}, 34))
	if !strings.Contains(out, "3 waiting on pid 48213") || !strings.Contains(out, "42s") {
		t.Errorf("one root: %q", out)
	}
	if out := ansi.Strip(blockedBody(blockInfo{waiting: 2}, 34)); !strings.Contains(out, "waiting on each other") {
		t.Errorf("lock cycle: %q", out)
	}
}
