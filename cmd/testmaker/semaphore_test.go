package main

import (
	"context"
	"testing"
)

func TestSemaphoreTryAcquire(t *testing.T) {
	sem := newSemaphore(1)
	if !sem.tryAcquire() {
		t.Fatal("first acquire on a free semaphore must succeed")
	}
	if sem.tryAcquire() {
		t.Fatal("second acquire must fail (capacity 1)")
	}
	sem.release()
	if !sem.tryAcquire() {
		t.Fatal("acquire after release must succeed")
	}
}

func TestSemaphoreAcquireRespectsContext(t *testing.T) {
	sem := newSemaphore(1)
	if !sem.tryAcquire() { // fill it
		t.Fatal("initial acquire must succeed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sem.acquire(ctx); err == nil {
		t.Fatal("acquire on a cancelled context must return its error")
	}
}
