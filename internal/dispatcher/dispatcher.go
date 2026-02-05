package dispatcher

import (
	"context"
	"sync"

	"github.com/jarrod-lowe/jmap-service-core/internal/resultref"
	"github.com/jarrod-lowe/jmap-service-libs/plugincontract"
)

// CallProcessor processes a single JMAP method call
type CallProcessor interface {
	Process(ctx context.Context, idx int, call []any, depResponses []resultref.MethodResponse) []any
}

// Config holds configuration for the dispatcher
type Config struct {
	Calls     [][]any
	PoolSize  int
	Processor CallProcessor
}

// workItem carries all data needed to process a call
type workItem struct {
	idx          int
	call         []any
	depResponses []resultref.MethodResponse
}

// completion signals that a work item finished processing
type completion struct {
	idx      int
	response []any
	isError  bool
}

// Execute processes JMAP method calls in parallel while respecting dependencies.
// Returns responses in the same order as the input calls.
func Execute(ctx context.Context, cfg Config) [][]any {
	if len(cfg.Calls) == 0 {
		return [][]any{}
	}

	// Ensure pool size is at least 1
	poolSize := cfg.PoolSize
	if poolSize < 1 {
		poolSize = 1
	}

	// Build dependency graph
	deps, dependents, err := BuildGraph(cfg.Calls)
	if err != nil {
		// Return error responses for all calls if graph building fails
		return makeAllErrorResponses(cfg.Calls, "invalidResultReference", err.Error())
	}

	responses := make([][]any, len(cfg.Calls))
	workQueue := make(chan workItem, len(cfg.Calls))
	completions := make(chan completion, len(cfg.Calls))

	// Start fixed worker pool
	var wg sync.WaitGroup
	startWorkers(ctx, poolSize, workQueue, completions, cfg.Processor, &wg)

	// Run coordinator (enqueues work, processes completions)
	coordinate(ctx, cfg.Calls, deps, dependents, responses, workQueue, completions)

	// Workers exit when workQueue is closed
	wg.Wait()

	return responses
}

// startWorkers spawns a fixed pool of workers that pull from the work queue
func startWorkers(ctx context.Context, poolSize int, workQueue <-chan workItem,
	completions chan<- completion, processor CallProcessor, wg *sync.WaitGroup) {
	for w := 0; w < poolSize; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-workQueue:
					if !ok {
						return // Queue closed, shut down
					}
					resp := processor.Process(ctx, item.idx, item.call, item.depResponses)
					isErr := isErrorResponse(resp)
					completions <- completion{item.idx, resp, isErr}
				}
			}
		}()
	}
}

// coordinate manages work distribution and completion tracking
func coordinate(ctx context.Context, calls [][]any, deps, dependents map[int][]int,
	responses [][]any, workQueue chan<- workItem, completions <-chan completion) {

	remainingDeps := make(map[int]int)
	for i := range calls {
		remainingDeps[i] = len(deps[i])
	}
	failed := make(map[int]bool)
	pending := len(calls)

	// Seed work queue with calls that have no dependencies
	for i, call := range calls {
		if remainingDeps[i] == 0 {
			workQueue <- workItem{
				idx:          i,
				call:         call,
				depResponses: nil,
			}
		}
	}

	// Process completions until all done
	for pending > 0 {
		c := <-completions
		pending--
		responses[c.idx] = c.response

		if c.isError {
			failed[c.idx] = true
		}

		// Check dependents - enqueue if ready, or mark failed if dependency failed
		for _, depIdx := range dependents[c.idx] {
			if failed[depIdx] {
				continue // Already handled
			}
			remainingDeps[depIdx]--
			if remainingDeps[depIdx] == 0 {
				// All deps complete - but did any fail?
				if hasFailedDep(depIdx, deps, failed) {
					// Mark failed without invoking processor
					failed[depIdx] = true
					clientID := extractClientID(calls[depIdx])
					responses[depIdx] = []any{"error", map[string]any{
						"type":        "invalidResultReference",
						"description": "A dependency of this method call failed",
					}, clientID}
					// Decrement pending since we're handling this inline
					pending--
					// Process this call's dependents (they may become ready now)
					// Add them back to pending consideration
					for _, transitiveDepIdx := range dependents[depIdx] {
						if !failed[transitiveDepIdx] {
							remainingDeps[transitiveDepIdx]--
							if remainingDeps[transitiveDepIdx] == 0 {
								// This transitive dependent is now ready
								if hasFailedDep(transitiveDepIdx, deps, failed) {
									failed[transitiveDepIdx] = true
									transitiveClientID := extractClientID(calls[transitiveDepIdx])
									responses[transitiveDepIdx] = []any{"error", map[string]any{
										"type":        "invalidResultReference",
										"description": "A dependency of this method call failed",
									}, transitiveClientID}
									pending--
								} else {
									workQueue <- workItem{
										idx:          transitiveDepIdx,
										call:         calls[transitiveDepIdx],
										depResponses: gatherDepResponses(transitiveDepIdx, deps, responses),
									}
								}
							}
						}
					}
				} else {
					// All deps succeeded - safe to execute
					workQueue <- workItem{
						idx:          depIdx,
						call:         calls[depIdx],
						depResponses: gatherDepResponses(depIdx, deps, responses),
					}
				}
			}
		}
	}

	close(workQueue)
}

// hasFailedDep checks if any dependency of the given call index has failed
func hasFailedDep(idx int, deps map[int][]int, failed map[int]bool) bool {
	for _, depIdx := range deps[idx] {
		if failed[depIdx] {
			return true
		}
	}
	return false
}

// gatherDepResponses collects responses from dependencies for result reference resolution
func gatherDepResponses(idx int, deps map[int][]int, responses [][]any) []resultref.MethodResponse {
	var result []resultref.MethodResponse
	for _, depIdx := range deps[idx] {
		result = append(result, toMethodResponse(responses[depIdx]))
	}
	return result
}

// toMethodResponse converts a JMAP response array to a MethodResponse struct
func toMethodResponse(resp []any) resultref.MethodResponse {
	var name string
	var args map[string]any
	var clientID string

	if len(resp) >= 1 {
		name, _ = resp[0].(string)
	}
	if len(resp) >= 2 {
		switch v := resp[1].(type) {
		case map[string]any:
			args = v
		case plugincontract.Args:
			args = map[string]any(v)
		}
	}
	if len(resp) >= 3 {
		clientID, _ = resp[2].(string)
	}

	return resultref.MethodResponse{
		Name:     name,
		Args:     args,
		ClientID: clientID,
	}
}

// isErrorResponse checks if a response is an error response
func isErrorResponse(resp []any) bool {
	if len(resp) >= 1 {
		name, _ := resp[0].(string)
		return name == "error"
	}
	return false
}

// extractClientID gets the clientId from a call
func extractClientID(call []any) string {
	if len(call) >= 3 {
		clientID, _ := call[2].(string)
		return clientID
	}
	return ""
}

// makeAllErrorResponses creates error responses for all calls
func makeAllErrorResponses(calls [][]any, errType, description string) [][]any {
	responses := make([][]any, len(calls))
	for i, call := range calls {
		clientID := extractClientID(call)
		responses[i] = []any{"error", map[string]any{
			"type":        errType,
			"description": description,
		}, clientID}
	}
	return responses
}
