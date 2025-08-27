package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type App struct {
	model   *Model
	service string
}

func NewApp(service string) *App {
	return &App{
		model:   NewModel(service),
		service: service,
	}
}

func (a *App) Run() error {
	p := tea.NewProgram(a.model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run program: %w", err)
	}
	return nil
}

type Model struct {
	queries     []Query
	selected    int
	results     string
	loading     bool
	err         string
	width       int
	height      int
	service     string
	lastQuery   Query
	autoRefresh bool
}

type Query struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
}

type tickMsg time.Time

func NewModel(service string) *Model {
	queries, err := loadQueries()
	if err != nil {
		return &Model{
			queries:     []Query{},
			selected:    0,
			err:         fmt.Sprintf("Failed to load queries: %v", err),
			service:     service,
			autoRefresh: false,
		}
	}

	return &Model{
		queries:     queries,
		selected:    0,
		results:     "Select a query to run...",
		service:     service,
		autoRefresh: false,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.queries)-1 {
				m.selected++
			}
		case "enter", " ":
			if len(m.queries) > 0 {
				m.loading = true
				m.err = ""
				m.lastQuery = m.queries[m.selected]
				m.autoRefresh = true
				return m, m.runQuery(m.queries[m.selected])
			}
		case "r":
			if len(m.queries) > 0 {
				m.loading = true
				m.err = ""
				m.lastQuery = m.queries[m.selected]
				m.autoRefresh = false
				return m, m.runQuery(m.queries[m.selected])
			}
		case "a":
			m.autoRefresh = !m.autoRefresh
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case queryResultMsg:
		m.results = string(msg)
		m.loading = false
	case queryErrorMsg:
		m.err = string(msg)
		m.loading = false
	case tickMsg:
		if m.autoRefresh && len(m.queries) > 0 {
			return m, m.runQuery(m.lastQuery)
		}
		return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}

	return m, nil
}

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	s := "PostgreSQL Monitor\n\n"
	s += "Use ↑/↓ to navigate, Enter to run query (auto-refresh), r to run once, a to toggle auto-refresh, q to quit\n"
	if m.autoRefresh {
		s += "Auto-refresh: ON (every 5s)\n"
	} else {
		s += "Auto-refresh: OFF\n"
	}
	s += "\n"

	// Query list
	s += "Queries:\n"
	for i, query := range m.queries {
		if i == m.selected {
			s += "> " + query.Name + "\n"
		} else {
			s += "  " + query.Name + "\n"
		}
	}

	s += "\n" + strings.Repeat("─", m.width) + "\n\n"

	// Results
	if m.loading {
		s += "Running query..."
	} else if m.err != "" {
		s += "Error: " + m.err
	} else {
		s += m.results
	}

	return s
}
