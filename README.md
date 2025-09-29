# > psq

Postgres monitoring in the CLI. Partially inspired from the old UI of PgAdmin.
![Made with VHS](https://vhs.charm.sh/vhs-TLXlgU5wqp7tjiBPWHsAE.gif)

## Install

### Homebrew

```bash
brew install benjaminsanborn/psq/psq
```

### Source
1. Clone the repository
2. Install dependencies:
   ```bash
   go mod tidy
   ```
3. Build the application:
   ```bash
   go build -o psq
   ```

## Features

- Interactive TUI interface with keyboard navigation
- Reads database connections from `~/.pg_service.conf`
- Pre-configured monitoring queries for common PostgreSQL operations
- Configurable queries via built-in editor with optional ChatGPT query generation (be careful!)
- Real-time query execution and results display

## Configuration

### Queries Configuration

The tool automatically creates `~/.psq/queries.db` sqllite table on first run with default monitoring queries.

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

## Requirements

- Go 1.21+
- `~/.pg_service.conf` file with database connection details
- `$OPENAI_API_KEY` environment variable, for query generation
