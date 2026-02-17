package main

import (
	"database/sql"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ActiveViewMode represents the current mode of the Active view
type ActiveViewMode int

const (
	ActiveModeList             ActiveViewMode = iota
	ActiveModeDetail
	ActiveModeConfirmTerminate
)

// ActiveProcess holds structured data for a single pg_stat_activity row
type ActiveProcess struct {
	PID           int
	Username      string
	Database      string
	ClientAddr    string
	State         string
	QueryStart    string
	Duration      string
	WaitEvent     string
	WaitEventType string
	Query         string
	BackendType   string
}

// ActiveView holds the state for the interactive Active tab
type ActiveView struct {
	Processes     []ActiveProcess
	SelectedIndex int
	SelectedPID   int    // preserved across refreshes
	Mode          ActiveViewMode
	TerminateType string // "terminate" or "cancel"
	LastError     string
	ScrollOffset  int
	DetailProcess   *ActiveProcess // snapshot of the process when entering detail/confirm mode
	DetailCompleted bool           // true when the detail PID is no longer in pg_stat_activity
	CopyStatus      string         // brief feedback after clipboard copy ("Copied!" or error)
}

// ActiveQuery returns the hardcoded Active query (used for tab display; actual data fetched structurally)
func ActiveQuery() Query {
	return Query{
		Name:        "Active",
		Description: "Interactive active connections view",
		SQL:         "-- built-in interactive view",
	}
}

// IsActiveTab checks if the given query name is the Active tab
func IsActiveTab(queryName string) bool {
	return queryName == "Active"
}

// NewActiveView creates a new ActiveView in list mode
func NewActiveView() *ActiveView {
	return &ActiveView{
		Mode: ActiveModeList,
	}
}

var collapseSpaces = regexp.MustCompile(`\s{2,}`)

// scrubNewlines replaces \n, \r with spaces and collapses runs of whitespace
func scrubNewlines(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = collapseSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// FetchActiveProcesses queries pg_stat_activity for non-idle, non-self processes
func FetchActiveProcesses(db *sql.DB) ([]ActiveProcess, error) {
	query := `
		SELECT
			pid,
			COALESCE(usename, '') AS usename,
			COALESCE(datname, '') AS datname,
			COALESCE(client_addr::text, '') AS client_addr,
			COALESCE(state, '') AS state,
			COALESCE(query_start::text, '') AS query_start,
			COALESCE(LEFT((NOW() - query_start)::text, 15), '') AS duration,
			COALESCE(wait_event, '') AS wait_event,
			COALESCE(wait_event_type, '') AS wait_event_type,
			COALESCE(query, '') AS query,
			COALESCE(backend_type, '') AS backend_type
		FROM pg_stat_activity
		WHERE pid != pg_backend_pid()
		  AND state IS NOT NULL
		  AND state != 'idle'
		ORDER BY query_start ASC NULLS LAST`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query pg_stat_activity: %w", err)
	}
	defer rows.Close()

	var processes []ActiveProcess
	for rows.Next() {
		var p ActiveProcess
		if err := rows.Scan(
			&p.PID, &p.Username, &p.Database, &p.ClientAddr,
			&p.State, &p.QueryStart, &p.Duration,
			&p.WaitEvent, &p.WaitEventType, &p.Query, &p.BackendType,
		); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		processes = append(processes, p)
	}
	return processes, rows.Err()
}

// TerminateBackend calls pg_terminate_backend for the given PID
func TerminateBackend(db *sql.DB, pid int) error {
	var result bool
	err := db.QueryRow("SELECT pg_terminate_backend($1)", pid).Scan(&result)
	if err != nil {
		return fmt.Errorf("pg_terminate_backend failed: %w", err)
	}
	if !result {
		return fmt.Errorf("pg_terminate_backend returned false for PID %d", pid)
	}
	return nil
}

// CancelBackend calls pg_cancel_backend for the given PID
func CancelBackend(db *sql.DB, pid int) error {
	var result bool
	err := db.QueryRow("SELECT pg_cancel_backend($1)", pid).Scan(&result)
	if err != nil {
		return fmt.Errorf("pg_cancel_backend failed: %w", err)
	}
	if !result {
		return fmt.Errorf("pg_cancel_backend returned false for PID %d", pid)
	}
	return nil
}

// UpdateSelection preserves selected PID across data refreshes
func (av *ActiveView) UpdateSelection(processes []ActiveProcess) {
	av.Processes = processes

	// If we're in detail/confirm mode, keep DetailProcess live while the PID exists
	if av.DetailProcess != nil && !av.DetailCompleted {
		found := false
		for _, p := range processes {
			if p.PID == av.DetailProcess.PID {
				updated := p
				av.DetailProcess = &updated
				found = true
				break
			}
		}
		if !found {
			av.DetailCompleted = true
		}
	}

	if av.SelectedPID != 0 {
		for i, p := range processes {
			if p.PID == av.SelectedPID {
				av.SelectedIndex = i
				av.ensureVisible()
				return
			}
		}
	}

	// PID not found; clamp index
	if av.SelectedIndex >= len(processes) {
		if len(processes) > 0 {
			av.SelectedIndex = len(processes) - 1
		} else {
			av.SelectedIndex = 0
		}
	}
	if len(processes) > 0 {
		av.SelectedPID = processes[av.SelectedIndex].PID
	}
	av.ensureVisible()
}

// SelectedProcess returns the currently selected process, or nil
func (av *ActiveView) SelectedProcess() *ActiveProcess {
	if len(av.Processes) == 0 || av.SelectedIndex >= len(av.Processes) {
		return nil
	}
	return &av.Processes[av.SelectedIndex]
}

// pageSize returns how many rows fit in a page given terminal height
func (av *ActiveView) pageSize(height int) int {
	// Reserve lines for header row + footer hints + borders
	ps := height - 10
	if ps < 5 {
		ps = 5
	}
	return ps
}

// ensureVisible adjusts ScrollOffset so the selected row is on screen
func (av *ActiveView) ensureVisible() {
	if av.SelectedIndex < av.ScrollOffset {
		av.ScrollOffset = av.SelectedIndex
	}
	// We'll use a default page size; actual clamping happens at render time
}

// RenderActiveList renders the process list table with selection highlighting
func RenderActiveList(av *ActiveView, width, height int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	if len(av.Processes) == 0 {
		return titleStyle.Render("Active Connections") + "\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No active (non-idle) connections") + "\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("esc: quit")
	}

	pageSize := av.pageSize(height)

	// Clamp scroll offset so selected row is visible
	if av.SelectedIndex < av.ScrollOffset {
		av.ScrollOffset = av.SelectedIndex
	}
	if av.SelectedIndex >= av.ScrollOffset+pageSize {
		av.ScrollOffset = av.SelectedIndex - pageSize + 1
	}
	if av.ScrollOffset < 0 {
		av.ScrollOffset = 0
	}

	// Column widths
	pidW := 8
	userW := 12
	stateW := 12
	durationW := 12
	waitW := 16

	// Query column gets the remaining width
	fixedW := pidW + userW + stateW + durationW + waitW + 7 // 7 for separators
	queryW := width - fixedW - 4
	if queryW < 20 {
		queryW = 20
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED"))

	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Background(lipgloss.Color("235"))

	rowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Active Connections (%d)", len(av.Processes))))
	b.WriteString("\n\n")

	// Header
	header := fmt.Sprintf("%-*s %-*s %-*s %-*s %-*s %-*s",
		pidW, "PID", userW, "User",
		stateW, "State", durationW, "Duration", waitW, "Wait Event",
		queryW, "Query")
	b.WriteString(headerStyle.Render(truncate(header, width-2)))
	b.WriteString("\n")

	// Rows
	end := av.ScrollOffset + pageSize
	if end > len(av.Processes) {
		end = len(av.Processes)
	}

	for i := av.ScrollOffset; i < end; i++ {
		p := av.Processes[i]
		queryTrunc := scrubNewlines(p.Query)
		if len(queryTrunc) > queryW {
			queryTrunc = queryTrunc[:queryW-1] + "~"
		}

		line := fmt.Sprintf("%-*d %-*s %-*s %-*s %-*s %-*s",
			pidW, p.PID,
			userW, truncate(p.Username, userW),
			stateW, truncate(p.State, stateW),
			durationW, truncate(p.Duration, durationW),
			waitW, truncate(p.WaitEvent, waitW),
			queryW, queryTrunc)

		if i == av.SelectedIndex {
			b.WriteString(selectedStyle.Render(truncate(line, width-2)))
		} else {
			b.WriteString(rowStyle.Render(truncate(line, width-2)))
		}
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(av.Processes) > pageSize {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  showing %d-%d of %d", av.ScrollOffset+1, end, len(av.Processes))))
		b.WriteString("\n")
	}

	// Footer hints
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  up/down: select  enter: details  t: terminate  c: cancel query  esc: quit"))

	if av.LastError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		b.WriteString("\n")
		b.WriteString(errStyle.Render("  Error: " + av.LastError))
	}

	return b.String()
}

// RenderActiveDetail renders full details for the selected process
func RenderActiveDetail(av *ActiveView, width int) string {
	proc := av.DetailProcess
	if proc == nil {
		return "No process selected"
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("244")).
		Width(18)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Process Detail - PID %d", proc.PID)))

	if av.DetailCompleted {
		completedStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("8"))
		b.WriteString("  " + completedStyle.Render("(process completed)"))
	}

	b.WriteString("\n\n")

	fields := []struct{ label, value string }{
		{"PID", fmt.Sprintf("%d", proc.PID)},
		{"Username", proc.Username},
		{"Database", proc.Database},
		{"Client Address", proc.ClientAddr},
		{"State", proc.State},
		{"Backend Type", proc.BackendType},
		{"Query Start", proc.QueryStart},
		{"Duration", proc.Duration},
		{"Wait Event", proc.WaitEvent},
		{"Wait Event Type", proc.WaitEventType},
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
		b.WriteString(dimStyle.Render("  y: copy query  esc: back to list"))
	} else {
		b.WriteString(dimStyle.Render("  y: copy query  t: terminate  c: cancel query  esc: back to list"))
	}

	if av.CopyStatus != "" {
		copyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		b.WriteString("  " + copyStyle.Render(av.CopyStatus))
	}

	if av.LastError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		b.WriteString("\n")
		b.WriteString(errStyle.Render("  Error: " + av.LastError))
	}

	return b.String()
}

// RenderTerminateConfirm renders the confirmation prompt
func RenderTerminateConfirm(av *ActiveView) string {
	proc := av.DetailProcess
	if proc == nil {
		return "No process selected"
	}

	warnStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("9"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	action := "Terminate"
	if av.TerminateType == "cancel" {
		action = "Cancel query on"
	}

	var b strings.Builder
	b.WriteString(warnStyle.Render(fmt.Sprintf("%s PID %d? (y/n)", action, proc.PID)))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("  User: %s  Database: %s", proc.Username, proc.Database)))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("  Query: %s", truncate(scrubNewlines(proc.Query), 60))))
	return b.String()
}

// truncate shortens a string to max length
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return "~"
	}
	return s[:max-1] + "~"
}

// clipboardResultMsg is sent after a clipboard copy attempt
type clipboardResultMsg struct {
	err error
}

// copyToClipboard copies text to the system clipboard
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
