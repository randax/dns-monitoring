package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/dns"
	"github.com/randax/dns-monitoring/internal/exporters"
	"github.com/randax/dns-monitoring/internal/metrics"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	var configPath string
	var continuous bool
	var detailedView bool
	var noColor bool
	var refreshInterval time.Duration
	var rawOutput bool
	
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
			
			if cmd.Flags().Changed("detailed") {
				cfg.Output.CLI.DetailedView = detailedView
			}
			if cmd.Flags().Changed("no-color") {
				cfg.Output.CLI.ShowColors = !noColor
			}
			if cmd.Flags().Changed("refresh-interval") {
				cfg.Output.CLI.RefreshInterval = refreshInterval
			}
			
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			
			var finalMetrics *metrics.Metrics
			var metricsCollector *metrics.Collector
			var cliFormatter *exporters.CLIFormatter
			
			if !rawOutput {
				metricsCollector = metrics.NewCollector(cfg.Metrics)
				cliFormatter = exporters.NewCLIFormatter(
					cfg.Output.CLI.ShowColors,
					cfg.Output.CLI.DetailedView,
					cfg.Output.CLI.CompactMode,
					cfg.Output.CLI.ShowDistributions,
				)
			}
			
			go func() {
				<-sigChan
				fmt.Println("\nShutting down gracefully...")
				if metricsCollector != nil {
					finalMetrics = metricsCollector.GetMetrics()
				}
				cancel()
			}()
			
			engine := dns.NewEngine(cfg)
			
			if continuous {
				if rawOutput {
					err = runContinuous(ctx, engine, cfg)
				} else {
					err = runContinuousWithMetrics(ctx, engine, cfg, metricsCollector, cliFormatter)
				}
			} else {
				if rawOutput {
					err = runOnce(ctx, engine, cfg)
				} else {
					err = runOnceWithMetrics(ctx, engine, cfg, metricsCollector, cliFormatter)
				}
			}
			
			if err != nil {
				return err
			}
			
			if finalMetrics == nil && metricsCollector != nil {
				finalMetrics = metricsCollector.GetMetrics()
			}
			
			if finalMetrics != nil && cliFormatter != nil {
				fmt.Println(cliFormatter.FormatSummary(finalMetrics))
			}
			
			return nil
		},
	}
	
	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")
	cmd.Flags().BoolVarP(&continuous, "continuous", "k", false, "Run continuously based on interval")
	cmd.Flags().BoolVar(&detailedView, "detailed", false, "Show detailed metrics view")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable ANSI color output")
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 10*time.Second, "Metrics refresh interval for continuous monitoring")
	cmd.Flags().BoolVar(&rawOutput, "raw-output", false, "Show individual results instead of aggregated metrics")
	
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

func runOnceWithMetrics(ctx context.Context, engine *dns.Engine, cfg *config.Config, collector *metrics.Collector, formatter *exporters.CLIFormatter) error {
	// Initialize Zabbix exporter if enabled
	var zabbixExporter *exporters.ZabbixExporter
	if cfg.Metrics.Export.Zabbix.Enabled {
		var err error
		zabbixExporter, err = exporters.NewZabbixExporter(cfg.Metrics.Export.Zabbix, collector)
		if err != nil {
			return fmt.Errorf("failed to create Zabbix exporter: %w", err)
		}
		if err := zabbixExporter.Start(); err != nil {
			return fmt.Errorf("failed to start Zabbix exporter: %w", err)
		}
		defer func() {
			if err := zabbixExporter.Stop(); err != nil {
				fmt.Printf("Error stopping Zabbix exporter: %v\n", err)
			}
		}()
	}
	
	results, err := engine.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to run DNS queries: %w", err)
	}
	
	for result := range results {
		collector.AddResult(result)
	}
	
	m := collector.GetMetrics()
	fmt.Println(formatter.FormatMetrics(m))
	
	return nil
}

func runContinuousWithMetrics(ctx context.Context, engine *dns.Engine, cfg *config.Config, collector *metrics.Collector, formatter *exporters.CLIFormatter) error {
	// Initialize Zabbix exporter if enabled
	var zabbixExporter *exporters.ZabbixExporter
	if cfg.Metrics.Export.Zabbix.Enabled {
		var err error
		zabbixExporter, err = exporters.NewZabbixExporter(cfg.Metrics.Export.Zabbix, collector)
		if err != nil {
			return fmt.Errorf("failed to create Zabbix exporter: %w", err)
		}
		if err := zabbixExporter.Start(); err != nil {
			return fmt.Errorf("failed to start Zabbix exporter: %w", err)
		}
		defer func() {
			if err := zabbixExporter.Stop(); err != nil {
				fmt.Printf("Error stopping Zabbix exporter: %v\n", err)
			}
		}()
	}
	
	queryTicker := time.NewTicker(cfg.Monitor.Interval)
	defer queryTicker.Stop()
	
	displayTicker := time.NewTicker(cfg.Output.CLI.RefreshInterval)
	defer displayTicker.Stop()
	
	resultsChan := make(chan dns.Result, 1000)
	var wg sync.WaitGroup
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		for result := range resultsChan {
			collector.AddResult(result)
		}
	}()
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-queryTicker.C:
				queryCtx, cancel := context.WithTimeout(ctx, cfg.Monitor.Interval)
				results, err := engine.Run(queryCtx)
				cancel()
				
				if err != nil {
					fmt.Printf("Error running queries: %v\n", err)
					continue
				}
				
				for result := range results {
					select {
					case resultsChan <- result:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	
	prevMetrics := (*metrics.Metrics)(nil)
	for {
		select {
		case <-ctx.Done():
			close(resultsChan)
			wg.Wait()
			return nil
		case <-displayTicker.C:
			m := collector.GetMetrics()
			if m != nil && m.Rates.TotalQueries > 0 {
				layout := exporters.NewLayout(exporters.LayoutDashboard, formatter.Terminal())
				fmt.Print(layout.Render(m, prevMetrics))
				prevMetrics = m
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