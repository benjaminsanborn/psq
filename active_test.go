package main

import (
	"testing"
)

func TestScrubNewlines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no newlines",
			input:    "SELECT 1",
			expected: "SELECT 1",
		},
		{
			name:     "single newline",
			input:    "SELECT\n1",
			expected: "SELECT 1",
		},
		{
			name:     "carriage return",
			input:    "SELECT\r\n1",
			expected: "SELECT 1",
		},
		{
			name:     "multiple newlines collapsed",
			input:    "SELECT\n\n\n1",
			expected: "SELECT 1",
		},
		{
			name:     "mixed whitespace collapsed",
			input:    "SELECT  \n  1  \n  FROM  \n  t",
			expected: "SELECT 1 FROM t",
		},
		{
			name:     "leading and trailing whitespace trimmed",
			input:    "\n  SELECT 1  \n",
			expected: "SELECT 1",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "  \n\r\n  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scrubNewlines(tt.input)
			if result != tt.expected {
				t.Errorf("scrubNewlines(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsActiveTab(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"active tab", "Active", true},
		{"home tab", "Home", false},
		{"empty string", "", false},
		{"lowercase", "active", false},
		{"with suffix", "Active Connections", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsActiveTab(tt.input)
			if result != tt.expected {
				t.Errorf("IsActiveTab(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestActiveViewSelectionPreservation(t *testing.T) {
	av := NewActiveView()

	// Initial data
	initial := []ActiveProcess{
		{PID: 100, Username: "alice", State: "active"},
		{PID: 200, Username: "bob", State: "active"},
		{PID: 300, Username: "carol", State: "active"},
	}
	av.UpdateSelection(initial)

	// Select PID 200
	av.SelectedIndex = 1
	av.SelectedPID = 200

	// Refresh with new data where PID 200 moved to index 2
	refreshed := []ActiveProcess{
		{PID: 50, Username: "new", State: "active"},
		{PID: 100, Username: "alice", State: "active"},
		{PID: 200, Username: "bob", State: "active"},
		{PID: 300, Username: "carol", State: "active"},
	}
	av.UpdateSelection(refreshed)

	if av.SelectedIndex != 2 {
		t.Errorf("expected SelectedIndex=2 (PID 200), got %d", av.SelectedIndex)
	}
	if av.SelectedPID != 200 {
		t.Errorf("expected SelectedPID=200, got %d", av.SelectedPID)
	}
}

func TestActiveViewSelectionPIDGone(t *testing.T) {
	av := NewActiveView()

	initial := []ActiveProcess{
		{PID: 100, Username: "alice", State: "active"},
		{PID: 200, Username: "bob", State: "active"},
	}
	av.UpdateSelection(initial)

	// Select PID 200 at index 1
	av.SelectedIndex = 1
	av.SelectedPID = 200

	// Refresh: PID 200 is gone
	refreshed := []ActiveProcess{
		{PID: 100, Username: "alice", State: "active"},
	}
	av.UpdateSelection(refreshed)

	// Should clamp to last valid index
	if av.SelectedIndex != 0 {
		t.Errorf("expected SelectedIndex=0 after PID gone, got %d", av.SelectedIndex)
	}
}

func TestActiveViewSelectionEmptyList(t *testing.T) {
	av := NewActiveView()
	av.SelectedPID = 100
	av.SelectedIndex = 0

	av.UpdateSelection([]ActiveProcess{})

	if av.SelectedIndex != 0 {
		t.Errorf("expected SelectedIndex=0 for empty list, got %d", av.SelectedIndex)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell~"},
		{"hi", 2, "hi"},
		{"hi", 1, "~"},
		{"", 5, ""},
		{"abc", 0, ""},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.max)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, result, tt.expected)
		}
	}
}
