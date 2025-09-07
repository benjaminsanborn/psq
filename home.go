package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/NimbleMarkets/ntcharts/barchart"
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
		60, 5,
		barchart.WithDataSet(chartData),            // Your data
		barchart.WithStyles(axisStyle, labelStyle), // Style axis & labels
		barchart.WithHorizontalBars(),              // Horizontal bar layout
	)

	// Draw the chart
	bc.Draw()

	chartView := bc.View()

	return chartView, nil
}

// IsHomeTab checks if the given query is the Home tab
func IsHomeTab(queryName string) bool {
	return queryName == "Home"
}
