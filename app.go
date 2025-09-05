package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
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
	p := tea.NewProgram(a.model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run program: %w", err)
	}
	return nil
}

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Click    key.Binding
	Execute  key.Binding
	Search   key.Binding
	Edit     key.Binding
	New      key.Binding
	Dump     key.Binding
	Psql     key.Binding
	ChatGPT  key.Binding
	Help     key.Binding
	Quit     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Left, k.Right, k.Click, k.Execute, k.Search, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Left, k.Right, k.Click, k.Execute},
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End},
		{k.Search, k.Edit, k.New, k.Dump, k.Psql},
		{k.ChatGPT, k.Help, k.Quit},
	}
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "scroll up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "scroll down"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "previous query"),
	),
	Right: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "next query"),
	),
	Click: key.NewBinding(
		key.WithKeys(),
		key.WithHelp("click", "select query"),
	),
	Execute: key.NewBinding(
		key.WithKeys("enter", " ", "r"),
		key.WithHelp("enter/space/r", "execute query"),
	),
	Search: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "search queries (type to filter, ↑/↓ navigate, enter select, esc cancel)"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit query"),
	),
	New: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new query"),
	),
	Dump: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "dump queries"),
	),
	Psql: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "psql prompt"),
	),
	ChatGPT: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "chatgpt prompt (in new/edit mode)"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pageup"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pagedown"),
		key.WithHelp("pgdn", "page down"),
	),
	Home: key.NewBinding(
		key.WithKeys("home"),
		key.WithHelp("home", "go to top"),
	),
	End: key.NewBinding(
		key.WithKeys("end"),
		key.WithHelp("end", "go to bottom"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("esc", "ctrl+c"),
		key.WithHelp("esc", "quit"),
	),
}

type Model struct {
	queries          []Query
	allQueries       []Query        // includes hidden queries for search
	tempQueries      map[string]int // temporary order positions for hidden queries
	selected         int
	previousSelected int // track selection before entering modals
	results          string
	loading          bool
	err              string
	width            int
	height           int
	service          string
	lastQuery        Query
	viewport         viewport.Model
	ready            bool
	lastRefreshAt    time.Time
	searchMode       bool
	searchQuery      string
	filteredQueries  []Query
	editMode         bool
	editQuery        Query
	nameInput        textinput.Model
	descInput        textinput.Model
	orderInput       textinput.Model
	sqlTextarea      textarea.Model
	editFocus        int // 0=name, 1=description, 2=order, 3=sql
	chatgptMode      bool
	chatgptInput     textinput.Model
	chatgptLoading   bool
	chatgptResponse  string // Store the generated SQL for review
	help             help.Model
	showHelp         bool
}

type Query struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	SQL           string `json:"sql"`
	OrderPosition *int   `json:"order_position,omitempty"` // nil means hidden from top bar
}

// ChatGPT API types
type ChatGPTRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatGPTResponse struct {
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Message Message `json:"message"`
}

type chatgptResponseMsg string
type chatgptErrorMsg string

type tickMsg time.Time

