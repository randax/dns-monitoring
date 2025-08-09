// Package exporters provides data export functionality for monitoring metrics.
//
// ZabbixBatcher implements batching and retry logic for sending metrics to Zabbix.
// 
// Partial Failure Handling:
// When Zabbix reports partial failures (some items accepted, some rejected), this
// implementation takes a conservative approach by treating partial failures as 
// complete failures. This ensures data consistency and prevents silent data loss.
// All items in a partially failed batch will be retried together or moved to the
// dead letter queue as a unit. This approach prioritizes data integrity over 
// partial success optimization.
//
// Future Enhancement:
// For item-level tracking, consider implementing:
// - Individual item retry queues with failure counters
// - Item-level dead letter queue with failure reasons
// - Selective retry of only failed items from partial failures
package exporters

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// ZabbixSenderInterface defines the interface for sending data to Zabbix
type ZabbixSenderInterface interface {
	SendBatch(items []ZabbixItem) (*ZabbixResponse, error)
}

type ZabbixBatcher struct {
	sender        ZabbixSenderInterface
	batchSize     int
	retryAttempts int
	retryDelay    time.Duration
	
	items         []ZabbixItem
	mu            sync.Mutex
	
	// Circuit breaker
	failureCount  int
	lastFailure   time.Time
	circuitOpen   bool
	circuitOpenAt time.Time
	
	// Statistics
	totalSent     uint64
	totalFailed   uint64
	totalRetries  uint64
	
	// Dead letter queue
	deadLetterQueue []ZabbixItem
	maxDLQSize      int
}

func NewZabbixBatcher(sender ZabbixSenderInterface, batchSize, retryAttempts int, retryDelay time.Duration) *ZabbixBatcher {
	return &ZabbixBatcher{
		sender:          sender,
		batchSize:       batchSize,
		retryAttempts:   retryAttempts,
		retryDelay:      retryDelay,
		items:           make([]ZabbixItem, 0, batchSize),
		deadLetterQueue: make([]ZabbixItem, 0),
		maxDLQSize:      1000,
	}
}

func (b *ZabbixBatcher) AddItem(item ZabbixItem) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	// Check circuit breaker
	if b.isCircuitOpen() {
		return fmt.Errorf("circuit breaker is open, not accepting items")
	}
	
	// Validate item
	if err := validateItemKey(item.Key); err != nil {
		return fmt.Errorf("invalid item key: %w", err)
	}
	
	if err := validateItemValue(item.Value); err != nil {
		return fmt.Errorf("invalid item value: %w", err)
	}
	
	b.items = append(b.items, item)
	
	// Check if batch is full
	if len(b.items) >= b.batchSize {
		return b.flushUnlocked()
	}
	
	return nil
}

func (b *ZabbixBatcher) AddItems(items []ZabbixItem) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	// Check circuit breaker
	if b.isCircuitOpen() {
		return fmt.Errorf("circuit breaker is open, not accepting items")
	}
	
	// Validate all items
	for _, item := range items {
		if err := validateItemKey(item.Key); err != nil {
			return fmt.Errorf("invalid item key %s: %w", item.Key, err)
		}
		
		if err := validateItemValue(item.Value); err != nil {
			return fmt.Errorf("invalid item value for %s: %w", item.Key, err)
		}
	}
	
	// Add items in batches if necessary
	for len(items) > 0 {
		remaining := b.batchSize - len(b.items)
		if remaining <= 0 {
			if err := b.flushUnlocked(); err != nil {
				return err
			}
			remaining = b.batchSize
		}
		
		toAdd := remaining
		if toAdd > len(items) {
			toAdd = len(items)
		}
		
		b.items = append(b.items, items[:toAdd]...)
		items = items[toAdd:]
	}
	
	// Flush if batch is full
	if len(b.items) >= b.batchSize {
		return b.flushUnlocked()
	}
	
	return nil
}

func (b *ZabbixBatcher) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	return b.flushUnlocked()
}

func (b *ZabbixBatcher) flushUnlocked() error {
	if len(b.items) == 0 {
		return nil
	}
	
	// Check circuit breaker
	if b.isCircuitOpen() {
		// Move items to dead letter queue
		b.addToDeadLetterQueue(b.items)
		b.items = b.items[:0]
		return fmt.Errorf("circuit breaker is open")
	}
	
	// Create a copy of items to send
	itemsToSend := make([]ZabbixItem, len(b.items))
	copy(itemsToSend, b.items)
	
	// Clear the batch
	b.items = b.items[:0]
	
	// Send with retry logic
	err := b.sendWithRetry(itemsToSend)
	if err != nil {
		b.handleFailure()
		// Add failed items to dead letter queue
		b.addToDeadLetterQueue(itemsToSend)
		return fmt.Errorf("failed to send batch: %w", err)
	}
	
	b.handleSuccess(len(itemsToSend))
	return nil
}

