package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	activeTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	activeHeaderStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7C3AED"))
	activeRowStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	activeBlockerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // root blocker
	activeBlockedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // waiting on a lock
	activeSelectedStyle = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("235"))
	activeDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	activeWarnStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	activeErrStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
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

// timeCell shows how long the session has been in its current state: query
// runtime when active, time sitting idle when idle in transaction
func timeCell(p *ActiveProcess) string {
	if strings.HasPrefix(p.State, "idle in transaction") {
		return formatSeconds(p.StateSec)
	}
	return formatSeconds(p.DurationSec)
}

// stateCell shortens the long idle-in-transaction states to fit the column
func stateCell(state string) string {
	switch state {
	case "idle in transaction":
		return "idle in xact"
	case "idle in transaction (aborted)":
		return "idle in xact!"
	}
	return state
}

// RenderActiveList renders the process list table with selection highlighting
func RenderActiveList(av *ActiveView, width, height int) string {
	var b strings.Builder

	title := fmt.Sprintf("Active Connections (%d)", len(av.Processes))
	if av.TreeMode {
		title = "Lock Tree"
	}
	b.WriteString(activeTitleStyle.Render(title))
	var tags []string
	if !av.TreeMode {
		tags = append(tags, "sort: "+av.Sort.String())
	}
	if av.Filter != "" || av.Filtering {
		tags = append(tags, fmt.Sprintf("filter: %q", av.Filter))
	}
	if len(tags) > 0 {
		b.WriteString(activeDimStyle.Render("  · " + strings.Join(tags, " · ")))
	}
	b.WriteString("\n")

	if roots, blocked := av.BlockingSummary(); roots > 0 || blocked > 0 {
		msg := fmt.Sprintf("⚠ %d blocked by %d root blocker(s)", blocked, roots)
		if !av.TreeMode {
			msg += " — press b for the lock tree"
		}
		b.WriteString(activeWarnStyle.Render(msg))
	}
	b.WriteString("\n")

	if av.Filtering {
		b.WriteString("Filter: " + av.Filter + "█\n")
	}

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
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(empty) + "\n\n")
		b.WriteString(activeDimStyle.Render(activeFooter(av)))
		return b.String()
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

	pidW, userW, stateW, timeW, waitW, lockW := 8, 12, 13, 9, 16, 11
	if av.TreeMode {
		pidW = 14
	}
	fixedW := pidW + userW + stateW + timeW + waitW + lockW + 6 // separators
	queryW := max(20, width-fixedW-4)

	header := strings.Join([]string{
		fitWidth("PID", pidW), fitWidth("User", userW), fitWidth("State", stateW),
		fitWidth("Time", timeW), fitWidth("Wait Event", waitW), fitWidth("Locks", lockW),
		fitWidth("Query", queryW),
	}, " ")
	b.WriteString(activeHeaderStyle.Render(truncate(header, width-2)))
	b.WriteString("\n")

	end := min(av.ScrollOffset+pageSize, len(av.Rows))
	for i := av.ScrollOffset; i < end; i++ {
		r := av.Rows[i]
		p := r.proc

		pid := strconv.Itoa(p.PID)
		if r.depth > 0 {
			pid = strings.Repeat("  ", r.depth-1) + "└ " + pid
		}
		line := strings.Join([]string{
			fitWidth(pid, pidW),
			fitWidth(p.Username, userW),
			fitWidth(stateCell(p.State), stateW),
			fitWidth(timeCell(p), timeW),
			fitWidth(p.WaitEvent, waitW),
			fitWidth(lockCell(p), lockW),
			fitWidth(scrubNewlines(p.Query), queryW),
		}, " ")

		style := activeRowStyle
		switch {
		case p.IsRootBlocker():
			style = activeBlockerStyle
		case p.IsBlocked():
			style = activeBlockedStyle
		}
		if i == av.SelectedIndex {
			style = style.Inherit(activeSelectedStyle)
		}
		b.WriteString(style.Render(truncate(line, width-2)))
		b.WriteString("\n")
	}

	if len(av.Rows) > pageSize {
		b.WriteString(activeDimStyle.Render(fmt.Sprintf("\n  showing %d-%d of %d", av.ScrollOffset+1, end, len(av.Rows))))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(activeDimStyle.Render(activeFooter(av)))

	if av.LastError != "" {
		b.WriteString("\n")
		b.WriteString(activeErrStyle.Render("  Error: " + av.LastError))
	}

	return b.String()
}

