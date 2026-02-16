package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestQueryDB(t *testing.T) (*QueryDB, string) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_queries.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	qdb := &QueryDB{db: db}
	if err := qdb.initSchema(); err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	return qdb, tmpDir
}

func TestSaveAndLoadQuery(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	testQuery := Query{
		Name:          "Test Query",
		Description:   "A test query for unit testing",
		SQL:           "SELECT * FROM test_table",
		OrderPosition: intPtr(1),
	}

	// Test Save
	if err := qdb.SaveQuery(testQuery); err != nil {
		t.Fatalf("SaveQuery() error = %v", err)
	}

	// Test Load
	queries, err := qdb.LoadQueries()
	if err != nil {
		t.Fatalf("LoadQueries() error = %v", err)
	}

	if len(queries) != 1 {
		t.Fatalf("LoadQueries() returned %d queries, want 1", len(queries))
	}

	got := queries[0]
	if got.Name != testQuery.Name {
		t.Errorf("Name = %v, want %v", got.Name, testQuery.Name)
	}
	if got.Description != testQuery.Description {
		t.Errorf("Description = %v, want %v", got.Description, testQuery.Description)
	}
	if got.SQL != testQuery.SQL {
		t.Errorf("SQL = %v, want %v", got.SQL, testQuery.SQL)
	}
	if *got.OrderPosition != *testQuery.OrderPosition {
		t.Errorf("OrderPosition = %v, want %v", *got.OrderPosition, *testQuery.OrderPosition)
	}
}

func TestUpdateQuery(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	// Insert initial query
	originalQuery := Query{
		Name:          "Test Query",
		Description:   "Original Description",
		SQL:           "SELECT 1",
		OrderPosition: intPtr(1),
	}
	if err := qdb.SaveQuery(originalQuery); err != nil {
		t.Fatalf("SaveQuery() error = %v", err)
	}

	// Update the query (SaveQuery does INSERT OR REPLACE)
	updatedQuery := Query{
		Name:          "Test Query", // Same name to update
		Description:   "Updated Description",
		SQL:           "SELECT 2",
		OrderPosition: intPtr(2),
	}
	if err := qdb.SaveQuery(updatedQuery); err != nil {
		t.Fatalf("SaveQuery() error = %v", err)
	}

	// Load and verify
	queries, err := qdb.LoadQueries()
	if err != nil {
		t.Fatalf("LoadQueries() error = %v", err)
	}

	if len(queries) != 1 {
		t.Fatalf("Expected 1 query after update, got %d", len(queries))
	}

	got := queries[0]
	if got.Name != updatedQuery.Name {
		t.Errorf("Name = %v, want %v", got.Name, updatedQuery.Name)
	}
	if got.Description != updatedQuery.Description {
		t.Errorf("Description = %v, want %v", got.Description, updatedQuery.Description)
	}
	if got.SQL != updatedQuery.SQL {
		t.Errorf("SQL = %v, want %v", got.SQL, updatedQuery.SQL)
	}
}

func TestDeleteQuery(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	// Insert queries
	queries := []Query{
		{Name: "Query 1", Description: "First", SQL: "SELECT 1", OrderPosition: intPtr(1)},
		{Name: "Query 2", Description: "Second", SQL: "SELECT 2", OrderPosition: intPtr(2)},
		{Name: "Query 3", Description: "Third", SQL: "SELECT 3", OrderPosition: intPtr(3)},
	}

	for _, q := range queries {
		if err := qdb.SaveQuery(q); err != nil {
			t.Fatalf("SaveQuery() error = %v", err)
		}
	}

	// Delete middle query
	if err := qdb.DeleteQuery("Query 2"); err != nil {
		t.Fatalf("DeleteQuery() error = %v", err)
	}

	// Verify
	remaining, err := qdb.LoadQueries()
	if err != nil {
		t.Fatalf("LoadQueries() error = %v", err)
	}

	if len(remaining) != 2 {
		t.Fatalf("Expected 2 queries after delete, got %d", len(remaining))
	}

	names := []string{remaining[0].Name, remaining[1].Name}
	if names[0] != "Query 1" || names[1] != "Query 3" {
		t.Errorf("Unexpected remaining queries: %v", names)
	}
}

func TestLoadAllQueries(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	// Insert visible and hidden queries
	visibleQuery := Query{
		Name:          "Visible Query",
		Description:   "This is visible",
		SQL:           "SELECT 1",
		OrderPosition: intPtr(1),
	}
	hiddenQuery := Query{
		Name:        "Hidden Query",
		Description: "This is hidden",
		SQL:         "SELECT 2",
		// OrderPosition is nil = hidden
	}

	if err := qdb.SaveQuery(visibleQuery); err != nil {
		t.Fatalf("SaveQuery() error = %v", err)
	}
	if err := qdb.SaveQuery(hiddenQuery); err != nil {
		t.Fatalf("SaveQuery() error = %v", err)
	}

	// LoadQueries should only return visible
	visible, err := qdb.LoadQueries()
	if err != nil {
		t.Fatalf("LoadQueries() error = %v", err)
	}
	if len(visible) != 1 {
		t.Errorf("LoadQueries() returned %d queries, want 1", len(visible))
	}

	// LoadAllQueries should return both
	all, err := qdb.LoadAllQueries()
	if err != nil {
		t.Fatalf("LoadAllQueries() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("LoadAllQueries() returned %d queries, want 2", len(all))
	}
}

func TestGetQuery(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	// Insert test query
	testQuery := Query{
		Name:          "Test Query",
		Description:   "Test Description",
		SQL:           "SELECT * FROM test",
		OrderPosition: intPtr(1),
	}
	if err := qdb.SaveQuery(testQuery); err != nil {
		t.Fatalf("SaveQuery() error = %v", err)
	}

	// Get the query
	got, err := qdb.GetQuery("Test Query")
	if err != nil {
		t.Fatalf("GetQuery() error = %v", err)
	}

	if got.Name != testQuery.Name {
		t.Errorf("Name = %v, want %v", got.Name, testQuery.Name)
	}
	if got.Description != testQuery.Description {
		t.Errorf("Description = %v, want %v", got.Description, testQuery.Description)
	}
	if got.SQL != testQuery.SQL {
		t.Errorf("SQL = %v, want %v", got.SQL, testQuery.SQL)
	}

	// Test non-existent query
	_, err = qdb.GetQuery("Does Not Exist")
	if err == nil {
		t.Errorf("GetQuery() should return error for non-existent query")
	}
}

// Helper function for tests
func intPtr(i int) *int {
	return &i
}
