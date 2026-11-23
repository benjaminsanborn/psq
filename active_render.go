package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	activeTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	activeHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(colorDim)
	activeRowStyle    = styleFg
	activeDimStyle    = styleDim
	activeWarnStyle   = styleRed.Bold(true)
	activeErrStyle    = styleRed
)

// lockCell summarizes a process's place in a blocking chain
func lockCell(p *ActiveProcess) string {
	switch {
	case p.IsRootBlocker():
		return fmt.Sprintf("blocks %d", p.BlockingTotal)
	case p.IsBlocked():
		s := "by " + strconv.Itoa(p.BlockedBy[0])
		if n := len(p.BlockedBy); n > 1 {
			s += fmt.Sprintf(" +%d", n-1)
		}
		return s
	}
	return ""
}

// lockStyle colours the Locks cell: root blockers red, waiters yellow
func lockStyle(p *ActiveProcess) lipgloss.Style {
	switch {
	case p.IsRootBlocker():
		return styleRed.Bold(true)
	case p.IsBlocked():
		return styleYellow
	}
	return styleFg
}

// timeCell shows how long the session has been in its current state: query
// runtime when active, time sitting idle when idle in transaction
func timeCell(p *ActiveProcess) string {
	// Clamp at zero: a state that changed during psq's own query can read as
	// a hair in the future
	if strings.HasPrefix(p.State, "idle in transaction") {
		return formatSeconds(max(0, p.StateSec))
	}
	return formatSeconds(max(0, p.DurationSec))
}

// stateCell shortens the long idle-in-transaction states to fit the column
func stateCell(state string) string {
	switch state {
	case "idle in transaction":
		return "idle in tx"
	case "idle in transaction (aborted)":
		return "idle in tx!"
	}
	return state
}

// seg is a run of text in one style, for rows whose background changes
// when selected
type seg struct {
	text  string
	style lipgloss.Style
}

// renderSegs renders a row of segments, padding it to width. A selected row
// gets the selection background across its full width.
func renderSegs(segs []seg, width int, selected bool) string {
	var b strings.Builder
	used := 0
	for _, s := range segs {
		text := s.text
		if used+len([]rune(text)) > width {
			text = truncate(text, width-used)
		}
		b.WriteString(onSelection(s.style, selected).Render(text))
		used += lipgloss.Width(text)
		if used >= width {
			break
		}
	}
	if used < width {
		b.WriteString(onSelection(lipgloss.NewStyle(), selected).Render(strings.Repeat(" ", width-used)))
	}
	return b.String()
}

// activeStatus is the line above the list: what's shown, how it's sorted
// and filtered, and any blocking or result of the last action
func activeStatus(av *ActiveView) string {
	var parts []string
	if av.TreeMode {
		parts = append(parts, styleBold.Render("Lock tree"))
		roots, blocked := av.BlockingSummary()
		if roots > 0 {
			parts = append(parts, styleDim.Render(fmt.Sprintf("%d root blocker(s) · %d waiting", roots, blocked)))
		}
	} else {
		noun := "sessions"
		if len(av.Processes) == 1 {
			noun = "session"
		}
		parts = append(parts, styleDim.Render(fmt.Sprintf("%d %s · sort: %s", len(av.Processes), noun, av.Sort.String())))
	}
	if av.Filter != "" && !av.Filtering {
		parts = append(parts, styleDim.Render(fmt.Sprintf("filter: %q", av.Filter)))
	}
	line := " " + strings.Join(parts, styleDim.Render(" · "))

	// The result of the last action comes first so it's never cut off
	if av.Notice != "" {
		line += "   " + styleGreen.Render(av.Notice)
	}
	if roots, blocked := av.BlockingSummary(); !av.TreeMode && (roots > 0 || blocked > 0) {
		line += "   " + activeWarnStyle.Render(fmt.Sprintf("⚠ %d blocked by %d root blocker(s)", blocked, roots)) +
			styleDim.Render(" · ") + styleKey.Render("b") + styleDim.Render(" lock tree")
	}
	return line
}

