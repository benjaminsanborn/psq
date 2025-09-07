package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
)

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
	aiPromptInput    textinput.Model
	editFocus        int    // 0=name, 1=description, 2=order, 3=sql, 4=ai-prompt
	chatgptResponse  string // Store the generated SQL for review
	help             help.Model
	showHelp         bool
	sparklineData    *SparklineData // Transaction commits sparkline data
	lastCommits      float64        // Last transaction commit count for rate calculation
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

// Message types for Bubble Tea
type chatgptResponseMsg string
type chatgptErrorMsg string
type tickMsg time.Time

func NewModel(service string) *Model {
	dbQueries, err := loadQueries()
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

	// Create hardcoded Home tab as first query
	homeQuery := HomeQuery()

	// Combine Home tab with database queries
	queries := append([]Query{homeQuery}, dbQueries...)

	// Also load all queries (including hidden ones) for search, but include Home
	allQueries := queries
	if globalQueryDB != nil {
		if allQueriesFromDB, err := globalQueryDB.LoadAllQueries(); err == nil {
			allQueries = append([]Query{homeQuery}, allQueriesFromDB...)
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
