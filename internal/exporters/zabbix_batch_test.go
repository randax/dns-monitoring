package exporters

import (
	"fmt"
	"testing"
	"time"
)

// MockZabbixSender implements ZabbixSenderInterface for testing
type MockZabbixSender struct {
	sendFunc func(items []ZabbixItem) (*ZabbixResponse, error)
	calls    int
}

func (m *MockZabbixSender) SendBatch(items []ZabbixItem) (*ZabbixResponse, error) {
	m.calls++
	if m.sendFunc != nil {
		return m.sendFunc(items)
	}
	return &ZabbixResponse{
		Response: "success",
		Success:  len(items),
		Failed:   0,
		Total:    len(items),
	}, nil
}

func TestSendWithRetry_CompleteSuccess(t *testing.T) {
	mock := &MockZabbixSender{
		sendFunc: func(items []ZabbixItem) (*ZabbixResponse, error) {
			return &ZabbixResponse{
				Response: "success",
				Success:  len(items),
				Failed:   0,
				Total:    len(items),
			}, nil
		},
	}
	
	batcher := &ZabbixBatcher{
		sender:        mock,
		retryAttempts: 3,
		retryDelay:    100 * time.Millisecond,
	}
	
	items := []ZabbixItem{
		{Key: "test.item1", Value: "value1"},
		{Key: "test.item2", Value: "value2"},
	}
	
	err := batcher.sendWithRetry(items)
	if err != nil {
		t.Errorf("Expected no error for complete success, got: %v", err)
	}
	
	if mock.calls != 1 {
		t.Errorf("Expected 1 call, got %d", mock.calls)
	}
}

func TestSendWithRetry_PartialFailure(t *testing.T) {
	attemptCount := 0
	mock := &MockZabbixSender{
		sendFunc: func(items []ZabbixItem) (*ZabbixResponse, error) {
			attemptCount++
			// Simulate partial failure
			return &ZabbixResponse{
				Response: "failed",
				Success:  1,
				Failed:   1,
				Total:    2,
				Info:     "Item key validation failed",
			}, nil
		},
	}
	
	batcher := &ZabbixBatcher{
		sender:        mock,
		retryAttempts: 2,
		retryDelay:    10 * time.Millisecond,
	}
	
	items := []ZabbixItem{
		{Key: "test.item1", Value: "value1"},
		{Key: "test.item2", Value: "value2"},
	}
	
	err := batcher.sendWithRetry(items)
	
	// Should fail after retries because partial failures are treated as complete failures
	if err == nil {
		t.Error("Expected error for partial failure, got nil")
	}
	
	// Should have retried the configured number of times
	expectedAttempts := batcher.retryAttempts + 1
	if attemptCount != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, attemptCount)
	}
	
	// Error message should indicate partial failure
	if err != nil && err.Error() == "" {
		t.Error("Expected detailed error message for partial failure")
	}
}

func TestSendWithRetry_CompleteFailure(t *testing.T) {
	attemptCount := 0
	mock := &MockZabbixSender{
		sendFunc: func(items []ZabbixItem) (*ZabbixResponse, error) {
			attemptCount++
			return nil, fmt.Errorf("connection refused")
		},
	}
	
	batcher := &ZabbixBatcher{
		sender:        mock,
		retryAttempts: 2,
		retryDelay:    10 * time.Millisecond,
	}
	
	items := []ZabbixItem{
		{Key: "test.item1", Value: "value1"},
	}
	
	err := batcher.sendWithRetry(items)
	
	if err == nil {
		t.Error("Expected error for complete failure, got nil")
	}
	
	expectedAttempts := batcher.retryAttempts + 1
	if attemptCount != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, attemptCount)
	}
}

func TestSendWithRetry_EventualSuccess(t *testing.T) {
	attemptCount := 0
	mock := &MockZabbixSender{
		sendFunc: func(items []ZabbixItem) (*ZabbixResponse, error) {
			attemptCount++
			if attemptCount < 3 {
				// Fail first two attempts
				return nil, fmt.Errorf("temporary error")
			}
			// Succeed on third attempt
			return &ZabbixResponse{
				Response: "success",
				Success:  len(items),
				Failed:   0,
				Total:    len(items),
			}, nil
		},
	}
	
	batcher := &ZabbixBatcher{
		sender:        mock,
		retryAttempts: 3,
		retryDelay:    10 * time.Millisecond,
	}
	
	items := []ZabbixItem{
		{Key: "test.item1", Value: "value1"},
	}
	
	err := batcher.sendWithRetry(items)
	
	if err != nil {
		t.Errorf("Expected success after retries, got error: %v", err)
	}
	
	if attemptCount != 3 {
		t.Errorf("Expected 3 attempts, got %d", attemptCount)
	}
}

func TestSendWithRetry_PartialFailureConsistency(t *testing.T) {
	// Test that partial failures maintain data consistency
	// by treating the entire batch as failed
	
	mock := &MockZabbixSender{
		sendFunc: func(items []ZabbixItem) (*ZabbixResponse, error) {
			// 3 out of 5 items succeed
			return &ZabbixResponse{
				Response: "partial",
				Success:  3,
				Failed:   2,
				Total:    5,
				Info:     "Some items failed validation",
			}, nil
		},
	}
	
	batcher := &ZabbixBatcher{
		sender:        mock,
		retryAttempts: 1,
		retryDelay:    10 * time.Millisecond,
	}
	
	items := make([]ZabbixItem, 5)
	for i := 0; i < 5; i++ {
		items[i] = ZabbixItem{
			Key:   fmt.Sprintf("test.item%d", i),
			Value: fmt.Sprintf("value%d", i),
		}
	}
	
	err := batcher.sendWithRetry(items)
	
	// Should fail because we treat partial failures as complete failures
	if err == nil {
		t.Error("Expected error for partial failure to ensure consistency")
	}
	
	// Verify error contains partial failure information
	if err != nil {
		errStr := err.Error()
		if errStr == "" {
			t.Error("Expected error message to contain partial failure details")
		}
	}
	
	// All 5 items should be considered failed and eligible for retry/DLQ
	// This ensures no silent data loss
	if mock.calls != 2 { // Initial + 1 retry
		t.Errorf("Expected 2 attempts for partial failure, got %d", mock.calls)
	}
}