# psq

> PostgreSQL monitoring in the CLI

A modern TUI-based PostgreSQL monitoring tool built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea). Inspired by the classic PgAdmin interface, designed for terminal lovers.

![Made with VHS](https://vhs.charm.sh/vhs-5cK5mfgMlRZXM2f5elnpfs.gif)

## Features

- **Home dashboard**: connection states, transactions/sec, cache hit ratio, replication lag and blocked queries at a glance
- **Active sessions**: live view of every non-idle session, with terminate/cancel, sort and filter
- **Lock tree**: see who is blocking whom, with root blockers first and their waiters nested underneath
- **Statements**: the top `pg_stat_statements` entries over the last 10s, 1m or 5m, instead of totals since the last reset
- **Saved queries**: a starter pack of monitoring queries, plus your own, stored in SQLite and searchable with fuzzy search
- **Scriptable**: run any saved query once from the command line and get a table, CSV or JSON
- **Safe on production**: saved queries run read-only with a statement timeout, and every session is labeled `psq`
- **Refresh control**: pause, change the interval live, or give each query its own interval
- **Service picker**: fuzzy-filter the services in `~/.pg_service.conf`
- **Mouse support**: click tabs, services and the blocked-queries widget

## Installation

### Homebrew

```bash
brew install benjaminsanborn/psq/psq
```

### From Source

```bash
git clone https://github.com/benjaminsanborn/psq.git
cd psq
go build -o psq     # or: go install
```

## Quick Start

1. **Set up a PostgreSQL service file** (`~/.pg_service.conf`):
   ```ini
   [prod]
   host=prod.example.com
   port=5432
   dbname=mydb
   user=monitor

   [local]
   host=localhost
   dbname=postgres
   user=postgres
   ```
   Put passwords in `~/.pgpass` rather than in the service file.

2. **Run psq**:
   ```bash
   psq          # pick a service
   psq prod     # connect directly
   ```

3. Press `?` for the keyboard shortcuts.

## Usage

```bash
psq                         # service picker
psq prod                    # connect to a service (or: psq -s prod)

# Non-interactive: run once, print, exit
psq prod --query Locks                     # saved query as a table
psq prod -q "lock information" -f csv      # names are case-insensitive
psq prod -q Active -f json                 # sessions, including blocked_by and blocking_total
psq prod --sql "SELECT now()" -f json      # ad-hoc SQL (also read-only)
psq --list-queries                         # what's saved

# Options
psq prod --refresh 5s                      # default auto-refresh interval (default 1s)
psq prod --statement-timeout 30s           # cancel slow statements (default 10s, 0 disables)
psq --import-defaults                      # add new starter queries to your library
```

Non-interactive output has no colors or truncation. NULL is empty in CSV and `null` in JSON, and `numeric` and `json` columns come out as JSON values rather than strings. The exit status is non-zero on any error.

## Keyboard Shortcuts

### Tabs and refresh
- **←/→** or **h/l**: switch tabs (or click one)
- **Enter/Space/r**: refresh now
- **p**: pause / resume auto-refresh
- **+ / -**: refresh less / more often (1s to 1m). The header shows the current interval.

### Scrolling
- **↑/↓** or **k/j**: scroll results
- **PgUp/PgDn**, **Home/End**: page, jump to top/bottom

### Saved queries
- **s**: fuzzy search all queries, including hidden ones
- **e**: edit the current query, **n**: new query
- In the editor: **Tab** switches fields, **Ctrl+S** saves, **Ctrl+D** twice deletes, **Esc** cancels
- **x**: open `psql` on the current connection

### Home
- **b** (or click the blocked-queries widget): open the lock tree

### Active
- **↑/↓** or **k/j**: select a session, **Enter**: details
- **t**: terminate (`pg_terminate_backend`), **c**: cancel query (`pg_cancel_backend`), both with confirmation
- **b**: lock tree. Shows only blocking chains: root blockers in red, waiting sessions in yellow, nested under what they wait on.
- **o**: cycle sort: duration, blocking, state, user, database
- **/**: filter by PID, user, database, state, application, client or query text
- **y**: copy the query (in details)
- **Esc**: back out of details, the filter, or the lock tree

The **Locks** column shows `blocks N` for a root blocker (N = sessions held up, directly or not) and `by PID` for a waiting session. For `idle in transaction` sessions, **Time** is how long the session has sat idle, which is usually the number that matters.

### Statements
- **w**: window: last 10s, 1m or 5m
- **o**: cycle sort: total time, calls, rows, mean time, disk reads

Statements needs the `pg_stat_statements` extension. psq takes a sample on each refresh and shows the difference over the window. While a window is still filling, the header says how much data it has so far.

### Other
- **?**: help
- **c**: back to the service picker
- **q** or **Ctrl+C**: quit. **Esc** never quits; it always means "back".

In the service picker: **/** to fuzzy-filter, **e** to edit `~/.pg_service.conf`, **q/Esc** to quit.

## Safety

psq is meant to be left running against production, so:

- **Saved queries and `--sql` run in a `READ ONLY` transaction.** A query that writes fails instead of running every second.
- **Every statement has a timeout** (`--statement-timeout`, default 10s), set with `SET statement_timeout` on each connection so it works through PgBouncer.
- **Sessions are labeled `application_name=psq`**, so they're easy to spot and exclude.
- **Switching tabs cancels the previous tab's query** on the server rather than letting it finish.
- **The TUI shows at most 1000 rows** of a saved query. Add a `LIMIT` or `ORDER BY` for big results.
- **Slow-changing queries can refresh less often or only on demand** (see the editor's *Refresh Every* field).

## Configuration

### Service file

psq reads `~/.pg_service.conf` ([format](https://www.postgresql.org/docs/current/libpq-pgservice.html)). It understands `host`, `port`, `dbname`, `user`, `password`, `sslmode`, `sslrootcert`, `sslcert` and `sslkey`. A missing password is looked up in `~/.pgpass`.

`sslmode` follows libpq: when it isn't set (in the file or `$PGSSLMODE`), psq tries TLS and falls back to a plain connection if the server doesn't support TLS. Set `sslmode=require` (or `verify-full`) to insist on TLS.

#### AWS RDS IAM Authentication

To connect to RDS or Aurora with IAM auth instead of a password, add `#psq:` lines to the service block. They are written as comments so `psql` and libpq can still read the file:

```ini
[prod-iam]
host=mydb.cluster-abc123.us-east-1.rds.amazonaws.com
port=5432
dbname=app
user=iam_db_user
#psq:auth=rds-iam
#psq:aws_profile=prod        # optional, defaults to AWS_PROFILE / default chain
#psq:aws_region=us-east-1    # optional, inferred from the RDS hostname
```

psq uses the standard AWS credential chain (env vars, `~/.aws/config` including SSO, instance roles) and creates a new 15-minute token for each new connection, so long sessions stay connected. The `x` psql prompt receives a fresh token too. The database user needs `GRANT rds_iam TO iam_db_user`, and the IAM principal needs `rds-db:connect`.

### Saved queries

Queries live in `~/.psq/queries.db` (SQLite). A new database starts with psq's starter pack. When a release adds starter queries, `psq --import-defaults` adds the ones you don't have as hidden queries, and never changes the ones you do have.

Each query has:
- **Order position**: its place in the tab bar. Leave it empty to hide the query; hidden queries are still reachable with `s` and `--query`.
- **Refresh every**: seconds between refreshes. Leave it empty to use the global interval, or set `0` to refresh only when you press `r`.

The names `Home`, `Active` and `Statements` are reserved for the built-in tabs.

## Built-in Tabs and Starter Queries

Built-in tabs: **Home**, **Active**, **Statements**.

Starter queries:
- Shown as tabs: **Connection States**, **Lock Information**, **Replication Lag**, **Vacuum Progress**
- Hidden (press `s`): **Autovacuum Stats**, **Configuration Settings**, **Index Creation**, **Table IO Stats**, **Table Replication State**, **Top Queries**

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
go test -race ./...                              # run tests
go build -ldflags "-X main.version=v1.0.0" -o psq  # build with version info
go fmt ./...
```

## Requirements

- Go 1.24+ (for building from source)
- PostgreSQL 10+ to monitor (the Statements tab also needs `pg_stat_statements`)
- `~/.pg_service.conf` configured

## Roadmap

- [ ] Parameterized queries (prompt for `$1`, `$2`, ...)
- [ ] Persist Home and Statements history across restarts
- [ ] Configurable Home dashboard widgets
- [ ] Color themes
- [ ] EXPLAIN plan view

## Contributing

Contributions welcome! Please open an issue or PR.

## License

MIT

## Author

Benjamin Sanborn ([@benjaminsanborn](https://github.com/benjaminsanborn))

---

**Like psq?** Star the repo ⭐ and share with other PostgreSQL users!
