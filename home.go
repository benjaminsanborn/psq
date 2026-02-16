package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/barchart"
	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"
)

// HomeQuery returns the hardcoded Home query
func HomeQuery() Query {
	return Query{
		Name:        "Home",
		Description: "Activity state counts overview",
		SQL:         "SELECT state, COUNT(*) as count FROM pg_stat_activity WHERE state IS NOT NULL GROUP BY state ORDER BY count DESC;",
	}
}

// RenderHomeChart renders the PostgreSQL activity state chart for the Home tab
func RenderHomeChart(db *sql.DB, query string, chartWidth int) (string, error) {
	rows, err := db.Query(query)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %w", err)
	}

	var chartData []barchart.BarData
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}

		var state string
		var count float64

		// Extract state (first column)
		if values[0] == nil {
			state = "NULL"
		} else if bytes, ok := values[0].([]byte); ok {
			state = string(bytes)
		} else {
			state = fmt.Sprintf("%v", values[0])
		}

		// Extract count (second column)
		if values[1] == nil {
			count = 0
		} else if bytes, ok := values[1].([]byte); ok {
			if parsed, err := strconv.ParseFloat(string(bytes), 64); err == nil {
				count = parsed
			}
		} else if str, ok := values[1].(string); ok {
			if parsed, err := strconv.ParseFloat(str, 64); err == nil {
				count = parsed
			}
		} else if i64, ok := values[1].(int64); ok {
			count = float64(i64)
		} else if f64, ok := values[1].(float64); ok {
			count = f64
		}

		// Choose colors based on state
		var color lipgloss.Color
		switch strings.ToLower(state) {
		case "active":
			color = lipgloss.Color("10") // Green
		case "idle":
			color = lipgloss.Color("8") // Gray
		case "idle in transaction":
			color = lipgloss.Color("11") // Yellow
		case "idle in transaction (aborted)":
			color = lipgloss.Color("9") // Red
		default:
			color = lipgloss.Color("12") // Blue
		}

		chartData = append(chartData, barchart.BarData{
			Label: fmt.Sprintf("%s (%d)", state, int(count)),
			Values: []barchart.BarValue{
				{
					Value: count,
					Style: lipgloss.NewStyle().Foreground(color),
				},
			},
		})
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating rows: %w", err)
	}

	if len(chartData) == 0 {
		return "No data to display", nil
	}

	var axisStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")) // yellow

	var labelStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("63")).
		Align(lipgloss.Right)

	// Calculate responsive chart dimensions
	// Subtract padding and border space from the available width
	responsiveWidth := chartWidth - 6 // Account for border and padding
	if responsiveWidth < 20 {
		responsiveWidth = 20 // Minimum width
	}

	// Create the bar chart with responsive width
	bc := barchart.New(
		responsiveWidth, 5,
		barchart.WithDataSet(chartData),            // Your data
		barchart.WithStyles(axisStyle, labelStyle), // Style axis & labels
		barchart.WithHorizontalBars(),              // Horizontal bar layout
	)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	bc.Draw()

	// Calculate total connections
	totalConnectionsCount := 0
	for _, bar := range chartData {
		totalConnectionsCount += int(bar.Values[0].Value)
	}
	title := fmt.Sprintf("Connections (%d)", totalConnectionsCount)
	result := titleStyle.Render(title) + "\n" + bc.View()

	return result, nil
}

// IsHomeTab checks if the given query is the Home tab
func IsHomeTab(queryName string) bool {
	return queryName == "Home"
}

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

// GetTransactionCommits queries the database for transaction commits
func GetTransactionCommits(db *sql.DB) (float64, error) {
	var commits float64
	err := db.QueryRow("SELECT SUM(xact_commit) FROM pg_stat_database").Scan(&commits)
	if err != nil {
		return 0, fmt.Errorf("failed to query transaction commits: %w", err)
	}
	return commits, nil
}

// RenderSparklineChart renders the transaction commits sparkline
func RenderSparklineChart(sparklineData *SparklineData, chartWidth int) string {
	if len(sparklineData.Values) == 0 {
		return "No data yet..."
	}

	// Get the latest value (current transactions per second)
	currentTPS := sparklineData.Values[len(sparklineData.Values)-1]

	// Calculate responsive sparkline dimensions
	// Subtract padding and border space from the available width
	responsiveWidth := chartWidth - 6 // Account for border and padding
	if responsiveWidth < 20 {
		responsiveWidth = 20 // Minimum width
	}

	// Create sparkline chart with responsive width
	sl := sparkline.New(responsiveWidth, 5)
	for _, value := range sparklineData.Values {
		sl.Push(value)
	}
	sl.Draw()

	// Add title with current TPS count
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	title := fmt.Sprintf("Transactions/sec (%.1f)", currentTPS)
	result := titleStyle.Render(title) + "\n" + sl.View()
	return result
}

