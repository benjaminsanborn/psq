package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Output formats for non-interactive mode
const (
	formatTable = "table"
	formatCSV   = "csv"
	formatJSON  = "json"
)

func validFormat(f string) bool {
	return f == formatTable || f == formatCSV || f == formatJSON
}

// runOnce connects to service, runs a saved query (by name) or raw SQL once,
// and writes the result to w. It is psq's non-interactive mode.
func runOnce(w io.Writer, service, queryName, rawSQL, format string) error {
	if !validFormat(format) {
		return fmt.Errorf("unknown format %q (use table, csv or json)", format)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var res *resultSet
	if rawSQL == "" && IsActiveTab(queryName) {
		db, err := connectDB(service)
		if err != nil {
			return err
		}
		defer db.Close()
		processes, err := FetchActiveProcesses(ctx, db)
		if err != nil {
			return err
		}
		linkBlocking(processes)
		res = activeResultSet(processes)
	} else {
		query := rawSQL
		if query == "" {
			q, err := findSavedQuery(queryName)
			if err != nil {
				return err
			}
			query = q.SQL
		}
		db, err := connectDB(service)
		if err != nil {
			return err
		}
		defer db.Close()
		if res, err = queryRows(ctx, db, query, 0); err != nil {
			return err
		}
	}

	switch format {
	case formatCSV:
		return writeCSV(w, res)
	case formatJSON:
		return writeJSON(w, res)
	default:
		return writeTable(w, res)
	}
}

// findSavedQuery looks a query up by name, exactly first, then ignoring case
func findSavedQuery(name string) (Query, error) {
	if IsHomeTab(name) || IsStatementsTab(name) {
		return Query{}, fmt.Errorf("%q is an interactive dashboard; run psq without --query to see it", name)
	}
	if globalQueryDB == nil {
		if err := initQueryDB(); err != nil {
			return Query{}, err
		}
	}
	all, err := globalQueryDB.LoadAllQueries()
	if err != nil {
		return Query{}, err
	}
	for _, q := range all {
		if q.Name == name {
			return q, nil
		}
	}
	var matches []Query
	for _, q := range all {
		if strings.EqualFold(q.Name, name) {
			matches = append(matches, q)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return Query{}, fmt.Errorf("no saved query named %q (see psq --list-queries)", name)
}

// listQueries prints the saved query library
func listQueries(w io.Writer) error {
	if globalQueryDB == nil {
		if err := initQueryDB(); err != nil {
			return err
		}
	}
	all, err := globalQueryDB.LoadAllQueries()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTAB\tDESCRIPTION")
	fmt.Fprintf(tw, "%s\t%s\t%s\n", HomeQuery().Name, "built-in", HomeQuery().Description)
	fmt.Fprintf(tw, "%s\t%s\t%s\n", ActiveQuery().Name, "built-in", ActiveQuery().Description)
	fmt.Fprintf(tw, "%s\t%s\t%s\n", StatementsQuery().Name, "built-in", StatementsQuery().Description)
	for _, q := range all {
		tab := "hidden"
		if q.OrderPosition != nil {
			tab = strconv.Itoa(*q.OrderPosition)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", q.Name, tab, q.Description)
	}
	return tw.Flush()
}

// activeResultSet flattens Active tab data into rows for output
func activeResultSet(processes []ActiveProcess) *resultSet {
	res := &resultSet{
		Columns: []string{
			"pid", "user", "database", "application", "client_addr", "state",
			"state_seconds", "query_seconds", "xact_seconds", "wait_event_type", "wait_event",
			"blocked_by", "blocking_total", "query",
		},
		Types: []string{
			"INT4", "TEXT", "TEXT", "TEXT", "TEXT", "TEXT",
			"FLOAT8", "FLOAT8", "FLOAT8", "TEXT", "TEXT",
			"_INT4", "INT4", "TEXT",
		},
	}
	for _, p := range processes {
		var xact any = p.XactSec
		if p.XactSec < 0 {
			xact = nil
		}
		blockedBy := p.BlockedBy
		if blockedBy == nil {
			blockedBy = []int{}
		}
		res.Rows = append(res.Rows, []any{
			p.PID, p.Username, p.Database, p.ApplicationName, p.ClientAddr, p.State,
			p.StateSec, p.DurationSec, xact, p.WaitEventType, p.WaitEvent,
			blockedBy, p.BlockingTotal, p.Query,
		})
	}
	return res
}

// plainCell formats a value for table and CSV output; NULL becomes empty
func plainCell(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case []int:
		return pidList(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return cellString(v)
	}
}

func writeTable(w io.Writer, res *resultSet) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(res.Columns, "\t"))
	for _, row := range res.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			if v == nil {
				cells[i] = "NULL"
			} else {
				cells[i] = strings.ReplaceAll(scrubNewlines(plainCell(v)), "\t", " ")
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

func writeCSV(w io.Writer, res *resultSet) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(res.Columns); err != nil {
		return err
	}
	for _, row := range res.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = plainCell(v)
		}
		if err := cw.Write(cells); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// numericTypes come back from lib/pq as text but should be JSON numbers
var numericTypes = map[string]bool{"NUMERIC": true, "DECIMAL": true}

// writeJSON writes an array of objects whose keys follow column order.
// Repeated column names get a numeric suffix so no value is lost.
func writeJSON(w io.Writer, res *resultSet) error {
	keys := make([]string, len(res.Columns))
	seen := map[string]int{}
	for i, c := range res.Columns {
		seen[c]++
		keys[i] = c
		if n := seen[c]; n > 1 {
			keys[i] = fmt.Sprintf("%s_%d", c, n)
		}
	}

	var b strings.Builder
	b.WriteString("[")
	for r, row := range res.Rows {
		if r > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n  {")
		for i, v := range row {
			if i > 0 {
				b.WriteString(", ")
			}
			k, _ := json.Marshal(keys[i])
			b.Write(k)
			b.WriteString(": ")

			var val []byte
			var err error
			s, isString := v.(string)
			switch {
			case isString && i < len(res.Types) && numericTypes[res.Types[i]] && json.Valid([]byte(s)):
				val = []byte(s)
			case isString && i < len(res.Types) && (res.Types[i] == "JSON" || res.Types[i] == "JSONB") && json.Valid([]byte(s)):
				val = []byte(s)
			default:
				val, err = json.Marshal(v)
			}
			if err != nil {
				return fmt.Errorf("column %s: %w", res.Columns[i], err)
			}
			b.Write(val)
		}
		b.WriteString("}")
	}
	if len(res.Rows) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("]\n")
	_, err := io.WriteString(w, b.String())
	return err
}
