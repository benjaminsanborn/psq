package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	_ "github.com/lib/pq"
)

type DBConfig struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
}

func getDBConfig(serviceName string) (*DBConfig, error) {
	configPath := os.ExpandEnv("$HOME/.pg_service.conf")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ~/.pg_service.conf: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var currentService string
	config := &DBConfig{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentService = strings.Trim(line, "[]")
			if currentService == serviceName {
				config = &DBConfig{}
			}
			continue
		}

		if currentService == serviceName {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				switch key {
				case "host":
					config.Host = value
				case "port":
					config.Port = value
				case "dbname":
					config.Database = value
				case "user":
					config.User = value
				case "password":
					config.Password = value
				}
			}
		}
	}

	if config.Host == "" {
		return nil, fmt.Errorf("service '%s' not found in ~/.pg_service.conf", serviceName)
	}

	// Set defaults
	if config.Port == "" {
		config.Port = "5432"
	}

	return config, nil
}

func listServices() ([]string, error) {
	configPath := os.ExpandEnv("$HOME/.pg_service.conf")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ~/.pg_service.conf: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var services []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			service := strings.Trim(line, "[]")
			services = append(services, service)
		}
	}

	return services, nil
}

func connectDB(serviceName string) (*sql.DB, error) {
	config, err := getDBConfig(serviceName)
	if err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=require",
		config.Host, config.Port, config.Database, config.User, config.Password)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func executeQuery(db *sql.DB, query string) (string, error) {
	rows, err := db.Query(query)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w", err)
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
			} else if bytes, ok := val.([]byte); ok {
				row[i] = scrubNewlines(string(bytes))
			} else {
				row[i] = scrubNewlines(fmt.Sprintf("%v", val))
			}
		}
		allRows = append(allRows, row)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating rows: %w", err)
	}

	return renderTable(columns, allRows, len(allRows)), nil
}

// renderTable renders columns and rows in the same styled plain-text table as the Active tab
func renderTable(columns []string, allRows [][]string, totalRows int) string {
	if len(columns) == 0 {
		return "No columns returned"
	}

	// Calculate optimal column widths
	colWidths := make([]int, len(columns))
	for i, col := range columns {
		colWidths[i] = len(col) + 1
	}
	for _, row := range allRows {
		for i, cell := range row {
			if len(cell)+1 > colWidths[i] {
				colWidths[i] = len(cell) + 1
			}
		}
	}

	// Cap column widths
	for i := range colWidths {
		if colWidths[i] > 50 {
			colWidths[i] = 50
		}
		if colWidths[i] < 6 {
			colWidths[i] = 6
		}
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C3AED"))

	rowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	var b strings.Builder

	// Header
	var headerParts []string
	for i, col := range columns {
		headerParts = append(headerParts, fmt.Sprintf("%-*s", colWidths[i], truncate(col, colWidths[i])))
	}
	b.WriteString(headerStyle.Render(strings.Join(headerParts, " ")))
	b.WriteString("\n")

	// Rows
	if len(allRows) == 0 {
		b.WriteString(dimStyle.Render("  (no rows)"))
		b.WriteString("\n")
	} else {
		for _, row := range allRows {
			var parts []string
			for i, cell := range row {
				parts = append(parts, fmt.Sprintf("%-*s", colWidths[i], truncate(cell, colWidths[i])))
			}
			b.WriteString(rowStyle.Render(strings.Join(parts, " ")))
			b.WriteString("\n")
		}
	}

	// Row count
	b.WriteString(dimStyle.Render(fmt.Sprintf("\n  %d rows", totalRows)))

	return b.String()
}

func renderConnectionBarChart(db *sql.DB, query string, queryName string, model *Model) (string, error) {
	// Render interactive Active view
	if IsActiveTab(queryName) {
		return renderActiveView(db, model)
	}

	// Only render charts for the Home query
	if IsHomeTab(queryName) {
		// Calculate chart width for responsive rendering
		chartWidth := GetChartWidth(model.width)

		// Get the bar chart with responsive width
		barChart, err := RenderHomeChart(db, query, chartWidth)
		if err != nil {
			return "", err
		}

		// Update sparkline data with transaction commits
		currentCommits, dbNow, err := GetTransactionCommits(db)
		if err != nil {
			// If we can't get commits, just show the bar chart
			return barChart, nil
		}

		// Calculate commits per second using DB timestamps for accurate elapsed time
		var commitsPerSec float64
		if model.lastCommits > 0 && !model.lastCommitTime.IsZero() {
			elapsed := dbNow.Sub(model.lastCommitTime).Seconds()
			if elapsed > 0 {
				commitsPerSec = (currentCommits - model.lastCommits) / elapsed
			}
		}
		model.lastCommits = currentCommits
		model.lastCommitTime = dbNow

		// Add data point to sparkline
		model.sparklineData.AddPoint(commitsPerSec, dbNow)

		// Render sparkline chart with responsive width
		sparklineChart := RenderSparklineChart(model.sparklineData, chartWidth)

		// Render bottom row widgets
		cacheHitRatio := RenderCacheHitRatio(db)
		replicationLag := RenderReplicationLag(db)

		// Render blocking locks widget (full width)
		blockingLocks := RenderBlockingLocks(db)

		return RenderHomeDashboard(barChart, sparklineChart, cacheHitRatio, replicationLag, blockingLocks, model.width), nil
	}
	return executeQuery(db, query)
}

// renderActiveView fetches active processes and renders the interactive Active view
func renderActiveView(db *sql.DB, model *Model) (string, error) {
	if model.activeView == nil {
		model.activeView = NewActiveView()
	}

	processes, err := FetchActiveProcesses(db)
	if err != nil {
		return "", err
	}

	model.activeView.UpdateSelection(processes)

	switch model.activeView.Mode {
	case ActiveModeDetail:
		return RenderActiveDetail(model.activeView, model.width), nil
	case ActiveModeConfirmTerminate:
		return RenderTerminateConfirm(model.activeView), nil
	default:
		return RenderActiveList(model.activeView, model.width, model.height), nil
	}
}