// GetCacheHitRatio queries the database for cache hit ratio percentage
func GetCacheHitRatio(db *sql.DB) (float64, bool, error) {
	var ratio sql.NullFloat64
	err := db.QueryRow("SELECT ROUND(SUM(blks_hit) * 100.0 / NULLIF(SUM(blks_hit) + SUM(blks_read), 0), 2) as ratio FROM pg_stat_database").Scan(&ratio)
	if err != nil {
		return 0, false, fmt.Errorf("failed to query cache hit ratio: %w", err)
	}
	if !ratio.Valid {
		return 0, false, nil
	}
	return ratio.Float64, true, nil
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
func GetReplicationLag(db *sql.DB) (ReplicationLagInfo, error) {
	var isInRecovery bool
	if err := db.QueryRow("SELECT pg_is_in_recovery()").Scan(&isInRecovery); err != nil {
		return ReplicationLagInfo{}, fmt.Errorf("failed to check recovery state: %w", err)
	}

	if isInRecovery {
		// Replica: show replay lag
		var lagSeconds sql.NullInt64
		err := db.QueryRow("SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))::int").Scan(&lagSeconds)
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
	rows, err := db.Query(`
		SELECT slot_type, COUNT(*)
		FROM pg_replication_slots
		GROUP BY slot_type`)
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
	err = db.QueryRow(`
		SELECT slot_name, slot_type,
			pg_wal_lsn_diff(pg_current_wal_lsn(), COALESCE(confirmed_flush_lsn, restart_lsn))::bigint AS lag_bytes
		FROM pg_replication_slots
		WHERE active OR NOT active
		ORDER BY lag_bytes DESC NULLS LAST
		LIMIT 1`).Scan(&slotName, &slotType, &lagBytes)
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

// RenderCacheHitRatio renders the cache hit ratio widget
func RenderCacheHitRatio(db *sql.DB) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	ratio, valid, err := GetCacheHitRatio(db)
	if err != nil || !valid {
		return titleStyle.Render("Cache Hit Ratio") + "\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("N/A")
	}

	var color lipgloss.Color
	switch {
	case ratio >= 99:
		color = lipgloss.Color("10") // Green
	case ratio >= 95:
		color = lipgloss.Color("11") // Yellow
	default:
		color = lipgloss.Color("9") // Red
	}

	valueStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(color).
		PaddingTop(1)

	return titleStyle.Render("Cache Hit Ratio") + "\n" +
		valueStyle.Render(fmt.Sprintf("%.2f%%", ratio))
}

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

// RenderReplicationLag renders the replication lag widget
func RenderReplicationLag(db *sql.DB) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	info, err := GetReplicationLag(db)
	if err != nil {
		return titleStyle.Render("Replication") + "\n" + dimStyle.Render("error")
	}

	if info.IsReplica {
		// Replica mode: show replay lag in seconds
		if !info.Valid {
			return titleStyle.Render("Replica Lag") + "\n" + dimStyle.Render("N/A")
		}
		var color lipgloss.Color
		switch {
		case info.LagSeconds < 1:
			color = lipgloss.Color("10") // Green
		case info.LagSeconds <= 10:
			color = lipgloss.Color("11") // Yellow
		default:
			color = lipgloss.Color("9") // Red
		}
		valueStyle := lipgloss.NewStyle().Bold(true).Foreground(color).PaddingTop(1)
		return titleStyle.Render("Replica Lag") + "\n" +
			valueStyle.Render(fmt.Sprintf("%ds", info.LagSeconds))
	}

	// Primary mode
	if info.TotalSlots == 0 {
		return titleStyle.Render("Replication Slots") + "\n" + dimStyle.Render("none")
	}

	// Slot count summary
	slotSummary := fmt.Sprintf("%d slots", info.TotalSlots)
	parts := []string{}
	if info.StreamingSlots > 0 {
		parts = append(parts, fmt.Sprintf("%d streaming", info.StreamingSlots))
	}
	if info.LogicalSlots > 0 {
		parts = append(parts, fmt.Sprintf("%d logical", info.LogicalSlots))
	}
	if len(parts) > 0 {
		slotSummary += " (" + strings.Join(parts, ", ") + ")"
	}

	if !info.Valid {
		return titleStyle.Render("Replication Slots") + "\n" +
			dimStyle.Render(slotSummary) + "\n" +
			dimStyle.Render("no lag data")
	}

	// Color by WAL lag severity
	lagStr := formatBytes(info.LagSeconds) // LagSeconds holds bytes for primary
	var color lipgloss.Color
	switch {
	case info.LagSeconds < 1<<20: // < 1 MB
		color = lipgloss.Color("10") // Green
	case info.LagSeconds < 100<<20: // < 100 MB
		color = lipgloss.Color("11") // Yellow
	default:
		color = lipgloss.Color("9") // Red
	}

	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(color)
	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	return titleStyle.Render("Replication Slots") + "\n" +
		dimStyle.Render(slotSummary) + "\n" +
		valueStyle.Render(lagStr+" behind") +
		detailStyle.Render(" · "+info.SlotType+" · "+info.SlotName)
}

// RenderHomeDashboard renders all widgets: 2x2 grid + full-width blocking locks
func RenderHomeDashboard(barChart, sparklineChart, cacheHitRatio, replicationLag, blockingLocks string, width int) string {
	halfWidth := GetChartWidth(width)
	borderColor := lipgloss.Color("62")

	leftStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		MarginRight(2)

	rightStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	// Bottom widgets: shorter height for single-value displays
	bottomLeftStyle := leftStyle.Height(5)
	bottomRightStyle := rightStyle.Height(5)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(barChart),
		rightStyle.Render(sparklineChart),
	)

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top,
		bottomLeftStyle.Render(cacheHitRatio),
		bottomRightStyle.Render(replicationLag),
	)

	// Full-width blocking locks widget at the top
	fullWidthStyle := lipgloss.NewStyle().
		Width(width - 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		MarginBottom(1)

	blockingLocksRow := fullWidthStyle.Render(blockingLocks)

	return lipgloss.JoinVertical(lipgloss.Left, blockingLocksRow, topRow, bottomRow)
}

