package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

func TestMain(m *testing.M) {
	// Rendering marks clickable zones, which needs the global manager main() sets up
	zone.NewGlobal()
	os.Exit(m.Run())
}

func newTestModel() *Model {
	return &Model{
		queries:       builtinQueries(),
		allQueries:    builtinQueries(),
		tempQueries:   map[string]int{},
		sparklineData: NewSparklineData(60),
		ready:         true,
		width:         120,
		height:        40,
	}
}

func TestRefreshDelayBacksOff(t *testing.T) {
	tests := map[int]time.Duration{
		0:  time.Second,
		1:  time.Second,
		2:  2 * time.Second,
		3:  4 * time.Second,
		5:  16 * time.Second,
		6:  maxRefreshBackoff,
		50: maxRefreshBackoff,
	}
	for n, want := range tests {
		if got := refreshDelay(n); got != want {
			t.Errorf("refreshDelay(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestStaleQueryResultIsDropped(t *testing.T) {
	m := newTestModel()
	m.querySeq = 2
	m.results = "current"

	_, cmd := m.Update(queryResultMsg{seq: 1, result: "stale"})
	if m.results != "current" {
		t.Errorf("stale result applied: results = %q", m.results)
	}
	if cmd != nil {
		t.Error("stale result should not schedule a tick")
	}

	_, cmd = m.Update(queryErrorMsg{seq: 1, err: "stale error"})
	if m.err != "" {
		t.Errorf("stale error applied: err = %q", m.err)
	}
	if cmd != nil {
		t.Error("stale error should not schedule a tick")
	}

	_, cmd = m.Update(queryResultMsg{seq: 2, result: "fresh"})
	if m.results != "fresh" {
		t.Errorf("current result not applied: results = %q", m.results)
	}
	if cmd == nil {
		t.Error("current result should schedule the next tick")
	}
}

func TestQueryErrorKeepsRefreshingAndRecovers(t *testing.T) {
	m := newTestModel()
	m.querySeq = 1

	_, cmd := m.Update(queryErrorMsg{seq: 1, err: "connection reset"})
	if cmd == nil {
		t.Fatal("an error must still schedule a tick, or auto-refresh stops for good")
	}
	if m.consecutiveErrors != 1 || m.err != "connection reset" {
		t.Errorf("after error: consecutiveErrors=%d err=%q", m.consecutiveErrors, m.err)
	}

	m.Update(queryErrorMsg{seq: 1, err: "connection reset"})
	if m.consecutiveErrors != 2 {
		t.Errorf("consecutiveErrors = %d, want 2", m.consecutiveErrors)
	}

	m.Update(queryResultMsg{seq: 1, result: "ok"})
	if m.err != "" || m.consecutiveErrors != 0 {
		t.Errorf("success should clear the error state: err=%q consecutiveErrors=%d", m.err, m.consecutiveErrors)
	}
}

func TestRunQueryAdvancesSequence(t *testing.T) {
	m := newTestModel()
	m.runQuery(HomeQuery())
	m.runQuery(HomeQuery())
	if m.querySeq != 2 {
		t.Errorf("querySeq = %d, want 2", m.querySeq)
	}
}

func TestHomeDataComputesRateOnMainGoroutine(t *testing.T) {
	m := newTestModel()
	m.querySeq = 1
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	m.Update(homeDataMsg{seq: 1, data: homeData{barChart: "bars", commits: 100, commitsAt: start, commitsOK: true}})
	m.Update(homeDataMsg{seq: 1, data: homeData{barChart: "bars", commits: 200, commitsAt: start.Add(2 * time.Second), commitsOK: true}})

	values := m.sparklineData.Values
	if len(values) != 2 {
		t.Fatalf("sparkline has %d points, want 2", len(values))
	}
	if values[1] != 50 {
		t.Errorf("commit rate = %v, want 50/s", values[1])
	}

	// A stale refresh must not touch the rate history
	m.querySeq = 2
	m.Update(homeDataMsg{seq: 1, data: homeData{commits: 999, commitsAt: start.Add(3 * time.Second), commitsOK: true}})
	if len(m.sparklineData.Values) != 2 || m.lastCommits != 200 {
		t.Error("stale Home data was applied")
	}
}

func TestBuildDSNOmitsEmptyPassword(t *testing.T) {
	cfg := &DBConfig{Host: "h", Port: "5432", Database: "d", User: "u"}
	dsn := buildDSN(cfg, "", "require")
	if strings.Contains(dsn, "password") {
		t.Errorf("empty password must be omitted so lib/pq reads ~/.pgpass: %s", dsn)
	}
	if !strings.Contains(dsn, "sslmode='require'") {
		t.Errorf("sslmode missing: %s", dsn)
	}

	cfg.SSLRootCert = "/etc/ca.pem"
	if dsn := buildDSN(cfg, "pw", "verify-full"); !strings.Contains(dsn, "sslrootcert='/etc/ca.pem'") || !strings.Contains(dsn, "sslmode='verify-full'") {
		t.Errorf("TLS settings not passed through: %s", dsn)
	}
}

func TestDriverSSLMode(t *testing.T) {
	tests := []struct {
		name         string
		cfg          DBConfig
		env          string
		wantMode     string
		wantFallback bool
	}{
		{"unset defaults to prefer", DBConfig{}, "", "require", true},
		{"explicit prefer", DBConfig{SSLMode: "prefer"}, "", "require", true},
		{"disable", DBConfig{SSLMode: "disable"}, "", "disable", false},
		{"allow", DBConfig{SSLMode: "allow"}, "", "disable", false},
		{"verify-full", DBConfig{SSLMode: "verify-full"}, "", "verify-full", false},
		{"env used when unset", DBConfig{}, "disable", "disable", false},
		{"service file beats env", DBConfig{SSLMode: "require"}, "disable", "require", false},
		{"rds iam never falls back", DBConfig{Auth: "rds-iam"}, "", "require", false},
		{"rds iam allow still uses tls", DBConfig{Auth: "rds-iam", SSLMode: "allow"}, "", "require", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PGSSLMODE", tt.env)
			mode, fallback := tt.cfg.driverSSLMode()
			if mode != tt.wantMode || fallback != tt.wantFallback {
				t.Errorf("driverSSLMode() = (%q, %v), want (%q, %v)", mode, fallback, tt.wantMode, tt.wantFallback)
			}
		})
	}
}

func TestGetDBConfigParsesSSLSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	conf := "[svc]\nhost=h\nsslmode=verify-full\nsslrootcert=/ca.pem\nsslcert=/c.pem\nsslkey=/k.pem\n"
	if err := os.WriteFile(filepath.Join(home, ".pg_service.conf"), []byte(conf), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := getDBConfig("svc")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSLMode != "verify-full" || cfg.SSLRootCert != "/ca.pem" || cfg.SSLCert != "/c.pem" || cfg.SSLKey != "/k.pem" {
		t.Errorf("TLS settings not parsed: %+v", cfg)
	}
}

func TestCreateQueryRefusesOverwrite(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	q := Query{Name: "Q", Description: "d", SQL: "SELECT 1"}
	if err := qdb.CreateQuery(q); err != nil {
		t.Fatal(err)
	}
	q.SQL = "SELECT 2"
	if err := qdb.CreateQuery(q); !errors.Is(err, ErrQueryExists) {
		t.Fatalf("CreateQuery duplicate err = %v, want ErrQueryExists", err)
	}
	got, _ := qdb.GetQuery("Q")
	if got.SQL != "SELECT 1" {
		t.Errorf("existing query was overwritten: %q", got.SQL)
	}
}

func TestUpdateQueryRenames(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()

	pos := 1
	for _, q := range []Query{
		{Name: "Old", Description: "d", SQL: "SELECT 1", OrderPosition: &pos},
		{Name: "Other", Description: "d", SQL: "SELECT 2"},
	} {
		if err := qdb.CreateQuery(q); err != nil {
			t.Fatal(err)
		}
	}

	if err := qdb.UpdateQuery("Old", Query{Name: "New", Description: "d2", SQL: "SELECT 3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := qdb.GetQuery("Old"); err == nil {
		t.Error("rename left the old row behind")
	}
	got, err := qdb.GetQuery("New")
	if err != nil || got.SQL != "SELECT 3" || got.OrderPosition != nil {
		t.Errorf("renamed query = %+v, err %v", got, err)
	}

	if err := qdb.UpdateQuery("New", Query{Name: "Other", Description: "d", SQL: "x"}); !errors.Is(err, ErrQueryExists) {
		t.Errorf("rename onto existing name err = %v, want ErrQueryExists", err)
	}
	if err := qdb.UpdateQuery("Missing", Query{Name: "Missing"}); err == nil {
		t.Error("updating a missing query should fail")
	}
}

func TestReloadQueriesKeepsBuiltinAndTemporaryTabs(t *testing.T) {
	qdb, _ := setupTestQueryDB(t)
	defer qdb.Close()
	prev := globalQueryDB
	globalQueryDB = qdb
	defer func() { globalQueryDB = prev }()

	pos := 1
	qdb.CreateQuery(Query{Name: "Visible", Description: "d", SQL: "SELECT 1", OrderPosition: &pos})
	qdb.CreateQuery(Query{Name: "Hidden", Description: "d", SQL: "SELECT 2"})
	qdb.CreateQuery(Query{Name: "Gone", Description: "d", SQL: "SELECT 3"})

	m := newTestModel()
	m.tempQueries = map[string]int{"Hidden": 10, "Gone": 11}
	qdb.DeleteQuery("Gone")

	if err := m.reloadQueries(); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, q := range m.queries {
		names = append(names, q.Name)
	}
	if got := strings.Join(names, ","); got != "Home,Active,Statements,Visible,Hidden" {
		t.Errorf("tabs after reload = %s", got)
	}
	if _, ok := m.tempQueries["Gone"]; ok {
		t.Error("temporary entry for a deleted query was kept")
	}

	m.addTemporaryQuery(Query{Name: "Hidden"})
	if len(m.queries) != 5 {
		t.Error("addTemporaryQuery duplicated an existing tab")
	}
}

func TestValidateQueryName(t *testing.T) {
	for _, name := range []string{"", "Home", "Active"} {
		if validateQueryName(name) == nil {
			t.Errorf("validateQueryName(%q) should fail", name)
		}
	}
	if err := validateQueryName("Locks"); err != nil {
		t.Errorf("validateQueryName(Locks) = %v", err)
	}
}

func TestTruncateUnicode(t *testing.T) {
	if got := truncate("héllo wörld", 5); got != "héll~" {
		t.Errorf("truncate accented = %q, want %q", got, "héll~")
	}
	wide := "日本語テキスト" // 2 cells per character
	got := truncate(wide, 5)
	if w := ansi.StringWidth(got); w > 5 {
		t.Errorf("truncate(wide, 5) = %q with width %d", got, w)
	}
	if !strings.HasSuffix(got, "~") || strings.ContainsRune(got, '�') {
		t.Errorf("truncate(wide, 5) = %q split a character", got)
	}
	for _, s := range []string{"abc", "ñandú", wide} {
		if w := ansi.StringWidth(fitWidth(s, 8)); w != 8 {
			t.Errorf("fitWidth(%q, 8) has width %d", s, w)
		}
	}
}

func TestSearchInputHandlesMultibyte(t *testing.T) {
	m := newTestModel()
	m.searchMode = true
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ñ")})
	m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")})
	if m.searchQuery != "ñ é" {
		t.Errorf("searchQuery = %q, want %q", m.searchQuery, "ñ é")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.searchQuery != "ñ " {
		t.Errorf("after backspace searchQuery = %q, want %q", m.searchQuery, "ñ ")
	}
}
