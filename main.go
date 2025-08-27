package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "pgmon",
		Short: "PostgreSQL monitoring CLI tool",
		Long:  `A TUI-based PostgreSQL monitoring tool that reads connection from ~/.pg_service.conf`,
	}

	var service string
	var monitorCmd = &cobra.Command{
		Use:   "monitor",
		Short: "Start the interactive monitoring interface",
		Run: func(cmd *cobra.Command, args []string) {
			app := NewApp(service)
			if err := app.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	monitorCmd.Flags().StringVarP(&service, "service", "s", "default", "Database service name from ~/.pg_service.conf")

	rootCmd.AddCommand(monitorCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
