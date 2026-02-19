# psq

> PostgreSQL monitoring in the CLI

A modern TUI-based PostgreSQL monitoring tool built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea). Inspired by the classic PgAdmin interface, designed for terminal lovers.

![Made with VHS](https://vhs.charm.sh/vhs-5cK5mfgMlRZXM2f5elnpfs.gif)

## Features

### 🚀 Core Features
- **Interactive Service Picker** - Fuzzy search through your database connections
- **Pre-configured Queries** - Common monitoring queries ready to go (connections, locks, queries, replication, etc.)
- **Active Connection Viewer** - Real-time view of active queries with terminate/cancel capabilities
- **Custom Query Editor** - Create and edit your own monitoring queries
- **Query Search** - Fast search across all saved queries (including hidden ones)
- **Mouse Support** - Click tabs to navigate, full keyboard shortcuts available
- **Persistent Queries** - SQLite-backed query storage with import/export

### 🎨 UI Features
- Clean tabbed interface with status indicators
- Real-time query results with syntax highlighting
- Sparkline charts for transaction rate visualization
- Detailed process view with full query text and stats
- Smart refresh rate limiting (500ms cooldown)

### 🤖 AI-Powered (Optional)
- ChatGPT integration for query generation (requires `$OPENAI_API_KEY`)
- Generate complex queries from natural language descriptions
- **Use with caution** - always review generated queries before running

## Installation

### Homebrew

```bash
brew install benjaminsanborn/psq/psq
```

### From Source

```bash
# Clone the repository
git clone https://github.com/benjaminsanborn/psq.git
cd psq

# Build
go build -o psq

# Optional: Install to $GOPATH/bin
go install
```

## Quick Start

1. **Set up PostgreSQL service file** (`~/.pg_service.conf`):
   ```ini
   [prod]
   host=prod.example.com
   port=5432
   dbname=mydb
   user=monitor
   password=secret

   [staging]
   host=staging.example.com
   port=5432
   dbname=mydb
   user=monitor
   ```

2. **Run psq**:
   ```bash
   # Interactive service picker
   psq

   # Connect directly to a service
   psq prod
   ```

3. **Navigate** with keyboard shortcuts (press `?` for help)

## Usage

```bash
# Show service picker with fuzzy search
psq

# Connect to specific service
psq prod
psq --service staging
psq -s dev

# Show help
psq --help

# Show version
psq --version
```

## Keyboard Shortcuts

### Navigation
- **←/→** or **h/l** - Switch between query tabs
- **↑/↓** or **k/j** - Scroll viewport up/down
- **PgUp/PgDn** - Page up/down
- **Home/End** - Jump to top/bottom
- **Click** - Mouse navigation on query tabs

### Query Operations
- **Enter/Space/R** - Execute current query (refresh)
- **S** - Search queries (fuzzy search, works on hidden queries too)
- **E** - Edit current query
- **N** - Create new query
- **D** - Dump queries to file
- **X** - Open psql prompt for current database

### Active Connections View
When on the "Active" tab:
- **↑/↓** or **k/j** - Select process
- **Enter** - View process details
- **T** - Terminate backend (`pg_terminate_backend`)
- **C** - Cancel query (`pg_cancel_backend`)
- **Y** - Copy query to clipboard (in detail view)
- **Esc** - Back to list / exit detail view

### Edit Mode
- **Tab** - Switch between fields (name, description, order, SQL)
- **Ctrl+S** - Save query
- **Ctrl+D** - Delete query
- **Ctrl+G** - Generate query with ChatGPT (requires `$OPENAI_API_KEY`)
- **Esc** - Cancel and return

### Other
- **?** - Toggle help
- **C** - Return to service picker
- **Esc/Ctrl+C** - Quit

## Configuration

### Query Database

Queries are stored in `~/.psq/queries.db` (SQLite). The database is auto-created on first run with sensible defaults.

**Query Structure:**
```sql
CREATE TABLE queries (
    name TEXT PRIMARY KEY,
    description TEXT,
    sql TEXT,
    order_position INTEGER  -- NULL = hidden from tabs
);
```

**Importing Queries:**
I periodically export my query collection. You can download and copy it to `~/.psq/queries.db` to use my defaults.

### Service Configuration

psq uses the standard PostgreSQL service file format (`~/.pg_service.conf`):

```ini
[service_name]
host=hostname
port=5432
dbname=database_name
user=username
password=password  # or use .pgpass for security
sslmode=require    # optional SSL settings
```

See [PostgreSQL documentation](https://www.postgresql.org/docs/current/libpq-pgservice.html) for more options.

### AI Features

To enable ChatGPT query generation:

```bash
export OPENAI_API_KEY=sk-...
```

**⚠️ Important**: AI-generated queries should always be reviewed before execution. Never blindly run AI-generated queries on production databases.

## Built-in Queries

psq comes with several pre-configured monitoring queries:

- **Home** - Connection overview with sparkline charts
- **Active** - Interactive active connections viewer
- **Connections** - Current database connections by state
- **Locks** - Lock information and blocking queries
- **Long Queries** - Queries running longer than 5 minutes
- **Tables** - Table sizes and statistics
- **Indexes** - Index usage and sizes
- **Replication** - Replication lag and status
- **Cache Hit Ratio** - Buffer cache effectiveness
- **Vacuum** - Autovacuum status

## Architecture

**Tech Stack:**
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Style rendering
- [Cobra](https://github.com/spf13/cobra) - CLI framework
- SQLite - Query storage
- PostgreSQL - Target monitoring database

**Design Philosophy:**
- Fast and responsive (persistent DB connections)
- Keyboard-first with mouse support
- Extensible query system
- Safe defaults with power-user features

## Development

```bash
# Run tests
go test ./...

# Run with race detector
go run -race main.go

# Build with version info
go build -ldflags "-X main.version=v1.0.0" -o psq

# Format code
go fmt ./...
```

## Requirements

- Go 1.21+ (for building from source)
- PostgreSQL database(s) to monitor
- `~/.pg_service.conf` configured
- Optional: `$OPENAI_API_KEY` for AI features

## Tips & Tricks

1. **Hidden Queries** - Set `order_position` to `NULL` in SQLite to hide queries from tabs. They're still searchable via `S`.

2. **Quick Switching** - Use the service picker (`C` key) to quickly switch between databases without quitting.

3. **Custom Queries** - Create custom queries for your specific monitoring needs. Use `N` to create, `E` to edit.

4. **Refresh Rate** - There's a 500ms cooldown between query executions to prevent hammering the database.

5. **psql Integration** - Press `X` to drop into `psql` with the current connection. Great for ad-hoc queries.

6. **Query Export** - Use `D` to dump your query collection for backup or sharing.

## Roadmap

- [ ] Query result export (CSV, JSON)
- [ ] Configurable refresh intervals
- [ ] Query history and favorites
- [ ] Dashboard mode (multiple queries visible)
- [ ] Plugin system for custom visualizations
- [ ] PostgreSQL explain plan visualization

## Contributing

Contributions welcome! Please open an issue or PR.

## License

MIT

## Author

Benjamin Sanborn ([@benjaminsanborn](https://github.com/benjaminsanborn))

---

**Like psq?** Star the repo ⭐ and share with other PostgreSQL users!
