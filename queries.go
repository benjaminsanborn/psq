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
			SQL:         "SELECT pid, usename, state, NOW() - query_start AS age, state_change FROM pg_stat_activity WHERE state IS NOT NULL AND state != 'idle' ORDER BY query_start DESC;",
		},
		{
			Name:        "Subscriptions",
			Description: "Show logical replication subscription status",
			SQL:         "SELECT subname, pid, received_lsn, latest_end_lsn, latest_end_time FROM pg_stat_subscription;",
		},
		{
			Name:        "Index Creation",
			Description: "Show progress of index creation operations",
			SQL:         "SELECT pid, datname, relid, index_relid, command, phase, blocks_total, blocks_done, tuples_total, tuples_done FROM pg_stat_progress_create_index;",
		},
		{
			Name:        "Locks",
			Description: "Show current locks",
			SQL:         "SELECT l.pid, l.mode, l.granted, a.usename, a.query FROM pg_locks l JOIN pg_stat_activity a ON l.pid = a.pid WHERE NOT l.granted ORDER BY l.pid;",
		},
		{
			Name:        "Slow Queries",
			Description: "Show long-running queries",
			SQL:         "SELECT pid, usename, application_name, client_addr, state, query_start, now() - query_start as duration, query FROM pg_stat_activity WHERE state = 'active' AND query_start < now() - interval '5 seconds' ORDER BY query_start;",
		},
		{
			Name:        "Table Replication",
			Description: "Replication status of all tables in public schema",
			SQL:         "SELECT s.subname AS subscription, r.srsubstate AS table_state, ARRAY_AGG(c.relname ORDER BY c.relname) AS tables FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_subscription_rel r ON r.srrelid = c.oid LEFT JOIN pg_subscription s     ON s.oid = r.srsubid WHERE n.nspname = 'public' AND c.relkind IN ('r','p','f') GROUP BY s.subname, r.srsubstate ORDER BY s.subname, r.srsubstate;",
		},
		{
			Name:        "Replication Lag",
			Description: "Show replication lag information",
			SQL:         "SELECT application_name, client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn, pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes FROM pg_stat_replication;",
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
