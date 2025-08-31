package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type queryResultMsg string
type queryErrorMsg string

func loadQueries() ([]Query, error) {
	configDir := filepath.Join(os.ExpandEnv("$HOME"), ".psqi")
	sqlDir := filepath.Join(configDir, "queries")

	// Check if SQL directory exists and has files
	if sqlFiles, err := filepath.Glob(filepath.Join(sqlDir, "*.sql")); err == nil && len(sqlFiles) > 0 {
		return loadQueriesFromSQL(sqlDir)
	}

	// If SQL directory doesn't exist, copy from examples/queries
	if _, err := os.Stat(sqlDir); os.IsNotExist(err) {
		if err := copyQueriesFromExamples(sqlDir); err != nil {
			return nil, err
		}
		return loadQueriesFromSQL(sqlDir)
	}

	// If directory exists but is empty, copy from examples
	if err := copyQueriesFromExamples(sqlDir); err != nil {
		return nil, fmt.Errorf("failed to initialize queries: %w", err)
	}

	return loadQueriesFromSQL(sqlDir)
}

func loadQueriesFromSQL(sqlDir string) ([]Query, error) {
	files, err := filepath.Glob(filepath.Join(sqlDir, "*.sql"))
	if err != nil {
		return nil, fmt.Errorf("failed to read SQL directory: %w", err)
	}

	var queries []Query
	for _, file := range files {
		query, err := loadQueryFromSQLFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to load query from %s: %w", file, err)
		}
		queries = append(queries, query)
	}

	return queries, nil
}

func copyQueriesFromExamples(targetDir string) error {
	// Create target directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create queries directory: %w", err)
	}

	// Embedded example queries since they won't be available in compiled binary
	exampleQueries := map[string]string{
		"active.sql": `-- Active
-- Show current active connections
SELECT pid, LEFT(query,50) AS query, LEFT(usename,8) AS name, LEFT(state,10) AS state, LEFT((NOW() - query_start)::text,8) as age, wait_event, wait_event_type FROM pg_stat_activity WHERE state IS NOT NULL AND state != 'idle' ORDER BY NOW() - query_start DESC;`,

		"lock_information.sql": `-- Lock Information
-- Show current locks
SELECT l.pid, l.mode, l.granted, a.usename, a.query FROM pg_locks l JOIN pg_stat_activity a ON l.pid = a.pid WHERE NOT l.granted ORDER BY l.pid;`,

		"replication_lag.sql": `-- Replication Lag
-- Show replication lag information
SELECT application_name, pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes, client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn FROM pg_stat_replication;`,

		"top_queries.sql": `-- Top Queries
-- Requires pg_stat_statements; identifies heavy hitters
SELECT LEFT(query, 40) AS query, calls, total_exec_time, mean_exec_time, rows, shared_blks_hit, shared_blks_read, temp_blks_written FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 25;`,

		"index_creation.sql": `-- Index Creation
-- Show progress of index creation operations
SELECT p.pid, c.relname AS table_name, ic.relname AS index_name, p.phase, p.lockers_done || '/' || p.lockers_total AS locks, p.blocks_done || '/' || p.blocks_total AS blocks, p.tuples_done || '/' || p.tuples_total AS tupes, p.partitions_done || '/' || p.partitions_total AS parts FROM pg_stat_progress_create_index p JOIN pg_class c  ON p.relid = c.oid JOIN pg_class ic ON p.index_relid = ic.oid;`,

		"table_replication_state.sql": `-- Table Replication State
-- The state of logical replication for each table in the public schema
SELECT s.subname AS subscription, r.srsubstate AS table_state, ARRAY_AGG(c.relname ORDER BY c.relname) AS tables FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_subscription_rel r ON r.srrelid = c.oid LEFT JOIN pg_subscription s     ON s.oid = r.srsubid WHERE n.nspname = 'public' AND c.relkind IN ('r','p','f') GROUP BY s.subname, r.srsubstate ORDER BY s.subname, r.srsubstate;`,

		"configuration_settings.sql": `-- Configuration Settings
-- All current PostgreSQL configuration settings
SELECT name, setting, unit, category, short_desc FROM pg_settings ORDER BY category, name;`,
	}

	for filename, content := range exampleQueries {
		filePath := filepath.Join(targetDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write query file %s: %w", filename, err)
		}
	}

	return nil
}

func loadQueryFromSQLFile(filename string) (Query, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Query{}, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 {
		return Query{}, fmt.Errorf("invalid SQL file format: must have title, description, and query")
	}

	// Parse title from first line (-- Title)
	title := strings.TrimSpace(strings.TrimPrefix(lines[0], "--"))
	if title == "" {
		return Query{}, fmt.Errorf("missing title in first line")
	}

	// Parse description from second line (-- Description)
	description := strings.TrimSpace(strings.TrimPrefix(lines[1], "--"))
	if description == "" {
		return Query{}, fmt.Errorf("missing description in second line")
	}

	// Join remaining lines as SQL (excluding comment lines)
	var sqlLines []string
	for i := 2; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line != "" && !strings.HasPrefix(line, "--") {
			sqlLines = append(sqlLines, line)
		}
	}

	if len(sqlLines) == 0 {
		return Query{}, fmt.Errorf("no SQL content found")
	}

	sql := strings.Join(sqlLines, " ")

	return Query{
		Name:        title,
		Description: description,
		SQL:         sql,
	}, nil
}

func findQueryFile(sqlDir string, targetQuery Query) (string, error) {
	files, err := filepath.Glob(filepath.Join(sqlDir, "*.sql"))
	if err != nil {
		return "", err
	}

	for _, file := range files {
		query, err := loadQueryFromSQLFile(file)
		if err != nil {
			continue // Skip files that can't be parsed
		}

		// Match by name and SQL content
		if query.Name == targetQuery.Name && query.SQL == targetQuery.SQL {
			return file, nil
		}
	}

	return "", fmt.Errorf("query file not found")
}

func (m *Model) runQuery(query Query) tea.Cmd {
	return func() tea.Msg {
		db, err := connectDB(m.service)
		if err != nil {
			return queryErrorMsg(fmt.Sprintf("Failed to connect: %v", err))
		}
		defer db.Close()

		result, err := executeQuery(db, query.SQL)
		if err != nil {
			return queryErrorMsg(fmt.Sprintf("Query failed: %v", err))
		}

		return queryResultMsg(result)
	}
}
