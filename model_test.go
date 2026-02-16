package main

import (
	"testing"
)

func TestFilterQueries(t *testing.T) {
	// Create a model with test queries
	homeQuery := HomeQuery()
	testQueries := []Query{
		homeQuery,
		{Name: "Active Connections", Description: "Show all active database connections", SQL: "SELECT * FROM pg_stat_activity"},
		{Name: "Table Sizes", Description: "Show table sizes in MB", SQL: "SELECT ..."},
		{Name: "Slow Queries", Description: "Find queries taking longer than 1 second", SQL: "SELECT ..."},
		{Name: "Index Usage", Description: "Check index usage statistics", SQL: "SELECT ..."},
	}

	model := &Model{
		queries:    testQueries,
		allQueries: testQueries,
		selected:   0,
	}

	tests := []struct {
		name        string
		searchQuery string
		wantCount   int
		wantNames   []string
	}{
		{
			name:        "empty search shows all",
			searchQuery: "",
			wantCount:   5,
			wantNames:   []string{"Home", "Active Connections", "Table Sizes", "Slow Queries", "Index Usage"},
		},
		{
			name:        "search by name",
			searchQuery: "active",
			wantCount:   1,
			wantNames:   []string{"Active Connections"},
		},
		{
			name:        "search by description",
			searchQuery: "statistics",
			wantCount:   1,
			wantNames:   []string{"Index Usage"},
		},
		{
			name:        "case insensitive",
			searchQuery: "SLOW",
			wantCount:   1,
			wantNames:   []string{"Slow Queries"},
		},
		{
			name:        "partial match",
			searchQuery: "size",
			wantCount:   1,
			wantNames:   []string{"Table Sizes"},
		},
		{
			name:        "no matches",
			searchQuery: "nonexistent",
			wantCount:   0,
			wantNames:   []string{},
		},
		{
			name:        "multiple matches",
			searchQuery: "show",
			wantCount:   2, // "Active Connections" and "Table Sizes" have "show" in description
			wantNames:   []string{"Active Connections", "Table Sizes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model.searchQuery = tt.searchQuery
			model.filterQueries()

			if len(model.filteredQueries) != tt.wantCount {
				t.Errorf("filterQueries() returned %d queries, want %d", len(model.filteredQueries), tt.wantCount)
			}

			for i, wantName := range tt.wantNames {
				if i >= len(model.filteredQueries) {
					t.Errorf("Missing expected query %q at index %d", wantName, i)
					continue
				}
				if model.filteredQueries[i].Name != wantName {
					t.Errorf("filteredQueries[%d].Name = %q, want %q", i, model.filteredQueries[i].Name, wantName)
				}
			}
		})
	}
}

func TestEnsureValidSelection(t *testing.T) {
	tests := []struct {
		name          string
		queriesCount  int
		initialSelect int
		wantSelect    int
	}{
		{
			name:          "empty queries list",
			queriesCount:  0,
			initialSelect: 5,
			wantSelect:    0,
		},
		{
			name:          "selection too high",
			queriesCount:  3,
			initialSelect: 10,
			wantSelect:    2, // len-1
		},
		{
			name:          "selection negative",
			queriesCount:  5,
			initialSelect: -2,
			wantSelect:    0,
		},
		{
			name:          "valid selection",
			queriesCount:  5,
			initialSelect: 2,
			wantSelect:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := make([]Query, tt.queriesCount)
			model := &Model{
				queries:  queries,
				selected: tt.initialSelect,
			}

			model.ensureValidSelection()

			if model.selected != tt.wantSelect {
				t.Errorf("ensureValidSelection() selected = %d, want %d", model.selected, tt.wantSelect)
			}
		})
	}
}

func TestIsTemporaryQuery(t *testing.T) {
	model := &Model{
		tempQueries: map[string]int{
			"temp-query-1": 1,
			"temp-query-2": 2,
		},
	}

	tests := []struct {
		name      string
		queryName string
		want      bool
	}{
		{
			name:      "is temporary",
			queryName: "temp-query-1",
			want:      true,
		},
		{
			name:      "not temporary",
			queryName: "regular-query",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.isTemporaryQuery(tt.queryName)
			if got != tt.want {
				t.Errorf("isTemporaryQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}
