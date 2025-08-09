package dns

import (
	"context"
	"testing"
	"time"
)

func TestSimpleQueryMatcherExactMatch(t *testing.T) {
	qm := NewSimpleQueryMatcher(5 * time.Second)

	query := Result{
		Timestamp: time.Now(),
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "192.168.1.100",
			DestIP:        "8.8.8.8",
		},
	}

	qm.AddQuery(query)

	response := Result{
		Timestamp: time.Now().Add(50 * time.Millisecond),
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "8.8.8.8",
			DestIP:        "192.168.1.100",
		},
	}

	matched := qm.MatchResponse(response)
	if matched == nil {
		t.Error("Expected exact match, got nil")
	}

	if matched != nil && matched.Duration < 50*time.Millisecond {
		t.Errorf("Expected duration >= 50ms, got %v", matched.Duration)
	}
}

func TestSimpleQueryMatcherNoMatch(t *testing.T) {
	qm := NewSimpleQueryMatcher(5 * time.Second)

	query := Result{
		Timestamp: time.Now(),
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "192.168.1.100",
			DestIP:        "8.8.8.8",
		},
	}

	qm.AddQuery(query)

	// Different transaction ID
	response := Result{
		Timestamp: time.Now().Add(50 * time.Millisecond),
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 54321, // Different ID
			SourceIP:      "8.8.8.8",
			DestIP:        "192.168.1.100",
		},
	}

	matched := qm.MatchResponse(response)
	if matched != nil {
		t.Error("Expected no match for different transaction ID")
	}
}

func TestSimpleQueryMatcherDomainMismatch(t *testing.T) {
	qm := NewSimpleQueryMatcher(5 * time.Second)

	query := Result{
		Timestamp: time.Now(),
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "192.168.1.100",
			DestIP:        "8.8.8.8",
		},
	}

	qm.AddQuery(query)

	// Different domain
	response := Result{
		Timestamp: time.Now().Add(50 * time.Millisecond),
		Domain:    "different.com", // Different domain
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "8.8.8.8",
			DestIP:        "192.168.1.100",
		},
	}

	matched := qm.MatchResponse(response)
	if matched != nil {
		t.Error("Expected no match for different domain")
	}
}

func TestSimpleQueryMatcherExpiredQueries(t *testing.T) {
	qm := NewSimpleQueryMatcher(100 * time.Millisecond)

	query := Result{
		Timestamp: time.Now(),
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "192.168.1.100",
			DestIP:        "8.8.8.8",
		},
	}

	qm.AddQuery(query)

	// Response after timeout
	response := Result{
		Timestamp: time.Now().Add(200 * time.Millisecond), // After timeout
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "8.8.8.8",
			DestIP:        "192.168.1.100",
		},
	}

	matched := qm.MatchResponse(response)
	if matched != nil {
		t.Error("Expected no match for expired query")
	}
}

func TestSimpleQueryMatcherCleanup(t *testing.T) {
	qm := NewSimpleQueryMatcher(100 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup goroutine
	go qm.CleanupExpired(ctx)

	query := Result{
		Timestamp: time.Now(),
		Domain:    "example.com",
		QueryType: "A",
		Extra: &ExtraPacketData{
			TransactionID: 12345,
			SourceIP:      "192.168.1.100",
			DestIP:        "8.8.8.8",
		},
	}

	qm.AddQuery(query)

	if qm.GetPendingCount() != 1 {
		t.Errorf("Expected 1 pending query, got %d", qm.GetPendingCount())
	}

	// Wait for cleanup
	time.Sleep(150 * time.Millisecond)

	if qm.GetPendingCount() != 0 {
		t.Errorf("Expected 0 pending queries after cleanup, got %d", qm.GetPendingCount())
	}

	if qm.GetExpiredCount() != 1 {
		t.Errorf("Expected 1 expired query, got %d", qm.GetExpiredCount())
	}
}