// RenderActiveList renders the session list, or the lock tree, with the
// selected row highlighted
func RenderActiveList(av *ActiveView, width, height int) string {
	var b strings.Builder
	b.WriteString(activeStatus(av) + "\n")

	if av.Filtering {
		b.WriteString(" " + styleBlue.Render("/ ") + av.Filter + "█\n")
	}
	b.WriteString("\n")

	if len(av.Rows) == 0 {
		var empty string
		switch {
		case av.Filter != "":
			empty = "No sessions match the filter"
		case av.TreeMode:
			empty = "✓ No blocked sessions"
		default:
			empty = "No active (non-idle) connections"
		}
		style := styleDim
		if av.TreeMode && av.Filter == "" {
			style = styleGreen
		}
		b.WriteString(" " + style.Render(empty) + "\n")
		return b.String() + activeErrorLine(av)
	}

	pageSize := av.pageSize(height)
	if av.SelectedIndex < av.ScrollOffset {
		av.ScrollOffset = av.SelectedIndex
	}
	if av.SelectedIndex >= av.ScrollOffset+pageSize {
		av.ScrollOffset = av.SelectedIndex - pageSize + 1
	}
	if av.ScrollOffset < 0 {
		av.ScrollOffset = 0
	}
	end := min(av.ScrollOffset+pageSize, len(av.Rows))

	if av.TreeMode {
		b.WriteString(renderLockTree(av, av.ScrollOffset, end, width))
	} else {
		b.WriteString(renderSessionTable(av, av.ScrollOffset, end, width))
	}

	if len(av.Rows) > pageSize {
		b.WriteString("\n" + activeDimStyle.Render(fmt.Sprintf(" showing %d-%d of %d", av.ScrollOffset+1, end, len(av.Rows))) + "\n")
	}
	return b.String() + activeErrorLine(av)
}

func activeErrorLine(av *ActiveView) string {
	if av.LastError == "" {
		return ""
	}
	return "\n" + activeErrStyle.Render(" Error: "+av.LastError)
}

// renderSessionTable draws the Active list: one row per session
func renderSessionTable(av *ActiveView, start, end, width int) string {
	type col struct {
		name  string
		width int
	}
	cols := []col{{"PID", 8}, {"USER", 11}, {"STATE", 13}, {"TIME", 9}}
	showWait := width >= 120 // room for the wait event without squeezing the query
	if showWait {
		cols = append(cols, col{"WAIT EVENT", 17})
	}
	cols = append(cols, col{"LOCKS", 12})
	fixed := 1
	for _, c := range cols {
		fixed += c.width
	}
	queryW := max(10, width-fixed-1)

	var b strings.Builder
	header := " "
	for _, c := range cols {
		header += fitWidth(c.name, c.width)
	}
	b.WriteString(activeHeaderStyle.Render(header+"QUERY") + "\n")

	for i := start; i < end; i++ {
		p := av.Rows[i].proc
		selected := i == av.SelectedIndex
		segs := []seg{
			{" ", styleFg},
			{fitWidth(strconv.Itoa(p.PID), 8), styleFg.Bold(selected)},
			{fitWidth(p.Username, 10) + " ", styleFg},
			{fitWidth(stateCell(p.State), 13), stateStyle(p.State)},
			{fmt.Sprintf("%7s  ", timeCell(p)), styleFg},
		}
		if showWait {
			segs = append(segs, seg{fitWidth(p.WaitEvent, 17), styleDim})
		}
		segs = append(segs,
			seg{fitWidth(lockCell(p), 12), lockStyle(p)},
			seg{fitWidth(scrubNewlines(p.Query), queryW), styleDim},
		)
		b.WriteString(renderSegs(segs, width, selected) + "\n")
	}
	return b.String()
}

