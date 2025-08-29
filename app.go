package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	queries       []Query
	selected      int
	results       string
	loading       bool
	err           string
	width         int
	height        int
	service       string
	lastQuery     Query
	viewport      viewport.Model
	ready         bool
	lastRefreshAt time.Time
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
			queries:  []Query{},
			selected: 0,
			err:      fmt.Sprintf("Failed to load queries: %v", err),
			service:  service,
			ready:    false,
		}
	}

	return &Model{
		queries:  queries,
		selected: 0,
		results:  "Select a query to run...",
		service:  service,
		ready:    false,
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
		if !m.ready {
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		// Query selection
		case "left", "h":
			if m.selected > 0 {
				m.selected--
				m.loading = true
				m.err = ""
				m.results = ""
				m.lastQuery = m.queries[m.selected]
				m.lastRefreshAt = time.Now()
				// Update display immediately
				return m, m.runQuery(m.queries[m.selected])
			}
		case "right", "l":
			if m.selected < len(m.queries)-1 {
				m.selected++
				m.loading = true
				m.err = ""
				m.results = ""
				m.lastQuery = m.queries[m.selected]
				m.lastRefreshAt = time.Now()
				// Update display immediately
				m.updateContent()
				return m, m.runQuery(m.queries[m.selected])
			}

		// Results viewport scrolling
		case "up", "k":
			m.viewport.LineUp(1)
		case "down", "j":
			m.viewport.LineDown(1)
		case "pageup":
			m.viewport.HalfViewUp()
		case "pagedown":
			m.viewport.HalfViewDown()
		case "home":
			m.viewport.GotoTop()
		case "end":
			m.viewport.GotoBottom()

		// Query execution
		case "enter", " ", "r":
			if len(m.queries) > 0 && m.canRefresh() {
				m.loading = true
				m.err = ""
				m.lastQuery = m.queries[m.selected]
				return m, m.runQuery(m.queries[m.selected])
			}
		case "e":
			if len(m.queries) > 0 {
				configPath := filepath.Join(os.ExpandEnv("$HOME"), ".pgi", "queries.json")
				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vi"
				}
				cmd := exec.Command(editor, configPath)
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					if err != nil {
						return queryErrorMsg(fmt.Sprintf("Failed to edit query: %v", err))
					}
					// Reload queries
					queries, err := loadQueries()
					if err != nil {
						return queryErrorMsg(fmt.Sprintf("Failed to reload queries: %v", err))
					}
					m.queries = queries
					if m.selected >= len(queries) {
						m.selected = len(queries) - 1
					}
					return nil
				})
			}
		}

		// Update content after any key press
		if m.ready {
			m.updateContent()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height)
			m.viewport.Style = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62"))
			m.ready = true

			// Execute first query immediately when ready
			if len(m.queries) > 0 {
				m.loading = true
				m.lastQuery = m.queries[m.selected]
				m.updateContent()
				return m, m.runQuery(m.queries[m.selected])
			}
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height
		}

		m.updateContent()
	case queryResultMsg:
		m.results = string(msg)
		m.loading = false
		m.lastRefreshAt = time.Now()
		m.updateContent()
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	case queryErrorMsg:
		m.err = string(msg)
		m.loading = false
		m.updateContent()
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	case tickMsg:
		if len(m.queries) > 0 && m.canRefresh() {
			m.loading = true
			m.updateContent()
			return m, m.runQuery(m.lastQuery)
		}
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}

	return m, nil
}

func (m *Model) updateContent() {
	var content string

	// Header section
	content += lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Padding(0, 1).
		Render("pgi")
	content += ": ←/→ to select query, e to edit query, q to quit\n"

	// Query list
	content += "\n "
	for i, query := range m.queries {
		if i == m.selected {
			content += lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render(query.Name)
		} else {
			content += query.Name
		}
		if i < len(m.queries)-1 {
			content += " | "
		}
	}
	content += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(strings.Repeat("─", m.width)) + "\n"

	// Results section
	if m.err != "" {
		content += "Error: " + m.err
	} else {
		content += m.results
	}

	m.viewport.SetContent(content)
}

func (m *Model) canRefresh() bool {
	return time.Since(m.lastRefreshAt) >= 500*time.Millisecond
}

func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	if !m.ready {
		return "Getting ready..."
	}

	return m.viewport.View()
}
