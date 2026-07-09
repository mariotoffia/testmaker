package main

import (
	"context"
	"fmt"
)

// semaphore is a counting gate over concurrent ingest runs (sync and async
// share one instance) so outbound fetches and paid LLM calls stay bounded no
// matter the request path (DESIGN.md §7.4). A buffered channel is the whole
// implementation.
type semaphore chan struct{}

func newSemaphore(n int) semaphore { return make(semaphore, n) }

// tryAcquire takes a slot without blocking; false means the gate is full (the
// sync path turns this into a 429).
func (s semaphore) tryAcquire() bool {
	select {
	case s <- struct{}{}:
		return true
	default:
		return false
	}
}

// acquire blocks for a slot or until ctx is done (the async path waits here).
// A cancelled/expired context returns its cause wrapped, so callers can still
// match with errors.Is while the boundary stays wrapped (wrapcheck).
func (s semaphore) acquire(ctx context.Context) error {
	select {
	case s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("acquire ingest slot: %w", ctx.Err())
	}
}

func (s semaphore) release() { <-s }
