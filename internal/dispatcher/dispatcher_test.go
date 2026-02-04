package dispatcher

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jarrod-lowe/jmap-service-core/internal/resultref"
)

// MockCallProcessor tracks call invocations and allows configurable responses
type MockCallProcessor struct {
	mu           sync.Mutex
	callOrder    []int      // Records order of call completions
	callTimes    []time.Time // Records when each call started
	delays       map[int]time.Duration
	responses    map[int][]any
	errors       map[int]bool
	concurrent   int32 // Max concurrent calls observed
	currentConc  int32 // Current concurrent calls
}

func NewMockCallProcessor() *MockCallProcessor {
	return &MockCallProcessor{
		delays:    make(map[int]time.Duration),
		responses: make(map[int][]any),
		errors:    make(map[int]bool),
	}
}

func (m *MockCallProcessor) SetDelay(idx int, d time.Duration) {
	m.delays[idx] = d
}

func (m *MockCallProcessor) SetResponse(idx int, resp []any) {
	m.responses[idx] = resp
}

func (m *MockCallProcessor) SetError(idx int) {
	m.errors[idx] = true
}

func (m *MockCallProcessor) Process(ctx context.Context, idx int, call []any, depResponses []resultref.MethodResponse) []any {
	// Track concurrency
	curr := atomic.AddInt32(&m.currentConc, 1)
	for {
		old := atomic.LoadInt32(&m.concurrent)
		if curr > old {
			if atomic.CompareAndSwapInt32(&m.concurrent, old, curr) {
				break
			}
		} else {
			break
		}
	}

	m.mu.Lock()
	m.callTimes = append(m.callTimes, time.Now())
	m.mu.Unlock()

	// Apply delay if configured, respecting context cancellation
	if d, ok := m.delays[idx]; ok {
		select {
		case <-ctx.Done():
			// Context cancelled - return early
			atomic.AddInt32(&m.currentConc, -1)
			clientID := ""
			if len(call) >= 3 {
				clientID, _ = call[2].(string)
			}
			return []any{"error", map[string]any{
				"type":        "serverFail",
				"description": "context cancelled",
			}, clientID}
		case <-time.After(d):
			// Delay completed
		}
	}

	atomic.AddInt32(&m.currentConc, -1)

	m.mu.Lock()
	m.callOrder = append(m.callOrder, idx)
	m.mu.Unlock()

	// Return configured response or default
	if resp, ok := m.responses[idx]; ok {
		return resp
	}

	// Default success response
	clientID := ""
	if len(call) >= 3 {
		clientID, _ = call[2].(string)
	}
	methodName := ""
	if len(call) >= 1 {
		methodName, _ = call[0].(string)
	}

	if m.errors[idx] {
		return []any{"error", map[string]any{
			"type":        "serverFail",
			"description": "mock error",
		}, clientID}
	}

	return []any{methodName, map[string]any{
		"accountId": "acc1",
		"list":      []any{},
	}, clientID}
}

func (m *MockCallProcessor) MaxConcurrent() int32 {
	return atomic.LoadInt32(&m.concurrent)
}

func (m *MockCallProcessor) CallOrder() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]int, len(m.callOrder))
	copy(result, m.callOrder)
	return result
}

func TestExecute_IndependentCallsRunConcurrently(t *testing.T) {
	// Three independent calls, each with a delay
	// With parallelism >= 3, they should run concurrently
	calls := [][]any{
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e1"}}, "c0"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e2"}}, "c1"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e3"}}, "c2"},
	}

	mock := NewMockCallProcessor()
	mock.SetDelay(0, 50*time.Millisecond)
	mock.SetDelay(1, 50*time.Millisecond)
	mock.SetDelay(2, 50*time.Millisecond)

	cfg := Config{
		Calls:     calls,
		PoolSize:  4,
		Processor: mock,
	}

	start := time.Now()
	responses := Execute(context.Background(), cfg)
	elapsed := time.Since(start)

	// Should complete in ~50ms (parallel), not ~150ms (serial)
	if elapsed > 120*time.Millisecond {
		t.Errorf("expected parallel execution (~50ms), took %v", elapsed)
	}

	// Should have 3 responses
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	// Max concurrency should be >= 3
	if mock.MaxConcurrent() < 3 {
		t.Errorf("expected max concurrency >= 3, got %d", mock.MaxConcurrent())
	}
}

