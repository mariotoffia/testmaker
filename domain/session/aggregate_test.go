package session_test

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// t0 is a fixed, UTC start instant for deterministic timing assertions.
var t0 = time.Date(2024, 3, 4, 9, 0, 0, 0, time.UTC)

func fixedSpec() session.SessionSpec {
	return session.SessionSpec{
		ID:     "sess-1",
		TestID: "gia",
		Policy: session.PolicyFixedIncreasing,
		Timing: session.Timing{Total: 30 * time.Minute},
		Sections: []session.PlanSection{
			{
				Title:  "Reasoning",
				Family: shared.FamilyLogical,
				Timing: session.Timing{PerItem: 60 * time.Second},
				Items:  []session.PlanItem{{ItemID: "log-1", Difficulty: 1}, {ItemID: "log-2", Difficulty: 3}},
			},
			{
				Title:  "Numeric",
				Family: shared.FamilyNumerical,
				Items:  []session.PlanItem{{ItemID: "num-1", Difficulty: 2}},
			},
		},
	}
}

func TestNewSessionRejectsInvalidSpecs(t *testing.T) {
	base := fixedSpec()
	cases := map[string]func(*session.SessionSpec){
		"empty id":          func(s *session.SessionSpec) { s.ID = "" },
		"empty test id":     func(s *session.SessionSpec) { s.TestID = "" },
		"invalid policy":    func(s *session.SessionSpec) { s.Policy = "sideways" },
		"negative total":    func(s *session.SessionSpec) { s.Timing = session.Timing{Total: -1} },
		"per-item > total":  func(s *session.SessionSpec) { s.Timing = session.Timing{Total: time.Second, PerItem: time.Minute} },
		"no sections":       func(s *session.SessionSpec) { s.Sections = nil },
		"bad family":        func(s *session.SessionSpec) { s.Sections[0].Family = "telepathy" },
		"empty section":     func(s *session.SessionSpec) { s.Sections[0].Items = nil },
		"empty item id":     func(s *session.SessionSpec) { s.Sections[0].Items[0].ItemID = "" },
		"band below one":    func(s *session.SessionSpec) { s.Sections[0].Items[0].Difficulty = 0 },
		"duplicate item id": func(s *session.SessionSpec) { s.Sections[1].Items[0].ItemID = "log-1" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			spec := base
			spec.Sections = deepCopySections(base.Sections)
			mutate(&spec)
			if _, err := session.NewSession(spec); !errors.Is(err, session.ErrInvalidSession) {
				t.Fatalf("want ErrInvalidSession, got %v", err)
			}
		})
	}
}

func TestNewSessionStartsInCreatedWithNoItem(t *testing.T) {
	s := session.MustSession(fixedSpec())
	if s.State() != session.StateCreated {
		t.Fatalf("state = %q, want created", s.State())
	}
	if !s.Presented().DeliveredAt.IsZero() || s.Presented().ItemID != "" {
		t.Fatalf("created session must present nothing, got %+v", s.Presented())
	}
	if !s.StartedAt().IsZero() {
		t.Fatalf("created session must not have a start time")
	}
}

func TestBeginPresentsFirstItemAndRejectsDoubleBegin(t *testing.T) {
	s := session.MustSession(fixedSpec())
	if err := s.Begin(t0); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if s.State() != session.StateInProgress {
		t.Fatalf("state = %q, want in-progress", s.State())
	}
	p := s.Presented()
	if p.ItemID != "log-1" || p.Section != 0 || !p.DeliveredAt.Equal(t0) {
		t.Fatalf("first presented = %+v, want log-1 @ t0 in section 0", p)
	}
	if err := s.Begin(t0); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("second begin must fail with ErrInvalidSession, got %v", err)
	}
}

func TestRecordRequiresInProgress(t *testing.T) {
	s := session.MustSession(fixedSpec())
	if err := s.Record("log-1", session.Answer{}, true, t0); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("record before begin must fail, got %v", err)
	}
}

func TestFixedNavigationDeliversAuthoredOrderThenCompletes(t *testing.T) {
	s := session.MustSession(fixedSpec())
	mustBegin(t, s, t0)

	got := driveAll(t, s, map[string]bool{"log-1": true, "log-2": false, "num-1": true})
	want := []string{"log-1", "log-2", "num-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery order = %v, want %v", got, want)
	}
	// exhausting the plan leaves no item but keeps the session in progress.
	if s.State() != session.StateInProgress {
		t.Fatalf("state after last answer = %q, want in-progress", s.State())
	}
	if s.Presented().ItemID != "" {
		t.Fatalf("no item should be presented after exhaustion, got %q", s.Presented().ItemID)
	}
	// the responses captured every item, in order, with the graded correctness.
	rs := s.Responses()
	if len(rs) != 3 || rs[0].ItemID != "log-1" || !rs[0].Correct || rs[1].Correct || rs[1].Section != 0 || rs[2].Section != 1 {
		t.Fatalf("responses not captured as expected: %+v", rs)
	}

	end := t0.Add(10 * time.Minute)
	if err := s.Complete(end); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if s.State() != session.StateCompleted || !s.EndedAt().Equal(end) {
		t.Fatalf("after complete: state=%q ended=%v", s.State(), s.EndedAt())
	}
}

