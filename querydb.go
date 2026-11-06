package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type QueryDB struct {
	db *sql.DB
}

func NewQueryDB() (*QueryDB, error) {
	configDir := filepath.Join(os.ExpandEnv("$HOME"), ".psq")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	dbPath := filepath.Join(configDir, "queries.db")

	// Connect to SQLite database
	db, err := sql.Open("sqlite", dbPath)
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
			refresh_seconds INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_queries_name ON queries(name);
	`

	_, err := qdb.db.Exec(schema)
	if err != nil {
		return err
	}

	// Remove legacy "Active" query now that it's a built-in view
	_, _ = qdb.db.Exec("DELETE FROM queries WHERE name = 'Active'")
	// Saved queries can't share a name with a built-in tab; keep them under a new name
	_, _ = qdb.db.Exec("UPDATE OR IGNORE queries SET name = name || ' (saved)' WHERE name IN ('Home', 'Statements')")

	// Databases from older versions may predate these columns
	if !qdb.columnExists("order_position") {
		if _, err := qdb.db.Exec("ALTER TABLE queries ADD COLUMN order_position INTEGER"); err != nil {
			return err
		}
		// Show every existing query as a tab, alphabetically
		if _, err := qdb.db.Exec(`
			UPDATE queries
			SET order_position = (SELECT COUNT(*) FROM queries q2 WHERE q2.name <= queries.name)
		`); err != nil {
			return err
		}
	}
	if !qdb.columnExists("refresh_seconds") {
		if _, err := qdb.db.Exec("ALTER TABLE queries ADD COLUMN refresh_seconds INTEGER"); err != nil {
			return err
		}
	}
	_, err = qdb.db.Exec("CREATE INDEX IF NOT EXISTS idx_queries_order ON queries(order_position)")
	return err
}

func (qdb *QueryDB) columnExists(column string) bool {
	var n int
	err := qdb.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('queries') WHERE name = ?", column).Scan(&n)
	return err == nil && n > 0
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
	configDir := filepath.Join(os.ExpandEnv("$HOME"), ".psq")
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

const queryColumns = "name, description, sql, order_position, refresh_seconds"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanQuery(r rowScanner) (Query, error) {
	var q Query
	var orderPos, refresh sql.NullInt64
	if err := r.Scan(&q.Name, &q.Description, &q.SQL, &orderPos, &refresh); err != nil {
		return q, err
	}
	if orderPos.Valid {
		pos := int(orderPos.Int64)
		q.OrderPosition = &pos
	}
	if refresh.Valid {
		sec := int(refresh.Int64)
		q.RefreshSeconds = &sec
	}
	return q, nil
}

func (qdb *QueryDB) selectQueries(where string) ([]Query, error) {
	rows, err := qdb.db.Query("SELECT " + queryColumns + " FROM queries " + where)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []Query
	for rows.Next() {
		q, err := scanQuery(rows)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, rows.Err()
}

// LoadQueries returns the queries shown as tabs, in tab order
func (qdb *QueryDB) LoadQueries() ([]Query, error) {
	return qdb.selectQueries("WHERE order_position IS NOT NULL ORDER BY order_position, name")
}

// LoadAllQueries returns every saved query, tabs first, then hidden ones by name
func (qdb *QueryDB) LoadAllQueries() ([]Query, error) {
	return qdb.selectQueries("ORDER BY COALESCE(order_position, 999999), name")
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// SaveQuery inserts or replaces a query by name; used for imports
func (qdb *QueryDB) SaveQuery(query Query) error {
	_, err := qdb.db.Exec(`
		INSERT OR REPLACE INTO queries (name, description, sql, order_position, refresh_seconds, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, query.Name, query.Description, query.SQL, nullableInt(query.OrderPosition), nullableInt(query.RefreshSeconds))
	return err
}

// ErrQueryExists is returned when creating or renaming a query would take a
// name another saved query already uses
var ErrQueryExists = errors.New("a query with that name already exists")

// CreateQuery inserts a new query, refusing to overwrite an existing one
func (qdb *QueryDB) CreateQuery(query Query) error {
	res, err := qdb.db.Exec(`
		INSERT INTO queries (name, description, sql, order_position, refresh_seconds)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO NOTHING
	`, query.Name, query.Description, query.SQL, nullableInt(query.OrderPosition), nullableInt(query.RefreshSeconds))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrQueryExists
	}
	return nil
}

// UpdateQuery replaces the query saved as oldName, which may involve a rename
func (qdb *QueryDB) UpdateQuery(oldName string, query Query) error {
	if query.Name != oldName {
		var exists bool
		if err := qdb.db.QueryRow("SELECT EXISTS(SELECT 1 FROM queries WHERE name = ?)", query.Name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrQueryExists
		}
	}
	res, err := qdb.db.Exec(`
		UPDATE queries
		SET name = ?, description = ?, sql = ?, order_position = ?, refresh_seconds = ?, updated_at = CURRENT_TIMESTAMP
		WHERE name = ?
	`, query.Name, query.Description, query.SQL, nullableInt(query.OrderPosition), nullableInt(query.RefreshSeconds), oldName)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("query %q no longer exists", oldName)
	}
	return nil
}

func (qdb *QueryDB) DeleteQuery(name string) error {
	_, err := qdb.db.Exec("DELETE FROM queries WHERE name = ?", name)
	return err
}

func (qdb *QueryDB) GetQuery(name string) (Query, error) {
	return scanQuery(qdb.db.QueryRow("SELECT "+queryColumns+" FROM queries WHERE name = ?", name))
}

func (qdb *QueryDB) LoadFromDumpFile(filePath string) error {
	// Check if dump file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("dump file does not exist: %s", filePath)
	}

	// Open the dump database
	dumpDB, err := sql.Open("sqlite", filePath)
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
