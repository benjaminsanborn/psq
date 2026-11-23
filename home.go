package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/lib/pq"
	zone "github.com/lrstanley/bubblezone"
)

// HomeQuery returns the hardcoded Home query
func HomeQuery() Query {
	return Query{
		Name:        "Home",
		Description: "Activity state counts overview",
		SQL:         "SELECT state, COUNT(*) as count FROM pg_stat_activity WHERE state IS NOT NULL GROUP BY state ORDER BY count DESC;",
	}
}

// IsHomeTab checks if the given query is the Home tab
func IsHomeTab(queryName string) bool {
	return queryName == "Home"
}

// homeData is everything the Home dashboard reads from the database in one
// refresh. It's plain data: the main goroutine turns the commit counter into
// a rate against its own history and renders the widgets at the current
// terminal width.
type homeData struct {
	states         map[string]int // session count by pg_stat_activity state
	maxConnections int

	commits   float64
	commitsAt time.Time
	commitsOK bool

	cache cacheInfo
	repl  replInfo
	block blockInfo
	xact  xactInfo
}

// fetchHomeData runs the Home dashboard queries. It must not touch the Model.
func fetchHomeData(ctx context.Context, db *sql.DB, query string) (homeData, error) {
	states, err := getStateCounts(ctx, db, query)
	if err != nil {
		return homeData{}, err
	}
	data := homeData{states: states}

	if commits, at, maxConn, err := GetTransactionCommits(ctx, db); err == nil {
		data.commits, data.commitsAt, data.maxConnections, data.commitsOK = commits, at, maxConn, true
	} else {
		// Without commit stats the rest of the dashboard isn't rendered
		return data, nil
	}

	data.cache = getCacheInfo(ctx, db)
	data.repl = getReplInfo(ctx, db)
	data.block = getBlockInfo(ctx, db)
	data.xact = getLongestXact(ctx, db)
	return data, nil
}

// getStateCounts runs the Home query: one row per state with its count
func getStateCounts(ctx context.Context, db *sql.DB, query string) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, tagged(query))
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	states := map[string]int{}
	for rows.Next() {
		var state sql.NullString
		var count sql.NullString
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		n, _ := strconv.ParseFloat(count.String, 64)
		states[state.String] += int(n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}
	return states, nil
}

// renderHome folds a Home refresh into the commit-rate history and renders
// the dashboard. Call it only from the main goroutine.
func (m *Model) renderHome(data homeData) string {
	if !data.commitsOK {
		colW := homeColumnWidth(m.width)
		return " " + titledBox("Connections", connectionsBody(data, colW-4), colW, 5, styleDim)
	}

	// Commits per second, using DB timestamps for accurate elapsed time
	var commitsPerSec float64
	if m.lastCommits > 0 && !m.lastCommitTime.IsZero() {
		if elapsed := data.commitsAt.Sub(m.lastCommitTime).Seconds(); elapsed > 0 {
			commitsPerSec = (data.commits - m.lastCommits) / elapsed
		}
	}
	m.lastCommits = data.commits
	m.lastCommitTime = data.commitsAt
	m.sparklineData.AddPoint(commitsPerSec, data.commitsAt)

	return RenderHomeDashboard(data, m.sparklineData, m.width)
}

// homeColumnWidth is the outer width of each of the two widget columns
func homeColumnWidth(width int) int {
	return max(30, (width-3)/2)
}

