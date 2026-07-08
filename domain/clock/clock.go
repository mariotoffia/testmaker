package clock

import "time"

// Clock yields the current instant. The composition root wires System(); tests
// wire a Fake they advance by hand.
type Clock interface {
	Now() time.Time
}

// systemClock reads the real wall clock. It is the one place in production code
// allowed to call time.Now.
type systemClock struct{}

// Now returns the current wall-clock time.
func (systemClock) Now() time.Time {
	//nolint:forbidigo // the single sanctioned wall-clock read; everything else injects a Clock.
	return time.Now()
}

// System returns the real wall clock for the composition root.
//
//nolint:ireturn // a factory for the Clock port; handing back the interface is the point.
func System() Clock { return systemClock{} }

// Fake is a manually-driven Clock for deterministic tests: it returns whatever
// instant was last Set (or the start instant) and only moves when Advance/Set is
// called. It is intentionally not safe for concurrent use — a test drives it
// from one goroutine.
type Fake struct {
	now time.Time
}

// NewFake returns a Fake positioned at start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now returns the Fake's current instant.
func (f *Fake) Now() time.Time { return f.now }

// Set moves the Fake to an absolute instant.
func (f *Fake) Set(t time.Time) { f.now = t }

// Advance moves the Fake forward by d and returns the new instant.
func (f *Fake) Advance(d time.Duration) time.Time {
	f.now = f.now.Add(d)
	return f.now
}
