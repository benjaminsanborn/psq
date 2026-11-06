package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lib/pq"
)

type DBConfig struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string

	// TLS settings, passed through to the driver and to psql
	SSLMode     string
	SSLRootCert string
	SSLCert     string
	SSLKey      string

	// psq-specific settings, read from "#psq:key=value" comment lines inside
	// a service block so libpq/psql still accept the file.
	Auth       string // "rds-iam" enables AWS RDS IAM authentication
	AWSRegion  string
	AWSProfile string
}

// UsesRDSIAM reports whether the service authenticates with RDS IAM tokens.
func (c *DBConfig) UsesRDSIAM() bool {
	return c.Auth == "rds-iam"
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
		if directive, ok := strings.CutPrefix(line, "#psq:"); ok && currentService == serviceName {
			parts := strings.SplitN(directive, "=", 2)
			if len(parts) == 2 {
				value := strings.TrimSpace(parts[1])
				switch strings.TrimSpace(parts[0]) {
				case "auth":
					config.Auth = value
				case "aws_region":
					config.AWSRegion = value
				case "aws_profile":
					config.AWSProfile = value
				}
			}
			continue
		}
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
				case "sslmode":
					config.SSLMode = value
				case "sslrootcert":
					config.SSLRootCert = value
				case "sslcert":
					config.SSLCert = value
				case "sslkey":
					config.SSLKey = value
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

	if config.UsesRDSIAM() {
		connector, err := newRDSIAMConnector(config)
		if err != nil {
			return nil, err
		}
		return pingOrClose(sql.OpenDB(newSessionConnector(connector)))
	}

	mode, fallback := config.driverSSLMode()
	db, err := openPostgres(buildDSN(config, config.Password, mode))
	if err != nil && fallback && errors.Is(err, pq.ErrSSLNotSupported) {
		// libpq "prefer": the server doesn't do TLS, so connect without it
		db, err = openPostgres(buildDSN(config, config.Password, "disable"))
	}
	return db, err
}

func openPostgres(dsn string) (*sql.DB, error) {
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return pingOrClose(sql.OpenDB(newSessionConnector(connector)))
}

func pingOrClose(db *sql.DB) (*sql.DB, error) {
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return db, nil
}

// libpqSSLMode is the sslmode psql would use: the service file's value, then
// $PGSSLMODE, then libpq's default of "prefer".
func (c *DBConfig) libpqSSLMode() string {
	if c.SSLMode != "" {
		return c.SSLMode
	}
	if env := os.Getenv("PGSSLMODE"); env != "" {
		return env
	}
	return "prefer"
}

// driverSSLMode maps the libpq sslmode onto what lib/pq supports. lib/pq has
// no "prefer" or "allow", so "prefer" becomes "require" with fallback set,
// meaning the caller should retry with "disable" if the server has no TLS.
// RDS IAM always uses TLS.
func (c *DBConfig) driverSSLMode() (mode string, fallback bool) {
	mode = c.libpqSSLMode()
	switch mode {
	case "prefer":
		if c.UsesRDSIAM() {
			return "require", false
		}
		return "require", true
	case "allow":
		if c.UsesRDSIAM() {
			return "require", false
		}
		return "disable", false
	}
	return mode, false
}

// buildDSN renders a lib/pq key/value connection string, quoting values so
// passwords (and IAM tokens) containing special characters survive. An empty
// password is left out so lib/pq falls back to ~/.pgpass.
func buildDSN(config *DBConfig, password, sslmode string) string {
	quote := func(v string) string {
		v = strings.ReplaceAll(v, `\`, `\\`)
		return "'" + strings.ReplaceAll(v, "'", `\'`) + "'"
	}
	parts := []string{
		"host=" + quote(config.Host),
		"port=" + quote(config.Port),
		"dbname=" + quote(config.Database),
		"user=" + quote(config.User),
		"application_name=" + quote(applicationName),
	}
	if password != "" {
		parts = append(parts, "password="+quote(password))
	}
	parts = append(parts, "sslmode="+quote(sslmode))
	for _, kv := range []struct{ key, value string }{
		{"sslrootcert", config.SSLRootCert},
		{"sslcert", config.SSLCert},
		{"sslkey", config.SSLKey},
	} {
		if kv.value != "" {
			parts = append(parts, kv.key+"="+quote(kv.value))
		}
	}
	return strings.Join(parts, " ")
}

// executeQuery runs a saved query read-only and renders it as a table
func executeQuery(ctx context.Context, db *sql.DB, query string) (string, error) {
	res, err := queryRows(ctx, db, query, settings.MaxRows)
	if err != nil {
		return "", err
	}

	cells := make([][]string, len(res.Rows))
	for i, row := range res.Rows {
		cells[i] = make([]string, len(row))
		for j, v := range row {
			cells[i][j] = scrubNewlines(cellString(v))
		}
	}

	out := renderTable(res.Columns, cells)
	if res.Truncated {
		out += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
			Render(fmt.Sprintf("  showing the first %d rows; add a LIMIT or ORDER BY to see the ones you need", settings.MaxRows)) + "\n"
	}
	return out, nil
}

// renderTable renders columns and rows in the same styled plain-text table as the Active tab
func renderTable(columns []string, allRows [][]string) string {
	if len(columns) == 0 {
		return "No columns returned"
	}

	// Calculate optimal column widths
	colWidths := make([]int, len(columns))
	for i, col := range columns {
		colWidths[i] = ansi.StringWidth(col) + 1
	}
	for _, row := range allRows {
		for i, cell := range row {
			if w := ansi.StringWidth(cell) + 1; w > colWidths[i] {
				colWidths[i] = w
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
		headerParts = append(headerParts, fitWidth(col, colWidths[i]))
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
				parts = append(parts, fitWidth(cell, colWidths[i]))
			}
			b.WriteString(rowStyle.Render(strings.Join(parts, " ")))
			b.WriteString("\n")
		}
	}

	return b.String()
}