// RenderHomeDashboard lays the widgets out in two columns:
//
//	Connections      Transactions/sec
//	Cache hit ratio  Replication
//	Blocked queries  Longest transaction
func RenderHomeDashboard(data homeData, tps *SparklineData, width int) string {
	colW := homeColumnWidth(width)
	inner := colW - 4
	blocked := data.block.waiting > 0

	blockBorder := styleDim
	if blocked {
		blockBorder = styleRed
	}
	tpsStyle := styleBlue
	if blocked {
		tpsStyle = styleYellow // throughput under a pile-up is worth a second look
	}

	rows := [][2]string{
		{
			titledBox("Connections", connectionsBody(data, inner), colW, 5, styleDim),
			titledBox("Transactions/sec", tpsBody(tps, inner, tpsStyle), colW, 5, styleDim),
		},
		{
			titledBox("Cache hit ratio", cacheBody(data.cache, inner), colW, 4, styleDim),
			titledBox("Replication", replBody(data.repl, inner), colW, 4, styleDim),
		},
		{
			zone.Mark(homeBlockingZone, titledBox("Blocked queries", blockedBody(data.block, inner), colW, 4, blockBorder)),
			titledBox("Longest transaction", xactBody(data.xact, inner), colW, 4, styleDim),
		},
	}

	var out []string
	for _, r := range rows {
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, " ", r[0], "  ", r[1]))
	}
	return strings.Join(out, "\n")
}

// homeBlockingZone is the clickable area of the Home blocked-queries widget
const homeBlockingZone = "home_blocking"

// label renders "name value" with the name dimmed
func label(name, value string, style lipgloss.Style) string {
	return styleDim.Render(name+" ") + style.Render(value)
}

func connectionsBody(data homeData, inner int) string {
	total := 0
	for _, n := range data.states {
		total += n
	}
	active := data.states["active"]
	idle := data.states["idle"]
	idleTx := data.states["idle in transaction"] + data.states["idle in transaction (aborted)"]

	var lines []string
	if data.maxConnections > 0 {
		count := fmt.Sprintf(" %d / %d", total, data.maxConnections)
		frac := float64(total) / float64(data.maxConnections)
		fill := styleBlue
		switch {
		case frac >= 0.9:
			fill = styleRed
		case frac >= 0.7:
			fill = styleYellow
		}
		lines = append(lines, gauge(frac, inner-len(count), fill)+count)
	} else {
		lines = append(lines, styleBold.Render(strconv.Itoa(total))+styleDim.Render(" sessions"))
	}

	idleTxStyle := styleFg
	if idleTx > 0 {
		idleTxStyle = styleYellow
	}
	lines = append(lines, label("active", strconv.Itoa(active), styleFg)+"  "+
		label("idle", strconv.Itoa(idle), styleFg)+"  "+
		label("idle in tx", strconv.Itoa(idleTx), idleTxStyle))

	waitStyle := styleFg
	if data.block.waiting > 0 {
		waitStyle = styleRed.Bold(true)
	}
	lines = append(lines, label("waiting on a lock", strconv.Itoa(data.block.waiting), waitStyle))
	return strings.Join(lines, "\n")
}

func tpsBody(tps *SparklineData, inner int, style lipgloss.Style) string {
	if len(tps.Values) < 2 {
		return styleDim.Render("collecting…")
	}
	// The first point has no previous sample to diff against
	values := tps.Values[1:]

	lo, hi, sum := values[0], values[0], 0.0
	for _, v := range values {
		lo, hi, sum = min(lo, v), max(hi, v), sum+v
	}
	// Leave some floor under the lowest bar so small swings still read as swings
	lo = max(0, lo-(hi-lo))
	if hi-lo < 1 {
		hi = lo + 1
	}
	top, bottom := sparkline(values, inner, lo, hi, style)
	now := values[len(values)-1]
	avg := sum / float64(len(values))
	return top + "\n" + bottom + "\n" +
		label("now", formatRate(now)+"/s", styleBold) + "   " + label("avg", formatRate(avg)+"/s", styleFg)
}

