package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/lib/pq"
)

// StatementsQuery is the built-in tab showing recent pg_stat_statements activity
func StatementsQuery() Query {
	return Query{
		Name:        "Statements",
		Description: "Top statements over the last 10s/1m/5m (pg_stat_statements)",
		SQL:         "-- built-in interactive view",
	}
}

// IsStatementsTab checks if the given query name is the Statements tab
func IsStatementsTab(queryName string) bool {
	return queryName == "Statements"
}

// stmtKey identifies a pg_stat_statements entry. Entries that differ only by
// "toplevel" (PG 14+) are summed together.
type stmtKey struct {
	userID, dbID, queryID int64
}

// stmtCounters are the cumulative counters psq diffs between samples
type stmtCounters struct {
	Calls      int64
	TotalMs    float64
	Rows       int64
	SharedHit  int64
	SharedRead int64
}

func (c stmtCounters) sub(o stmtCounters) stmtCounters {
	return stmtCounters{
		Calls:      c.Calls - o.Calls,
		TotalMs:    c.TotalMs - o.TotalMs,
		Rows:       c.Rows - o.Rows,
		SharedHit:  c.SharedHit - o.SharedHit,
		SharedRead: c.SharedRead - o.SharedRead,
	}
}

// stmtSnapshot is one sample of every entry's counters. It is never modified
// after creation, so the fetch goroutine can read a baseline safely.
type stmtSnapshot struct {
	At    time.Time
	Stats map[stmtKey]stmtCounters
}

// stmtWindows are the selectable look-back windows
var stmtWindows = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute}

// StmtSort is a sort order for the Statements tab
type StmtSort int

const (
	StmtSortTime StmtSort = iota
	StmtSortCalls
	StmtSortRows
	StmtSortMean
	StmtSortReads
	stmtSortCount
)

func (s StmtSort) String() string {
	return [...]string{"total time", "calls", "rows", "mean time", "disk reads"}[s]
}

// stmtTextCache remembers query texts by queryid, which is much cheaper than
// asking pg_stat_statements to read its text file on every refresh. It is
// shared with fetch goroutines, hence the lock.
type stmtTextCache struct {
	mu    sync.Mutex
	texts map[int64]string
}

func (c *stmtTextCache) get(id int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.texts[id]
	return t, ok
}

func (c *stmtTextCache) put(id int64, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.texts == nil {
		c.texts = map[int64]string{}
	}
	c.texts[id] = text
}

// StmtView holds the Statements tab's state; unlike the Active view it lives
// for the whole session, because its value is the history it accumulates
type StmtView struct {
	history []*stmtSnapshot // oldest first, downsampled by prune
	Window  int             // index into stmtWindows
	Sort    StmtSort
	texts   *stmtTextCache
}

func NewStmtView() *StmtView {
	return &StmtView{Window: 1, texts: &stmtTextCache{}}
}

// Add records a sample and drops ones no window can use any more
func (v *StmtView) Add(s *stmtSnapshot) {
	v.history = append(v.history, s)
	v.prune(s.At)
}

// prune keeps every sample from the last 15s, then one per 10s up to the
// longest window, so memory stays bounded however long psq runs
func (v *StmtView) prune(now time.Time) {
	longest := stmtWindows[len(stmtWindows)-1]
	kept := v.history[:0]
	lastBucket := int64(-1)
	for _, s := range v.history {
		age := now.Sub(s.At)
		if age > longest+20*time.Second {
			continue
		}
		if age > 15*time.Second {
			bucket := s.At.Unix() / 10
			if bucket == lastBucket {
				continue
			}
			lastBucket = bucket
		}
		kept = append(kept, s)
	}
	v.history = kept
}

// Baseline picks the sample to diff against for the current window: the
// newest one at least a window old, else the oldest available (a partial
// window). It returns nil before the first sample.
func (v *StmtView) Baseline(now time.Time) *stmtSnapshot {
	if len(v.history) == 0 {
		return nil
	}
	cutoff := now.Add(-stmtWindows[v.Window])
	var best *stmtSnapshot
	for _, s := range v.history {
		if !s.At.After(cutoff) {
			best = s
		}
	}
	if best == nil {
		best = v.history[0]
	}
	return best
}

func (v *StmtView) CycleWindow() { v.Window = (v.Window + 1) % len(stmtWindows) }
func (v *StmtView) CycleSort()   { v.Sort = (v.Sort + 1) % stmtSortCount }

// stmtRow is one statement's activity within the window
type stmtRow struct {
	key   stmtKey
	delta stmtCounters
	query string
}

func (r stmtRow) meanMs() float64 {
	if r.delta.Calls == 0 {
		return 0
	}
	return r.delta.TotalMs / float64(r.delta.Calls)
}

// stmtResult is what a Statements refresh hands back to the main goroutine
type stmtResult struct {
	snapshot *stmtSnapshot
	rendered string
}