func NewModel(service string) *Model {
	queries, err := loadQueries()
	if err != nil {
		return &Model{
			queries:     []Query{},
			allQueries:  []Query{},
			tempQueries: make(map[string]int),
			selected:    0,
			err:         fmt.Sprintf("Failed to load queries: %v", err),
			service:     service,
			ready:       false,
		}
	}

	// Also load all queries (including hidden ones) for search
	allQueries := queries
	if globalQueryDB != nil {
		if allQueriesFromDB, err := globalQueryDB.LoadAllQueries(); err == nil {
			allQueries = allQueriesFromDB
		}
	}

	return &Model{
		queries:         queries,
		allQueries:      allQueries,
		tempQueries:     make(map[string]int),
		selected:        0,
		results:         "Select a query to run...",
		service:         service,
		ready:           false,
		searchMode:      false,
		searchQuery:     "",
		filteredQueries: queries,
		editMode:        false,
		help:            help.New(),
		showHelp:        false,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) getNextTempOrder() int {
	maxOrder := 0
	for _, q := range m.queries {
		if q.OrderPosition != nil && *q.OrderPosition > maxOrder {
			maxOrder = *q.OrderPosition
		}
	}
	for _, tempOrder := range m.tempQueries {
		if tempOrder > maxOrder {
			maxOrder = tempOrder
		}
	}
	return maxOrder + 1
}

func (m *Model) addTemporaryQuery(query Query) {
	if query.OrderPosition == nil {
		// Assign temporary order position
		tempOrder := m.getNextTempOrder()
		m.tempQueries[query.Name] = tempOrder

		// Create a copy with temporary order
		tempQuery := query
		tempQuery.OrderPosition = &tempOrder

		// Add to visible queries
		m.queries = append(m.queries, tempQuery)
	}
}

func (m *Model) isTemporaryQuery(queryName string) bool {
	_, exists := m.tempQueries[queryName]
	return exists
}

func (m *Model) ensureValidSelection() {
	if len(m.queries) == 0 {
		m.selected = 0
		return
	}
	if m.selected >= len(m.queries) {
		m.selected = len(m.queries) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
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

	// Initialize order input
	m.orderInput = textinput.New()
	m.orderInput.Placeholder = "Order position (empty to hide)"
	if query.OrderPosition != nil {
		m.orderInput.SetValue(fmt.Sprintf("%d", *query.OrderPosition))
	}
	m.orderInput.CharLimit = 10
	m.orderInput.Width = 30

	// Initialize SQL textarea
	m.sqlTextarea = textarea.New()
	m.sqlTextarea.Placeholder = "Enter your SQL query here..."
	m.sqlTextarea.SetValue(query.SQL)
	m.sqlTextarea.SetWidth(80)
	m.sqlTextarea.SetHeight(10)

	// Initialize ChatGPT input
	m.chatgptInput = textinput.New()
	m.chatgptInput.Placeholder = "Describe the SQL query you want (e.g., 'find all users created last week')"
	m.chatgptInput.CharLimit = 200
	m.chatgptInput.Width = 80

	// Focus on the first input
	m.editFocus = 0
	m.nameInput.Focus()
	m.chatgptMode = false
	m.chatgptLoading = false
	m.chatgptResponse = ""
}

func (m *Model) callChatGPT(prompt string) tea.Cmd {
	return func() tea.Msg {
		// Get OpenAI API key from environment
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return chatgptErrorMsg("OPENAI_API_KEY environment variable not set")
		}

		// Prepare the request
		fullPrompt := fmt.Sprintf("Generate a PostgreSQL query for the following request: %s\n\nPlease respond with ONLY the SQL query, no explanations or additional text.", prompt)

		reqBody := ChatGPTRequest{
			Model: "gpt-3.5-turbo",
			Messages: []Message{
				{
					Role:    "user",
					Content: fullPrompt,
				},
			},
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to marshal request: %v", err))
		}

		// Make the API call
		req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to create request: %v", err))
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to make API call: %v", err))
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return chatgptErrorMsg(fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(body)))
		}

		var chatResp ChatGPTResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to decode response: %v", err))
		}

		if len(chatResp.Choices) == 0 {
			return chatgptErrorMsg("No response from ChatGPT")
		}

		sql := strings.TrimSpace(chatResp.Choices[0].Message.Content)

		// Clean up the response - remove code block markers if present
		sql = strings.TrimPrefix(sql, "```sql")
		sql = strings.TrimPrefix(sql, "```")
		sql = strings.TrimSuffix(sql, "```")
		sql = strings.TrimSpace(sql)

		return chatgptResponseMsg(sql)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		// Handle mouse events only when ready and not in edit/search mode
		if !m.ready || m.editMode || m.searchMode {
			return m, nil
		}

		// Only handle left mouse button release (clicks)
		if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
			return m, nil
		}

		// Check if any query zone was clicked
		for i := range m.queries {
			zoneID := fmt.Sprintf("query_%d", i)
			if zone.Get(zoneID).InBounds(msg) {
				// Query was clicked, select it and run it
				if i < len(m.queries) {
					m.selected = i
					m.ensureValidSelection()
					m.loading = true
					m.err = ""
					m.results = ""
					m.lastQuery = m.queries[m.selected]
					m.updateContent()
					return m, m.runQuery(m.queries[m.selected])
				}
			}
		}

		return m, nil

	case tea.KeyMsg:
		if !m.ready {
			return m, nil
		}

		// Handle edit mode
		if m.editMode {
			// Handle ChatGPT mode within edit mode
			if m.chatgptMode {
				// Check for escape key by type as well as string
				if msg.Type == tea.KeyEscape || msg.String() == "escape" || msg.String() == "esc" || msg.String() == "ctrl+[" {
					m.chatgptMode = false
					m.chatgptInput.SetValue("")
					m.chatgptResponse = ""
					m.updateContent()
					return m, nil
				}
				switch msg.String() {
				case "enter":
					if !m.chatgptLoading && m.chatgptInput.Value() != "" && m.chatgptResponse == "" {
						m.chatgptLoading = true
						prompt := m.chatgptInput.Value()
						m.updateContent()
						return m, m.callChatGPT(prompt)
					}
				case "c":
					// Confirm and use the generated SQL
					if m.chatgptResponse != "" {
						m.chatgptMode = false
						m.sqlTextarea.SetValue(m.chatgptResponse)
						m.editFocus = 3 // Focus on SQL textarea
						m.sqlTextarea.Focus()
						m.nameInput.Blur()
						m.descInput.Blur()
						m.orderInput.Blur()
						m.chatgptInput.Blur()
						m.chatgptInput.SetValue("")
						m.chatgptResponse = ""
						m.updateContent()
						return m, nil
					}
				default:
					// Only allow editing the input if we haven't generated a response yet
					if m.chatgptResponse == "" && !m.chatgptLoading {
						var cmd tea.Cmd
						m.chatgptInput, cmd = m.chatgptInput.Update(msg)
						m.updateContent()
						return m, cmd
					}
				}
				return m, nil
			}

			// Check for escape key by type as well as string
			if msg.Type == tea.KeyEscape || msg.String() == "escape" || msg.String() == "esc" || msg.String() == "ctrl+c" || msg.String() == "ctrl+[" {
				m.editMode = false
				// Restore previous selection
				if m.previousSelected < len(m.queries) {
					m.selected = m.previousSelected
				}
				m.ensureValidSelection()
				m.updateContent()
				return m, nil
			}
			switch msg.String() {
			case "ctrl+s":
				// Save the query
				newQuery := Query{
					Name:        m.nameInput.Value(),
					Description: m.descInput.Value(),
					SQL:         m.sqlTextarea.Value(),
				}

				// Parse order position (but don't save temporary ones)
				if orderStr := strings.TrimSpace(m.orderInput.Value()); orderStr != "" {
					var pos int
					if n, err := fmt.Sscanf(orderStr, "%d", &pos); err == nil && n == 1 {
						// Only set if it's not a temporary position or user changed it
						if !m.isTemporaryQuery(m.editQuery.Name) || orderStr != fmt.Sprintf("%d", m.tempQueries[m.editQuery.Name]) {
							newQuery.OrderPosition = &pos
						}
					}
				}

				if globalQueryDB != nil {
					if err := globalQueryDB.SaveQuery(newQuery); err != nil {
						m.err = fmt.Sprintf("Failed to save query: %v", err)
						m.updateContent()
						return m, nil
					} else {
						// Reload queries
						queries, err := loadQueries()
						if err != nil {
							m.err = fmt.Sprintf("Failed to reload queries: %v", err)
							m.updateContent()
							return m, nil
						} else {
							m.queries = queries
							// Also reload all queries for search
							if allQueries, err := globalQueryDB.LoadAllQueries(); err == nil {
								m.allQueries = allQueries
							}
							// Find the updated/new query in the list
							for i, q := range queries {
								if q.Name == newQuery.Name {
									m.selected = i
									break
								}
							}
							m.ensureValidSelection()
						}
						m.editMode = false
						m.loading = true
						m.err = ""
						m.lastQuery = newQuery
						m.updateContent()
						// Execute the updated query
						return m, m.runQuery(newQuery)
					}
				}
				m.updateContent()
				return m, nil
			case "a":
				// Open ChatGPT prompt
				m.chatgptMode = true
				m.chatgptInput.Focus()
				m.chatgptInput.SetValue("")
				m.updateContent()
				return m, nil
			case "tab", "shift+tab":
				// Cycle through inputs (4 total: name, description, order, sql)
				if msg.String() == "tab" {
					m.editFocus = (m.editFocus + 1) % 4
				} else {
					m.editFocus = (m.editFocus + 3) % 4
				}

				// Update focus
				m.nameInput.Blur()
				m.descInput.Blur()
				m.orderInput.Blur()
				m.sqlTextarea.Blur()

				switch m.editFocus {
				case 0:
					m.nameInput.Focus()
				case 1:
					m.descInput.Focus()
				case 2:
					m.orderInput.Focus()
				case 3:
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
					m.orderInput, cmd = m.orderInput.Update(msg)
				case 3:
					m.sqlTextarea, cmd = m.sqlTextarea.Update(msg)
				}
				m.updateContent()
				return m, cmd
			}
		}

		// Handle search mode
		if m.searchMode {
			// Check for escape key by type as well as string
			if msg.Type == tea.KeyEscape || msg.String() == "escape" || msg.String() == "esc" || msg.String() == "ctrl+[" {
				m.searchMode = false
				m.searchQuery = ""
				m.filteredQueries = m.queries // Back to visible queries only
				// Restore previous selection
				if m.previousSelected < len(m.queries) {
					m.selected = m.previousSelected
				}
				m.ensureValidSelection()
				m.updateContent()
				return m, nil
			}
			switch msg.String() {
			case "enter":
				if len(m.filteredQueries) > 0 {
					m.searchMode = false
					selectedQuery := m.filteredQueries[m.selected]

					// If this is a hidden query, add it temporarily
					m.addTemporaryQuery(selectedQuery)

					// Find index in current queries (including temporary ones)
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

		// Handle help mode escape
		if m.showHelp && (msg.Type == tea.KeyEscape || msg.String() == "escape" || msg.String() == "esc" || msg.String() == "ctrl+[") {
			m.showHelp = false
			// Restore previous selection
			if m.previousSelected < len(m.queries) {
				m.selected = m.previousSelected
			}
			m.ensureValidSelection()
			m.updateContent()
			return m, nil
		}

		// Normal mode
		switch msg.String() {
		case "?":
			if !m.showHelp {
				m.previousSelected = m.selected
			} else {
				// Closing help, restore previous selection
				if m.previousSelected < len(m.queries) {
					m.selected = m.previousSelected
				}
				m.ensureValidSelection()
			}
			m.showHelp = !m.showHelp
			m.updateContent()
			return m, nil
		case "esc", "ctrl+c":
			return m, tea.Quit

		case "s":
			m.previousSelected = m.selected
			m.searchMode = true
			m.searchQuery = ""
			m.filteredQueries = m.allQueries // Use all queries for search
			m.selected = 0
			m.updateContent()
			return m, nil

		// Query selection
		case "left", "h":
			if m.selected > 0 {
				m.selected--
				m.ensureValidSelection()
				if len(m.queries) > 0 {
					m.loading = true
					m.err = ""
					m.results = ""
					m.lastQuery = m.queries[m.selected]
					// Update display immediately
					return m, m.runQuery(m.queries[m.selected])
				}
			}
		case "right", "l":
			if m.selected < len(m.queries)-1 {
				m.selected++
				m.ensureValidSelection()
				if len(m.queries) > 0 {
					m.loading = true
					m.err = ""
					m.results = ""
					m.lastQuery = m.queries[m.selected]
					// Update display immediately
					m.updateContent()
					return m, m.runQuery(m.queries[m.selected])
				}
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
				m.ensureValidSelection()
				m.loading = true
				m.err = ""
				m.lastQuery = m.queries[m.selected]
				return m, m.runQuery(m.queries[m.selected])
			}
		case "e":
			if len(m.queries) > 0 {
				m.ensureValidSelection()
				m.previousSelected = m.selected
				m.editMode = true
				m.editQuery = m.queries[m.selected]
				m.initEditor(m.editQuery)
				m.updateContent()
				return m, nil
			}
		case "n":
			// Create new query
			m.previousSelected = m.selected
			m.editMode = true
			m.editQuery = Query{} // Empty query
			m.initEditor(m.editQuery)
			m.updateContent()
			return m, nil
		case "d":
			// Dump current queries to default file
			if globalQueryDB != nil {
				configDir := os.ExpandEnv("$HOME/.psq")
				defaultDumpFile := filepath.Join(configDir, "default_queries.db")

				if err := globalQueryDB.DumpToFile(defaultDumpFile); err != nil {
					m.err = fmt.Sprintf("Failed to dump queries: %v", err)
				} else {
					m.err = fmt.Sprintf("Queries dumped to: %s", defaultDumpFile)
				}
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
				m.ensureValidSelection()
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
		return m, tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	case queryErrorMsg:
		m.err = string(msg)
		m.loading = false
		m.updateContent()
	case chatgptResponseMsg:
		// Handle successful ChatGPT response - store it for review
		m.chatgptResponse = string(msg)
		m.chatgptLoading = false
		m.updateContent()
		return m, nil
	case chatgptErrorMsg:
		// Handle ChatGPT error
		m.err = string(msg)
		m.chatgptLoading = false
		m.chatgptMode = false
		m.chatgptResponse = ""
		m.updateContent()
		return m, nil
	case tickMsg:
		if len(m.queries) > 0 && m.canRefresh() {
			m.loading = true
			m.updateContent()
			return m, m.runQuery(m.lastQuery)
		}
	}

	return m, nil
}

func (m *Model) updateContent() {
	var content string

	// Header section
	content += " " + lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("psq@") +
		lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("201")).
			Render(m.service)

	// Show help if requested
	if m.showHelp {
		content += "\n\n" + m.customHelpView()
		m.viewport.SetContent(content)
		return
	}
	if m.searchMode {
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
		if m.chatgptMode {
			if m.chatgptResponse != "" {
				content += ": Press 'c' to confirm and use this SQL, Esc to cancel\n\n"
			} else {
				content += ": Enter to generate SQL, Esc to cancel\n\n"
			}

			// ChatGPT prompt
			content += lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("ChatGPT SQL Generator") + "\n\n"

			if m.chatgptLoading {
				content += "Generating SQL query...\n\n"
				content += lipgloss.NewStyle().
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("240")).
					Render(m.chatgptInput.View()) + "\n"
			} else if m.chatgptResponse != "" {
				// Show the original prompt (read-only)
				content += "Your request:\n"
				content += lipgloss.NewStyle().
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("240")).
					Render(m.chatgptInput.View()) + "\n\n"

				// Show the generated SQL
				content += "Generated SQL:\n"
				sqlPreview := lipgloss.NewStyle().
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("86")).
					Padding(1).
					Width(80)
				content += sqlPreview.Render(m.chatgptResponse) + "\n"
			} else {
				content += "Describe what you want to query:\n"
				content += lipgloss.NewStyle().
					BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("86")).
					Render(m.chatgptInput.View()) + "\n"
			}
		} else {
			content += ": Tab to switch fields, Ctrl+S to save, 'a' for ChatGPT, Esc to cancel\n\n"

			// Query editor
			editorTitle := "Edit Query"
			if m.editQuery.Name == "" {
				editorTitle = "Create New Query"
			}
			content += lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render(editorTitle) + "\n\n"

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

			// Order input
			orderStyle := lipgloss.NewStyle()
			if m.editFocus == 2 {
				orderStyle = orderStyle.BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("86"))
			}
			content += "Order Position (empty to hide from tabs):\n" + orderStyle.Render(m.orderInput.View()) + "\n\n"

			// SQL textarea
			sqlStyle := lipgloss.NewStyle()
			if m.editFocus == 3 {
				sqlStyle = sqlStyle.BorderStyle(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("86"))
			}
			content += "SQL:\n" + sqlStyle.Render(m.sqlTextarea.View()) + "\n"
		}
	} else {
		content += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(": Press ? for help\n")

		// Query list
		content += "\n "
		for i, query := range m.queries {
			var queryText string
			if i == m.selected {
				style := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("86")).
					Background(lipgloss.Color("235"))

				// Add italics for temporary queries
				if m.isTemporaryQuery(query.Name) {
					style = style.Italic(true)
				}

				queryText = style.Render(query.Name)
			} else {
				// Non-selected queries: subtle background and padding to show they're clickable
				baseStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("238")).
					Foreground(lipgloss.Color("252"))

				if m.isTemporaryQuery(query.Name) {
					baseStyle = baseStyle.Italic(true)
				}

				queryText = baseStyle.Render(query.Name)
			}

			// Wrap in bubblezone mark for clickability
			content += zone.Mark(fmt.Sprintf("query_%d", i), queryText)

			if i < len(m.queries)-1 {
				content += " "
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

func (m *Model) customHelpView() string {
	var helpText strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	helpText.WriteString(titleStyle.Render("Help") + "\n\n")

	// Query Navigation
	helpText.WriteString(titleStyle.Render("Query Navigation:") + "\n")
	helpText.WriteString(keyStyle.Render("←/h") + " " + descStyle.Render("previous query") + "\n")
	helpText.WriteString(keyStyle.Render("→/l") + " " + descStyle.Render("next query") + "\n")
	helpText.WriteString(keyStyle.Render("click") + " " + descStyle.Render("select query") + "\n")
	helpText.WriteString(keyStyle.Render("enter/space/r") + " " + descStyle.Render("execute query") + "\n\n")

	// Viewport Navigation
	helpText.WriteString(titleStyle.Render("Viewport Navigation:") + "\n")
	helpText.WriteString(keyStyle.Render("↑/k") + " " + descStyle.Render("scroll up") + "\n")
	helpText.WriteString(keyStyle.Render("↓/j") + " " + descStyle.Render("scroll down") + "\n")
	helpText.WriteString(keyStyle.Render("pgup") + " " + descStyle.Render("page up") + "\n")
	helpText.WriteString(keyStyle.Render("pgdn") + " " + descStyle.Render("page down") + "\n")
	helpText.WriteString(keyStyle.Render("home") + " " + descStyle.Render("go to top") + "\n")
	helpText.WriteString(keyStyle.Render("end") + " " + descStyle.Render("go to bottom") + "\n\n")

	// Query Operations
	helpText.WriteString(titleStyle.Render("Query Operations:") + "\n")
	helpText.WriteString(keyStyle.Render("s") + " " + descStyle.Render("search queries (type to filter, ↑/↓ navigate, enter select, esc cancel)") + "\n")
	helpText.WriteString(keyStyle.Render("e") + " " + descStyle.Render("edit query") + "\n")
	helpText.WriteString(keyStyle.Render("n") + " " + descStyle.Render("new query") + "\n")
	helpText.WriteString(keyStyle.Render("a") + " " + descStyle.Render("chatgpt prompt (in new/edit mode)") + "\n")
	helpText.WriteString(keyStyle.Render("c") + " " + descStyle.Render("confirm chatgpt response") + "\n")
	helpText.WriteString(keyStyle.Render("d") + " " + descStyle.Render("dump queries") + "\n")
	helpText.WriteString(keyStyle.Render("x") + " " + descStyle.Render("psql prompt") + "\n\n")

	// System
	helpText.WriteString(titleStyle.Render("System:") + "\n")
	helpText.WriteString(keyStyle.Render("?") + " " + descStyle.Render("toggle help") + "\n")
	helpText.WriteString(keyStyle.Render("esc") + " " + descStyle.Render("quit") + "\n")

	return helpText.String()
}

func (m *Model) filterQueries() {
	if m.searchQuery == "" {
		m.filteredQueries = m.allQueries // Show all queries when no search
		return
	}

	var filtered []Query
	searchLower := strings.ToLower(m.searchQuery)

	for _, query := range m.allQueries { // Search through all queries
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

	return zone.Scan(m.viewport.View())
}
