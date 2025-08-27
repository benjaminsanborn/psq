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

	// First pass: collect all data to determine column widths
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
					row[i] = string(bytes)
				} else {
					row[i] = fmt.Sprintf("%v", val)
				}
			}
		}
		allRows = append(allRows, row)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Calculate column widths
	colWidths := make([]int, len(columns))
	for i, col := range columns {
		// Account for header padding (2 extra spaces from Padding(0, 1))
		colWidths[i] = len(col) + 2
	}

	for _, row := range allRows {
		for i, cell := range row {
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	// Define styles
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Bold(true).
		Padding(0, 1)

	borderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262"))

	rowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E5E5"))

	altRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CCCCCC"))

	// Build the formatted table
	var result strings.Builder

	// Write header border
	result.WriteString(borderStyle.Render("┌"))
	for i := range columns {
		if i > 0 {
			result.WriteString(borderStyle.Render("┬"))
		}
		result.WriteString(borderStyle.Render(strings.Repeat("─", colWidths[i])))
	}
	result.WriteString(borderStyle.Render("┐\n"))

	// Write column names
	result.WriteString(borderStyle.Render("│"))
	for i, col := range columns {
		result.WriteString(headerStyle.Render(fmt.Sprintf("%-*s", colWidths[i], col)))
		if i < len(columns)-1 {
			result.WriteString(borderStyle.Render("│"))
		}
	}
	result.WriteString(borderStyle.Render("│\n"))

	// Write separator
	result.WriteString(borderStyle.Render("├"))
	for i := range columns {
		if i > 0 {
			result.WriteString(borderStyle.Render("┼"))
		}
		result.WriteString(borderStyle.Render(strings.Repeat("─", colWidths[i])))
	}
	result.WriteString(borderStyle.Render("┤\n"))

	// Write data rows
	for rowIndex, row := range allRows {
		result.WriteString(borderStyle.Render("│"))
		for i, cell := range row {
			// Alternate row colors
			if rowIndex%2 == 0 {
				result.WriteString(rowStyle.Render(fmt.Sprintf("%-*s", colWidths[i], cell)))
			} else {
				result.WriteString(altRowStyle.Render(fmt.Sprintf("%-*s", colWidths[i], cell)))
			}
			if i < len(row)-1 {
				result.WriteString(borderStyle.Render("│"))
			}
		}
		result.WriteString(borderStyle.Render("│\n"))
	}

	// Write bottom border
	result.WriteString(borderStyle.Render("└"))
	for i := range columns {
		if i > 0 {
			result.WriteString(borderStyle.Render("┴"))
		}
		result.WriteString(borderStyle.Render(strings.Repeat("─", colWidths[i])))
	}
	result.WriteString(borderStyle.Render("┘\n"))

	return result.String(), nil
}
