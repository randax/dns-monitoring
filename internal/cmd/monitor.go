package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/dns"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	var configPath string
	var continuous bool
	
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Start DNS monitoring",
		Long:  `Start monitoring DNS servers based on the configuration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}
			
			fmt.Printf("Starting DNS monitoring with %d servers and %d domains\n", 
				len(cfg.DNS.Servers), len(cfg.DNS.Queries.Domains))
			
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigChan
				fmt.Println("\nShutting down gracefully...")
				cancel()
			}()
			
			engine := dns.NewEngine(cfg)
			
			if continuous {
				return runContinuous(ctx, engine, cfg)
			}
			
			return runOnce(ctx, engine, cfg)
		},
	}
	
	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")
	cmd.Flags().BoolVarP(&continuous, "continuous", "k", false, "Run continuously based on interval")
	
	return cmd
}

func runOnce(ctx context.Context, engine *dns.Engine, cfg *config.Config) error {
	results, err := engine.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to run DNS queries: %w", err)
	}
	
	return displayResults(results, cfg)
}

func runContinuous(ctx context.Context, engine *dns.Engine, cfg *config.Config) error {
	ticker := time.NewTicker(cfg.Monitor.Interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			fmt.Printf("\n[%s] Running DNS queries...\n", time.Now().Format(time.RFC3339))
			
			queryCtx, cancel := context.WithTimeout(ctx, cfg.Monitor.Interval)
			results, err := engine.Run(queryCtx)
			cancel()
			
			if err != nil {
				fmt.Printf("Error running queries: %v\n", err)
				continue
			}
			
			if err := displayResults(results, cfg); err != nil {
				fmt.Printf("Error displaying results: %v\n", err)
			}
		}
	}
}

func displayResults(results <-chan dns.Result, cfg *config.Config) error {
	var successCount, errorCount int
	var totalLatency time.Duration
	
	for result := range results {
		if cfg.Output.Console {
			switch cfg.Output.Format {
			case "json":
				data, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal result: %w", err)
				}
				fmt.Println(string(data))
			case "text":
				fmt.Printf("[%s] %s @ %s (%s): ", 
					result.Timestamp.Format("15:04:05"),
					result.Domain,
					result.Server,
					result.QueryType)
				
				if result.Error != nil {
					fmt.Printf("ERROR - %v (retries: %d)\n", result.Error, result.Retries)
					errorCount++
				} else {
					fmt.Printf("OK - %v", result.Duration)
					if len(result.Answers) > 0 {
						fmt.Printf(" [%s]", result.Answers[0])
						if len(result.Answers) > 1 {
							fmt.Printf(" +%d more", len(result.Answers)-1)
						}
					}
					fmt.Printf(" (retries: %d)\n", result.Retries)
					successCount++
					totalLatency += result.Duration
				}
			case "csv":
				if result.Error != nil {
					fmt.Printf("%s,%s,%s,%s,%v,ERROR,%s,%d\n",
						result.Timestamp.Format(time.RFC3339),
						result.Server,
						result.Domain,
						result.QueryType,
						result.Duration,
						result.Error.Error(),
						result.Retries)
				} else {
					answers := ""
					if len(result.Answers) > 0 {
						answers = result.Answers[0]
					}
					fmt.Printf("%s,%s,%s,%s,%v,OK,%s,%d\n",
						result.Timestamp.Format(time.RFC3339),
						result.Server,
						result.Domain,
						result.QueryType,
						result.Duration,
						answers,
						result.Retries)
				}
			}
		}
		
		if result.Error == nil {
			totalLatency += result.Duration
		}
	}
	
	if cfg.Output.Format == "text" && cfg.Output.Console {
		fmt.Printf("\nSummary: %d successful, %d errors", successCount, errorCount)
		if successCount > 0 {
			avgLatency := totalLatency / time.Duration(successCount)
			fmt.Printf(", avg latency: %v", avgLatency)
		}
		fmt.Println()
	}
	
	return nil
}