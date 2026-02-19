package main

import (
	"fmt"
	"os"

	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"
)

// Set via ldflags at build time
var version = "dev"

func main() {
	// Initialize bubblezone global manager
	zone.NewGlobal()
	defer zone.Close()

	var service string

	var rootCmd = &cobra.Command{
		Use:   "psq [service]",
		Short: "PostgreSQL monitoring CLI tool",
		Long: `psq - PostgreSQL monitoring in the CLI

A TUI-based PostgreSQL monitoring tool that reads database connections from ~/.pg_service.conf

Features:
  • Interactive service picker with fuzzy search
  • Pre-configured monitoring queries (connections, locks, queries, replication)
  • Custom query editor with AI-powered query generation
  • Real-time active connection viewer with terminate/cancel capabilities
  • Query search across all saved queries
  • Mouse support for tab navigation

Examples:
  psq                    # Show service picker
  psq prod               # Connect directly to 'prod' service
  psq -s staging         # Connect to 'staging' service

Keyboard Shortcuts:
  Navigation:    ←/→ (h/l) switch tabs, ↑/↓ (k/j) scroll, Home/End jump
  Queries:       Enter/Space/R refresh, S search, E edit, N new query
  Active View:   Enter details, T terminate, C cancel, Y copy query
  Other:         ? help, X psql prompt, C service picker, Esc/Ctrl+C quit

Configuration:
  Queries:       ~/.psq/queries.db (SQLite, auto-created)
  Connections:   ~/.pg_service.conf (PostgreSQL service file)
  AI Features:   $OPENAI_API_KEY (optional, for query generation)`,
		Version: version,
		Args:    cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Use provided service name or show picker if none provided
			if len(args) > 0 {
				service = args[0]
				app := NewApp(service)
				if err := app.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			} else if service != "" {
				app := NewApp(service)
				if err := app.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			} else {
				// Show service picker in a loop to allow returning
				for {
					picker := NewServicePicker()
					selectedService, err := picker.Run()
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						os.Exit(1)
					}
					if selectedService == "" {
						// User quit from picker
						break
					}

					app := NewApp(selectedService)
					if err := app.Run(); err != nil {
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						os.Exit(1)
					}
					// App exited normally, return to picker
				}
			}
		},
	}

	rootCmd.Flags().StringVarP(&service, "service", "s", "", "Database service name from ~/.pg_service.conf (default: 'default')")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
