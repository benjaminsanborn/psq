package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
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
	queries         []Query
	selected        int
	results         string
	loading         bool
	err             string
	width           int
	height          int
	service         string
	lastQuery       Query
	viewport        viewport.Model
	ready           bool
	lastRefreshAt   time.Time
	searchMode      bool
	searchQuery     string
	filteredQueries []Query
	editMode        bool
	editQuery       Query
	nameInput       textinput.Model
	descInput       textinput.Model
	sqlTextarea     textarea.Model
	editFocus       int // 0=name, 1=description, 2=sql
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
		queries:         queries,
		selected:        0,
		results:         "Select a query to run...",
		service:         service,
		ready:           false,
		searchMode:      false,
		searchQuery:     "",
		filteredQueries: queries,
		editMode:        false,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) initEditor(query Query) {
	// Initialize name input
	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "Query name"
	m.nameInput.SetValue(query.Name)
	m.nameInput.CharLimit = 50
	m.nameInput.Width = 50

	// Initialize description input
	m.descInput = textinput.New()
	m.descInput.Placeholder = "Query description"
	m.descInput.SetValue(query.Description)
	m.descInput.CharLimit = 100
	m.descInput.Width = 50

	// Initialize SQL textarea
	m.sqlTextarea = textarea.New()
	m.sqlTextarea.Placeholder = "Enter your SQL query here..."
	m.sqlTextarea.SetValue(query.SQL)
	m.sqlTextarea.SetWidth(80)
	m.sqlTextarea.SetHeight(10)

	// Focus on the first input
	m.editFocus = 0
	m.nameInput.Focus()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.ready {
			return m, nil
		}

		// Handle edit mode
		if m.editMode {
			switch msg.String() {
			case "ctrl+c", "escape":
				m.editMode = false
				m.updateContent()
				return m, nil
			case "ctrl+s":
				// Save the query
				newQuery := Query{
					Name:        m.nameInput.Value(),
					Description: m.descInput.Value(),
					SQL:         m.sqlTextarea.Value(),
				}

				if globalQueryDB != nil {
					if err := globalQueryDB.SaveQuery(newQuery); err != nil {
						m.err = fmt.Sprintf("Failed to save query: %v", err)
					} else {
						// Reload queries
						queries, err := loadQueries()
						if err != nil {
							m.err = fmt.Sprintf("Failed to reload queries: %v", err)
						} else {
							m.queries = queries
							// Find the updated query in the list
							for i, q := range queries {
								if q.Name == newQuery.Name {
									m.selected = i
									break
								}
							}
						}
						m.editMode = false
					}
				}
				m.updateContent()
				return m, nil
			case "tab", "shift+tab":
				// Cycle through inputs
				if msg.String() == "tab" {
					m.editFocus = (m.editFocus + 1) % 3
				} else {
					m.editFocus = (m.editFocus + 2) % 3
				}

				// Update focus
				m.nameInput.Blur()
				m.descInput.Blur()
				m.sqlTextarea.Blur()

				switch m.editFocus {
				case 0:
					m.nameInput.Focus()
				case 1:
					m.descInput.Focus()
				case 2:
					m.sqlTextarea.Focus()
				}
				m.updateContent()
				return m, nil
			default:
				// Update the focused input
				var cmd tea.Cmd
				switch m.editFocus {
				case 0:
					m.nameInput, cmd = m.nameInput.Update(msg)
				case 1:
					m.descInput, cmd = m.descInput.Update(msg)
				case 2:
					m.sqlTextarea, cmd = m.sqlTextarea.Update(msg)
				}
				m.updateContent()
				return m, cmd
			}
		}

		// Handle search mode
		if m.searchMode {
			switch msg.String() {
			case "escape":
				m.searchMode = false
				m.searchQuery = ""
				m.filteredQueries = m.queries
				m.selected = 0
				m.updateContent()
				return m, nil
			case "enter":
				if len(m.filteredQueries) > 0 {
					m.searchMode = false
					selectedQuery := m.filteredQueries[m.selected]
					// Find index in original queries
					for i, q := range m.queries {
						if q.Name == selectedQuery.Name && q.SQL == selectedQuery.SQL {
							m.selected = i
							break
						}
					}
					m.loading = true
					m.err = ""
					m.results = ""
					m.lastQuery = selectedQuery
					m.updateContent()
					return m, m.runQuery(selectedQuery)
				}
			case "up", "ctrl+k":
				if m.selected > 0 {
					m.selected--
					m.updateContent()
				}
			case "down", "ctrl+j":
				if m.selected < len(m.filteredQueries)-1 {
					m.selected++
					m.updateContent()
				}
			case "backspace", "ctrl+h":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.filterQueries()
					m.updateContent()
				}
			default:
				if len(msg.String()) == 1 {
					m.searchQuery += msg.String()
					m.filterQueries()
					m.updateContent()
				}
			}
			return m, nil
		}

		// Normal mode
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "s":
			m.searchMode = true
			m.searchQuery = ""
			m.filteredQueries = m.queries
			m.selected = 0
			m.updateContent()
			return m, nil

		// Query selection
		case "left", "h":
			if m.selected > 0 {
				m.selected--
				m.loading = true
				m.err = ""
				m.results = ""
				m.lastQuery = m.queries[m.selected]
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
				m.editMode = true
				m.editQuery = m.queries[m.selected]
				m.initEditor(m.editQuery)
				m.updateContent()
				return m, nil
			}
		case "x":
			// Open psql prompt for current service
			config, err := getDBConfig(m.service)
			if err != nil {
				m.err = fmt.Sprintf("Failed to get DB config: %v", err)
				m.updateContent()
				return m, nil
			}

			// Build psql command with connection parameters
			args := []string{
				"-h", config.Host,
				"-p", config.Port,
				"-d", config.Database,
				"-U", config.User,
			}

			cmd := exec.Command("psql", args...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			// Set PGPASSWORD environment variable
			cmd.Env = append(os.Environ(), "PGPASSWORD="+config.Password)

			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				if err != nil {
					return queryErrorMsg(fmt.Sprintf("Failed to open psql: %v", err))
				}
				return nil
			})
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
	content += " " + lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("psqi@") +
		lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")).
			Render(m.service)
	if m.searchMode {
		content += ": type to search queries, ↑/↓ to navigate, Enter to select, Esc to cancel\n"
		content += "\nSearch: " + m.searchQuery + "█\n\n"

		// Display filtered queries
		if len(m.filteredQueries) == 0 {
			content += "No queries match your search"
		} else {
			for i, query := range m.filteredQueries {
				if i == m.selected {
					content += lipgloss.NewStyle().
						Bold(true).
						Foreground(lipgloss.Color("86")).
						Render("▶ " + query.Name + " - " + query.Description)
				} else {
					content += "  " + query.Name + " - " + query.Description
				}
				content += "\n"
			}
		}
	} else if m.editMode {
		content += ": Tab to switch fields, Ctrl+S to save, Esc to cancel\n\n"

		// Query editor
		content += lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("Edit Query") + "\n\n"

		// Name input
		nameStyle := lipgloss.NewStyle()
		if m.editFocus == 0 {
			nameStyle = nameStyle.BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("86"))
		}
		content += "Name:\n" + nameStyle.Render(m.nameInput.View()) + "\n\n"

		// Description input
		descStyle := lipgloss.NewStyle()
		if m.editFocus == 1 {
			descStyle = descStyle.BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("86"))
		}
		content += "Description:\n" + descStyle.Render(m.descInput.View()) + "\n\n"

		// SQL textarea
		sqlStyle := lipgloss.NewStyle()
		if m.editFocus == 2 {
			sqlStyle = sqlStyle.BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("86"))
		}
		content += "SQL:\n" + sqlStyle.Render(m.sqlTextarea.View()) + "\n"
	} else {
		content += ": ←/→ to select query, s to search, e to edit query, x for psql prompt, q to quit\n"

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

func (m *Model) filterQueries() {
	if m.searchQuery == "" {
		m.filteredQueries = m.queries
		return
	}

	var filtered []Query
	searchLower := strings.ToLower(m.searchQuery)

	for _, query := range m.queries {
		if strings.Contains(strings.ToLower(query.Name), searchLower) ||
			strings.Contains(strings.ToLower(query.Description), searchLower) {
			filtered = append(filtered, query)
		}
	}

	m.filteredQueries = filtered

	// Reset selection if out of bounds
	if m.selected >= len(m.filteredQueries) {
		m.selected = 0
	}
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
