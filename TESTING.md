# Testing Guide

## Running Tests

psq includes a comprehensive test suite covering core functionality.

### Quick Start

```bash
# Run all unit tests
make test

# Or directly with go test
go test -v ./...

# Run with race detector (recommended for development)
go test -v -race ./...

# Generate coverage report
make coverage
```

### Test Coverage

Current test files:

- **database_test.go** - PostgreSQL config parsing (`~/.pg_service.conf`)
  - Service configuration parsing
  - Default port handling
  - Service listing
  - Error handling for missing services

- **model_test.go** - Query filtering and UI state management
  - Query search/filtering (case-insensitive, partial matching)
  - Selection boundary validation
  - Temporary query tracking
  
- **querydb_test.go** - SQLite query database operations
  - Query CRUD operations (Create, Read, Update, Delete)
  - Visible vs hidden queries (`LoadQueries` vs `LoadAllQueries`)
  - Query retrieval by name

### What's Tested

✅ Config file parsing  
✅ Query database operations (SQLite)  
✅ Search and filtering logic  
✅ Selection state management  
✅ Temporary query handling  

### What's NOT Tested (Yet)

⚠️ **Integration tests** - No tests that connect to a real PostgreSQL instance  
⚠️ **UI rendering** - BubbleTea views are not tested  
⚠️ **ChatGPT integration** - API calls are not mocked/tested  
⚠️ **Home dashboard widgets** - Chart rendering, metrics collection  

### Adding New Tests

When adding new features, please add corresponding tests:

1. **Pure logic functions** → unit tests (easy to test)
2. **Database operations** → use `t.TempDir()` for isolated SQLite DBs
3. **Config parsing** → use temporary files with `t.TempDir()`

Example test structure:

```go
func TestNewFeature(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"case 1", "input1", "output1", false},
        {"error case", "bad", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := YourFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Continuous Testing

Tests run automatically on every change during development:

```bash
# Watch mode (requires entr or similar)
find . -name "*.go" | entr -c make test
```

### GitHub Actions / CI

TODO: Add `.github/workflows/test.yml` for automated testing on push/PR

```yaml
name: Tests
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - run: make test
```

### Test Philosophy

- **Fast** - Unit tests should run in &lt;2 seconds
- **Isolated** - Use temp directories, no shared state
- **Table-driven** - Use subtests for multiple scenarios
- **Descriptive** - Test names should explain what's being tested
- **Coverage over 100%** - Aim for high coverage of core logic (not UI)

### Future Work

- [ ] Add integration tests with dockerized Postgres
- [ ] Mock ChatGPT API for AI feature tests
- [ ] Add benchmark tests for query performance
- [ ] Test connection pooling/reuse behavior
- [ ] Add fuzzing for config parser
