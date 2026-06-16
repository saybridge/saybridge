package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version info (set at build time via ldflags)
var (
	version = "0.1.0"
	commit  = "dev"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "chat-cli",
		Short: "Saybridge Plugin Developer CLI",
		Long: `chat-cli is the developer toolchain for building, testing, and publishing
Saybridge WASM plugins. It provides scaffolding, hot-reload development,
testing, simulation, and packaging capabilities.`,
		Version: fmt.Sprintf("%s (%s)", version, commit),
	}

	// Register subcommands
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(devCmd())
	rootCmd.AddCommand(testCmd())
	rootCmd.AddCommand(simulateCmd())
	rootCmd.AddCommand(publishCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
