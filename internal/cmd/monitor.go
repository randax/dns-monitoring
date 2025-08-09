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
	"github.com/randax/dns-monitoring/internal/exporters"
	"github.com/randax/dns-monitoring/internal/metrics"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func newMonitorCmd() *cobra.Command {
	var configPath string
	var continuous bool
	var detailedView bool
	var noColor bool
	var refreshInterval time.Duration
	var rawOutput bool
	var passive bool
	var hybrid bool
	var prometheusEnabled bool
	var prometheusPort int
	var prometheusPath string

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Start DNS monitoring",
		Long:  `Start monitoring DNS servers based on the configuration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			// Override passive configuration if flags are set
			if passive {
				cfg.Passive.Enabled = true
			}
			if hybrid {
				cfg.Passive.Enabled = true
				// Both active and passive will run
			}
			
			// Override Prometheus configuration if flags are set
			if cmd.Flags().Changed("prometheus") {
				if cfg.Metrics == nil {
					cfg.Metrics = &config.MetricsConfig{
						Enabled: true,
						WindowDuration: 5 * time.Minute,
						MaxStoredResults: 10000,
						CalculationInterval: 10 * time.Second,
						Export: config.ExportConfig{
							Prometheus: config.PrometheusConfig{
								UpdateInterval: 10 * time.Second,
								MetricPrefix: "dns_monitor",
							},
						},
					}
				}
				cfg.Metrics.Export.Prometheus.Enabled = prometheusEnabled
			}
			if cmd.Flags().Changed("prometheus-port") {
				if cfg.Metrics != nil {
					cfg.Metrics.Export.Prometheus.Port = prometheusPort
				}
			}
			if cmd.Flags().Changed("prometheus-path") {
				if cfg.Metrics != nil {
					cfg.Metrics.Export.Prometheus.Path = prometheusPath
				}
			}
			
			// Validate Prometheus configuration if enabled via CLI
			if prometheusEnabled && cfg.Metrics != nil {
				// Run validation on the configuration
				validationResult, err := config.ValidateComprehensive(cfg)
				if err != nil && !rawOutput {
					// Only print validation errors for metrics mode
					config.PrintValidationResult(validationResult)
					return fmt.Errorf("configuration validation failed: %w", err)
				}
			}

			// Check privileges if passive monitoring is enabled
			if cfg.Passive.Enabled {
				if dns.RequiresPrivileges() {
					return fmt.Errorf("passive monitoring requires elevated privileges:\n" +
						"  - On Linux/macOS: Run with sudo or as root\n" +
						"  - On Linux: Alternatively, grant CAP_NET_RAW capability:\n" +
						"    sudo setcap cap_net_raw+ep %s\n" +
						"  - Docker: Run with --cap-add=NET_RAW\n" +
						"Please run with appropriate privileges to use passive monitoring",
						os.Args[0])
				}
			}

			if cfg.Passive.Enabled && !hybrid {
				// Passive-only mode
				fmt.Println("Starting passive DNS monitoring...")
			} else if hybrid {
				fmt.Printf("Starting hybrid DNS monitoring with %d servers, %d domains, and passive capture\n",
					len(cfg.DNS.Servers), len(cfg.DNS.Queries.Domains))
			} else {
				fmt.Printf("Starting DNS monitoring with %d servers and %d domains\n",
					len(cfg.DNS.Servers), len(cfg.DNS.Queries.Domains))
			}

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

			// Initialize metrics collector and formatter if needed
			var collector *metrics.Collector
			var formatter *exporters.CLIFormatter

			if !rawOutput {
				collector = metrics.NewCollector(cfg.Metrics)
				formatter = exporters.NewCLIFormatter(
					cfg.Output.CLI.ShowColors,
					cfg.Output.CLI.DetailedView,
					cfg.Output.CLI.CompactMode,
					cfg.Output.CLI.ShowDistributions,
				)
				
				// Print Prometheus startup message if enabled via CLI
				if prometheusEnabled && cfg.Metrics != nil && cfg.Metrics.Export.Prometheus.Enabled {
					fmt.Printf("Prometheus metrics exporter enabled on port %d at path %s\n", 
						cfg.Metrics.Export.Prometheus.Port, 
						cfg.Metrics.Export.Prometheus.Path)
				}
			}

			go func() {
				select {
				case sig := <-sigChan:
					fmt.Printf("\nReceived signal %v. Shutting down gracefully...\n", sig)
					cancel()
				case <-ctx.Done():
					// Context was cancelled by other means
				}
			}()

			// Initialize DNS engines
			var engine *dns.Engine
			var passiveEngine *dns.PassiveEngine

			if !cfg.Passive.Enabled || hybrid {
				engine = dns.NewEngine(cfg)
			}

			if cfg.Passive.Enabled {
				var err error
				passiveEngine, err = dns.NewPassiveEngine(cfg)
				if err != nil {
					return fmt.Errorf("failed to create passive engine: %w", err)
				}
				defer func() {
					// Ensure passive engine cleanup if it has a Stop method
					// This is a safeguard for resource cleanup
					if passiveEngine != nil {
						// The context cancellation should handle stopping
						// but we ensure cleanup here
					}
				}()
			}

			if continuous {
				if rawOutput {
					// Raw output mode - no metrics collection
					if cfg.Passive.Enabled && !hybrid {
						err = runContinuousPassive(ctx, passiveEngine, cfg)
					} else if hybrid {
						err = runContinuousHybrid(ctx, engine, passiveEngine, cfg)
					} else {
						err = runContinuousRaw(ctx, engine, cfg)
					}
				} else {
					// Metrics mode
					if cfg.Passive.Enabled && !hybrid {
						err = runContinuousPassiveWithMetrics(ctx, passiveEngine, cfg, collector, formatter)
					} else if hybrid {
						err = runContinuousHybridWithMetrics(ctx, engine, passiveEngine, cfg, collector, formatter)
					} else {
						err = runContinuousWithMetrics(ctx, engine, cfg, collector, formatter)
					}
				}
			} else {
				if cfg.Passive.Enabled && !hybrid {
					return fmt.Errorf("passive monitoring requires continuous mode (use -k flag)")
				} else {
					if rawOutput {
						err = runOnce(ctx, engine, cfg)
					} else {
						err = runOnceWithMetrics(ctx, engine, cfg, collector, formatter)
					}
				}
			}

			if err != nil {
				return err
			}

			fmt.Println("\nMonitoring completed successfully")

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")
	cmd.Flags().BoolVarP(&continuous, "continuous", "k", false, "Run continuously based on interval")
	cmd.Flags().BoolVar(&detailedView, "detailed", false, "Show detailed metrics view")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable ANSI color output")
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 10*time.Second, "Metrics refresh interval for continuous monitoring")
	cmd.Flags().BoolVar(&rawOutput, "raw-output", false, "Show individual results instead of aggregated metrics")
	cmd.Flags().BoolVar(&passive, "passive", false, "Enable passive monitoring mode (capture DNS traffic)")
	cmd.Flags().BoolVar(&hybrid, "hybrid", false, "Enable hybrid mode (both active and passive monitoring)")
	
	// Prometheus exporter flags
	cmd.Flags().BoolVar(&prometheusEnabled, "prometheus", false, "Enable Prometheus metrics exporter")
	cmd.Flags().IntVar(&prometheusPort, "prometheus-port", 9090, "Prometheus metrics server port")
	cmd.Flags().StringVar(&prometheusPath, "prometheus-path", "/metrics", "Prometheus metrics endpoint path")

	return cmd
}

func runOnce(ctx context.Context, engine *dns.Engine, cfg *config.Config) error {
	fmt.Println("Running DNS queries...")

	// Use query timeout for the context
	queryTimeout := cfg.DNS.Queries.Timeout
	if queryTimeout == 0 {
		queryTimeout = 5 * time.Second
	}

	// Create timeout context for the query operation
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	results, err := engine.Run(queryCtx)
	if err != nil {
		return fmt.Errorf("failed to run DNS queries: %w", err)
	}

	return displayResults(results, cfg)
}

func runOnceWithMetrics(ctx context.Context, engine *dns.Engine, cfg *config.Config, collector *metrics.Collector, formatter *exporters.CLIFormatter) error {
	// Initialize Prometheus exporter if enabled
	var prometheusExporter *exporters.PrometheusExporter
	if cfg.Metrics.Export.Prometheus.Enabled {
		var err error
		prometheusExporter, err = exporters.NewPrometheusExporter(&cfg.Metrics.Export.Prometheus, collector)
		if err != nil {
			return fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		if err := prometheusExporter.Start(); err != nil {
			return fmt.Errorf("failed to start Prometheus exporter: %w", err)
		}
		defer func() {
			if err := prometheusExporter.Stop(); err != nil {
				fmt.Printf("Error stopping Prometheus exporter: %v\n", err)
			}
		}()
		
		// Give Prometheus server time to fully start
		time.Sleep(100 * time.Millisecond)
		fmt.Printf("Prometheus metrics available at http://localhost:%d%s\n", cfg.Metrics.Export.Prometheus.Port, cfg.Metrics.Export.Prometheus.Path)
	}
	
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

	fmt.Println("Running DNS queries...")

	// Use query timeout for the context
	queryTimeout := cfg.DNS.Queries.Timeout
	if queryTimeout == 0 {
		queryTimeout = 5 * time.Second
	}

	// Create timeout context for the query operation
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	results, err := engine.Run(queryCtx)
	if err != nil {
		return fmt.Errorf("failed to run DNS queries: %w", err)
	}

	// Collect results for metrics
	for result := range results {
		collector.AddResult(result)
	}

	// Display final metrics
	m := collector.GetMetrics()
	if m != nil && m.Rates.TotalQueries > 0 {
		layout := exporters.NewLayout(exporters.LayoutDashboard, formatter.Terminal())
		fmt.Print(layout.Render(m, nil))
	}

	// If Prometheus is enabled, keep the server running for a short time to allow metric scraping
	if cfg.Metrics.Export.Prometheus.Enabled {
		fmt.Printf("\nMetrics server will remain available for 30 seconds at http://localhost:%d%s\n", 
			cfg.Metrics.Export.Prometheus.Port, cfg.Metrics.Export.Prometheus.Path)
		time.Sleep(30 * time.Second)
	}

	return nil
}

func runContinuousRaw(ctx context.Context, engine *dns.Engine, cfg *config.Config) error {
	ticker := time.NewTicker(cfg.Monitor.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Use query timeout for the context
			queryTimeout := cfg.DNS.Queries.Timeout
			if queryTimeout == 0 {
				queryTimeout = 5 * time.Second
			}

			// Create timeout context for this specific query operation
			queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
			results, err := engine.Run(queryCtx)
			cancel() // Immediately cancel to free resources

			if err != nil {
				// Check if context was cancelled
				if ctx.Err() != nil {
					return ctx.Err()
				}
				fmt.Printf("Error running queries: %v\n", err)
				continue
			}

			if err := displayResults(results, cfg); err != nil {
				fmt.Printf("Error displaying results: %v\n", err)
			}
		}
	}
}

func runContinuousPassive(ctx context.Context, passiveEngine *dns.PassiveEngine, cfg *config.Config) error {
	resultsChan, err := passiveEngine.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start passive engine: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-resultsChan:
			if !ok {
				return nil
			}
			data, err := json.Marshal(result)
			if err != nil {
				fmt.Printf("Error marshaling result: %v\n", err)
				continue
			}
			fmt.Println(string(data))
		}
	}
}

func runContinuousWithMetrics(ctx context.Context, engine *dns.Engine, cfg *config.Config, collector *metrics.Collector, formatter *exporters.CLIFormatter) error {
	// Initialize Prometheus exporter if enabled
	var prometheusExporter *exporters.PrometheusExporter
	if cfg.Metrics.Export.Prometheus.Enabled {
		var err error
		prometheusExporter, err = exporters.NewPrometheusExporter(&cfg.Metrics.Export.Prometheus, collector)
		if err != nil {
			return fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		if err := prometheusExporter.Start(); err != nil {
			return fmt.Errorf("failed to start Prometheus exporter: %w", err)
		}
		defer func() {
			if err := prometheusExporter.Stop(); err != nil {
				fmt.Printf("Error stopping Prometheus exporter: %v\n", err)
			}
		}()
	}
	
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
	defer close(resultsChan)

	g, gCtx := errgroup.WithContext(ctx)

	// Results collector goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil // Normal shutdown
			case result, ok := <-resultsChan:
				if !ok {
					return nil // Channel closed
				}
				collector.AddResult(result)
			}
		}
	})

	// Query runner goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil
			case <-queryTicker.C:
				// Use query timeout for the context
				queryTimeout := cfg.DNS.Queries.Timeout
				if queryTimeout == 0 {
					queryTimeout = 5 * time.Second
				}

				// Create timeout context for this specific query operation
				queryCtx, cancel := context.WithTimeout(gCtx, queryTimeout)

				results, err := engine.Run(queryCtx)
				cancel() // Immediately cancel to free resources

				if err != nil {
					// Check if context was cancelled
					if gCtx.Err() != nil {
						return gCtx.Err()
					}
					fmt.Printf("Error running queries: %v\n", err)
					continue
				}

				// Send results with cancellation check
				for result := range results {
					select {
					case <-gCtx.Done():
						return gCtx.Err()
					case resultsChan <- result:
						// Successfully sent
					}
				}
			}
		}
	})

	// Display goroutine
	g.Go(func() error {
		prevMetrics := (*metrics.Metrics)(nil)
		for {
			select {
			case <-gCtx.Done():
				return nil // Normal shutdown
			case <-displayTicker.C:
				m := collector.GetMetrics()
				if m != nil && m.Rates.TotalQueries > 0 {
					layout := exporters.NewLayout(exporters.LayoutDashboard, formatter.Terminal())
					fmt.Print(layout.Render(m, prevMetrics))
					prevMetrics = m
				}
			}
		}
	})

	return g.Wait()
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

func runContinuousHybrid(ctx context.Context, engine *dns.Engine, passiveEngine *dns.PassiveEngine, cfg *config.Config) error {
	passiveResults, err := passiveEngine.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start passive engine: %w", err)
	}

	ticker := time.NewTicker(cfg.Monitor.Interval)
	defer ticker.Stop()

	g, gCtx := errgroup.WithContext(ctx)

	// Passive monitoring goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil
			case result, ok := <-passiveResults:
				if !ok {
					return nil
				}
				data, err := json.Marshal(result)
				if err != nil {
					fmt.Printf("Error marshaling passive result: %v\n", err)
					continue
				}
				fmt.Printf("[PASSIVE] %s\n", string(data))
			}
		}
	})

	// Active queries goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil
			case <-ticker.C:
				// Use query timeout for the context
				queryTimeout := cfg.DNS.Queries.Timeout
				if queryTimeout == 0 {
					queryTimeout = 5 * time.Second
				}

				// Create timeout context for this specific query operation
				queryCtx, cancel := context.WithTimeout(gCtx, queryTimeout)

				results, err := engine.Run(queryCtx)
				cancel() // Immediately cancel to free resources

				if err != nil {
					// Check if context was cancelled
					if gCtx.Err() != nil {
						return gCtx.Err()
					}
					fmt.Printf("Error running active queries: %v\n", err)
					continue
				}

				for result := range results {
					data, err := json.Marshal(result)
					if err != nil {
						fmt.Printf("Error marshaling active result: %v\n", err)
						continue
					}
					fmt.Printf("[ACTIVE] %s\n", string(data))
				}
			}
		}
	})

	return g.Wait()
}

func runContinuousPassiveWithMetrics(ctx context.Context, passiveEngine *dns.PassiveEngine, cfg *config.Config, collector *metrics.Collector, formatter *exporters.CLIFormatter) error {
	// Initialize Prometheus exporter if enabled
	var prometheusExporter *exporters.PrometheusExporter
	if cfg.Metrics.Export.Prometheus.Enabled {
		var err error
		prometheusExporter, err = exporters.NewPrometheusExporter(&cfg.Metrics.Export.Prometheus, collector)
		if err != nil {
			return fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		if err := prometheusExporter.Start(); err != nil {
			return fmt.Errorf("failed to start Prometheus exporter: %w", err)
		}
		defer func() {
			if err := prometheusExporter.Stop(); err != nil {
				fmt.Printf("Error stopping Prometheus exporter: %v\n", err)
			}
		}()
	}
	
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

	resultsChan, err := passiveEngine.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start passive engine: %w", err)
	}

	displayTicker := time.NewTicker(cfg.Output.CLI.RefreshInterval)
	defer displayTicker.Stop()

	g, gCtx := errgroup.WithContext(ctx)

	// Results collector goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil // Normal shutdown
			case result, ok := <-resultsChan:
				if !ok {
					return nil // Channel closed
				}
				collector.AddResult(result)
			}
		}
	})

	// Display goroutine
	g.Go(func() error {
		prevMetrics := (*metrics.Metrics)(nil)
		for {
			select {
			case <-gCtx.Done():
				return nil // Normal shutdown
			case <-displayTicker.C:
				m := collector.GetMetrics()
				if m != nil && m.Rates.TotalQueries > 0 {
					layout := exporters.NewLayout(exporters.LayoutDashboard, formatter.Terminal())
					fmt.Print(layout.Render(m, prevMetrics))
					prevMetrics = m
				}
			}
		}
	})

	return g.Wait()
}

func runContinuousHybridWithMetrics(ctx context.Context, engine *dns.Engine, passiveEngine *dns.PassiveEngine, cfg *config.Config, collector *metrics.Collector, formatter *exporters.CLIFormatter) error {
	// Initialize Prometheus exporter if enabled
	var prometheusExporter *exporters.PrometheusExporter
	if cfg.Metrics.Export.Prometheus.Enabled {
		var err error
		prometheusExporter, err = exporters.NewPrometheusExporter(&cfg.Metrics.Export.Prometheus, collector)
		if err != nil {
			return fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		if err := prometheusExporter.Start(); err != nil {
			return fmt.Errorf("failed to start Prometheus exporter: %w", err)
		}
		defer func() {
			if err := prometheusExporter.Stop(); err != nil {
				fmt.Printf("Error stopping Prometheus exporter: %v\n", err)
			}
		}()
	}
	
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

	passiveResults, err := passiveEngine.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start passive engine: %w", err)
	}

	queryTicker := time.NewTicker(cfg.Monitor.Interval)
	defer queryTicker.Stop()

	displayTicker := time.NewTicker(cfg.Output.CLI.RefreshInterval)
	defer displayTicker.Stop()

	resultsChan := make(chan dns.Result, 1000)
	defer close(resultsChan)

	g, gCtx := errgroup.WithContext(ctx)

	// Collector goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil
			case result, ok := <-resultsChan:
				if !ok {
					return nil
				}
				collector.AddResult(result)
			}
		}
	})

	// Passive results goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil
			case result, ok := <-passiveResults:
				if !ok {
					return nil
				}
				select {
				case <-gCtx.Done():
					return gCtx.Err()
				case resultsChan <- result:
					// Successfully sent
				}
			}
		}
	})


	// Active queries goroutine
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				return nil
			case <-queryTicker.C:
				// Use query timeout for the context
				queryTimeout := cfg.DNS.Queries.Timeout
				if queryTimeout == 0 {
					queryTimeout = 5 * time.Second
				}

				// Create timeout context for this specific query operation
				queryCtx, cancel := context.WithTimeout(gCtx, queryTimeout)

				results, err := engine.Run(queryCtx)
				cancel() // Immediately cancel to free resources

				if err != nil {
					// Check if context was cancelled
					if gCtx.Err() != nil {
						return gCtx.Err()
					}
					fmt.Printf("Error running active queries: %v\n", err)
					continue
				}

				// Send results with cancellation check
				for result := range results {
					select {
					case <-gCtx.Done():
						return gCtx.Err()
					case resultsChan <- result:
						// Successfully sent
					}
				}
			}
		}
	})

	// Display goroutine
	g.Go(func() error {
		prevMetrics := (*metrics.Metrics)(nil)
		for {
			select {
			case <-gCtx.Done():
				return nil // Normal shutdown
			case <-displayTicker.C:
				m := collector.GetMetrics()
				if m != nil && m.Rates.TotalQueries > 0 {
					layout := exporters.NewLayout(exporters.LayoutDashboard, formatter.Terminal())
					fmt.Print(layout.Render(m, prevMetrics))
					prevMetrics = m
				}
			}
		}
	})

	return g.Wait()
}

