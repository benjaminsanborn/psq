package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type QueryDB struct {
	db *sql.DB
}

func NewQueryDB() (*QueryDB, error) {
	configDir := filepath.Join(os.ExpandEnv("$HOME"), ".psqi")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	dbPath := filepath.Join(configDir, "queries.db")

	// Connect to SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	queryDB := &QueryDB{db: db}

	// Initialize schema
	if err := queryDB.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Migrate existing queries if needed
	if err := queryDB.migrateFromFiles(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate queries: %w", err)
	}

	return queryDB, nil
}

func (qdb *QueryDB) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS queries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL,
			sql TEXT NOT NULL,
			order_position INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_queries_name ON queries(name);
		CREATE INDEX IF NOT EXISTS idx_queries_order ON queries(order_position);
	`

	_, err := qdb.db.Exec(schema)
	if err != nil {
		return err
	}

	// Check if order_position column exists (for migration)
	rows, err := qdb.db.Query("PRAGMA table_info(queries)")
	if err != nil {
		return err
	}
	defer rows.Close()

	hasOrderColumn := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue interface{}

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			continue
		}

		if name == "order_position" {
			hasOrderColumn = true
			break
		}
	}

	// Add order_position column if it doesn't exist
	if !hasOrderColumn {
		_, err = qdb.db.Exec("ALTER TABLE queries ADD COLUMN order_position INTEGER")
		if err != nil {
			return err
		}

		// Set default order for existing queries
		_, err = qdb.db.Exec(`
			UPDATE queries 
			SET order_position = (
				SELECT ROW_NUMBER() OVER (ORDER BY name) 
				FROM (SELECT DISTINCT name FROM queries q2 WHERE q2.name = queries.name)
			)
		`)
		if err != nil {
			return err
		}
	}

	return nil
}

func (qdb *QueryDB) migrateFromFiles() error {
	// Check if we already have queries in the database
	var count int
	err := qdb.db.QueryRow("SELECT COUNT(*) FROM queries").Scan(&count)
	if err != nil {
		return err
	}

	// If we already have queries, don't migrate
	if count > 0 {
		return nil
	}

	// Try to load queries from the old file system
	configDir := filepath.Join(os.ExpandEnv("$HOME"), ".psqi")
	sqlDir := filepath.Join(configDir, "queries")

	// Check for a default dump file first
	defaultDumpFile := filepath.Join(configDir, "default_queries.db")

	if _, err := os.Stat(defaultDumpFile); err == nil {
		// Load from default dump file
		return qdb.LoadFromDumpFile(defaultDumpFile)
	}

	// If SQL directory doesn't exist, create default queries
	if _, err := os.Stat(sqlDir); os.IsNotExist(err) {
		return qdb.createDefaultQueries()
	}

	// Load queries from SQL files
	queries, err := loadQueriesFromSQL(sqlDir)
	if err != nil {
		// If loading fails, create default queries
		return qdb.createDefaultQueries()
	}

	// Insert loaded queries into database
	for _, query := range queries {
		if err := qdb.SaveQuery(query); err != nil {
			return fmt.Errorf("failed to migrate query %s: %w", query.Name, err)
		}
	}

	return nil
}

func (qdb *QueryDB) createDefaultQueries() error {
	defaultQueries := []Query{
		{
			Name:          "Active",
			Description:   "Show current active connections",
			SQL:           "SELECT pid, LEFT(query,50) AS query, LEFT(usename,8) AS name, LEFT(state,10) AS state, LEFT((NOW() - query_start)::text,8) as age, wait_event, wait_event_type FROM pg_stat_activity WHERE state IS NOT NULL AND state != 'idle' ORDER BY NOW() - query_start DESC;",
			OrderPosition: &[]int{1}[0],
		},
		{
			Name:          "Lock Information",
			Description:   "Show current locks",
			SQL:           "SELECT l.pid, l.mode, l.granted, a.usename, a.query FROM pg_locks l JOIN pg_stat_activity a ON l.pid = a.pid WHERE NOT l.granted ORDER BY l.pid;",
			OrderPosition: &[]int{2}[0],
		},
		{
			Name:          "Replication Lag",
			Description:   "Show replication lag information",
			SQL:           "SELECT application_name, pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes, client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn FROM pg_stat_replication;",
			OrderPosition: &[]int{3}[0],
		},
		{
			Name:          "Top Queries",
			Description:   "Requires pg_stat_statements; identifies heavy hitters",
			SQL:           "SELECT LEFT(query, 40) AS query, calls, total_exec_time, mean_exec_time, rows, shared_blks_hit, shared_blks_read, temp_blks_written FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 25;",
			OrderPosition: &[]int{4}[0],
		},
		{
			Name:          "Index Creation",
			Description:   "Show progress of index creation operations",
			SQL:           "SELECT p.pid, c.relname AS table_name, ic.relname AS index_name, p.phase, p.lockers_done || '/' || p.lockers_total AS locks, p.blocks_done || '/' || p.blocks_total AS blocks, p.tuples_done || '/' || p.tuples_total AS tupes, p.partitions_done || '/' || p.partitions_total AS parts FROM pg_stat_progress_create_index p JOIN pg_class c  ON p.relid = c.oid JOIN pg_class ic ON p.index_relid = ic.oid;",
			OrderPosition: &[]int{5}[0],
		},
		{
			Name:          "Table Replication State",
			Description:   "The state of logical replication for each table in the public schema",
			SQL:           "SELECT s.subname AS subscription, r.srsubstate AS table_state, ARRAY_AGG(c.relname ORDER BY c.relname) AS tables FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_subscription_rel r ON r.srrelid = c.oid LEFT JOIN pg_subscription s     ON s.oid = r.srsubid WHERE n.nspname = 'public' AND c.relkind IN ('r','p','f') GROUP BY s.subname, r.srsubstate ORDER BY s.subname, r.srsubstate;",
			OrderPosition: &[]int{6}[0],
		},
		{
			Name:          "Configuration Settings",
			Description:   "All current PostgreSQL configuration settings",
			SQL:           "SELECT name, setting, unit, category, short_desc FROM pg_settings ORDER BY category, name;",
			OrderPosition: &[]int{7}[0],
		},
	}

	for _, query := range defaultQueries {
		if err := qdb.SaveQuery(query); err != nil {
			return fmt.Errorf("failed to create default query %s: %w", query.Name, err)
		}
	}

	return nil
}

func (qdb *QueryDB) LoadQueries() ([]Query, error) {
	// First check if order_position column exists
	hasOrderColumn := qdb.hasOrderPositionColumn()

	var query string
	if hasOrderColumn {
		query = `
			SELECT name, description, sql, order_position 
			FROM queries 
			WHERE order_position IS NOT NULL 
			ORDER BY order_position, name
		`
	} else {
		query = `
			SELECT name, description, sql 
			FROM queries 
			ORDER BY name
		`
	}

	rows, err := qdb.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []Query
	for rows.Next() {
		var query Query
		if hasOrderColumn {
			var orderPos sql.NullInt64
			if err := rows.Scan(&query.Name, &query.Description, &query.SQL, &orderPos); err != nil {
				return nil, err
			}

			if orderPos.Valid {
				pos := int(orderPos.Int64)
				query.OrderPosition = &pos
			}
		} else {
			if err := rows.Scan(&query.Name, &query.Description, &query.SQL); err != nil {
				return nil, err
			}
		}

		queries = append(queries, query)
	}

	return queries, rows.Err()
}

func (qdb *QueryDB) hasOrderPositionColumn() bool {
	rows, err := qdb.db.Query("PRAGMA table_info(queries)")
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue interface{}

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			continue
		}

		if name == "order_position" {
			return true
		}
	}
	return false
}

func (qdb *QueryDB) LoadAllQueries() ([]Query, error) {
	// First check if order_position column exists
	hasOrderColumn := qdb.hasOrderPositionColumn()

	var query string
	if hasOrderColumn {
		query = `
			SELECT name, description, sql, order_position 
			FROM queries 
			ORDER BY COALESCE(order_position, 999999), name
		`
	} else {
		query = `
			SELECT name, description, sql 
			FROM queries 
			ORDER BY name
		`
	}

	rows, err := qdb.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []Query
	for rows.Next() {
		var query Query
		if hasOrderColumn {
			var orderPos sql.NullInt64
			if err := rows.Scan(&query.Name, &query.Description, &query.SQL, &orderPos); err != nil {
				return nil, err
			}

			if orderPos.Valid {
				pos := int(orderPos.Int64)
				query.OrderPosition = &pos
			}
		} else {
			if err := rows.Scan(&query.Name, &query.Description, &query.SQL); err != nil {
				return nil, err
			}
		}

		queries = append(queries, query)
	}

	return queries, rows.Err()
}

func (qdb *QueryDB) SaveQuery(query Query) error {
	// Check if order_position column exists
	hasOrderColumn := qdb.hasOrderPositionColumn()

	if hasOrderColumn {
		var orderPos interface{}
		if query.OrderPosition != nil {
			orderPos = *query.OrderPosition
		}

		_, err := qdb.db.Exec(`
			INSERT OR REPLACE INTO queries (name, description, sql, order_position, updated_at) 
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		`, query.Name, query.Description, query.SQL, orderPos)

		return err
	} else {
		_, err := qdb.db.Exec(`
			INSERT OR REPLACE INTO queries (name, description, sql, updated_at) 
			VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		`, query.Name, query.Description, query.SQL)

		return err
	}
}

func (qdb *QueryDB) DeleteQuery(name string) error {
	_, err := qdb.db.Exec("DELETE FROM queries WHERE name = ?", name)
	return err
}

func (qdb *QueryDB) GetQuery(name string) (Query, error) {
	var query Query

	// First check if order_position column exists
	hasOrderColumn := qdb.hasOrderPositionColumn()

	if hasOrderColumn {
		var orderPos sql.NullInt64
		err := qdb.db.QueryRow("SELECT name, description, sql, order_position FROM queries WHERE name = ?", name).
			Scan(&query.Name, &query.Description, &query.SQL, &orderPos)

		if err != nil {
			return query, err
		}

		if orderPos.Valid {
			pos := int(orderPos.Int64)
			query.OrderPosition = &pos
		}
	} else {
		err := qdb.db.QueryRow("SELECT name, description, sql FROM queries WHERE name = ?", name).
			Scan(&query.Name, &query.Description, &query.SQL)

		if err != nil {
			return query, err
		}
	}

	return query, nil
}

func (qdb *QueryDB) DumpToFile(filePath string) error {
	// Create the dump database
	dumpDB, err := sql.Open("sqlite3", filePath)
	if err != nil {
		return fmt.Errorf("failed to create dump database: %w", err)
	}
	defer dumpDB.Close()

	// Create the schema in the dump database
	schema := `
		CREATE TABLE IF NOT EXISTS queries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL,
			sql TEXT NOT NULL,
			order_position INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_queries_name ON queries(name);
		CREATE INDEX IF NOT EXISTS idx_queries_order ON queries(order_position);
	`

	if _, err := dumpDB.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema in dump database: %w", err)
	}

	// Load all queries from current database (including hidden ones)
	queries, err := qdb.LoadAllQueries()
	if err != nil {
		return fmt.Errorf("failed to load queries for dump: %w", err)
	}

	// Insert queries into dump database
	for _, query := range queries {
		var orderPos interface{}
		if query.OrderPosition != nil {
			orderPos = *query.OrderPosition
		}

		_, err := dumpDB.Exec(`
			INSERT INTO queries (name, description, sql, order_position) 
			VALUES (?, ?, ?, ?)
		`, query.Name, query.Description, query.SQL, orderPos)

		if err != nil {
			return fmt.Errorf("failed to insert query %s into dump: %w", query.Name, err)
		}
	}

	return nil
}

func (qdb *QueryDB) LoadFromDumpFile(filePath string) error {
	// Check if dump file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("dump file does not exist: %s", filePath)
	}

	// Open the dump database
	dumpDB, err := sql.Open("sqlite3", filePath)
	if err != nil {
		return fmt.Errorf("failed to open dump database: %w", err)
	}
	defer dumpDB.Close()

	// Load queries from dump database
	rows, err := dumpDB.Query("SELECT name, description, sql, order_position FROM queries ORDER BY COALESCE(order_position, 999999), name")
	if err != nil {
		return fmt.Errorf("failed to query dump database: %w", err)
	}
	defer rows.Close()

	var queries []Query
	for rows.Next() {
		var query Query
		var orderPos sql.NullInt64
		if err := rows.Scan(&query.Name, &query.Description, &query.SQL, &orderPos); err != nil {
			return fmt.Errorf("failed to scan query from dump: %w", err)
		}

		if orderPos.Valid {
			pos := int(orderPos.Int64)
			query.OrderPosition = &pos
		}

		queries = append(queries, query)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating dump queries: %w", err)
	}

	// Insert queries into current database
	for _, query := range queries {
		if err := qdb.SaveQuery(query); err != nil {
			return fmt.Errorf("failed to load query %s from dump: %w", query.Name, err)
		}
	}

	return nil
}

func (qdb *QueryDB) Close() error {
	return qdb.db.Close()
}

// Legacy file loading functions for migration
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
