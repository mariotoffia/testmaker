// Package execution is the application service (use-case layer) for the renderer
// / test executor: it administers a composed test attempt end to end.
//
// The session aggregate (domain/session) is a clock-free state machine, so this
// service supplies everything time- and content-dependent that the aggregate
// deliberately does not know: it reads the clock (domain/clock) to stamp each
// transition, fetches the presented item from the bank (ports.ItemRepository) to
// grade the taker's answer against its key and to return its content, and
// persists every step through ports.SessionRepository so an attempt can be
// resumed or scored later. It also maps a composed test's snapshot
// (testset.TestSnapshot) onto the session's own plan value objects — the one
// place allowed to bridge the testset and session bounded contexts.
//
// It orchestrates driven ports only and holds no storage, timing-policy or
// grading knowledge beyond that mapping; the composition root injects the clock,
// bank, session repository and id generator.
package execution