func TestRecordRejectsWrongTargetAndBackwardsClock(t *testing.T) {
	s := session.MustSession(fixedSpec())
	mustBegin(t, s, t0) // presents log-1 @ t0

	if err := s.Record("num-1", session.Answer{}, true, t0.Add(time.Second)); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("answering a non-presented item must fail, got %v", err)
	}
	if err := s.Record("log-1", session.Answer{}, true, t0.Add(-time.Second)); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("backwards clock must fail, got %v", err)
	}
}

func TestRecordRejectsNonFiniteNumericAnswer(t *testing.T) {
	// A NaN/Inf numeric answer is not JSON-encodable: allowing it would persist in
	// the memory store but fail in sqlite, splitting backend parity.
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		s := session.MustSession(fixedSpec())
		mustBegin(t, s, t0) // presents log-1 @ t0
		err := s.Record("log-1", session.Answer{Numeric: bad}, false, t0.Add(time.Second))
		if !errors.Is(err, session.ErrInvalidSession) {
			t.Fatalf("numeric answer %v must be rejected, got %v", bad, err)
		}
	}
}

func TestRecordElapsedIsMeasuredFromDelivery(t *testing.T) {
	s := session.MustSession(fixedSpec())
	mustBegin(t, s, t0)
	if err := s.Record("log-1", session.Answer{OptionID: "b"}, true, t0.Add(15*time.Second)); err != nil {
		t.Fatalf("record: %v", err)
	}
	if got := s.Responses()[0].Elapsed; got != 15*time.Second {
		t.Fatalf("elapsed = %v, want 15s", got)
	}
	// the next item was presented at the same instant the previous was answered.
	if !s.Presented().DeliveredAt.Equal(t0.Add(15 * time.Second)) {
		t.Fatalf("next item delivery time = %v", s.Presented().DeliveredAt)
	}
}

func TestAdaptiveNavigationClimbsAndReactsToWrongAnswers(t *testing.T) {
	spec := session.SessionSpec{
		ID:     "sess-a",
		TestID: "matrigma",
		Policy: session.PolicyAdaptive,
		Sections: []session.PlanSection{{
			Title:  "Matrices",
			Family: shared.FamilySpatial,
			// authored order deliberately not band order, to prove selection is by
			// band-distance to the running target, not authored position.
			Items: []session.PlanItem{
				{ItemID: "a", Difficulty: 1},
				{ItemID: "b", Difficulty: 3},
				{ItemID: "c", Difficulty: 3},
				{ItemID: "d", Difficulty: 5},
			},
		}},
	}
	s := session.MustSession(spec)
	mustBegin(t, s, t0) // net 0 -> target 1 -> easiest band -> "a"

	// a(band1) correct  -> target 2 -> closest undelivered is band 3, tie b/c -> b
	// b(band3) wrong     -> target 1 -> closest undelivered {c:3, d:5} -> c
	// c(band3) correct   -> target 2 -> only d left -> d
	answers := map[string]bool{"a": true, "b": false, "c": true, "d": true}
	got := driveFrom(t, s, answers, "a")
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adaptive order = %v, want %v", got, want)
	}
}

func TestCompleteAndAbandonGuards(t *testing.T) {
	t.Run("complete requires in-progress", func(t *testing.T) {
		s := session.MustSession(fixedSpec())
		if err := s.Complete(t0); !errors.Is(err, session.ErrInvalidSession) {
			t.Fatalf("complete on created must fail, got %v", err)
		}
	})
	t.Run("cannot complete twice", func(t *testing.T) {
		s := session.MustSession(fixedSpec())
		mustBegin(t, s, t0)
		if err := s.Complete(t0.Add(time.Minute)); err != nil {
			t.Fatalf("complete: %v", err)
		}
		if err := s.Complete(t0.Add(2 * time.Minute)); !errors.Is(err, session.ErrInvalidSession) {
			t.Fatalf("second complete must fail, got %v", err)
		}
	})
	t.Run("abandon clears the presented item", func(t *testing.T) {
		s := session.MustSession(fixedSpec())
		mustBegin(t, s, t0)
		if err := s.Abandon(t0.Add(time.Minute)); err != nil {
			t.Fatalf("abandon: %v", err)
		}
		if s.State() != session.StateAbandoned || s.Presented().ItemID != "" {
			t.Fatalf("after abandon: state=%q presented=%q", s.State(), s.Presented().ItemID)
		}
	})
	t.Run("backwards clock rejected", func(t *testing.T) {
		s := session.MustSession(fixedSpec())
		mustBegin(t, s, t0)
		if err := s.Complete(t0.Add(-time.Second)); !errors.Is(err, session.ErrInvalidSession) {
			t.Fatalf("backwards complete must fail, got %v", err)
		}
	})
}

