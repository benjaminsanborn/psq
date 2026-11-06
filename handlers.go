package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// terminateResultMsg is sent after a pg_terminate_backend or pg_cancel_backend call
type terminateResultMsg struct {
	PID     int
	Action  string // "terminate" or "cancel"
	Success bool
	Error   string
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)
	case queryResultMsg:
		return m.handleQueryResult(msg)
	case queryErrorMsg:
		return m.handleQueryError(msg)
	case activeProcessesMsg:
		return m.handleActiveProcesses(msg)
	case homeDataMsg:
		return m.handleHomeData(msg)
	case stmtDataMsg:
		if msg.seq != m.querySeq {
			return m, nil
		}
		if msg.result.snapshot != nil {
			m.statements().Add(msg.result.snapshot)
		}
		return m.applyResult(msg.result.rendered)
	case dbConnectedMsg:
		return m.handleDBConnected(msg)
	case psqlExitMsg:
		if msg.err != nil {
			m.err = fmt.Sprintf("Failed to open psql: %v", msg.err)
			m.updateContent()
		}
		return m, nil
	case terminateResultMsg:
		return m.handleTerminateResult(msg)
	case clipboardResultMsg:
		if m.activeView != nil {
			if msg.err != nil {
				m.activeView.CopyStatus = fmt.Sprintf("Copy failed: %v", msg.err)
			} else {
				m.activeView.CopyStatus = "Copied!"
			}
			m.updateContent()
		}
		return m, nil
	case tickMsg:
		return m.handleTickMsg()
	case returnToPickerMsg:
		m.Close()
		return m, tea.Quit
	}

	return m, nil
}

func (m *Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Handle mouse events only when ready and not in edit/search mode
	if !m.ready || m.editMode || m.searchMode {
		return m, nil
	}

	// Only handle left mouse button release (clicks)
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	if m.selected < len(m.queries) && IsHomeTab(m.queries[m.selected].Name) && zone.Get(homeBlockingZone).InBounds(msg) {
		return m.openLockTree()
	}

	// Check if any query zone was clicked
	for i := range m.queries {
		zoneID := fmt.Sprintf("query_%d", i)
		if zone.Get(zoneID).InBounds(msg) {
			// Query was clicked, select it and run it
			if i < len(m.queries) {
				m.selected = i
				m.ensureValidSelection()
				m.syncActiveView()
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
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.ready {
		return m, nil
	}

	// Handle edit mode
	if m.editMode {
		return m.handleEditModeKeys(msg)
	}

	// Handle search mode
	if m.searchMode {
		return m.handleSearchModeKeys(msg)
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
	return m.handleNormalModeKeys(msg)
}

func (m *Model) handleSearchModeKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		if r := []rune(m.searchQuery); len(r) > 0 {
			m.searchQuery = string(r[:len(r)-1])
			m.filterQueries()
			m.updateContent()
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.searchQuery += string(msg.Runes)
			m.filterQueries()
			m.updateContent()
		}
	}
	return m, nil
}

func (m *Model) handleNormalModeKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Delegate to active view when on the Active tab
	if m.activeView != nil && m.selected < len(m.queries) && IsActiveTab(m.queries[m.selected].Name) {
		// In detail or confirm mode, or while typing a filter, fully delegate all keys
		if m.activeView.Mode != ActiveModeList || m.activeView.Filtering {
			return m.handleActiveViewKeys(msg)
		}
		// In list mode, delegate navigation/action keys but let tab-switch keys fall through
		switch msg.String() {
		case "up", "k", "down", "j", "pgup", "pgdown", "enter", "t", "c", "b", "o", "/", "esc":
			return m.handleActiveViewKeys(msg)
		}
	}

	if m.selected < len(m.queries) && IsStatementsTab(m.queries[m.selected].Name) {
		switch msg.String() {
		case "w", "o":
			if msg.String() == "w" {
				m.statements().CycleWindow()
			} else {
				m.statements().CycleSort()
			}
			m.loading = true
			m.updateContent()
			return m, m.runQuery(m.lastQuery)
		}
	}

	// The Home tab's blocked-queries widget leads to the lock tree
	if msg.String() == "b" && m.selected < len(m.queries) && IsHomeTab(m.queries[m.selected].Name) {
		return m.openLockTree()
	}

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
	case "q", "ctrl+c":
		// esc deliberately doesn't quit: it's the "back" key everywhere else
		m.Close()
		return m, tea.Quit
	case "c":
		return m, func() tea.Msg {
			return returnToPickerMsg{}
		}

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
			m.syncActiveView()
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
			m.syncActiveView()
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
		m.viewport.ScrollUp(1)
	case "down", "j":
		m.viewport.ScrollDown(1)
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
			// Don't allow editing the hardcoded Home or Active tabs
			if IsHomeTab(m.queries[m.selected].Name) || IsActiveTab(m.queries[m.selected].Name) {
				return m, nil
			}
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
	case "x":
		return m.handlePsqlPrompt()
	}

	// Update content after any key press
	if m.ready {
		m.updateContent()
	}
	return m, nil
}

