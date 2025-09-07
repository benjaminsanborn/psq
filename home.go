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
	"github.com/evertras/bubble-table/table"
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

// RenderHomeSideBySide renders both charts in side-by-side blocks
func RenderHomeSideBySide(barChart, sparklineChart string, width int) string {
	// Calculate half width for each chart
	halfWidth := (width - 8) / 2 // Leave some padding

	// Create styled blocks for each chart
	leftStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1).
		MarginRight(2)

	rightStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1)

	// Render charts in styled blocks side by side
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

// RenderActiveConnectionsTable renders the active connections table
func RenderActiveConnectionsTable(db *sql.DB) (string, error) {
	// Active query SQL (copied from querydb.go)
	activeQuery := "SELECT pid, LEFT(query,50) AS query, LEFT(usename,8) AS name, LEFT(state,10) AS state, LEFT((NOW() - query_start)::text,8) as age, wait_event_type FROM pg_stat_activity WHERE state IS NOT NULL AND state != 'idle' ORDER BY NOW() - query_start DESC;"

	rows, err := db.Query(activeQuery)
	if err != nil {
		return "", fmt.Errorf("failed to execute active connections query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %w", err)
	}

	// Collect all data
	var allRows [][]string
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}

		row := make([]string, len(columns))
		for i, val := range values {
			if val == nil {
				row[i] = "NULL"
			} else {
				// Handle byte arrays (convert to string)
				if bytes, ok := val.([]byte); ok {
					row[i] = strings.ReplaceAll(string(bytes), "\n", " ")
				} else {
					row[i] = strings.ReplaceAll(fmt.Sprintf("%v", val), "\n", " ")
				}
			}
		}
		allRows = append(allRows, row)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Create bubble-table
	if len(allRows) == 0 {
		return "No active connections", nil
	}

	// Calculate column widths
	colWidths := make([]int, len(columns))
	for i, col := range columns {
		colWidths[i] = len(col) + 2 // Start with header width + padding
	}

	for _, row := range allRows {
		for i, cell := range row {
			if len(cell)+2 > colWidths[i] {
				colWidths[i] = len(cell) + 2
			}
		}
	}

	// Create table columns with calculated widths
	var tableColumns []table.Column
	for i, col := range columns {
		// Limit maximum column width to prevent extremely wide columns
		maxWidth := colWidths[i]
		if maxWidth > 50 {
			maxWidth = 50
		}
		if maxWidth < 8 {
			maxWidth = 8
		}
		tableColumns = append(tableColumns, table.NewColumn(col, col, maxWidth).WithFiltered(true))
	}

	// Create table rows with the collected data (limit to first 10 for space)
	maxRows := 50
	if len(allRows) > maxRows {
		allRows = allRows[:maxRows]
	}

	var tableRows []table.Row
	for _, row := range allRows {
		rowData := table.RowData{}
		for i, cellValue := range row {
			rowData[columns[i]] = cellValue
		}
		tableRows = append(tableRows, table.NewRow(rowData))
	}

	// Define styles with nice colors
	baseStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Foreground(lipgloss.Color("#E5E7EB"))

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED")).
		Bold(true).
		Padding(0, 1)

	// Create and configure the bubble table with colors
	t := table.New(tableColumns).
		WithRows(tableRows).
		WithBaseStyle(baseStyle).
		HeaderStyle(headerStyle).
		WithMissingDataIndicator(lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true).
			Render("NULL"))

	// Add title above the table
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	return titleStyle.Render("Query Activity") + "\n" + t.View(), nil
}
