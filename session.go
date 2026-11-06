package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"
)

// Settings are process-wide options set from command-line flags before any
// connection is opened. They are read-only afterwards.
type Settings struct {
	// StatementTimeout bounds every statement psq runs; 0 disables it
	StatementTimeout time.Duration
	// RefreshInterval is the default auto-refresh period for tabs
	RefreshInterval time.Duration
	// MaxRows caps how many rows the TUI renders for a saved query
	MaxRows int
}

var settings = Settings{
	StatementTimeout: 10 * time.Second,
	RefreshInterval:  time.Second,
	MaxRows:          1000,
}

// applicationName identifies psq's sessions in pg_stat_activity
const applicationName = "psq"

// sessionConnector wraps a driver.Connector and runs setup statements on
// every new physical connection. The statement timeout is set this way rather
// than as a startup parameter because PgBouncer rejects unknown startup
// parameters by default.
type sessionConnector struct {
	base  driver.Connector
	setup []string
}

func newSessionConnector(base driver.Connector) *sessionConnector {
	var setup []string
	if ms := settings.StatementTimeout.Milliseconds(); ms > 0 {
		setup = append(setup, fmt.Sprintf("SET statement_timeout = %d", ms))
	}
	return &sessionConnector{base: base, setup: setup}
}

func (c *sessionConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if len(c.setup) == 0 {
		return conn, nil
	}
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("driver connection does not support session setup")
	}
	for _, stmt := range c.setup {
		if _, err := execer.ExecContext(ctx, stmt, nil); err != nil {
			conn.Close()
			return nil, fmt.Errorf("session setup %q failed: %w", stmt, err)
		}
	}
	return conn, nil
}

func (c *sessionConnector) Driver() driver.Driver {
	return c.base.Driver()
}

// queryRows runs a saved (user-written) query inside a READ ONLY transaction,
// so a query that writes fails instead of modifying data on every refresh.
// It returns at most maxRows rows (0 means no limit) and reports whether more
// were available.
func queryRows(ctx context.Context, db *sql.DB, query string, maxRows int) (columns []string, rows [][]any, truncated bool, err error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to start read-only transaction: %w", err)
	}
	defer tx.Rollback()

	r, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to execute query: %w", err)
	}
	defer r.Close()

	columns, err = r.Columns()
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to get columns: %w", err)
	}

	for r.Next() {
		if maxRows > 0 && len(rows) == maxRows {
			truncated = true
			break
		}
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := r.Scan(ptrs...); err != nil {
			return nil, nil, false, fmt.Errorf("failed to scan row: %w", err)
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		rows = append(rows, values)
	}
	if err := r.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("error iterating rows: %w", err)
	}
	return columns, rows, truncated, nil
}

// cellString formats a scanned value for display
func cellString(v any) string {
	switch v := v.(type) {
	case nil:
		return "NULL"
	case time.Time:
		return v.Format("2006-01-02 15:04:05.000Z07:00")
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}
