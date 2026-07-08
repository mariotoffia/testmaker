// Package clock is the injected time source for the execution context. Timing
// and adaptivity (Block 8) must be deterministic under test, so nothing reads
// the wall clock directly — forbidigo bans time.Now and friends and points here.
//
// The interface is tiny on purpose: the executor stamps one moment per
// transition (Now) and derives every elapsed/deadline from it, so a Fake driven
// by a test reproduces any timing scenario without sleeping.
package clock
