package main

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lib/pq"
)

// ActiveViewMode represents the current mode of the Active view
type ActiveViewMode int

const (
	ActiveModeList ActiveViewMode = iota
	ActiveModeDetail
	ActiveModeConfirmTerminate
)

// ActiveProcess holds structured data for a single pg_stat_activity row
type ActiveProcess struct {
	PID             int
	Username        string
	Database        string
	ClientAddr      string
	ApplicationName string
	State           string
	QueryStart      string
	DurationSec     float64 // since query_start
	StateSec        float64 // since state_change, e.g. how long a session has sat idle in transaction
	XactSec         float64 // since xact_start; negative when there is no open transaction
	WaitEvent       string
	WaitEventType   string
	Query           string
	BackendType     string
	BlockedBy       []int // PIDs holding locks this process is waiting for

	// Derived from BlockedBy across the whole result by UpdateSelection
	Blocking      []int // PIDs waiting directly on this process
	BlockingTotal int   // sessions held up directly or transitively
}

// IsBlocked reports whether the process is waiting on another session's lock
func (p *ActiveProcess) IsBlocked() bool { return len(p.BlockedBy) > 0 }

// IsRootBlocker reports whether the process holds others up without itself
// being blocked: the session to look at (or terminate) first
func (p *ActiveProcess) IsRootBlocker() bool { return len(p.Blocking) > 0 && !p.IsBlocked() }

// ActiveSort is a sort order for the Active list
type ActiveSort int

const (
	SortDuration ActiveSort = iota
	SortBlocking
	SortState
	SortUser
	SortDatabase
	activeSortCount
)

func (s ActiveSort) String() string {
	return [...]string{"duration", "blocking", "state", "user", "database"}[s]
}

// activeRow is one visible line of the Active list
type activeRow struct {
	proc  *ActiveProcess
	depth int // nesting in lock-tree mode
}