// RenderHomeSideBySide renders both charts in side-by-side blocks (legacy, unused)
func RenderHomeSideBySide(barChart, sparklineChart string, width int) string {
	halfWidth := GetChartWidth(width)

	leftStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		MarginRight(2)

	rightStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62"))

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftStyle.Render(barChart),
		rightStyle.Render(sparklineChart),
	)
}

// RenderHomeWithTable renders charts and table in a vertical layout
func RenderHomeWithTable(barChart, sparklineChart, activeTable string, width int) string {
	// Render charts side by side
	chartsLayout := RenderHomeSideBySide(barChart, sparklineChart, width)

	// Create table style that spans full width
	tableStyle := lipgloss.NewStyle().
		Width(width - 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1)

	// Combine charts and table vertically
	return lipgloss.JoinVertical(
		lipgloss.Left,
		chartsLayout,
		tableStyle.Render(activeTable),
	)
}

// GetChartWidth calculates the width available for each chart
func GetChartWidth(totalWidth int) int {
	return (totalWidth - 8) / 2 // Half width minus padding
}

// BlockingLockInfo holds information about the most blocking PID
type BlockingLockInfo struct {
	Valid            bool
	BlockingPID      int
	BlockedCount     int
	Username         string
	Query            string
	LongestWaitSec   int
	BlockerRunningSec int // How long the blocking query has been running
}