func TestDeadlinePicksEarliestOfPerItemAndGlobal(t *testing.T) {
	s := session.MustSession(fixedSpec()) // global 30m, section 0 per-item 60s
	mustBegin(t, s, t0)                   // presents log-1 @ t0
	// per-item (t0+60s) is earlier than global (t0+30m)
	if got, want := s.Deadline(), t0.Add(60*time.Second); !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v (per-item binds)", got, want)
	}
	if got, want := s.GlobalDeadline(), t0.Add(30*time.Minute); !got.Equal(want) {
		t.Fatalf("global deadline = %v, want %v", got, want)
	}
}

func TestDeadlineZeroWhenUntimedOrNoItem(t *testing.T) {
	spec := fixedSpec()
	spec.Timing = session.Timing{}
	spec.Sections[0].Timing = session.Timing{}
	spec.Sections[1].Timing = session.Timing{}
	s := session.MustSession(spec)
	mustBegin(t, s, t0)
	if !s.Deadline().IsZero() {
		t.Fatalf("untimed session must have a zero deadline, got %v", s.Deadline())
	}
	if !s.GlobalDeadline().IsZero() {
		t.Fatalf("untimed session must have a zero global deadline")
	}
}

func TestSnapshotRoundTripsThroughCloneAndJSON(t *testing.T) {
	s := session.MustSession(fixedSpec())
	mustBegin(t, s, t0)
	if err := s.Record("log-1", session.Answer{OptionID: "c"}, true, t0.Add(20*time.Second)); err != nil {
		t.Fatalf("record: %v", err)
	}
	snap := s.Snapshot()

	// clone round-trip (the memory-store path)
	if clone := session.RehydrateFromSnapshot(snap).Snapshot(); !reflect.DeepEqual(clone, snap) {
		t.Fatalf("clone round-trip mismatch:\n got %+v\nwant %+v", clone, snap)
	}

	// JSON round-trip (the sqlite-store path)
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back session.SessionSnapshot
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back, snap) {
		t.Fatalf("json round-trip mismatch:\n got %+v\nwant %+v", back, snap)
	}
}

func TestSnapshotNormalizesTimestampsToUTC(t *testing.T) {
	// feed a non-UTC, monotonic-free instant; the snapshot must pin it to UTC.
	loc := time.FixedZone("CET", 3600)
	start := time.Date(2024, 5, 6, 12, 0, 0, 0, loc)
	s := session.MustSession(fixedSpec())
	mustBegin(t, s, start)
	got := s.Snapshot().StartedAt
	if got.Location() != time.UTC || !got.Equal(start) {
		t.Fatalf("StartedAt = %v (loc %v), want the same instant in UTC", got, got.Location())
	}
}

// --- helpers ---

func mustBegin(t *testing.T, s *session.Session, now time.Time) {
	t.Helper()
	if err := s.Begin(now); err != nil {
		t.Fatalf("begin: %v", err)
	}
}

// driveAll answers every presented item (in delivery order) using the correctness
// map, one second apart, and returns the delivery order.
func driveAll(t *testing.T, s *session.Session, correct map[string]bool) []string {
	t.Helper()
	return driveFrom(t, s, correct, s.Presented().ItemID)
}

func driveFrom(t *testing.T, s *session.Session, correct map[string]bool, first string) []string {
	t.Helper()
	order := []string{first}
	now := s.Presented().DeliveredAt
	for s.Presented().ItemID != "" {
		id := s.Presented().ItemID
		now = now.Add(time.Second)
		if err := s.Record(id, session.Answer{}, correct[id], now); err != nil {
			t.Fatalf("record %s: %v", id, err)
		}
		if next := s.Presented().ItemID; next != "" {
			order = append(order, next)
		}
	}
	return order
}

func deepCopySections(in []session.PlanSection) []session.PlanSection {
	out := make([]session.PlanSection, len(in))
	for i, sec := range in {
		sec.Items = append([]session.PlanItem(nil), sec.Items...)
		out[i] = sec
	}
	return out
}