func activeFooter(av *ActiveView) string {
	if av.Filtering {
		return "  type to filter  enter: keep filter  esc: clear filter"
	}
	tree := "b: lock tree"
	if av.TreeMode {
		tree = "b: all sessions"
	}
	back := ""
	if av.Filter != "" || av.TreeMode {
		back = "  esc: back"
	}
	return "  ↑/↓: select  enter: details  t: terminate  c: cancel  " + tree + "  o: sort  /: filter" + back + "  q: quit"
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

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("244")).
		Width(18)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(activeTitleStyle.Render(fmt.Sprintf("Process Detail - PID %d", proc.PID)))

	if av.DetailCompleted {
		completedStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("8"))
		b.WriteString("  " + completedStyle.Render("(process completed)"))
	}

	b.WriteString("\n\n")

	xact := "none"
	if proc.XactSec >= 0 {
		xact = formatSeconds(proc.XactSec)
	}
	fields := []struct{ label, value string }{
		{"PID", strconv.Itoa(proc.PID)},
		{"Username", proc.Username},
		{"Database", proc.Database},
		{"Application", proc.ApplicationName},
		{"Client Address", proc.ClientAddr},
		{"State", proc.State},
		{"In State For", formatSeconds(proc.StateSec)},
		{"Transaction Age", xact},
		{"Backend Type", proc.BackendType},
		{"Query Start", proc.QueryStart},
		{"Query Duration", formatSeconds(proc.DurationSec)},
		{"Wait Event", proc.WaitEvent},
		{"Wait Event Type", proc.WaitEventType},
	}
	if proc.IsBlocked() {
		fields = append(fields, struct{ label, value string }{"Blocked By", pidList(proc.BlockedBy)})
	}
	if len(proc.Blocking) > 0 {
		fields = append(fields, struct{ label, value string }{
			"Blocking", fmt.Sprintf("%s (%d sessions in total)", pidList(proc.Blocking), proc.BlockingTotal),
		})
	}

	for _, f := range fields {
		b.WriteString(labelStyle.Render(f.label+":") + " " + valueStyle.Render(f.value) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(labelStyle.Render("Query:"))
	b.WriteString("\n")

	queryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("251")).
		Width(width - 4)

	b.WriteString(queryStyle.Render(scrubNewlines(proc.Query)))
	b.WriteString("\n\n")

	if av.DetailCompleted {
		b.WriteString(activeDimStyle.Render("  y: copy query  esc: back to list"))
	} else {
		b.WriteString(activeDimStyle.Render("  y: copy query  t: terminate  c: cancel query  esc: back to list"))
	}

	if av.CopyStatus != "" {
		copyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		b.WriteString("  " + copyStyle.Render(av.CopyStatus))
	}

	if av.LastError != "" {
		b.WriteString("\n")
		b.WriteString(activeErrStyle.Render("  Error: " + av.LastError))
	}

	return b.String()
}

// RenderTerminateConfirm renders the confirmation prompt
func RenderTerminateConfirm(av *ActiveView) string {
	proc := av.DetailProcess
	if proc == nil {
		return "No process selected"
	}

	action := "Terminate"
	if av.TerminateType == "cancel" {
		action = "Cancel query on"
	}

	var b strings.Builder
	b.WriteString(activeWarnStyle.Render(fmt.Sprintf("%s PID %d? (y/n)", action, proc.PID)))
	b.WriteString("\n\n")
	b.WriteString(activeDimStyle.Render(fmt.Sprintf("  User: %s  Database: %s", proc.Username, proc.Database)))
	b.WriteString("\n")
	if proc.BlockingTotal > 0 {
		b.WriteString(activeDimStyle.Render(fmt.Sprintf("  Holding up %d other session(s)", proc.BlockingTotal)))
		b.WriteString("\n")
	}
	b.WriteString(activeDimStyle.Render(fmt.Sprintf("  Query: %s", truncate(scrubNewlines(proc.Query), 60))))
	return b.String()
}