// treePrefixes works out the tree drawing for each lock-tree row: the
// branch in front of the session line, and the continuation in front of its
// query line
func treePrefixes(rows []activeRow) (heads, conts []string) {
	heads = make([]string, len(rows))
	conts = make([]string, len(rows))
	var more []bool // more[d]: the open branch at depth d has later siblings
	for i, r := range rows {
		d := r.depth
		last := true
		for j := i + 1; j < len(rows); j++ {
			if rows[j].depth < d {
				break
			}
			if rows[j].depth == d {
				last = false
				break
			}
		}
		for len(more) <= d {
			more = append(more, false)
		}
		more = more[:d+1]
		more[d] = !last

		var bars strings.Builder
		for k := 1; k < d; k++ {
			if more[k] {
				bars.WriteString("│  ")
			} else {
				bars.WriteString("   ")
			}
		}
		switch {
		case d == 0:
			heads[i] = "● "
			conts[i] = "  "
		case last:
			heads[i] = bars.String() + "└─ "
			conts[i] = bars.String() + "     "
		default:
			heads[i] = bars.String() + "├─ "
			conts[i] = bars.String() + "│    "
		}
	}
	return heads, conts
}

// renderLockTree draws each blocking chain: the root blocker in red, the
// sessions waiting on it in yellow, nested under what they wait on, each
// with its query underneath
func renderLockTree(av *ActiveView, start, end, width int) string {
	heads, conts := treePrefixes(av.Rows)
	var b strings.Builder
	for i := start; i < end; i++ {
		p := av.Rows[i].proc
		selected := i == av.SelectedIndex
		color := styleYellow
		if av.Rows[i].depth == 0 {
			color = styleRed
		}
		prefixStyle := styleDim
		if av.Rows[i].depth == 0 {
			prefixStyle = styleRed
		}

		head := []seg{
			{" " + heads[i], prefixStyle},
			{strconv.Itoa(p.PID), color.Bold(true)},
			{"  " + p.Username, color},
			{"  " + stateCell(p.State), color},
			{"  " + timeCell(p), color},
		}
		if p.BlockingTotal > 0 {
			head = append(head, seg{fmt.Sprintf("  blocks %d", p.BlockingTotal), color.Bold(true)})
		}
		if p.IsBlocked() && p.WaitEvent != "" {
			head = append(head, seg{"  waiting on " + p.WaitEvent, styleDim})
		}
		b.WriteString(renderSegs(head, width, selected) + "\n")

		query := []seg{
			{" " + conts[i], styleDim},
			{scrubNewlines(p.Query), styleDim},
		}
		b.WriteString(renderSegs(query, width, selected) + "\n")
	}
	b.WriteString("\n " + styleRed.Render("red") + styleDim.Render(" root blocker   ") +
		styleYellow.Render("yellow") + styleDim.Render(" waiting, nested under what it waits on") + "\n")
	return b.String()
}

// activeHints are the bottom-bar keys for the Active tab's current mode
func activeHints(av *ActiveView) []keyHint {
	switch {
	case av.Filtering:
		return []keyHint{{"type", "to filter"}, {"enter", "keep filter"}, {"esc", "clear filter"}}
	case av.Mode == ActiveModeConfirmTerminate:
		action := "terminate"
		if av.TerminateType == "cancel" {
			action = "cancel query"
		}
		return []keyHint{{"y", action}, {"n/esc", "keep it running"}}
	case av.Mode == ActiveModeDetail && av.DetailCompleted:
		return []keyHint{{"y", "copy query"}, {"esc", "back"}}
	case av.Mode == ActiveModeDetail:
		return []keyHint{{"y", "copy query"}, {"t", "terminate"}, {"c", "cancel query"}, {"esc", "back"}}
	}
	tree := keyHint{"b", "lock tree"}
	if av.TreeMode {
		tree = keyHint{"b", "all sessions"}
	}
	hints := []keyHint{{"↑/↓", "select"}, {"enter", "details"}, {"t", "terminate"}, {"c", "cancel"}, tree}
	if av.Filter != "" || av.TreeMode {
		hints = append(hints, keyHint{"esc", "back"})
	}
	return append(hints, keyHint{"o", "sort"}, keyHint{"/", "filter"}, keyHint{"←/→", "tabs"}, keyHint{"q", "quit"})
}

func pidList(pids []int) string {
	parts := make([]string, len(pids))
	for i, pid := range pids {
		parts[i] = strconv.Itoa(pid)
	}
	return strings.Join(parts, ", ")
}

