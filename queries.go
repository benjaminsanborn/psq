package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Query results are tagged with the sequence number of the runQuery call that
// produced them. Only the latest dispatch is applied; anything older is a
// stale response for a tab the user already left.
type queryResultMsg struct {
	seq    int
	result string
}

type queryErrorMsg struct {
	seq int
	err string
}

// activeProcessesMsg carries freshly fetched processes back to the main
// goroutine, which owns ActiveView state
type activeProcessesMsg struct {
	seq       int
	processes []ActiveProcess
}

// homeDataMsg carries Home dashboard data back to the main goroutine, which
// owns the sparkline history and commit-rate state
type homeDataMsg struct {
	seq  int
	data homeData
}

// dbConnectedMsg hands a newly opened connection to the main goroutine, which
// owns m.db, so it can install it and re-run the query
type dbConnectedMsg struct {
	seq   int
	db    *sql.DB
	query Query
}

// psqlExitMsg is sent when the psql subprocess opened with "x" exits
type psqlExitMsg struct {
	err error
}

const (
	refreshInterval   = 1 * time.Second
	maxRefreshBackoff = 30 * time.Second
)

// refreshDelay returns how long to wait before the next auto-refresh, backing
// off exponentially while queries keep failing
func refreshDelay(consecutiveErrors int) time.Duration {
	delay := refreshInterval
	for i := 1; i < consecutiveErrors; i++ {
		delay *= 2
		if delay >= maxRefreshBackoff {
			return maxRefreshBackoff
		}
	}
	return delay
}

var globalQueryDB *QueryDB

func initQueryDB() error {
	var err error
	globalQueryDB, err = NewQueryDB()
	return err
}

func loadQueries() ([]Query, error) {
	if globalQueryDB == nil {
		if err := initQueryDB(); err != nil {
			return nil, fmt.Errorf("failed to initialize query database: %w", err)
		}
	}

	return globalQueryDB.LoadQueries()
}

// runQuery must be called from the main goroutine (Update). It snapshots the
// model state the query needs; the returned command touches only that
// snapshot and reports back through messages, never by writing to m.
func (m *Model) runQuery(query Query) tea.Cmd {
	m.querySeq++
	seq := m.querySeq
	db := m.db
	service := m.service
	chartWidth := GetChartWidth(m.width)

	// Only the latest dispatch is ever shown, so stop the previous one on the
	// server instead of letting it run to completion
	if m.cancelInflight != nil {
		m.cancelInflight()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelInflight = cancel

	return func() tea.Msg {
		if db == nil {
			newDB, err := connectDB(service)
			if err != nil {
				return queryErrorMsg{seq: seq, err: fmt.Sprintf("Failed to connect: %v", err)}
			}
			return dbConnectedMsg{seq: seq, db: newDB, query: query}
		}

		if IsActiveTab(query.Name) {
			processes, err := FetchActiveProcesses(ctx, db)
			if err != nil {
				return queryErrorMsg{seq: seq, err: fmt.Sprintf("Query failed: %v", err)}
			}
			return activeProcessesMsg{seq: seq, processes: processes}
		}

		if IsHomeTab(query.Name) {
			data, err := fetchHomeData(ctx, db, query.SQL, chartWidth)
			if err != nil {
				return queryErrorMsg{seq: seq, err: fmt.Sprintf("Query failed: %v", err)}
			}
			return homeDataMsg{seq: seq, data: data}
		}

		result, err := executeQuery(ctx, db, query.SQL)
		if err != nil {
			return queryErrorMsg{seq: seq, err: fmt.Sprintf("Query failed: %v", err)}
		}
		return queryResultMsg{seq: seq, result: result}
	}
}
