package cmd

import (
	"errors"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/randax/dns-monitoring/internal/config"
)

var (
	cfgFile string
	verbose bool
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "dns-monitoring",
	Short: "A DNS monitoring tool",
	Long: `DNS Monitoring is a CLI tool for monitoring DNS servers and queries.
	
This tool allows you to monitor DNS server health, query response times,
and track DNS resolution patterns.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			if errors.Is(err, config.ErrFileNotFound) {
				if cfgFile != "" {
					return fmt.Errorf("specified config file not found: %w", err)
				}
				if verbose {
					log.Printf("Warning: Default config file not found, using built-in defaults")
				}
				return nil
			}
			if errors.Is(err, config.ErrParseFailed) {
				return fmt.Errorf("config file has syntax errors: %w", err)
			}
			if errors.Is(err, config.ErrInvalidConfig) {
				return fmt.Errorf("config validation failed: %w", err)
			}
			return fmt.Errorf("failed to load config: %w", err)
		}
		if verbose && cfgFile == "" {
			log.Printf("Loaded configuration from default location: config.yaml")
		} else if verbose {
			log.Printf("Loaded configuration from: %s", cfgFile)
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	rootCmd.AddCommand(newMonitorCmd())
	rootCmd.AddCommand(newVersionCmd())
}