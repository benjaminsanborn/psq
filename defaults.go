package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
)

// defaultQueriesJSON is the starter query pack shipped with psq. New
// databases are seeded from it; existing users can merge in additions with
// psq --import-defaults.
//
//go:embed defaults/queries.json
var defaultQueriesJSON []byte

func defaultQueries() ([]Query, error) {
	var queries []Query
	if err := json.Unmarshal(defaultQueriesJSON, &queries); err != nil {
		return nil, fmt.Errorf("embedded default queries are invalid: %w", err)
	}
	return queries, nil
}

func (qdb *QueryDB) createDefaultQueries() error {
	queries, err := defaultQueries()
	if err != nil {
		return err
	}
	for _, q := range queries {
		if err := qdb.CreateQuery(q); err != nil && !errors.Is(err, ErrQueryExists) {
			return fmt.Errorf("failed to create default query %s: %w", q.Name, err)
		}
	}
	return nil
}

// ImportDefaults adds pack queries the user doesn't have yet, never touching
// existing ones. Imported queries are hidden so they don't rearrange the
// user's tabs; they are reachable from search (s) right away.
func (qdb *QueryDB) ImportDefaults() (added, skipped []string, err error) {
	queries, err := defaultQueries()
	if err != nil {
		return nil, nil, err
	}
	for _, q := range queries {
		q.OrderPosition = nil
		switch err := qdb.CreateQuery(q); {
		case errors.Is(err, ErrQueryExists):
			skipped = append(skipped, q.Name)
		case err != nil:
			return added, skipped, fmt.Errorf("failed to import %s: %w", q.Name, err)
		default:
			added = append(added, q.Name)
		}
	}
	return added, skipped, nil
}
