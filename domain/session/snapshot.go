package session

import (
	"slices"
	"time"
)

// SessionSnapshot is the dependency-neutral DTO used to persist/transport a
// Session. It carries enough to resume an attempt: the plan, the lifecycle
// state, the timing anchors, the currently presented item and every captured
// response.
//
// Every time.Time is normalized to wall-clock UTC (Round(0).UTC()) in Snapshot,
// so the two round-trips a store must agree on — a memory clone
// (Rehydrate→Snapshot) and a sqlite JSON marshal/unmarshal — return byte- and
// reflect.DeepEqual-identical values. Durations are plain time.Duration (int64)
// and need no normalization. Presented is a value (not a pointer): an empty
// ItemID means "no item presented", which keeps the DTO pointer-free and
// DeepEqual-stable.
type SessionSnapshot struct {
	ID        SessionID
	TestID    string
	Policy    Policy
	State     State
	Timing    Timing
	Sections  []PlanSection
	StartedAt time.Time
	EndedAt   time.Time
	Presented Presented
	Responses []Response
}

// Snapshot returns the persistence/transport DTO for the aggregate, with every
// timestamp normalized to wall-clock UTC.
func (s *Session) Snapshot() SessionSnapshot {
	presented := s.presented
	presented.DeliveredAt = normalizeTime(presented.DeliveredAt)
	return SessionSnapshot{
		ID:        s.id,
		TestID:    s.testID,
		Policy:    s.policy,
		State:     s.state,
		Timing:    s.timing,
		Sections:  clonePlanSections(s.sections),
		StartedAt: normalizeTime(s.startedAt),
		EndedAt:   normalizeTime(s.endedAt),
		Presented: presented,
		Responses: slices.Clone(s.responses),
	}
}

// RehydrateFromSnapshot rebuilds an aggregate from a trusted snapshot without
// re-validating (the snapshot is assumed to have passed NewSession previously).
// It deep-copies slice fields, so a snapshot round-tripped through
// Rehydrate().Snapshot() shares no memory with stored state — the property the
// stores rely on to hand back isolated snapshots.
func RehydrateFromSnapshot(snap SessionSnapshot) *Session {
	return &Session{
		id:        snap.ID,
		testID:    snap.TestID,
		policy:    snap.Policy,
		state:     snap.State,
		timing:    snap.Timing,
		sections:  clonePlanSections(snap.Sections),
		startedAt: snap.StartedAt,
		endedAt:   snap.EndedAt,
		presented: snap.Presented,
		responses: slices.Clone(snap.Responses),
	}
}

// normalizeTime strips any monotonic reading and pins the location to UTC so a
// time survives a JSON round-trip reflect.DeepEqual-identical. The zero time is
// already UTC, so it round-trips unchanged.
func normalizeTime(t time.Time) time.Time { return t.Round(0).UTC() }
