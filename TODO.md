# TODO - psq improvements

## UX / UI
- [ ] Auto-refresh toggle — let users enable/disable the 1-second tick per query (some queries are expensive)
- [ ] Configurable refresh interval (1s is aggressive for remote/production databases)
- [ ] Status bar showing connection info, last refresh time, and row count
- [ ] Color themes / dark-mode variants (currently hardcoded lipgloss colors)
- [ ] Resize the SQL editor textarea dynamically to terminal height
- [ ] Keyboard shortcut cheat sheet overlay (beyond the current `?` help — something always-visible in the footer)
- [ ] Tab reordering via drag or keyboard shortcut

## Queries & Data
- [ ] Query history — keep a log of recently executed ad-hoc queries for easy re-run
- [ ] Export results to CSV/JSON (pipe-friendly for scripting)
- [ ] Parameterized queries — allow `$1`, `$2` placeholders and prompt for values at runtime
- [ ] Saved query folders/categories for organizing large query libraries
- [ ] Default query pack updates — ship new useful queries with releases, offer to merge into user's `queries.db`

## Home Dashboard
- [ ] Add more widgets: cache hit ratio, table bloat, replication lag, longest running query
- [ ] Configurable dashboard layout (pick which charts appear)
- [ ] Historical sparkline persistence — survive across restarts (write to a small local file)

## Connections
- [ ] Support `DATABASE_URL` / `PGCONNSTRING` in addition to `~/.pg_service.conf`
- [ ] Connection health indicator (latency ping in the service picker)
- [ ] SSL mode selector (currently hardcoded `sslmode=require`)
- [ ] Connection pooling / reuse — currently opens a new connection per query execution
- [ ] Multiple simultaneous connections (compare across envs side-by-side)

## AI / ChatGPT
- [ ] Let users pick the model (currently hardcoded `gpt-4o-mini`)
- [ ] Stream the AI response so users see progress
- [ ] Include database schema context in the prompt for smarter generation
- [ ] Support alternative LLM providers (Anthropic, local Ollama, etc.)

## Developer / Project
- [ ] Add tests — at minimum for `querydb.go`, config parsing, and query filtering
- [ ] CI: add `go vet` / `staticcheck` / `golangci-lint` to the release workflow
- [ ] Publish a Homebrew bottle for faster installs
- [ ] Man page / `--help` improvements with examples
- [ ] Config file (`~/.psq/config.toml`) for preferences (theme, refresh rate, default service, AI model)
