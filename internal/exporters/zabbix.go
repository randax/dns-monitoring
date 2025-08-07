package exporters

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/randax/dns-monitoring/internal/config"
	"github.com/randax/dns-monitoring/internal/metrics"
)

type ZabbixExporter struct {
	config    config.ZabbixConfig
	collector *metrics.Collector
	sender    *ZabbixSender
	batcher   *ZabbixBatcher
	
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	
	running    bool
	lastSend   time.Time
	sendErrors uint64
	sendCount  uint64
}

func NewZabbixExporter(cfg config.ZabbixConfig, collector *metrics.Collector) (*ZabbixExporter, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("Zabbix exporter is not enabled")
	}
	
	if err := validateZabbixConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid Zabbix configuration: %w", err)
	}
	
	sender := NewZabbixSender(cfg.Server, cfg.Port, cfg.Timeout)
	batcher := NewZabbixBatcher(sender, cfg.BatchSize, cfg.RetryAttempts, cfg.RetryDelay)
	
	return &ZabbixExporter{
		config:    cfg,
		collector: collector,
		sender:    sender,
		batcher:   batcher,
	}, nil
}

func (z *ZabbixExporter) Start() error {
	z.mu.Lock()
	defer z.mu.Unlock()
	
	if z.running {
		return fmt.Errorf("Zabbix exporter is already running")
	}
	
	z.ctx, z.cancel = context.WithCancel(context.Background())
	z.running = true
	
	z.wg.Add(1)
	go z.sendLoop()
	
	log.Printf("Zabbix exporter started (server: %s:%d, interval: %s)",
		z.config.Server, z.config.Port, z.config.SendInterval)
	
	return nil
}

func (z *ZabbixExporter) Stop() error {
	z.mu.Lock()
	defer z.mu.Unlock()
	
	if !z.running {
		return fmt.Errorf("Zabbix exporter is not running")
	}
	
	z.cancel()
	z.wg.Wait()
	
	// Flush any remaining items
	if err := z.batcher.Flush(); err != nil {
		log.Printf("Error flushing Zabbix batcher on stop: %v", err)
	}
	
	z.sender.Close()
	z.running = false
	
	log.Printf("Zabbix exporter stopped (sent: %d, errors: %d)", z.sendCount, z.sendErrors)
	
	return nil
}

func (z *ZabbixExporter) sendLoop() {
	defer z.wg.Done()
	
	ticker := time.NewTicker(z.config.SendInterval)
	defer ticker.Stop()
	
	// Send initial metrics
	if err := z.SendMetrics(); err != nil {
		log.Printf("Error sending initial Zabbix metrics: %v", err)
		z.sendErrors++
	}
	
	for {
		select {
		case <-z.ctx.Done():
			return
		case <-ticker.C:
			if err := z.SendMetrics(); err != nil {
				log.Printf("Error sending Zabbix metrics: %v", err)
				z.sendErrors++
			} else {
				z.sendCount++
			}
		}
	}
}

func (z *ZabbixExporter) SendMetrics() error {
	z.mu.RLock()
	defer z.mu.RUnlock()
	
	if !z.running {
		return fmt.Errorf("Zabbix exporter is not running")
	}
	
	metrics := z.collector.GetMetrics()
	if metrics == nil {
		return fmt.Errorf("no metrics available")
	}
	
	items, err := z.mapMetricsToItems(metrics)
	if err != nil {
		return fmt.Errorf("failed to map metrics to Zabbix items: %w", err)
	}
	
	for _, item := range items {
		if err := z.batcher.AddItem(item); err != nil {
			return fmt.Errorf("failed to add item to batcher: %w", err)
		}
	}
	
	// Flush the batch
	if err := z.batcher.Flush(); err != nil {
		return fmt.Errorf("failed to flush batch: %w", err)
	}
	
	z.lastSend = time.Now()
	return nil
}

func (z *ZabbixExporter) mapMetricsToItems(m *metrics.Metrics) ([]ZabbixItem, error) {
	var items []ZabbixItem
	timestamp := time.Now().Unix()
	
	// Map latency metrics
	latencyItems := mapLatencyMetrics(m, z.config.HostName, z.config.ItemPrefix, timestamp)
	items = append(items, latencyItems...)
	
	// Map rate metrics
	rateItems := mapRateMetrics(m, z.config.HostName, z.config.ItemPrefix, timestamp)
	items = append(items, rateItems...)
	
	// Map throughput metrics
	throughputItems := mapThroughputMetrics(m, z.config.HostName, z.config.ItemPrefix, timestamp)
	items = append(items, throughputItems...)
	
	// Map response code metrics
	responseCodeItems := mapResponseCodeMetrics(m, z.config.HostName, z.config.ItemPrefix, timestamp)
	items = append(items, responseCodeItems...)
	
	// Map query type metrics
	queryTypeItems := mapQueryTypeMetrics(m, z.config.HostName, z.config.ItemPrefix, timestamp)
	items = append(items, queryTypeItems...)
	
	return items, nil
}

func (z *ZabbixExporter) GetStats() (uint64, uint64, time.Time) {
	z.mu.RLock()
	defer z.mu.RUnlock()
	
	return z.sendCount, z.sendErrors, z.lastSend
}

func (z *ZabbixExporter) IsRunning() bool {
	z.mu.RLock()
	defer z.mu.RUnlock()
	
	return z.running
}

func validateZabbixConfig(cfg *config.ZabbixConfig) error {
	if cfg.Server == "" {
		return fmt.Errorf("server address is required")
	}
	
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid port: %d", cfg.Port)
	}
	
	if cfg.HostName == "" {
		return fmt.Errorf("hostname is required")
	}
	
	if cfg.SendInterval <= 0 {
		return fmt.Errorf("send interval must be positive")
	}
	
	if cfg.BatchSize <= 0 {
		return fmt.Errorf("batch size must be positive")
	}
	
	if cfg.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	
	if cfg.ItemPrefix == "" {
		return fmt.Errorf("item prefix is required")
	}
	
	return nil
}