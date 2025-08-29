package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	var service string

	var rootCmd = &cobra.Command{
		Use:   "pgi [service]",
		Short: "PostgreSQL monitoring CLI tool",
		Long:  `A TUI-based PostgreSQL monitoring tool that reads connection from ~/.pg_service.conf`,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Use provided service name or default to "default"
			if len(args) > 0 {
				service = args[0]
			} else if service == "" {
				service = "default"
			}

			app := NewApp(service)
			if err := app.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	rootCmd.Flags().StringVarP(&service, "service", "s", "", "Database service name from ~/.pg_service.conf (default: 'default')")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
