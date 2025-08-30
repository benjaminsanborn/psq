package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

type queryResultMsg string
type queryErrorMsg string

func loadQueries() ([]Query, error) {
	configPath := filepath.Join(os.ExpandEnv("$HOME"), ".pgi", "queries.json")

	// Create default queries if file doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := createDefaultQueries(configPath); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read queries config: %w", err)
	}

	var queries []Query
	if err := json.Unmarshal(data, &queries); err != nil {
		return nil, fmt.Errorf("failed to parse queries config: %w", err)
	}

	return queries, nil
}

func createDefaultQueries(configPath string) error {
	// Create directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	defaultQueries := []Query{
		{
			Name:        "Active",
			Description: "Show current active connections",
			SQL:         "SELECT pid, LEFT(query,50) AS query, LEFT(usename,8) AS name, LEFT(state,10) AS state, LEFT((NOW() - query_start)::text,8) as age, wait_event, wait_event_type FROM pg_stat_activity WHERE state IS NOT NULL AND state != 'idle' ORDER BY NOW() - query_start DESC;",
		},
		{
			Name:        "Lock Information",
			Description: "Show current locks",
			SQL:         "SELECT l.pid, l.mode, l.granted, a.usename, a.query FROM pg_locks l JOIN pg_stat_activity a ON l.pid = a.pid WHERE NOT l.granted ORDER BY l.pid;",
		},
		{
			Name:        "Replication Lag",
			Description: "Show replication lag information",
			SQL:         "SELECT application_name, pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes, client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn FROM pg_stat_replication;",
		},
		{
			Name:        "Top Queries",
			Description: "Requires pg_stat_statements; identifies heavy hitters",
			SQL:         "SELECT LEFT(query, 40) AS query, calls, total_exec_time, mean_exec_time, rows, shared_blks_hit, shared_blks_read, temp_blks_written FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 25;",
		},
		{
			Name:        "Index Creation",
			Description: "Show progress of index creation operations",
			SQL:         "SELECT p.pid, c.relname AS table_name, ic.relname AS index_name, p.phase, p.lockers_done || '/' || p.lockers_total AS locks, p.blocks_done || '/' || p.blocks_total AS blocks, p.tuples_done || '/' || p.tuples_total AS tupes, p.partitions_done || '/' || p.partitions_total AS parts FROM pg_stat_progress_create_index p JOIN pg_class c  ON p.relid = c.oid JOIN pg_class ic ON p.index_relid = ic.oid;",
		},
		{
			Name:        "Table Replication State",
			Description: "The state of logical replication for each table in the public schema",
			SQL:         "SELECT s.subname AS subscription, r.srsubstate AS table_state, ARRAY_AGG(c.relname ORDER BY c.relname) AS tables FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_subscription_rel r ON r.srrelid = c.oid LEFT JOIN pg_subscription s     ON s.oid = r.srsubid WHERE n.nspname = 'public' AND c.relkind IN ('r','p','f') GROUP BY s.subname, r.srsubstate ORDER BY s.subname, r.srsubstate;",
		},
		{
			Name:        "Configuration Settings",
			Description: "All current PostgreSQL configuration settings",
			SQL:         "SELECT name, setting, unit, category, short_desc FROM pg_settings ORDER BY category, name;",
		},
	}

	data, err := json.MarshalIndent(defaultQueries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal default queries: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write default queries: %w", err)
	}

	return nil
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