// formatRate renders a per-second rate: 0.4, 12, 1,284
func formatRate(v float64) string {
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	s := strconv.FormatInt(int64(v+0.5), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// ---- Cache hit ratio -----------------------------------------------------

type cacheInfo struct {
	ratio, heap, index       float64
	ratioOK, heapOK, indexOK bool
}

func getCacheInfo(ctx context.Context, db *sql.DB) cacheInfo {
	var info cacheInfo
	if ratio, ok, err := GetCacheHitRatio(ctx, db); err == nil && ok {
		info.ratio, info.ratioOK = ratio, true
	}
	var heap, index sql.NullFloat64
	err := db.QueryRowContext(ctx, tagged(`
		SELECT
			ROUND(SUM(heap_blks_hit) * 100.0 / NULLIF(SUM(heap_blks_hit) + SUM(heap_blks_read), 0), 2),
			ROUND(SUM(idx_blks_hit) * 100.0 / NULLIF(SUM(idx_blks_hit) + SUM(idx_blks_read), 0), 2)
		FROM pg_statio_user_tables`)).Scan(&heap, &index)
	if err == nil {
		info.heap, info.heapOK = heap.Float64, heap.Valid
		info.index, info.indexOK = index.Float64, index.Valid
	}
	return info
}

// GetCacheHitRatio queries the database for cache hit ratio percentage
func GetCacheHitRatio(ctx context.Context, db *sql.DB) (float64, bool, error) {
	var ratio sql.NullFloat64
	err := db.QueryRowContext(ctx, tagged("SELECT ROUND(SUM(blks_hit) * 100.0 / NULLIF(SUM(blks_hit) + SUM(blks_read), 0), 2) as ratio FROM pg_stat_database")).Scan(&ratio)
	if err != nil {
		return 0, false, fmt.Errorf("failed to query cache hit ratio: %w", err)
	}
	if !ratio.Valid {
		return 0, false, nil
	}
	return ratio.Float64, true, nil
}

// ratioStyle colours a hit ratio: green from 99%, yellow from 95%, else red
func ratioStyle(ratio float64) lipgloss.Style {
	switch {
	case ratio >= 99:
		return styleGreen
	case ratio >= 95:
		return styleYellow
	}
	return styleRed
}

func cacheBody(c cacheInfo, inner int) string {
	if !c.ratioOK {
		return styleDim.Render("N/A")
	}
	style := ratioStyle(c.ratio)
	value := fmt.Sprintf("%.2f%%", c.ratio)
	line := style.Bold(true).Render(value) + "  " + gauge(c.ratio/100, inner-len(value)-2, style)

	var parts []string
	if c.heapOK {
		parts = append(parts, fmt.Sprintf("heap %.1f%%", c.heap))
	}
	if c.indexOK {
		parts = append(parts, fmt.Sprintf("index %.1f%%", c.index))
	}
	detail := "no table stats yet"
	if len(parts) > 0 {
		detail = strings.Join(parts, "  ")
	}
	return line + "\n" + styleDim.Render(detail)
}

// ---- Replication ---------------------------------------------------------

// replica is one standby streaming from this server
type replica struct {
	name  string
	state string
	lag   float64
	lagOK bool
}

type replInfo struct {
	slots    ReplicationLagInfo // recovery state, and slot lag on a primary
	replicas []replica          // from pg_stat_replication, on a primary
	err      string
}

func getReplInfo(ctx context.Context, db *sql.DB) replInfo {
	slots, err := GetReplicationLag(ctx, db)
	if err != nil {
		return replInfo{err: err.Error()}
	}
	info := replInfo{slots: slots}
	if slots.IsReplica {
		return info
	}

	rows, err := db.QueryContext(ctx, tagged(`
		SELECT
			COALESCE(NULLIF(application_name, ''), client_addr::text, 'replica'),
			COALESCE(state, ''),
			EXTRACT(EPOCH FROM replay_lag)::float8
		FROM pg_stat_replication
		ORDER BY 1`))
	if err != nil {
		return info // no permission: fall back to the slot summary
	}
	defer rows.Close()
	for rows.Next() {
		var r replica
		var lag sql.NullFloat64
		if err := rows.Scan(&r.name, &r.state, &lag); err != nil {
			continue
		}
		r.lag, r.lagOK = lag.Float64, lag.Valid
		info.replicas = append(info.replicas, r)
	}
	return info
}

// ReplicationLagInfo holds replication lag details
type ReplicationLagInfo struct {
	IsReplica      bool
	LagSeconds     int
	SlotName       string
	SlotType       string // "streaming" or "logical"
	TotalSlots     int
	StreamingSlots int
	LogicalSlots   int
	Valid          bool
}

// GetReplicationLag queries replication lag — replica replay lag or primary slot lag
func GetReplicationLag(ctx context.Context, db *sql.DB) (ReplicationLagInfo, error) {
	var isInRecovery bool
	if err := db.QueryRowContext(ctx, tagged("SELECT pg_is_in_recovery()")).Scan(&isInRecovery); err != nil {
		return ReplicationLagInfo{}, fmt.Errorf("failed to check recovery state: %w", err)
	}

	if isInRecovery {
		// Replica: show replay lag
		var lagSeconds sql.NullInt64
		err := db.QueryRowContext(ctx, tagged("SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))::int")).Scan(&lagSeconds)
		if err != nil || !lagSeconds.Valid {
			return ReplicationLagInfo{IsReplica: true}, err
		}
		return ReplicationLagInfo{
			IsReplica:  true,
			LagSeconds: int(lagSeconds.Int64),
			Valid:      true,
		}, nil
	}

	// Primary: find the highest lag across all replication slots
	info := ReplicationLagInfo{IsReplica: false}

	// Count slots by type
	rows, err := db.QueryContext(ctx, tagged(`
		SELECT slot_type, COUNT(*)
		FROM pg_replication_slots
		GROUP BY slot_type`))
	if err != nil {
		return info, nil // no slots or no permission — not an error
	}
	defer rows.Close()
	for rows.Next() {
		var slotType string
		var count int
		if err := rows.Scan(&slotType, &count); err != nil {
			continue
		}
		info.TotalSlots += count
		if slotType == "physical" {
			info.StreamingSlots = count
		} else if slotType == "logical" {
			info.LogicalSlots = count
		}
	}

	if info.TotalSlots == 0 {
		return info, nil
	}

	// Get the slot with the highest lag (pg_wal_lsn_diff between current WAL and confirmed flush)
	var slotName sql.NullString
	var slotType sql.NullString
	var lagBytes sql.NullInt64
	err = db.QueryRowContext(ctx, tagged(`
		SELECT slot_name, slot_type,
			pg_wal_lsn_diff(pg_current_wal_lsn(), COALESCE(confirmed_flush_lsn, restart_lsn))::bigint AS lag_bytes
		FROM pg_replication_slots
		WHERE active OR NOT active
		ORDER BY lag_bytes DESC NULLS LAST
		LIMIT 1`)).Scan(&slotName, &slotType, &lagBytes)
	if err != nil || !slotName.Valid {
		return info, nil
	}

	info.SlotName = slotName.String
	if slotType.Valid {
		if slotType.String == "physical" {
			info.SlotType = "streaming"
		} else {
			info.SlotType = slotType.String
		}
	}
	if lagBytes.Valid {
		info.LagSeconds = int(lagBytes.Int64) // actually bytes, we'll format differently
		info.Valid = true
	}

	return info, nil
}

// lagStyle colours a replay lag in seconds: green under 1s, yellow to 10s
func lagStyle(sec float64) lipgloss.Style {
	switch {
	case sec < 1:
		return styleGreen
	case sec <= 10:
		return styleYellow
	}
	return styleRed
}

// formatLag renders a replay lag: 0.2s, 14s, 3m 5s
func formatLag(sec float64) string {
	if sec < 10 {
		return strconv.FormatFloat(sec, 'f', 1, 64) + "s"
	}
	return formatDuration(int(sec))
}

func replBody(r replInfo, inner int) string {
	if r.err != "" {
		return styleDim.Render(truncate("unavailable: "+r.err, inner))
	}
	s := r.slots
	if s.IsReplica {
		if !s.Valid {
			return styleDim.Render("this server is a replica") + "\n" + styleDim.Render("replay lag N/A")
		}
		return styleDim.Render("this server is a replica") + "\n" +
			label("replay lag", formatLag(float64(s.LagSeconds)), lagStyle(float64(s.LagSeconds)).Bold(true))
	}

	if len(r.replicas) > 0 {
		var lines []string
		nameW := max(8, inner-20)
		for i, rep := range r.replicas {
			if i == 1 && len(r.replicas) > 2 {
				lines = append(lines, styleDim.Render(fmt.Sprintf("+%d more replicas", len(r.replicas)-1)))
				break
			}
			lag, style := "–", styleDim
			if rep.lagOK {
				lag, style = formatLag(rep.lag), lagStyle(rep.lag).Bold(true)
			}
			stateStyle := styleGreen
			if rep.state != "streaming" {
				stateStyle = styleYellow
			}
			lines = append(lines, fitWidth(rep.name, nameW)+" "+style.Render(fmt.Sprintf("%6s", lag))+"  "+stateStyle.Render(rep.state))
		}
		return strings.Join(lines, "\n")
	}

	if s.TotalSlots == 0 {
		return styleDim.Render("no replicas or slots")
	}

	summary := fmt.Sprintf("%d slots", s.TotalSlots)
	var parts []string
	if s.StreamingSlots > 0 {
		parts = append(parts, fmt.Sprintf("%d streaming", s.StreamingSlots))
	}
	if s.LogicalSlots > 0 {
		parts = append(parts, fmt.Sprintf("%d logical", s.LogicalSlots))
	}
	if len(parts) > 0 {
		summary += " (" + strings.Join(parts, ", ") + ")"
	}
	if !s.Valid {
		return styleDim.Render(summary) + "\n" + styleDim.Render("no lag data")
	}

	// LagSeconds holds bytes of WAL for a primary's slots
	style := styleGreen
	switch {
	case s.LagSeconds >= 100<<20:
		style = styleRed
	case s.LagSeconds >= 1<<20:
		style = styleYellow
	}
	return styleDim.Render(summary) + "\n" +
		style.Bold(true).Render(formatBytes(s.LagSeconds)+" behind") + styleDim.Render(" · "+s.SlotType+" · "+s.SlotName)
}

// ---- Blocked queries -----------------------------------------------------

type blockInfo struct {
	waiting        int // sessions waiting on a lock held by another session
	longestWaitSec int
	roots          []int // root blockers: holding the locks, not waiting themselves
	err            string
}

// blockInfoSQL counts sessions waiting on locks and finds the root blockers.
// pg_blocking_pids only runs for sessions actually waiting on a lock, as on
// the Active tab.
const blockInfoSQL = `
	WITH w AS (
		SELECT pid, pg_blocking_pids(pid) AS blocked_by, state_change
		FROM pg_stat_activity
		WHERE wait_event_type = 'Lock'
	), waiting AS (
		SELECT * FROM w WHERE cardinality(blocked_by) > 0
	)
	SELECT
		(SELECT COUNT(*) FROM waiting),
		COALESCE((SELECT MAX(EXTRACT(EPOCH FROM (now() - state_change)))::int FROM waiting), 0),
		COALESCE((
			SELECT array_agg(DISTINCT b ORDER BY b)
			FROM waiting, unnest(blocked_by) AS b
			WHERE b NOT IN (SELECT pid FROM waiting)
		), '{}'::int[])`

func getBlockInfo(ctx context.Context, db *sql.DB) blockInfo {
	var info blockInfo
	var roots []int64
	err := db.QueryRowContext(ctx, tagged(blockInfoSQL)).Scan(&info.waiting, &info.longestWaitSec, pq.Array(&roots))
	if err != nil {
		return blockInfo{err: err.Error()}
	}
	for _, pid := range roots {
		info.roots = append(info.roots, int(pid))
	}
	return info
}

func blockedBody(b blockInfo, inner int) string {
	if b.err != "" {
		return styleDim.Render(truncate("unavailable: "+b.err, inner))
	}
	if b.waiting == 0 {
		return styleGreen.Bold(true).Render("0") + styleGreen.Render(" all clear")
	}

	var on string
	switch len(b.roots) {
	case 0:
		on = " waiting on each other" // a lock cycle the deadlock detector hasn't broken yet
	case 1:
		on = fmt.Sprintf(" waiting on pid %d", b.roots[0])
	default:
		on = fmt.Sprintf(" waiting on %d sessions", len(b.roots))
	}
	waitStyle := styleYellow
	switch {
	case b.longestWaitSec >= 30:
		waitStyle = styleRed
	case b.longestWaitSec >= 5:
		waitStyle = lipgloss.NewStyle().Foreground(colorOrange)
	}
	return styleRed.Bold(true).Render(strconv.Itoa(b.waiting)) + styleRed.Render(on) + "\n" +
		label("longest", formatDuration(b.longestWaitSec), waitStyle.Bold(true)) +
		styleDim.Render(" · press ") + styleKey.Render("b") + styleDim.Render(" for the lock tree")
}

// ---- Longest transaction -------------------------------------------------

type xactInfo struct {
	pid   int
	user  string
	state string
	sec   float64
	ok    bool
	err   string
}

// getLongestXact finds the oldest open transaction among client sessions,
// leaving out psq's own
func getLongestXact(ctx context.Context, db *sql.DB) xactInfo {
	var x xactInfo
	err := db.QueryRowContext(ctx, tagged(`
		SELECT pid, COALESCE(usename, ''), COALESCE(state, ''),
			EXTRACT(EPOCH FROM (now() - xact_start))::float8
		FROM pg_stat_activity
		WHERE xact_start IS NOT NULL
		  AND pid <> pg_backend_pid()
		  AND backend_type = 'client backend'
		  AND application_name <> 'psq'
		ORDER BY xact_start
		LIMIT 1`)).Scan(&x.pid, &x.user, &x.state, &x.sec)
	switch {
	case err == sql.ErrNoRows:
		return xactInfo{}
	case err != nil:
		return xactInfo{err: err.Error()}
	}
	x.ok = true
	return x
}

func xactBody(x xactInfo, inner int) string {
	if x.err != "" {
		return styleDim.Render(truncate("unavailable: "+x.err, inner))
	}
	if !x.ok {
		return styleDim.Render("no open transactions")
	}
	style := styleFg
	switch {
	case x.sec >= 300:
		style = styleRed
	case x.sec >= 60 || strings.HasPrefix(x.state, "idle in transaction"):
		style = styleYellow
	}
	state := x.state
	if state == "" {
		state = "unknown state"
	}
	return style.Bold(true).Render(formatSeconds(x.sec)) + "  " + truncate(fmt.Sprintf("pid %d · %s", x.pid, x.user), inner-12) + "\n" +
		stateStyle(x.state).Render(state)
}

// ---- Transactions/sec ----------------------------------------------------

// SparklineData holds the transaction commit data over time
type SparklineData struct {
	Values     []float64
	Timestamps []time.Time
	MaxPoints  int
}

// NewSparklineData creates a new sparkline data structure
func NewSparklineData(maxPoints int) *SparklineData {
	return &SparklineData{
		Values:     make([]float64, 0, maxPoints),
		Timestamps: make([]time.Time, 0, maxPoints),
		MaxPoints:  maxPoints,
	}
}

// AddPoint adds a new data point to the sparkline
func (s *SparklineData) AddPoint(value float64, timestamp time.Time) {
	s.Values = append(s.Values, value)
	s.Timestamps = append(s.Timestamps, timestamp)

	// Keep only the last MaxPoints
	if len(s.Values) > s.MaxPoints {
		s.Values = s.Values[1:]
		s.Timestamps = s.Timestamps[1:]
	}
}

// GetTransactionCommits queries the database for transaction commits, the
// current timestamp and max_connections
func GetTransactionCommits(ctx context.Context, db *sql.DB) (float64, time.Time, int, error) {
	var commits float64
	var now time.Time
	var maxConn int
	err := db.QueryRowContext(ctx, tagged("SELECT SUM(xact_commit), NOW(), current_setting('max_connections')::int FROM pg_stat_database")).Scan(&commits, &now, &maxConn)
	if err != nil {
		return 0, time.Time{}, 0, fmt.Errorf("failed to query transaction commits: %w", err)
	}
	return commits, now, maxConn, nil
}

// ---- Formatting ----------------------------------------------------------

// formatBytes formats byte counts into human-readable form
func formatBytes(bytes int) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatDuration formats seconds into human-readable duration
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	if minutes < 60 {
		if secs == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, secs)
	}
	hours := minutes / 60
	mins := minutes % 60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}
