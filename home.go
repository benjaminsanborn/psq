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
func RenderHomeChart(db *sql.DB, query string) (string, error) {
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

	// Create the bar chart
	bc := barchart.New(
		40, 5,
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
func RenderSparklineChart(sparklineData *SparklineData) string {
	if len(sparklineData.Values) == 0 {
		return "No data yet..."
	}

	// Get the latest value (current transactions per second)
	currentTPS := sparklineData.Values[len(sparklineData.Values)-1]

	// Create sparkline chart
	sl := sparkline.New(40, 5)
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

// RenderHomeLayout renders both the bar chart and sparkline side by side
func RenderHomeLayout(barChart, sparklineChart string) string {
	// Split the charts into lines for side-by-side layout
	barLines := strings.Split(barChart, "\n")
	sparkLines := strings.Split(sparklineChart, "\n")

	// Determine the maximum number of lines
	maxLines := len(barLines)
	if len(sparkLines) > maxLines {
		maxLines = len(sparkLines)
	}

	// Pad shorter chart with empty lines
	for len(barLines) < maxLines {
		barLines = append(barLines, "")
	}
	for len(sparkLines) < maxLines {
		sparkLines = append(sparkLines, "")
	}

	// Create side-by-side layout with padding
	var result strings.Builder
	padding := strings.Repeat(" ", 5) // 5 spaces between charts

	for i := 0; i < maxLines; i++ {
		// Ensure consistent width for the bar chart (pad to 65 characters)
		barLine := barLines[i]
		if len(barLine) < 65 {
			barLine += strings.Repeat(" ", 65-len(barLine))
		}

		result.WriteString(barLine)
		result.WriteString(padding)
		result.WriteString(sparkLines[i])
		if i < maxLines-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}
