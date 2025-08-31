package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/evertras/bubble-table/table"
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

	// Create table columns
	var tableColumns []table.Column
	for _, col := range columns {
		tableColumns = append(tableColumns, table.NewColumn(col, col, 15).WithFiltered(true))
	}

	// Collect data and create table rows
	var tableRows []table.Row
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}

		rowData := table.RowData{}
		for i, val := range values {
			var cellValue string
			if val == nil {
				cellValue = "NULL"
			} else {
				// Handle byte arrays (convert to string)
				if bytes, ok := val.([]byte); ok {
					cellValue = strings.ReplaceAll(string(bytes), "\n", " ")
				} else {
					cellValue = strings.ReplaceAll(fmt.Sprintf("%v", val), "\n", " ")
				}
			}
			rowData[columns[i]] = cellValue
		}
		tableRows = append(tableRows, table.NewRow(rowData))
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating rows: %w", err)
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

	return t.View(), nil
}
