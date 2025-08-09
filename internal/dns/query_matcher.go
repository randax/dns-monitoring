package dns

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Simple query tracking structure
type pendingQuery struct {
	Result    Result
	QueryTime time.Time
}

// QueryMatcher interface for matching DNS queries with responses
type QueryMatcher interface {
	AddQuery(result Result)
	MatchResponse(result Result) *Result
	CleanupExpired(ctx context.Context)
	GetPendingCount() int
	GetMatchedCount() uint64
	GetExpiredCount() uint64
}

// SimpleQueryMatcher uses only transaction ID for matching
type SimpleQueryMatcher struct {
	queries map[uint16]*pendingQuery
	mu      sync.RWMutex
	timeout time.Duration
	matched uint64
	expired uint64
}

// NewSimpleQueryMatcher creates a new simple query matcher
func NewSimpleQueryMatcher(timeout time.Duration) *SimpleQueryMatcher {
	return &SimpleQueryMatcher{
		queries: make(map[uint16]*pendingQuery),
		timeout: timeout,
	}
}

// AddQuery adds a query to be matched
func (qm *SimpleQueryMatcher) AddQuery(result Result) {
	extra, ok := result.Extra.(*ExtraPacketData)
	if !ok || extra == nil {
		return
	}

	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.queries[extra.TransactionID] = &pendingQuery{
		Result:    result,
		QueryTime: result.Timestamp,
	}
}

// MatchResponse matches a response with a query using transaction ID
func (qm *SimpleQueryMatcher) MatchResponse(result Result) *Result {
	extra, ok := result.Extra.(*ExtraPacketData)
	if !ok || extra == nil {
		return nil
	}

	qm.mu.Lock()
	defer qm.mu.Unlock()

	query, exists := qm.queries[extra.TransactionID]
	if !exists {
		return nil
	}

	// Basic validation: check domain and query type match
	if query.Result.Domain != result.Domain || query.Result.QueryType != result.QueryType {
		return nil
	}

	// Calculate latency
	latency := result.Timestamp.Sub(query.QueryTime)
	if latency < 0 || latency > qm.timeout {
		return nil
	}

	// Remove matched query
	delete(qm.queries, extra.TransactionID)

	// Create matched result
	matchedResult := result
	matchedResult.Duration = latency
	
	atomic.AddUint64(&qm.matched, 1)

	return &matchedResult
}

// CleanupExpired removes expired queries
func (qm *SimpleQueryMatcher) CleanupExpired(ctx context.Context) {
	ticker := time.NewTicker(qm.timeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			qm.cleanupExpiredQueries()
		}
	}
}

func (qm *SimpleQueryMatcher) cleanupExpiredQueries() {
	now := time.Now()
	expiredIDs := []uint16{}

	qm.mu.RLock()
	for tid, query := range qm.queries {
		if now.Sub(query.QueryTime) > qm.timeout {
			expiredIDs = append(expiredIDs, tid)
		}
	}
	qm.mu.RUnlock()

	if len(expiredIDs) > 0 {
		qm.mu.Lock()
		for _, tid := range expiredIDs {
			if query, exists := qm.queries[tid]; exists {
				if time.Now().Sub(query.QueryTime) > qm.timeout {
					delete(qm.queries, tid)
					atomic.AddUint64(&qm.expired, 1)
				}
			}
		}
		qm.mu.Unlock()
	}
}

// GetPendingCount returns the number of pending queries
func (qm *SimpleQueryMatcher) GetPendingCount() int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return len(qm.queries)
}

// GetMatchedCount returns the number of matched queries
func (qm *SimpleQueryMatcher) GetMatchedCount() uint64 {
	return atomic.LoadUint64(&qm.matched)
}

// GetExpiredCount returns the number of expired queries
func (qm *SimpleQueryMatcher) GetExpiredCount() uint64 {
	return atomic.LoadUint64(&qm.expired)
}