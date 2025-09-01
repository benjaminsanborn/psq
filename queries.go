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
