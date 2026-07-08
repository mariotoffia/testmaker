package clock_test

import (
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
)

func TestFakeIsDeterministicAndDoesNotMoveOnItsOwn(t *testing.T) {
	start := time.Date(2024, 3, 4, 12, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)

	if got := f.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}
	// Reading again must not advance the clock — a real time.Now() would drift.
	if got := f.Now(); !got.Equal(start) {
		t.Fatalf("Now() drifted without Advance: %v", got)
	}

	got := f.Advance(90 * time.Second)
	want := start.Add(90 * time.Second)
	if !got.Equal(want) || !f.Now().Equal(want) {
		t.Fatalf("after Advance: Now() = %v (returned %v), want %v", f.Now(), got, want)
	}

	reset := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	f.Set(reset)
	if !f.Now().Equal(reset) {
		t.Fatalf("after Set: Now() = %v, want %v", f.Now(), reset)
	}
}

func TestSystemClockMovesForward(t *testing.T) {
	c := clock.System()
	a := c.Now()
	b := c.Now()
	// The wall clock never runs backwards; a and b are real instants.
	if b.Before(a) {
		t.Fatalf("system clock went backwards: %v then %v", a, b)
	}
}