// GetBlockingLockInfo queries for the PID blocking the most queries
func GetBlockingLockInfo(db *sql.DB) (BlockingLockInfo, error) {
	// Recursively walk blocking chain to find root blockers
	sqlQuery := `
		WITH RECURSIVE blocking_chain AS (
			-- Start with all direct blocking relationships
			SELECT 
				sa.pid AS leaf_pid,
				sa.pid AS blocked_pid,
				UNNEST(pg_blocking_pids(sa.pid)) AS blocking_pid,
				sa.state_change,
				1 AS depth
			FROM pg_stat_activity sa
			WHERE cardinality(pg_blocking_pids(sa.pid)) > 0
			
			UNION ALL
			
			-- Walk up the chain to find the root blocker
			SELECT 
				bc.leaf_pid,
				bc.blocking_pid AS blocked_pid,
				UNNEST(pg_blocking_pids(sa.pid)) AS blocking_pid,
				bc.state_change,
				bc.depth + 1
			FROM blocking_chain bc
			JOIN pg_stat_activity sa ON sa.pid = bc.blocking_pid
			WHERE cardinality(pg_blocking_pids(sa.pid)) > 0
		),
		-- For each leaf (originally blocked PID), find its root blocker
		leaf_to_root AS (
			SELECT DISTINCT ON (leaf_pid)
				leaf_pid,
				blocking_pid AS root_blocker,
				state_change
			FROM blocking_chain
			ORDER BY leaf_pid, depth DESC
		),
		-- Count how many PIDs each root blocker is blocking
		blocking_summary AS (
			SELECT 
				root_blocker,
				COUNT(DISTINCT leaf_pid) AS blocked_count,
				MAX(EXTRACT(EPOCH FROM (NOW() - state_change))::int) AS longest_wait_sec
			FROM leaf_to_root
			GROUP BY root_blocker
		)
		SELECT 
			bs.root_blocker AS blocking_pid,
			bs.blocked_count,
			sa.usename,
			LEFT(sa.query, 40) AS query,
			bs.longest_wait_sec,
			EXTRACT(EPOCH FROM (NOW() - sa.query_start))::int AS blocker_running_sec
		FROM blocking_summary bs
		JOIN pg_stat_activity sa ON sa.pid = bs.root_blocker
		ORDER BY bs.blocked_count DESC, bs.longest_wait_sec DESC
		LIMIT 1`

	var info BlockingLockInfo
	var username, queryText sql.NullString
	var blockingPID, blockedCount, longestWait, blockerRunning sql.NullInt64

	err := db.QueryRow(sqlQuery).Scan(&blockingPID, &blockedCount, &username, &queryText, &longestWait, &blockerRunning)
	if err == sql.ErrNoRows {
		// No blocking locks - this is good!
		return BlockingLockInfo{Valid: false}, nil
	}
	if err != nil {
		return BlockingLockInfo{}, fmt.Errorf("failed to query blocking locks: %w", err)
	}

	if !blockingPID.Valid || !blockedCount.Valid {
		return BlockingLockInfo{Valid: false}, nil
	}

	info.Valid = true
	info.BlockingPID = int(blockingPID.Int64)
	info.BlockedCount = int(blockedCount.Int64)
	if username.Valid {
		info.Username = username.String
	}
	if queryText.Valid {
		info.Query = queryText.String
	}
	if longestWait.Valid {
		info.LongestWaitSec = int(longestWait.Int64)
	}
	if blockerRunning.Valid {
		info.BlockerRunningSec = int(blockerRunning.Int64)
	}

	return info, nil
}

// RenderBlockingLocks renders the blocking locks widget (full width)
func RenderBlockingLocks(db *sql.DB) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9"))

	info, err := GetBlockingLockInfo(db)
	if err != nil {
		return titleStyle.Render("Blocking Locks") + "\n" + 
			errorStyle.Render(fmt.Sprintf("Error: %v", err))
	}

	if !info.Valid {
		okStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			PaddingTop(1)
		return titleStyle.Render("Blocking Locks") + "\n" +
			okStyle.Render("✓ No blocking locks detected")
	}

	// We have blocking locks - show the worst offender
	var waitColor lipgloss.Color
	switch {
	case info.LongestWaitSec < 5:
		waitColor = lipgloss.Color("11") // Yellow - short wait
	case info.LongestWaitSec < 30:
		waitColor = lipgloss.Color("208") // Orange - concerning
	default:
		waitColor = lipgloss.Color("9") // Red - critical
	}

	// Color code blocker runtime
	var runtimeColor lipgloss.Color
	switch {
	case info.BlockerRunningSec < 10:
		runtimeColor = lipgloss.Color("10") // Green - quick query
	case info.BlockerRunningSec < 60:
		runtimeColor = lipgloss.Color("11") // Yellow - moderate
	case info.BlockerRunningSec < 300:
		runtimeColor = lipgloss.Color("208") // Orange - long
	default:
		runtimeColor = lipgloss.Color("9") // Red - very long, likely needs killing
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("9")). // Red for warning
		PaddingTop(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Bold(true)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15"))

	waitStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(waitColor)

	runtimeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(runtimeColor)

	queryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("251")).
		Italic(true)

	header := fmt.Sprintf("⚠ PID %d is blocking %d queries", info.BlockingPID, info.BlockedCount)

	// Format runtime nicely
	runtimeStr := formatDuration(info.BlockerRunningSec)

	details := lipgloss.JoinHorizontal(lipgloss.Top,
		labelStyle.Render("User: "),
		valueStyle.Render(info.Username),
		labelStyle.Render("  •  Wait: "),
		waitStyle.Render(fmt.Sprintf("%ds", info.LongestWaitSec)),
		labelStyle.Render("  •  Running: "),
		runtimeStyle.Render(runtimeStr),
	)

	queryText := queryStyle.Render(info.Query)

	return titleStyle.Render("Blocking Locks") + "\n" +
		headerStyle.Render(header) + "\n" +
		details + "\n" +
		queryText
}
