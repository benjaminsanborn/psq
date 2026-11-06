package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleResult() *resultSet {
	return &resultSet{
		Columns: []string{"name", "size", "count", "count", "seen", "meta"},
		Types:   []string{"TEXT", "NUMERIC", "INT8", "INT8", "TIMESTAMPTZ", "JSONB"},
		Rows: [][]any{
			{"orders", "12.50", int64(3), int64(4), time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), `{"a": 1}`},
			{"a,b \"q\"", nil, int64(0), nil, nil, nil},
		},
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, sampleResult()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(parsed) != 2 {
		t.Fatalf("got %d objects", len(parsed))
	}
	row := parsed[0]
	if row["size"] != 12.5 {
		t.Errorf("NUMERIC should be a JSON number, got %#v", row["size"])
	}
	if row["count_2"] != float64(4) {
		t.Errorf("repeated column should be suffixed, got %#v", row)
	}
	if meta, ok := row["meta"].(map[string]any); !ok || meta["a"] != float64(1) {
		t.Errorf("JSONB should be embedded as JSON, got %#v", row["meta"])
	}
	if row["seen"] != "2026-01-02T03:04:05Z" {
		t.Errorf("timestamp = %#v", row["seen"])
	}
	if parsed[1]["size"] != nil {
		t.Errorf("NULL should be null, got %#v", parsed[1]["size"])
	}
	// Keys follow column order
	if strings.Index(out, `"name"`) > strings.Index(out, `"size"`) {
		t.Error("keys out of column order")
	}

	buf.Reset()
	writeJSON(&buf, &resultSet{Columns: []string{"x"}})
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty result = %q", buf.String())
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCSV(&buf, sampleResult()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[0] != "name,size,count,count,seen,meta" {
		t.Errorf("header = %q", lines[0])
	}
	if lines[2] != `"a,b ""q""",,0,,,` {
		t.Errorf("quoting/NULLs = %q", lines[2])
	}
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	res := &resultSet{Columns: []string{"a", "b"}, Rows: [][]any{{"multi\nline", nil}}}
	if err := writeTable(&buf, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "multi line  NULL") {
		t.Errorf("table = %q", buf.String())
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Error("table output must not contain ANSI styling")
	}
}

func TestActiveResultSet(t *testing.T) {
	procs := lockChain()
	procs[0].XactSec = -1 // no open transaction
	linkBlocking(procs)
	var buf bytes.Buffer
	if err := writeJSON(&buf, activeResultSet(procs)); err != nil {
		t.Fatal(err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed[0]["blocking_total"] != float64(3) {
		t.Errorf("root blocker row = %#v", parsed[0])
	}
	if by, ok := parsed[1]["blocked_by"].([]any); !ok || len(by) != 1 || by[0] != float64(10) {
		t.Errorf("blocked_by = %#v", parsed[1]["blocked_by"])
	}
	if parsed[0]["xact_seconds"] != nil {
		t.Errorf("no open transaction should be null, got %#v", parsed[0]["xact_seconds"])
	}
}

func TestFindSavedQuery(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()
	prev := globalQueryDB
	globalQueryDB = qdb
	defer func() { globalQueryDB = prev }()

	qdb.CreateQuery(Query{Name: "Lock Information", Description: "d", SQL: "SELECT 1"})

	if q, err := findSavedQuery("lock information"); err != nil || q.SQL != "SELECT 1" {
		t.Errorf("case-insensitive lookup = %+v, %v", q, err)
	}
	if _, err := findSavedQuery("nope"); err == nil || !strings.Contains(err.Error(), "--list-queries") {
		t.Errorf("missing query error = %v", err)
	}
	if _, err := findSavedQuery("Home"); err == nil || !strings.Contains(err.Error(), "interactive") {
		t.Errorf("Home should be interactive-only, got %v", err)
	}
}

func TestRunOnceRejectsUnknownFormat(t *testing.T) {
	if err := runOnce(&bytes.Buffer{}, "svc", "x", "", "xml"); err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("err = %v", err)
	}
}