func TestExecute_DependentCallsWait(t *testing.T) {
	// Linear chain: c0 → c1 → c2
	// c1 should wait for c0, c2 should wait for c1
	calls := [][]any{
		{"Email/query", map[string]any{"accountId": "acc1"}, "c0"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c0",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c1"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c1",
				"name":     "Email/get",
				"path":     "/list/*/id",
			},
		}, "c2"},
	}

	mock := NewMockCallProcessor()
	mock.SetDelay(0, 20*time.Millisecond)
	mock.SetDelay(1, 20*time.Millisecond)
	mock.SetDelay(2, 20*time.Millisecond)

	cfg := Config{
		Calls:     calls,
		PoolSize:  4,
		Processor: mock,
	}

	start := time.Now()
	responses := Execute(context.Background(), cfg)
	elapsed := time.Since(start)

	// Should take at least ~60ms (sequential chain)
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected sequential execution (~60ms), took %v - calls may have run in parallel incorrectly", elapsed)
	}

	// Should have 3 responses
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	// Call order should be 0, 1, 2 (sequential)
	order := mock.CallOrder()
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Errorf("expected call order [0,1,2], got %v", order)
	}
}

func TestExecute_ErrorPropagation(t *testing.T) {
	// c0 fails, c1 depends on c0 - should also fail with invalidResultReference
	calls := [][]any{
		{"Email/query", map[string]any{"accountId": "acc1"}, "c0"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c0",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c1"},
	}

	mock := NewMockCallProcessor()
	mock.SetError(0) // c0 returns error

	cfg := Config{
		Calls:     calls,
		PoolSize:  4,
		Processor: mock,
	}

	responses := Execute(context.Background(), cfg)

	// c0 should be an error
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	if responses[0][0] != "error" {
		t.Errorf("c0: expected error response, got %v", responses[0][0])
	}

	// c1 should also be an error (dependency failed)
	if responses[1][0] != "error" {
		t.Errorf("c1: expected error response, got %v", responses[1][0])
	}
	errArgs, ok := responses[1][1].(map[string]any)
	if !ok {
		t.Fatalf("c1: expected error args map, got %T", responses[1][1])
	}
	if errArgs["type"] != "invalidResultReference" {
		t.Errorf("c1: expected invalidResultReference, got %v", errArgs["type"])
	}
}

func TestExecute_ResponseOrdering(t *testing.T) {
	// Calls complete in reverse order (c2 first, c0 last)
	// Responses should still be in original order
	calls := [][]any{
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e1"}}, "c0"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e2"}}, "c1"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e3"}}, "c2"},
	}

	mock := NewMockCallProcessor()
	mock.SetDelay(0, 60*time.Millisecond) // c0 slowest
	mock.SetDelay(1, 30*time.Millisecond) // c1 medium
	mock.SetDelay(2, 10*time.Millisecond) // c2 fastest

	// Set distinct responses to verify ordering
	mock.SetResponse(0, []any{"Email/get", map[string]any{"id": "resp0"}, "c0"})
	mock.SetResponse(1, []any{"Email/get", map[string]any{"id": "resp1"}, "c1"})
	mock.SetResponse(2, []any{"Email/get", map[string]any{"id": "resp2"}, "c2"})

	cfg := Config{
		Calls:     calls,
		PoolSize:  4,
		Processor: mock,
	}

	responses := Execute(context.Background(), cfg)

	// Verify responses are in original order (0, 1, 2)
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	for i := 0; i < 3; i++ {
		args, ok := responses[i][1].(map[string]any)
		if !ok {
			t.Fatalf("response %d: expected args map", i)
		}
		expectedID := "resp" + string(rune('0'+i))
		if args["id"] != expectedID {
			t.Errorf("response %d: expected id=%s, got %v", i, expectedID, args["id"])
		}
	}
}