func (m *Model) handlePsqlPrompt() (tea.Model, tea.Cmd) {
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
	password := config.Password
	if config.UsesRDSIAM() {
		password, err = rdsIAMTokenForConfig(config)
		if err != nil {
			m.err = fmt.Sprintf("Failed to get RDS IAM token: %v", err)
			m.updateContent()
			return m, nil
		}
	}
	cmd.Env = os.Environ()
	if password != "" {
		// Left unset otherwise so psql falls back to ~/.pgpass
		cmd.Env = append(cmd.Env, "PGPASSWORD="+password)
	}
	// psql understands every libpq sslmode natively, including "prefer"
	sslmode := config.libpqSSLMode()
	if config.UsesRDSIAM() {
		sslmode, _ = config.driverSSLMode()
	}
	cmd.Env = append(cmd.Env, "PGSSLMODE="+sslmode)
	for _, kv := range []struct{ name, value string }{
		{"PGSSLROOTCERT", config.SSLRootCert},
		{"PGSSLCERT", config.SSLCert},
		{"PGSSLKEY", config.SSLKey},
	} {
		if kv.value != "" {
			cmd.Env = append(cmd.Env, kv.name+"="+kv.value)
		}
	}

	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return psqlExitMsg{err: err}
	})
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
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
	return m, nil
}

func (m *Model) handleQueryResult(msg queryResultMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.querySeq {
		return m, nil // stale: a newer query is in flight and will schedule the next tick
	}
	return m.applyResult(msg.result)
}

// applyResult records a successful refresh and schedules the next tick
func (m *Model) applyResult(results string) (tea.Model, tea.Cmd) {
	m.results = results
	m.err = ""
	m.loading = false
	m.consecutiveErrors = 0
	m.lastRefreshAt = time.Now()
	m.updateContent()
	return m, scheduleTick(refreshInterval)
}

func scheduleTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// handleActiveProcesses applies fetched processes to the Active view on the main goroutine
func (m *Model) handleActiveProcesses(msg activeProcessesMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.querySeq {
		return m, nil
	}
	if m.activeView != nil {
		m.activeView.UpdateSelection(msg.processes)
	}
	// updateContent renders the Active tab straight from activeView
	return m.applyResult("")
}

// openLockTree switches to the Active tab showing only blocking chains
func (m *Model) openLockTree() (tea.Model, tea.Cmd) {
	for i, q := range m.queries {
		if IsActiveTab(q.Name) {
			m.selected = i
			break
		}
	}
	m.syncActiveView()
	if m.activeView == nil {
		return m, nil
	}
	m.activeView.SetTreeMode(true)
	m.loading = true
	m.err = ""
	m.results = ""
	m.lastQuery = m.queries[m.selected]
	m.updateContent()
	return m, m.runQuery(m.lastQuery)
}

// handleHomeData folds Home dashboard data into model state on the main goroutine
func (m *Model) handleHomeData(msg homeDataMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.querySeq {
		return m, nil
	}
	return m.applyResult(m.renderHome(msg.data))
}

// handleDBConnected installs a connection opened in the background and
// re-runs the query that needed it
func (m *Model) handleDBConnected(msg dbConnectedMsg) (tea.Model, tea.Cmd) {
	if m.db == nil {
		m.db = msg.db
	} else {
		// Another dispatch connected first; keep the installed connection
		msg.db.Close()
	}
	if msg.seq != m.querySeq {
		return m, nil
	}
	return m, m.runQuery(msg.query)
}

func (m *Model) handleQueryError(msg queryErrorMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.querySeq {
		return m, nil
	}
	m.err = msg.err
	m.loading = false
	m.consecutiveErrors++
	m.lastRefreshAt = time.Now()
	m.updateContent()
	// Keep refreshing so a transient failure doesn't freeze the view
	return m, scheduleTick(refreshDelay(m.consecutiveErrors))
}

func (m *Model) handleTickMsg() (tea.Model, tea.Cmd) {
	if len(m.queries) > 0 && m.canRefresh() {
		m.loading = true
		m.updateContent()
		return m, m.runQuery(m.lastQuery)
	}
	return m, nil
}

// syncActiveView initializes or clears activeView based on current tab
func (m *Model) syncActiveView() {
	if m.selected < len(m.queries) && IsActiveTab(m.queries[m.selected].Name) {
		if m.activeView == nil {
			m.activeView = NewActiveView()
		}
	} else {
		m.activeView = nil
	}
}

