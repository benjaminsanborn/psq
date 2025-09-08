package main

import (
	"fmt"
	"os"

	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"
)

func main() {
	// Initialize bubblezone global manager
	zone.NewGlobal()
	defer zone.Close()

	var service string

	var rootCmd = &cobra.Command{
		Use:   "psq [service]",
		Short: "PostgreSQL monitoring CLI tool",
		Long:  `A TUI-based PostgreSQL monitoring tool that reads connection from ~/.pg_service.conf`,
		Args:  cobra.MaximumNArgs(1),
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
