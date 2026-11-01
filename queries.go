package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type queryResultMsg string
type queryErrorMsg string

// activeProcessesMsg carries freshly fetched processes back to the main
// goroutine, which owns ActiveView state
type activeProcessesMsg []ActiveProcess

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

func (m *Model) runQuery(query Query) tea.Cmd {
	return func() tea.Msg {
		// Check if connection is still alive, reconnect if needed
		if m.db == nil || m.db.Ping() != nil {
			newDB, err := connectDB(m.service)
			if err != nil {
				return queryErrorMsg(fmt.Sprintf("Failed to reconnect: %v", err))
			}
			// Close old connection if it exists
			if m.db != nil {
				m.db.Close()
			}
			m.db = newDB
		}

		// Capture local ref — another goroutine (Close) may nil out m.db
		db := m.db
		if db == nil {
			return queryErrorMsg("Connection closed")
		}

		if IsActiveTab(query.Name) {
			processes, err := FetchActiveProcesses(db)
			if err != nil {
				return queryErrorMsg(fmt.Sprintf("Query failed: %v", err))
			}
			return activeProcessesMsg(processes)
		}

		result, err := renderConnectionBarChart(db, query.SQL, query.Name, m)
		if err != nil {
			return queryErrorMsg(fmt.Sprintf("Query failed: %v", err))
		}

		return queryResultMsg(result)
	}
}