// handleActiveViewKeys handles keyboard input when the Active tab is focused
func (m *Model) handleActiveViewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	av := m.activeView
	if av == nil {
		return m, nil
	}

	if av.Filtering {
		switch {
		case msg.Type == tea.KeyEscape:
			av.Filtering = false
			av.SetFilter("")
		case msg.Type == tea.KeyEnter:
			av.Filtering = false
		case msg.Type == tea.KeyBackspace:
			if r := []rune(av.Filter); len(r) > 0 {
				av.SetFilter(string(r[:len(r)-1]))
			}
		case msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace:
			av.SetFilter(av.Filter + string(msg.Runes))
		}
		m.updateContent()
		return m, nil
	}

	switch av.Mode {
	case ActiveModeList:
		switch msg.String() {
		case "up", "k":
			av.MoveSelection(-1)
		case "down", "j":
			av.MoveSelection(1)
		case "pgup":
			av.MoveSelection(-av.pageSize(m.height))
		case "pgdown":
			av.MoveSelection(av.pageSize(m.height))
		case "b":
			av.SetTreeMode(!av.TreeMode)
		case "o":
			av.CycleSort()
		case "/":
			av.Filtering = true
		case "esc":
			// Back out of a filter first, then out of the lock tree
			if av.Filter != "" {
				av.SetFilter("")
			} else if av.TreeMode {
				av.SetTreeMode(false)
			}
		case "enter":
			if p := av.SelectedProcess(); p != nil {
				snap := *p
				av.DetailProcess = &snap
				av.Mode = ActiveModeDetail
				av.LastError = ""
				av.CopyStatus = ""
			}
		case "t", "c":
			if p := av.SelectedProcess(); p != nil {
				snap := *p
				av.DetailProcess = &snap
				av.TerminateType = map[string]string{"t": "terminate", "c": "cancel"}[msg.String()]
				av.Mode = ActiveModeConfirmTerminate
				av.LastError = ""
			}
		}
		m.updateContent()

	case ActiveModeDetail:
		switch msg.String() {
		case "esc", "ctrl+[":
			av.DetailProcess = nil
			av.DetailCompleted = false
			av.Mode = ActiveModeList
			av.LastError = ""
			av.CopyStatus = ""
			m.updateContent()
		case "t":
			if !av.DetailCompleted {
				av.TerminateType = "terminate"
				av.Mode = ActiveModeConfirmTerminate
				av.LastError = ""
				m.updateContent()
			}
		case "c":
			if !av.DetailCompleted {
				av.TerminateType = "cancel"
				av.Mode = ActiveModeConfirmTerminate
				av.LastError = ""
				m.updateContent()
			}
		case "y":
			if av.DetailProcess != nil {
				return m, func() tea.Msg {
					return clipboardResultMsg{err: copyToClipboard(av.DetailProcess.Query)}
				}
			}
		}

	case ActiveModeConfirmTerminate:
		switch msg.String() {
		case "y":
			if av.DetailProcess != nil {
				return m, m.executeTerminate(av.DetailProcess.PID, av.TerminateType)
			}
		case "n", "esc", "ctrl+[":
			av.Mode = ActiveModeList
			av.DetailProcess = nil
			av.DetailCompleted = false
			av.LastError = ""
			m.updateContent()
		}
	}

	return m, nil
}

// executeTerminate runs pg_terminate_backend or pg_cancel_backend asynchronously
func (m *Model) executeTerminate(pid int, action string) tea.Cmd {
	db := m.db
	return func() tea.Msg {
		if db == nil {
			return terminateResultMsg{PID: pid, Action: action, Error: "no database connection"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var err error
		if action == "cancel" {
			err = CancelBackend(ctx, db, pid)
		} else {
			err = TerminateBackend(ctx, db, pid)
		}
		if err != nil {
			return terminateResultMsg{PID: pid, Action: action, Error: err.Error()}
		}
		return terminateResultMsg{PID: pid, Action: action, Success: true}
	}
}

// handleTerminateResult processes the result of a terminate/cancel action
func (m *Model) handleTerminateResult(msg terminateResultMsg) (tea.Model, tea.Cmd) {
	if m.activeView != nil {
		if msg.Success {
			m.activeView.Mode = ActiveModeList
			m.activeView.DetailProcess = nil
			m.activeView.DetailCompleted = false
			m.activeView.LastError = ""
			// Force refresh to reflect the terminated process
			m.loading = true
			m.updateContent()
			return m, m.runQuery(m.lastQuery)
		}
		m.activeView.LastError = msg.Error
		m.activeView.DetailProcess = nil
		m.activeView.DetailCompleted = false
		m.activeView.Mode = ActiveModeList
		m.updateContent()
	}
	return m, nil
}