func TestExecute_PoolSizeLimitsConcurrency(t *testing.T) {
	// 4 independent calls with pool size 2
	// Should see max concurrency of 2
	calls := [][]any{
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e1"}}, "c0"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e2"}}, "c1"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e3"}}, "c2"},
		{"Email/get", map[string]any{"accountId": "acc1", "ids": []string{"e4"}}, "c3"},
	}

	mock := NewMockCallProcessor()
	for i := 0; i < 4; i++ {
		mock.SetDelay(i, 30*time.Millisecond)
	}

	cfg := Config{
		Calls:     calls,
		PoolSize:  2, // Limit to 2 concurrent
		Processor: mock,
	}

	start := time.Now()
	responses := Execute(context.Background(), cfg)
	elapsed := time.Since(start)

	// With pool size 2 and 4 calls of 30ms each: ~60ms
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected ~60ms with pool size 2, took %v", elapsed)
	}

	if len(responses) != 4 {
		t.Fatalf("expected 4 responses, got %d", len(responses))
	}

	// Max concurrency should be <= 2
	if mock.MaxConcurrent() > 2 {
		t.Errorf("expected max concurrency <= 2, got %d", mock.MaxConcurrent())
	}
}

func TestExecute_EmptyCalls(t *testing.T) {
	cfg := Config{
		Calls:     [][]any{},
		PoolSize:  4,
		Processor: NewMockCallProcessor(),
	}

	responses := Execute(context.Background(), cfg)

	if len(responses) != 0 {
		t.Errorf("expected 0 responses for empty calls, got %d", len(responses))
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	// Long-running calls should be interruptible
	calls := [][]any{
		{"Email/get", map[string]any{"accountId": "acc1"}, "c0"},
	}

	mock := NewMockCallProcessor()
	mock.SetDelay(0, 5*time.Second) // Very long delay

	cfg := Config{
		Calls:     calls,
		PoolSize:  4,
		Processor: mock,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_ = Execute(ctx, cfg)
	elapsed := time.Since(start)

	// Should complete quickly due to cancellation, not wait 5 seconds
	if elapsed > 1*time.Second {
		t.Errorf("expected quick exit on cancellation, took %v", elapsed)
	}
}

func TestExecute_TransitiveFailurePropagation(t *testing.T) {
	// c0 fails, c1 depends on c0, c2 depends on c1
	// Both c1 and c2 should fail
	calls := [][]any{
		{"Email/query", map[string]any{"accountId": "acc1"}, "c0"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c0",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "c1"},
		{"Email/get", map[string]any{
			"accountId": "acc1",
			"#ids": map[string]any{
				"resultOf": "c1",
				"name":     "Email/get",
				"path":     "/list/*/id",
			},
		}, "c2"},
	}

	mock := NewMockCallProcessor()
	mock.SetError(0) // c0 returns error

	cfg := Config{
		Calls:     calls,
		PoolSize:  4,
		Processor: mock,
	}

	responses := Execute(context.Background(), cfg)

	// All should be errors
	for i, resp := range responses {
		if resp[0] != "error" {
			t.Errorf("c%d: expected error response, got %v", i, resp[0])
		}
	}

	// c1 and c2 should have invalidResultReference
	for i := 1; i <= 2; i++ {
		errArgs, ok := responses[i][1].(map[string]any)
		if !ok {
			t.Fatalf("c%d: expected error args map", i)
		}
		if errArgs["type"] != "invalidResultReference" {
			t.Errorf("c%d: expected invalidResultReference, got %v", i, errArgs["type"])
		}
	}
}
