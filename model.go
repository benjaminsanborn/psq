package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
)

type Model struct {
	queries           []Query
	allQueries        []Query        // includes hidden queries for search
	tempQueries       map[string]int // temporary order positions for hidden queries
	selected          int
	previousSelected  int // track selection before entering modals
	results           string
	loading           bool
	err               string
	width             int
	height            int
	service           string
	db                *sql.DB // persistent database connection
	lastQuery         Query
	viewport          viewport.Model
	ready             bool
	lastRefreshAt     time.Time
	searchMode        bool
	searchQuery       string
	filteredQueries   []Query
	editMode          bool
	editQuery         Query
	nameInput         textinput.Model
	descInput         textinput.Model
	orderInput        textinput.Model
	sqlTextarea       textarea.Model
	editFocus         int    // 0=name, 1=description, 2=order, 3=sql
	editErr           string // save/delete failure shown in the editor; survives background refreshes
	help              help.Model
	showHelp          bool
	sparklineData     *SparklineData // Transaction commits sparkline data
	lastCommits       float64        // Last transaction commit count for rate calculation
	lastCommitTime    time.Time      // DB timestamp of last commit query for accurate TPS
	activeView        *ActiveView    // Interactive active connections view (nil when not on Active tab)
	querySeq          int            // incremented per runQuery; results from older dispatches are dropped
	consecutiveErrors int            // drives auto-refresh backoff while queries keep failing
}

type Query struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	SQL           string `json:"sql"`
	OrderPosition *int   `json:"order_position,omitempty"` // nil means hidden from top bar
}

// Message types for Bubble Tea
type tickMsg time.Time
type returnToPickerMsg struct{}

func NewModel(service string) *Model {
	dbQueries, err := loadQueries()
	if err != nil {
		return &Model{
			queries:       []Query{},
			allQueries:    []Query{},
			tempQueries:   make(map[string]int),
			selected:      0,
			err:           fmt.Sprintf("Failed to load queries: %v", err),
			service:       service,
			ready:         false,
			sparklineData: NewSparklineData(60),
		}
	}

	// Create hardcoded Home tab as first query, Active tab as second
	homeQuery := HomeQuery()
	activeQuery := ActiveQuery()

	// Combine Home + Active tabs with database queries
	queries := append([]Query{homeQuery, activeQuery}, dbQueries...)

	// Also load all queries (including hidden ones) for search, but include Home and Active
	allQueries := queries
	if globalQueryDB != nil {
		if allQueriesFromDB, err := globalQueryDB.LoadAllQueries(); err == nil {
			allQueries = append([]Query{homeQuery, activeQuery}, allQueriesFromDB...)
		}
	}

	// Open persistent database connection
	db, err := connectDB(service)
	if err != nil {
		return &Model{
			queries:         queries,
			allQueries:      allQueries,
			tempQueries:     make(map[string]int),
			selected:        0,
			err:             fmt.Sprintf("Failed to connect to database: %v", err),
			service:         service,
			ready:           false,
			searchMode:      false,
			searchQuery:     "",
			filteredQueries: queries,
			editMode:        false,
			help:            help.New(),
			showHelp:        false,
			sparklineData:   NewSparklineData(60),
			lastCommits:     0,
		}
	}

	return &Model{
		queries:         queries,
		allQueries:      allQueries,
		tempQueries:     make(map[string]int),
		selected:        0,
		results:         "Select a query to run...",
		service:         service,
		db:              db,
		ready:           false,
		searchMode:      false,
		searchQuery:     "",
		filteredQueries: queries,
		editMode:        false,
		help:            help.New(),
		showHelp:        false,
		sparklineData:   NewSparklineData(60), // Keep 60 data points (1 minute at 1 second intervals)
		lastCommits:     0,
	}
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
	for _, q := range m.queries {
		if q.Name == query.Name {
			return // already a tab
		}
	}
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

// builtinQueries are the hardcoded tabs that precede saved queries
func builtinQueries() []Query {
	return []Query{HomeQuery(), ActiveQuery()}
}

// reloadQueries refreshes the tab and search lists from the query DB, keeping
// the built-in tabs and any temporary tabs whose queries are still hidden
func (m *Model) reloadQueries() error {
	dbQueries, err := loadQueries()
	if err != nil {
		return err
	}
	queries := append(builtinQueries(), dbQueries...)

	allQueries := queries
	if all, err := globalQueryDB.LoadAllQueries(); err == nil {
		allQueries = append(builtinQueries(), all...)
	}

	var temps []Query
	for name, pos := range m.tempQueries {
		found := false
		for _, q := range allQueries {
			if q.Name == name && q.OrderPosition == nil {
				p := pos
				q.OrderPosition = &p
				temps = append(temps, q)
				found = true
				break
			}
		}
		if !found {
			delete(m.tempQueries, name)
		}
	}
	sort.Slice(temps, func(i, j int) bool { return *temps[i].OrderPosition < *temps[j].OrderPosition })

	m.queries = append(queries, temps...)
	m.allQueries = allQueries
	return nil
}

func (m *Model) isTemporaryQuery(queryName string) bool {
	_, exists := m.tempQueries[queryName]
	return exists
}

func (m *Model) Close() {
	if m.db != nil {
		m.db.Close()
		m.db = nil
	}
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

func (m *Model) canRefresh() bool {
	return time.Since(m.lastRefreshAt) >= 500*time.Millisecond
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
