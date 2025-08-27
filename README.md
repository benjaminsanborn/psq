# pgmon - PostgreSQL Monitoring CLI

A TUI-based PostgreSQL monitoring tool built with Go and Bubble Tea that reads database connections from `~/.pg_service.conf`.

## Features

- Interactive TUI interface with keyboard navigation
- Reads database connections from `~/.pg_service.conf`
- Pre-configured monitoring queries for common PostgreSQL operations
- Configurable queries via JSON file
- Real-time query execution and results display

## Installation

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod tidy
   ```
3. Build the application:
   ```bash
   go build -o pgmon
   ```

## Configuration

### Database Connection

Create or update your `~/.pg_service.conf` file:

```ini
[default]
host=localhost
port=5432
dbname=your_database
user=your_user
password=your_password

[prod]
host=prod-server.example.com
port=5432
dbname=production_db
user=prod_user
password=prod_password

[staging]
host=staging-server.example.com
port=5432
dbname=staging_db
user=staging_user
password=staging_password
```

### Queries Configuration

The tool automatically creates `~/.pgmon/queries.json` on first run with default monitoring queries. You can edit this file to add, modify, or remove queries:

```json
[
  {
    "name": "Active Connections",
    "description": "Show current active connections",
    "sql": "SELECT pid, usename, application_name, client_addr, state, query_start, state_change FROM pg_stat_activity WHERE state IS NOT NULL ORDER BY query_start DESC;"
  },
  {
    "name": "Custom Query",
    "description": "Your custom monitoring query",
    "sql": "SELECT * FROM your_table WHERE condition = 'value';"
  }
]
```

## Usage

Run the monitoring interface:

```bash
# Use default service
./pgmon

# Use specific service from ~/.pg_service.conf
./pgmon prod
./pgmon --service prod
./pgmon -s prod
```

### Navigation

Query Selection:
- **↑/↓** or **j/k**: Navigate between queries
- **Enter** or **Space**: Execute selected query with auto-refresh (every 2 seconds)
- **r**: Execute selected query once (no auto-refresh)
- **a**: Toggle auto-refresh on/off
- **q** or **Ctrl+C**: Quit the application

Results Navigation:
- **Alt+↑/↓**: Scroll results up/down by line
- **PgUp/PgDn**: Scroll half page up/down
- **Home/End**: Jump to top/bottom of results

## Default Queries

The tool comes with these pre-configured monitoring queries:

1. **Active Connections** - Current active database connections
2. **Subscription Status** - Logical replication subscription status
3. **Index Creation Progress** - Progress of index creation operations
4. **Database Size** - Size of all databases
5. **Table Sizes** - Largest tables in the database
6. **Lock Information** - Current database locks
7. **Slow Queries** - Long-running queries
8. **Replication Lag** - Replication lag information

## Adding Custom Queries

To add your own monitoring queries:

1. Edit `~/.pgmon/queries.json`
2. Add a new query object with:
   - `name`: Display name for the query
   - `description`: Brief description
   - `sql`: The SQL query to execute
3. Restart the application

## Requirements

- Go 1.21+
- PostgreSQL database
- `~/.pg_service.conf` file with database connection details
