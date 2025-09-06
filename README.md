# psq - postgres status query

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
   go build -o psq
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

The tool automatically creates `~/.psq/queries.db` sqllite table on first run with default monitoring queries. You can edit this file to add, modify, or remove queries.

I periodically dump by queries.db file here and it can be moved to `~/.psq/queries.db` if you want to use my defaults.

## Usage

Run the monitoring interface:

```bash
# Use default service
./psq

# Use specific service from ~/.pg_service.conf
./psq prod
./psq --service prod
./psq -s prod
```

### Navigation

Query Selection:
- **←/→** or **h/l**: Navigate between queries
- **Enter** or **Space**: Execute selected query with auto-refresh (every 2 seconds)
- **r**: Execute selected query now
- **e**: Edit queries in vi (or $EDITOR)
- **q** or **Ctrl+C**: Quit the application

Results Navigation:
- **↑/↓** or **j/k**: Scroll results up/down by line
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

1. Add queries to `~/.psq/queries/`
2. Queries are .sql files where the first line include a comment which is the query title
3. Restart the application

## Requirements

- Go 1.21+
- `~/.pg_service.conf` file with database connection details
