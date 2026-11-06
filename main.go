package main

import (
	"fmt"
	"os"
	"time"

	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"
)

// Set via ldflags at build time
var version = "dev"

func main() {
	// Initialize bubblezone global manager
	zone.NewGlobal()
	defer zone.Close()

	var (
		service   string
		queryName string
		rawSQL    string
		format    string
		listOnly  bool
		importDef bool
	)

	var rootCmd = &cobra.Command{
		Use:   "psq [service]",
		Short: "PostgreSQL monitoring CLI tool",
		Long: `psq - PostgreSQL monitoring in the CLI

A TUI-based PostgreSQL monitoring tool that reads database connections from ~/.pg_service.conf

Examples:
  psq                                   # Show service picker
  psq prod                              # Connect directly to 'prod' service
  psq -s staging                        # Connect to 'staging' service
  psq prod --query Locks                # Run a saved query once and print it
  psq prod -q Active -f json            # Sessions and blocking info as JSON
  psq prod --sql "SELECT now()" -f csv  # Run ad-hoc SQL (read-only) as CSV
  psq --list-queries                    # List saved queries

Configuration:
  Queries:       ~/.psq/queries.db (SQLite, auto-created)
  Connections:   ~/.pg_service.conf (PostgreSQL service file)`,
		Version: version,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				service = args[0]
			}
			if settings.RefreshInterval < 500*time.Millisecond {
				return fmt.Errorf("--refresh must be at least 500ms")
			}
			if settings.StatementTimeout < 0 {
				return fmt.Errorf("--statement-timeout can't be negative")
			}

			if listOnly {
				return listQueries(os.Stdout)
			}
			if importDef {
				return importDefaults(os.Stdout)
			}

			if queryName != "" || rawSQL != "" {
				if queryName != "" && rawSQL != "" {
					return fmt.Errorf("use either --query or --sql, not both")
				}
				if service == "" {
					return fmt.Errorf("--query and --sql need a service, e.g. psq prod --query Locks")
				}
				return runOnce(os.Stdout, service, queryName, rawSQL, format)
			}

			if service != "" {
				return NewApp(service).Run()
			}

			// Show service picker in a loop to allow returning
			for {
				picker := NewServicePicker()
				selectedService, err := picker.Run()
				if err != nil {
					return err
				}
				if selectedService == "" {
					return nil // user quit from picker
				}
				if err := NewApp(selectedService).Run(); err != nil {
					return err
				}
				// App exited normally, return to picker
			}
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	flags := rootCmd.Flags()
	flags.StringVarP(&service, "service", "s", "", "service name from ~/.pg_service.conf")
	flags.StringVarP(&queryName, "query", "q", "", "run a saved query once, print it and exit")
	flags.StringVar(&rawSQL, "sql", "", "run SQL once (in a read-only transaction), print it and exit")
	flags.StringVarP(&format, "format", "f", formatTable, "output format for --query/--sql: table, csv or json")
	flags.BoolVar(&listOnly, "list-queries", false, "list saved queries and exit")
	flags.BoolVar(&importDef, "import-defaults", false, "add any starter queries you don't have yet (as hidden queries) and exit")
	flags.DurationVar(&settings.RefreshInterval, "refresh", settings.RefreshInterval, "default auto-refresh interval (change it live with +/-)")
	flags.DurationVar(&settings.StatementTimeout, "statement-timeout", settings.StatementTimeout, "cancel any statement psq runs after this long (0 to disable)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