// ActiveView holds the state for the interactive Active tab
type ActiveView struct {
	Processes     []ActiveProcess // everything fetched, in fetch order
	Rows          []activeRow     // what the list shows, after sort, filter and tree mode
	Loaded        bool            // at least one refresh has arrived
	SelectedIndex int             // index into Rows
	SelectedPID   int             // preserved across refreshes
	Mode          ActiveViewMode
	TerminateType string // "terminate" or "cancel"
	LastError     string
	ScrollOffset  int

	Sort      ActiveSort
	TreeMode  bool   // show only blocking chains, nested under their root blocker
	Filter    string // case-insensitive match on pid, user, database, state, app, client, query
	Filtering bool   // the filter prompt has focus

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

// activeProcessesSQL lists non-idle sessions plus any idle session that is
// blocking someone (e.g. one holding an advisory lock). pg_blocking_pids takes
// the lock manager's locks briefly, so it only runs for sessions actually
// waiting on a lock.
const activeProcessesSQL = `
	WITH a AS (
		SELECT *,
			CASE WHEN wait_event_type = 'Lock'
				THEN pg_blocking_pids(pid)
				ELSE '{}'::int[]
			END AS blocked_by
		FROM pg_stat_activity
		WHERE pid <> pg_backend_pid()
	)
	SELECT
		pid,
		COALESCE(usename, ''),
		COALESCE(datname, ''),
		COALESCE(client_addr::text, ''),
		COALESCE(application_name, ''),
		COALESCE(state, ''),
		COALESCE(query_start::text, ''),
		COALESCE(EXTRACT(EPOCH FROM (now() - query_start)), 0)::float8,
		COALESCE(EXTRACT(EPOCH FROM (now() - state_change)), 0)::float8,
		COALESCE(EXTRACT(EPOCH FROM (now() - xact_start)), -1)::float8,
		COALESCE(wait_event, ''),
		COALESCE(wait_event_type, ''),
		COALESCE(query, ''),
		COALESCE(backend_type, ''),
		blocked_by
	FROM a
	WHERE (state IS NOT NULL AND state <> 'idle')
	   OR pid IN (SELECT unnest(blocked_by) FROM a)
	ORDER BY query_start ASC NULLS LAST`

// FetchActiveProcesses queries pg_stat_activity for non-idle and blocking processes
func FetchActiveProcesses(ctx context.Context, db *sql.DB) ([]ActiveProcess, error) {
	rows, err := db.QueryContext(ctx, tagged(activeProcessesSQL))
	if err != nil {
		return nil, fmt.Errorf("failed to query pg_stat_activity: %w", err)
	}
	defer rows.Close()

	var processes []ActiveProcess
	for rows.Next() {
		var p ActiveProcess
		var blockedBy []int64
		if err := rows.Scan(
			&p.PID, &p.Username, &p.Database, &p.ClientAddr, &p.ApplicationName,
			&p.State, &p.QueryStart, &p.DurationSec, &p.StateSec, &p.XactSec,
			&p.WaitEvent, &p.WaitEventType, &p.Query, &p.BackendType,
			pq.Array(&blockedBy),
		); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		for _, pid := range blockedBy {
			p.BlockedBy = append(p.BlockedBy, int(pid))
		}
		processes = append(processes, p)
	}
	return processes, rows.Err()
}

// TerminateBackend calls pg_terminate_backend for the given PID
func TerminateBackend(ctx context.Context, db *sql.DB, pid int) error {
	var result bool
	err := db.QueryRowContext(ctx, tagged("SELECT pg_terminate_backend($1)"), pid).Scan(&result)
	if err != nil {
		return fmt.Errorf("pg_terminate_backend failed: %w", err)
	}
	if !result {
		return fmt.Errorf("pg_terminate_backend returned false for PID %d", pid)
	}
	return nil
}

// CancelBackend calls pg_cancel_backend for the given PID
func CancelBackend(ctx context.Context, db *sql.DB, pid int) error {
	var result bool
	err := db.QueryRowContext(ctx, tagged("SELECT pg_cancel_backend($1)"), pid).Scan(&result)
	if err != nil {
		return fmt.Errorf("pg_cancel_backend failed: %w", err)
	}
	if !result {
		return fmt.Errorf("pg_cancel_backend returned false for PID %d", pid)
	}
	return nil
}

// linkBlocking fills Blocking and BlockingTotal from every process's BlockedBy
func linkBlocking(processes []ActiveProcess) {
	index := make(map[int]int, len(processes))
	for i := range processes {
		processes[i].Blocking = nil
		processes[i].BlockingTotal = 0
		index[processes[i].PID] = i
	}
	for _, p := range processes {
		for _, blocker := range p.BlockedBy {
			if i, ok := index[blocker]; ok {
				processes[i].Blocking = append(processes[i].Blocking, p.PID)
			}
		}
	}
	for i := range processes {
		seen := map[int]bool{processes[i].PID: true}
		var walk func(pids []int)
		walk = func(pids []int) {
			for _, pid := range pids {
				if seen[pid] {
					continue // lock cycles exist briefly until the deadlock detector fires
				}
				seen[pid] = true
				processes[i].BlockingTotal++
				if j, ok := index[pid]; ok {
					walk(processes[j].Blocking)
				}
			}
		}
		walk(processes[i].Blocking)
	}
}

// BlockingSummary counts root blockers and the sessions they hold up
func (av *ActiveView) BlockingSummary() (roots, blocked int) {
	for i := range av.Processes {
		if av.Processes[i].IsRootBlocker() {
			roots++
		}
		if av.Processes[i].IsBlocked() {
			blocked++
		}
	}
	return roots, blocked
}

func (av *ActiveView) matchesFilter(p *ActiveProcess) bool {
	if av.Filter == "" {
		return true
	}
	needle := strings.ToLower(av.Filter)
	for _, field := range []string{
		strconv.Itoa(p.PID), p.Username, p.Database, p.State, p.ApplicationName, p.ClientAddr, p.Query,
	} {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

func (av *ActiveView) less(a, b *ActiveProcess) bool {
	switch av.Sort {
	case SortBlocking:
		if a.BlockingTotal != b.BlockingTotal {
			return a.BlockingTotal > b.BlockingTotal
		}
	case SortState:
		if a.State != b.State {
			return a.State < b.State
		}
	case SortUser:
		if a.Username != b.Username {
			return a.Username < b.Username
		}
	case SortDatabase:
		if a.Database != b.Database {
			return a.Database < b.Database
		}
	}
	return a.DurationSec > b.DurationSec // longest first, also the tiebreak
}

// rebuildRows recomputes the visible rows from Processes
func (av *ActiveView) rebuildRows() {
	var ordered []*ActiveProcess
	for i := range av.Processes {
		ordered = append(ordered, &av.Processes[i])
	}
	sort.SliceStable(ordered, func(i, j int) bool { return av.less(ordered[i], ordered[j]) })

	av.Rows = av.Rows[:0]
	if !av.TreeMode {
		for _, p := range ordered {
			if av.matchesFilter(p) {
				av.Rows = append(av.Rows, activeRow{proc: p})
			}
		}
		return
	}

	// Lock tree: each root blocker followed by its waiters, depth-first. A
	// filter keeps a whole chain when any session in it matches.
	byPID := make(map[int]*ActiveProcess, len(ordered))
	for _, p := range ordered {
		byPID[p.PID] = p
	}
	for _, root := range ordered {
		if !root.IsRootBlocker() {
			continue
		}
		var chain []activeRow
		seen := map[int]bool{}
		var walk func(p *ActiveProcess, depth int)
		walk = func(p *ActiveProcess, depth int) {
			if seen[p.PID] {
				return
			}
			seen[p.PID] = true
			chain = append(chain, activeRow{proc: p, depth: depth})
			waiters := make([]*ActiveProcess, 0, len(p.Blocking))
			for _, pid := range p.Blocking {
				if w, ok := byPID[pid]; ok {
					waiters = append(waiters, w)
				}
			}
			sort.SliceStable(waiters, func(i, j int) bool { return av.less(waiters[i], waiters[j]) })
			for _, w := range waiters {
				walk(w, depth+1)
			}
		}
		walk(root, 0)

		keep := av.Filter == ""
		for _, r := range chain {
			keep = keep || av.matchesFilter(r.proc)
		}
		if keep {
			av.Rows = append(av.Rows, chain...)
		}
	}
}

// UpdateSelection replaces the process list after a refresh, keeping the
// selected PID selected when it is still visible
func (av *ActiveView) UpdateSelection(processes []ActiveProcess) {
	av.Processes = processes
	av.Loaded = true
	linkBlocking(av.Processes)

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

	av.refreshRows()
}

// refreshRows rebuilds Rows and re-finds the selected PID, clamping the index
// when that PID is no longer visible
func (av *ActiveView) refreshRows() {
	av.rebuildRows()

	if av.SelectedPID != 0 {
		for i, r := range av.Rows {
			if r.proc.PID == av.SelectedPID {
				av.SelectedIndex = i
				av.ensureVisible()
				return
			}
		}
	}

	if av.SelectedIndex >= len(av.Rows) {
		av.SelectedIndex = len(av.Rows) - 1
	}
	if av.SelectedIndex < 0 {
		av.SelectedIndex = 0
	}
	if len(av.Rows) > 0 {
		av.SelectedPID = av.Rows[av.SelectedIndex].proc.PID
	}
	av.ensureVisible()
}

// SelectedProcess returns the currently selected process, or nil
func (av *ActiveView) SelectedProcess() *ActiveProcess {
	if av.SelectedIndex < 0 || av.SelectedIndex >= len(av.Rows) {
		return nil
	}
	return av.Rows[av.SelectedIndex].proc
}

// MoveSelection moves the cursor by delta rows, staying in bounds
func (av *ActiveView) MoveSelection(delta int) {
	if len(av.Rows) == 0 {
		return
	}
	av.SelectedIndex = max(0, min(len(av.Rows)-1, av.SelectedIndex+delta))
	av.SelectedPID = av.Rows[av.SelectedIndex].proc.PID
	av.ensureVisible()
}

// CycleSort switches to the next sort order
func (av *ActiveView) CycleSort() {
	av.Sort = (av.Sort + 1) % activeSortCount
	av.refreshRows()
}

// SetTreeMode turns the lock-tree view on or off
func (av *ActiveView) SetTreeMode(on bool) {
	av.TreeMode = on
	av.refreshRows()
}

// SetFilter changes the list filter
func (av *ActiveView) SetFilter(filter string) {
	av.Filter = filter
	av.refreshRows()
}

// pageSize returns how many rows fit in a page given terminal height
func (av *ActiveView) pageSize(height int) int {
	// Reserve lines for header row + footer hints + borders
	ps := height - 12
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
	// The bottom edge depends on terminal height and is clamped at render time
}

// formatSeconds renders an elapsed time compactly: 850ms, 42s, 3m 5s, 2h 10m
func formatSeconds(sec float64) string {
	if sec < 0 {
		return ""
	}
	if sec < 1 {
		return fmt.Sprintf("%dms", int(sec*1000))
	}
	return formatDuration(int(sec))
}

// truncate shortens s to at most max terminal cells, marking the cut with
// "~". It measures display width, so multi-byte and wide characters are
// never split.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= max {
		return s
	}
	return ansi.Truncate(s, max, "~")
}

// padRight pads s with spaces to a display width of w
func padRight(s string, w int) string {
	if pad := w - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// fitWidth truncates or pads s to exactly w terminal cells
func fitWidth(s string, w int) string {
	return padRight(truncate(s, w), w)
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