// RenderActiveDetail renders full details for the selected process
func RenderActiveDetail(av *ActiveView, width int) string {
	proc := av.DetailProcess
	if proc == nil {
		return "No process selected"
	}

	labelStyle := lipgloss.NewStyle().Foreground(colorDim).Width(18)

	var b strings.Builder
	b.WriteString(" " + activeTitleStyle.Render(fmt.Sprintf("Session %d", proc.PID)))
	if av.DetailCompleted {
		b.WriteString("  " + styleDim.Render("(process completed)"))
	}
	b.WriteString("\n\n")

	xact := "none"
	if proc.XactSec >= 0 {
		xact = formatSeconds(proc.XactSec)
	}
	fields := []struct {
		label, value string
		style        lipgloss.Style
	}{
		{"PID", strconv.Itoa(proc.PID), styleBold},
		{"Username", proc.Username, styleFg},
		{"Database", proc.Database, styleFg},
		{"Application", proc.ApplicationName, styleFg},
		{"Client Address", proc.ClientAddr, styleFg},
		{"State", proc.State, stateStyle(proc.State)},
		{"In State For", formatSeconds(proc.StateSec), styleFg},
		{"Transaction Age", xact, styleFg},
		{"Backend Type", proc.BackendType, styleFg},
		{"Query Start", proc.QueryStart, styleFg},
		{"Query Duration", formatSeconds(proc.DurationSec), styleFg},
		{"Wait Event", proc.WaitEvent, styleFg},
		{"Wait Event Type", proc.WaitEventType, styleFg},
	}
	if proc.IsBlocked() {
		fields = append(fields, struct {
			label, value string
			style        lipgloss.Style
		}{"Blocked By", pidList(proc.BlockedBy), styleYellow})
	}
	if len(proc.Blocking) > 0 {
		fields = append(fields, struct {
			label, value string
			style        lipgloss.Style
		}{"Blocking", fmt.Sprintf("%s (%d sessions in total)", pidList(proc.Blocking), proc.BlockingTotal), styleRed.Bold(true)})
	}

	for _, f := range fields {
		b.WriteString(" " + labelStyle.Render(f.label) + " " + f.style.Render(f.value) + "\n")
	}

	b.WriteString("\n " + labelStyle.Render("Query") + "\n")
	queryStyle := styleFg.Width(max(20, width-4)).PaddingLeft(1)
	b.WriteString(queryStyle.Render(scrubNewlines(proc.Query)))
	b.WriteString("\n")

	if av.CopyStatus != "" {
		b.WriteString("\n " + styleGreen.Render(av.CopyStatus))
	}
	return b.String() + activeErrorLine(av)
}

// RenderTerminateConfirm renders the terminate/cancel confirmation dialog,
// drawn over the list
func RenderTerminateConfirm(av *ActiveView, width int) string {
	proc := av.DetailProcess
	if proc == nil {
		return "No process selected"
	}

	title, call, action, accent := "Terminate session?", "pg_terminate_backend", "terminate", styleRed
	if av.TerminateType == "cancel" {
		title, call, action, accent = "Cancel query?", "pg_cancel_backend", "cancel query", styleYellow
	}

	boxW := max(40, min(66, width-6))
	inner := boxW - 4
	details := []string{proc.Username, proc.Database, stateCell(proc.State), timeCell(proc)}
	var kept []string
	for _, d := range details {
		if d != "" {
			kept = append(kept, d)
		}
	}

	lines := []string{
		"",
		styleBold.Render(fmt.Sprintf("%s(%d)", call, proc.PID)),
		styleDim.Render(truncate(strings.Join(kept, " · "), inner)),
	}
	if proc.BlockingTotal > 0 {
		lines = append(lines, styleYellow.Render(fmt.Sprintf("holding up %d other session(s)", proc.BlockingTotal)))
	}
	lines = append(lines, styleDim.Render(truncate(scrubNewlines(proc.Query), inner)), "")

	keys := accent.Bold(true).Render("y") + " " + action + "   " + styleBold.Render("n") + " keep it running"
	pad := max(0, inner-lipgloss.Width(keys))
	lines = append(lines, strings.Repeat(" ", pad)+keys)

	return modalBox(title, lines, boxW, accent)
}
