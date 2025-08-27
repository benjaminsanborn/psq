package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	viewport    viewport.Model
	ready       bool
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
			ready:       false,
		}
	}

	return &Model{
		queries:     queries,
		selected:    0,
		results:     "Select a query to run...",
		service:     service,
		autoRefresh: false,
		ready:       false,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
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
			if msg.Alt {
				m.viewport.LineUp(1)
			} else {
				if m.selected > 0 {
					m.selected--
				}
			}
		case "down", "j":
			if msg.Alt {
				m.viewport.LineDown(1)
			} else {
				if m.selected < len(m.queries)-1 {
					m.selected++
				}
			}
		case "pageup":
			m.viewport.HalfViewUp()
		case "pagedown":
			m.viewport.HalfViewDown()
		case "home":
			m.viewport.GotoTop()
		case "end":
			m.viewport.GotoBottom()
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

		if !m.ready {
			// Initialize viewport on first window size message
			headerHeight := 8 // Title + controls + query list
			footerHeight := 0
			verticalMarginHeight := headerHeight + footerHeight

			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.Style = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62")).
				PaddingRight(2)

			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 8
		}

		// Update viewport content
		if m.loading {
			m.viewport.SetContent("Running query...")
		} else if m.err != "" {
			m.viewport.SetContent("Error: " + m.err)
		} else {
			m.viewport.SetContent(m.results)
		}
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
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}

	return m, nil
}

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	s := "Navigation: ↑/↓ to select query, Alt+↑/↓ to scroll, PgUp/PgDn for half page, Home/End for top/bottom\n"
	s += "Controls: Enter to run query (auto-refresh), r to run once, a to toggle auto-refresh, q to quit\n"
	if m.autoRefresh {
		s += "Auto-refresh: ON (every 2s)\n"
	} else {
		s += "Auto-refresh: OFF\n"
	}
	s += "\n"

	// Query list
	for i, query := range m.queries {
		if i == m.selected {
			s += "> " + query.Name
		} else {
			s += "  " + query.Name
		}
		if i < len(m.queries)-1 {
			s += " | "
		}
	}
	s += "\n"

	s += "\n" + strings.Repeat("─", m.width) + "\n"

	// Update viewport content
	if m.loading {
		m.viewport.SetContent("Running query...")
	} else if m.err != "" {
		m.viewport.SetContent("Error: " + m.err)
	} else {
		m.viewport.SetContent(m.results)
	}

	// Add viewport to output
	s += "\n" + m.viewport.View()

	return s
}
