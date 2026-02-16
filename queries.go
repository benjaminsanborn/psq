package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type queryResultMsg string
type queryErrorMsg string

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

		result, err := renderConnectionBarChart(m.db, query.SQL, query.Name, m)
		if err != nil {
			return queryErrorMsg(fmt.Sprintf("Query failed: %v", err))
		}

		return queryResultMsg(result)
	}
}
