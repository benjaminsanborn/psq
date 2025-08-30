# Example Queries

This directory contains example PostgreSQL monitoring queries that can be used with pgi.

## Query Format

Each `.sql` file follows this format:
- First line: `-- Query Name`
- Second line: `-- Description of what the query does`
- Following lines: The SQL query

## Usage

You can copy these queries to your `~/.pgi/queries.json` file, or use them as inspiration for creating your own monitoring queries.

## Available Queries

- **active.sql** - Shows current active database connections
- **lock_information.sql** - Displays current database locks
- **replication_lag.sql** - Shows replication lag information
- **top_queries.sql** - Identifies heavy hitter queries (requires pg_stat_statements)
- **index_creation.sql** - Shows progress of index creation operations
- **table_replication_state.sql** - Shows logical replication state for tables
- **configuration_settings.sql** - Shows all PostgreSQL configuration settings

## Converting to JSON

To use these queries in pgi, you can manually add them to your `~/.pgi/queries.json` file in this format:

```json
{
  "name": "Query Name",
  "description": "Description from the SQL file",
  "sql": "SELECT statement from the SQL file"
}
```