// sendWithRetry attempts to send items to Zabbix with retry logic.
// 
// Partial failures (where some items succeed and others fail) are treated as
// complete failures to ensure data consistency. This means all items will be
// retried together or moved to the dead letter queue as a unit.
//
// Returns nil only when ALL items are successfully sent to Zabbix.
func (b *ZabbixBatcher) sendWithRetry(items []ZabbixItem) error {
	var lastErr error
	itemCount := len(items)
	
	for attempt := 0; attempt <= b.retryAttempts; attempt++ {
		if attempt > 0 {
			b.totalRetries++
			delay := b.retryDelay * time.Duration(attempt)
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			time.Sleep(delay)
			log.Printf("Retrying Zabbix send (attempt %d/%d) for %d items", attempt, b.retryAttempts, itemCount)
		}
		
		response, err := b.sender.SendBatch(items)
		if err == nil && response != nil {
			// Check if all items were accepted
			if response.Failed == 0 {
				log.Printf("Zabbix send successful: %d items sent", response.Success)
				return nil
			}
			
			// Partial failure handling - safer approach: treat as complete failure
			// This ensures data consistency and prevents silent data loss
			percentFailed := float64(response.Failed) / float64(itemCount) * 100
			log.Printf("WARN: Zabbix partial failure detected - treating as complete failure for data consistency")
			log.Printf("  Items sent: %d, Succeeded: %d, Failed: %d (%.1f%%)", 
				itemCount, response.Success, response.Failed, percentFailed)
			if response.Info != "" {
				log.Printf("  Failure info: %s", response.Info)
			}
			log.Printf("  NOTE: All %d items will be retried or moved to dead letter queue", itemCount)
			
			// Create detailed error for partial failure
			lastErr = fmt.Errorf("partial failure: %d/%d items failed (%.1f%%): %s", 
				response.Failed, itemCount, percentFailed, response.Info)
			
			// Continue retrying with all items to ensure consistency
		} else {
			// Complete network/protocol failure
			if err != nil {
				log.Printf("ERROR: Zabbix send failed completely (attempt %d/%d): %v", 
					attempt+1, b.retryAttempts+1, err)
				lastErr = err
			} else {
				log.Printf("ERROR: Zabbix send failed with nil response (attempt %d/%d)", 
					attempt+1, b.retryAttempts+1)
				lastErr = fmt.Errorf("nil response from Zabbix server")
			}
		}
	}
	
	log.Printf("ERROR: Failed to send %d items after %d retries: %v", 
		itemCount, b.retryAttempts+1, lastErr)
	return fmt.Errorf("failed after %d retries: %w", b.retryAttempts, lastErr)
}

func (b *ZabbixBatcher) FlushIfFull() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if len(b.items) >= b.batchSize {
		return b.flushUnlocked()
	}
	
	return nil
}

func (b *ZabbixBatcher) isCircuitOpen() bool {
	if !b.circuitOpen {
		return false
	}
	
	// Check if circuit should be closed (30 seconds cooldown)
	if time.Since(b.circuitOpenAt) > 30*time.Second {
		b.circuitOpen = false
		b.failureCount = 0
		log.Println("Zabbix circuit breaker closed")
		return false
	}
	
	return true
}

func (b *ZabbixBatcher) handleFailure() {
	b.failureCount++
	b.lastFailure = time.Now()
	b.totalFailed++
	
	// Open circuit breaker after 5 consecutive failures
	if b.failureCount >= 5 && !b.circuitOpen {
		b.circuitOpen = true
		b.circuitOpenAt = time.Now()
		log.Println("Zabbix circuit breaker opened due to consecutive failures")
	}
}

func (b *ZabbixBatcher) handleSuccess(itemCount int) {
	b.failureCount = 0
	b.totalSent += uint64(itemCount)
	
	// Close circuit breaker on success
	if b.circuitOpen {
		b.circuitOpen = false
		log.Println("Zabbix circuit breaker closed after successful send")
	}
}

func (b *ZabbixBatcher) addToDeadLetterQueue(items []ZabbixItem) {
	// Limit the size of the dead letter queue
	spaceAvailable := b.maxDLQSize - len(b.deadLetterQueue)
	if spaceAvailable <= 0 {
		log.Printf("Dead letter queue is full, dropping %d items", len(items))
		return
	}
	
	toAdd := len(items)
	if toAdd > spaceAvailable {
		toAdd = spaceAvailable
		log.Printf("Dead letter queue near capacity, dropping %d items", len(items)-toAdd)
	}
	
	b.deadLetterQueue = append(b.deadLetterQueue, items[:toAdd]...)
	log.Printf("Added %d items to dead letter queue (total: %d)", toAdd, len(b.deadLetterQueue))
}

func (b *ZabbixBatcher) RetryDeadLetterQueue() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if len(b.deadLetterQueue) == 0 {
		return nil
	}
	
	if b.isCircuitOpen() {
		return fmt.Errorf("circuit breaker is open")
	}
	
	items := b.deadLetterQueue
	b.deadLetterQueue = make([]ZabbixItem, 0)
	
	log.Printf("Retrying %d items from dead letter queue", len(items))
	
	// Send in batches
	for len(items) > 0 {
		batchSize := b.batchSize
		if batchSize > len(items) {
			batchSize = len(items)
		}
		
		batch := items[:batchSize]
		items = items[batchSize:]
		
		if err := b.sendWithRetry(batch); err != nil {
			// Put back in dead letter queue
			b.addToDeadLetterQueue(batch)
			b.handleFailure()
			return fmt.Errorf("failed to retry dead letter queue: %w", err)
		}
		
		b.handleSuccess(len(batch))
	}
	
	return nil
}

func (b *ZabbixBatcher) GetStats() (sent, failed, retries uint64, dlqSize int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	return b.totalSent, b.totalFailed, b.totalRetries, len(b.deadLetterQueue)
}

func (b *ZabbixBatcher) GetPendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	return len(b.items)
}

func (b *ZabbixBatcher) IsCircuitOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	return b.isCircuitOpen()
}