type stmtDataMsg struct {
	seq    int
	result stmtResult
}

var errNoPgStatStatements = errors.New("pg_stat_statements is not installed in this database")

// stmtSource describes where pg_stat_statements lives and what it calls
// execution time, which changed in PostgreSQL 13
type stmtSource struct {
	schema  string
	timeCol string
}

func detectStmtSource(ctx context.Context, db *sql.DB) (stmtSource, error) {
	var src stmtSource
	err := db.QueryRowContext(ctx, `
		SELECT n.nspname
		FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
		WHERE e.extname = 'pg_stat_statements'`).Scan(&src.schema)
	if errors.Is(err, sql.ErrNoRows) {
		return src, errNoPgStatStatements
	}
	if err != nil {
		return src, err
	}
	var version int
	if err := db.QueryRowContext(ctx, "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		return src, err
	}
	src.timeCol = "total_time"
	if version >= 130000 {
		src.timeCol = "total_exec_time"
	}
	return src, nil
}

// fetchStmtSnapshot reads counters without query texts, which keeps each
// refresh cheap even with thousands of entries
func fetchStmtSnapshot(ctx context.Context, db *sql.DB, src stmtSource) (*stmtSnapshot, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT userid::int8, dbid::int8, queryid, calls, %s, rows, shared_blks_hit, shared_blks_read
		FROM %s.pg_stat_statements(false)
		WHERE queryid IS NOT NULL`, src.timeCol, pq.QuoteIdentifier(src.schema)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snap := &stmtSnapshot{At: time.Now(), Stats: map[stmtKey]stmtCounters{}}
	for rows.Next() {
		var k stmtKey
		var c stmtCounters
		if err := rows.Scan(&k.userID, &k.dbID, &k.queryID, &c.Calls, &c.TotalMs, &c.Rows, &c.SharedHit, &c.SharedRead); err != nil {
			return nil, err
		}
		prev := snap.Stats[k]
		snap.Stats[k] = stmtCounters{
			Calls:      prev.Calls + c.Calls,
			TotalMs:    prev.TotalMs + c.TotalMs,
			Rows:       prev.Rows + c.Rows,
			SharedHit:  prev.SharedHit + c.SharedHit,
			SharedRead: prev.SharedRead + c.SharedRead,
		}
	}
	return snap, rows.Err()
}

// diffStatements returns statements with activity since baseline, sorted.
// A nil baseline means cumulative totals. An entry whose counters went
// backwards was reset or evicted, so its current value is the activity.
func diffStatements(cur, baseline *stmtSnapshot, order StmtSort) []stmtRow {
	var out []stmtRow
	for k, c := range cur.Stats {
		d := c
		if baseline != nil {
			if b, ok := baseline.Stats[k]; ok && c.Calls >= b.Calls {
				d = c.sub(b)
			}
		}
		if d.Calls <= 0 {
			continue
		}
		out = append(out, stmtRow{key: k, delta: d})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		var x, y float64
		switch order {
		case StmtSortCalls:
			x, y = float64(a.delta.Calls), float64(b.delta.Calls)
		case StmtSortRows:
			x, y = float64(a.delta.Rows), float64(b.delta.Rows)
		case StmtSortMean:
			x, y = a.meanMs(), b.meanMs()
		case StmtSortReads:
			x, y = float64(a.delta.SharedRead), float64(b.delta.SharedRead)
		default:
			x, y = a.delta.TotalMs, b.delta.TotalMs
		}
		if x != y {
			return x > y
		}
		return a.key.queryID < b.key.queryID
	})
	return out
}

// fillQueryTexts looks up texts for rows the cache doesn't know yet
func fillQueryTexts(ctx context.Context, db *sql.DB, src stmtSource, cache *stmtTextCache, rows []stmtRow) error {
	var missing []int64
	for _, r := range rows {
		if _, ok := cache.get(r.key.queryID); !ok {
			missing = append(missing, r.key.queryID)
		}
	}
	if len(missing) > 0 {
		res, err := db.QueryContext(ctx, fmt.Sprintf(`
			SELECT DISTINCT ON (queryid) queryid, query
			FROM %s.pg_stat_statements(true)
			WHERE queryid = ANY($1)`, pq.QuoteIdentifier(src.schema)), pq.Array(missing))
		if err != nil {
			return err
		}
		defer res.Close()
		for res.Next() {
			var id int64
			var text sql.NullString
			if err := res.Scan(&id, &text); err != nil {
				return err
			}
			cache.put(id, text.String)
		}
		if err := res.Err(); err != nil {
			return err
		}
	}
	for i := range rows {
		rows[i].query, _ = cache.get(rows[i].key.queryID)
	}
	return nil
}

// stmtRequest is everything a Statements refresh needs, captured on the
// main goroutine
type stmtRequest struct {
	baseline *stmtSnapshot
	window   time.Duration
	order    StmtSort
	texts    *stmtTextCache
	width    int
	height   int
}

// fetchStatements samples pg_stat_statements and renders the window
func fetchStatements(ctx context.Context, db *sql.DB, req stmtRequest) (stmtResult, error) {
	src, err := detectStmtSource(ctx, db)
	if errors.Is(err, errNoPgStatStatements) {
		return stmtResult{rendered: renderNoPgStatStatements()}, nil
	}
	if err != nil {
		return stmtResult{}, err
	}

	snap, err := fetchStmtSnapshot(ctx, db, src)
	if err != nil {
		if strings.Contains(err.Error(), "shared_preload_libraries") {
			return stmtResult{rendered: renderNoPgStatStatements()}, nil
		}
		return stmtResult{}, fmt.Errorf("failed to read pg_stat_statements: %w", err)
	}

	rows := diffStatements(snap, req.baseline, req.order)
	limit := max(5, req.height-12)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	if err := fillQueryTexts(ctx, db, src, req.texts, rows); err != nil {
		return stmtResult{}, fmt.Errorf("failed to read statement texts: %w", err)
	}

	return stmtResult{snapshot: snap, rendered: renderStatements(rows, snap, req)}, nil
}

func renderNoPgStatStatements() string {
	title := activeTitleStyle.Render("Statements")
	body := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(
		"pg_stat_statements isn't available in this database.\n\n" +
			"To enable it:\n" +
			"  1. add pg_stat_statements to shared_preload_libraries and restart\n" +
			"     (on RDS/Aurora: set it in the DB parameter group)\n" +
			"  2. CREATE EXTENSION pg_stat_statements;")
	return title + "\n\n" + body
}

func renderStatements(rows []stmtRow, snap *stmtSnapshot, req stmtRequest) string {
	var b strings.Builder
	b.WriteString(activeTitleStyle.Render("Top Statements"))

	var span string
	if req.baseline == nil {
		span = "since stats were last reset (collecting a baseline…)"
	} else {
		measured := snap.At.Sub(req.baseline.At)
		span = "last " + formatWindow(req.window)
		if measured < req.window-time.Second {
			span += fmt.Sprintf(" (only %s of data so far)", formatSeconds(measured.Seconds()))
		}
	}
	b.WriteString(activeDimStyle.Render("  · " + span + " · sort: " + req.order.String()))
	b.WriteString("\n\n")

	var total float64
	for _, r := range rows {
		total += r.delta.TotalMs
	}
	seconds := 0.0
	if req.baseline != nil {
		seconds = snap.At.Sub(req.baseline.At).Seconds()
	}

	cols := []struct {
		name  string
		width int
	}{{"Total", 10}, {"% Time", 7}, {"Calls", 9}, {"Calls/s", 8}, {"Mean", 9}, {"Rows", 9}, {"Reads", 8}}
	fixed := 0
	var header []string
	for _, c := range cols {
		header = append(header, fitWidth(c.name, c.width))
		fixed += c.width + 1
	}
	queryW := max(20, req.width-fixed-4)
	header = append(header, fitWidth("Query", queryW))
	b.WriteString(activeHeaderStyle.Render(truncate(strings.Join(header, " "), req.width-2)))
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(activeDimStyle.Render("  no statements ran in this window") + "\n")
	}
	for _, r := range rows {
		pct, rate := "", ""
		if total > 0 {
			pct = fmt.Sprintf("%.1f%%", 100*r.delta.TotalMs/total)
		}
		if seconds > 0 {
			rate = fmt.Sprintf("%.1f", float64(r.delta.Calls)/seconds)
		}
		cells := []string{
			formatMs(r.delta.TotalMs), pct, fmt.Sprint(r.delta.Calls), rate,
			formatMs(r.meanMs()), fmt.Sprint(r.delta.Rows), fmt.Sprint(r.delta.SharedRead),
		}
		var line []string
		for i, c := range cols {
			line = append(line, fitWidth(cells[i], c.width))
		}
		line = append(line, fitWidth(scrubNewlines(r.query), queryW))
		b.WriteString(activeRowStyle.Render(truncate(strings.Join(line, " "), req.width-2)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(activeDimStyle.Render("  w: window (10s/1m/5m)  o: sort  Reads = shared blocks read from disk or OS cache"))
	return b.String()
}

func formatWindow(d time.Duration) string {
	return formatDuration(int(d.Seconds()))
}

// formatMs renders milliseconds compactly: 0.42ms, 87ms, 3.2s, 4m 10s
func formatMs(ms float64) string {
	switch {
	case ms < 10:
		return fmt.Sprintf("%.2fms", ms)
	case ms < 1000:
		return fmt.Sprintf("%.0fms", ms)
	case ms < 60000:
		return fmt.Sprintf("%.1fs", ms/1000)
	default:
		return formatDuration(int(ms / 1000))
	}
}
