package execution_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/app/execution"
	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// Compile-time proof the service satisfies the driven port.
var _ ports.Executor = (*execution.Service)(nil)

// --- in-process fakes (no adapter imports, no I/O, no wall clock) ------------

// fakeBank is an in-memory ports.ItemRepository seeded by the test.
type fakeBank struct {
	items map[item.ItemID]item.ItemSnapshot
}

func newFakeBank() *fakeBank { return &fakeBank{items: map[item.ItemID]item.ItemSnapshot{}} }

// seededBank returns a fake bank preloaded with the given items.
func seededBank(t *testing.T, snaps ...item.ItemSnapshot) *fakeBank {
	t.Helper()
	b := newFakeBank()
	for _, s := range snaps {
		if err := b.SaveItem(context.Background(), s); err != nil {
			t.Fatalf("seed %s: %v", s.ID, err)
		}
	}
	return b
}

func (b *fakeBank) SaveItem(_ context.Context, snap item.ItemSnapshot) error {
	b.items[snap.ID] = snap
	return nil
}

func (b *fakeBank) GetItem(_ context.Context, id item.ItemID) (item.ItemSnapshot, error) {
	s, ok := b.items[id]
	if !ok {
		return item.ItemSnapshot{}, item.ErrUnknownItem
	}
	return s, nil
}

func (b *fakeBank) ListItems(_ context.Context, _ item.ItemFilter) ([]item.ItemSnapshot, error) {
	out := make([]item.ItemSnapshot, 0, len(b.items))
	for _, s := range b.items {
		out = append(out, s)
	}
	return out, nil
}

func (b *fakeBank) DeleteItem(_ context.Context, id item.ItemID) error {
	delete(b.items, id)
	return nil
}

// fakeSessions is an in-memory ports.SessionRepository. It clones through the
// aggregate on the way in and out, so a caller never shares slice storage with
// stored state — the isolation a real store gives, and what resume-via-repo
// depends on.
type fakeSessions struct {
	saved map[session.SessionID]session.SessionSnapshot
	saves int
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{saved: map[session.SessionID]session.SessionSnapshot{}}
}

func (r *fakeSessions) SaveSession(_ context.Context, snap session.SessionSnapshot) error {
	if snap.ID == "" {
		return session.ErrInvalidSession
	}
	r.saves++
	r.saved[snap.ID] = session.RehydrateFromSnapshot(snap).Snapshot()
	return nil
}

func (r *fakeSessions) GetSession(_ context.Context, id session.SessionID) (session.SessionSnapshot, error) {
	snap, ok := r.saved[id]
	if !ok {
		return session.SessionSnapshot{}, session.ErrUnknownSession
	}
	return session.RehydrateFromSnapshot(snap).Snapshot(), nil
}

// counterIDs hands out deterministic session ids (sess-1, sess-2, …).
func counterIDs() func() session.SessionID {
	n := 0
	return func() session.SessionID {
		n++
		return session.SessionID("sess-" + strconv.Itoa(n))
	}
}

// mcItem builds a valid multiple-choice bank item with a known answer key.
func mcItem(t *testing.T, id string, band int, key string) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           item.ItemID(id),
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which figure continues?"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:  item.AnswerKey{OptionID: key},
		Difficulty: item.Difficulty{Band: band},
	})
	if err != nil {
		t.Fatalf("build item %s: %v", id, err)
	}
	return it.Snapshot()
}

var epoch = time.Date(2024, 6, 7, 9, 0, 0, 0, time.UTC)

// numItem builds a valid open-numeric bank item with an absolute grading
// tolerance (epsilon). tol == 0 means exact equality.
func numItem(t *testing.T, id string, band int, key, tol float64) item.ItemSnapshot {
	t.Helper()
	it, err := item.NewItem(item.ItemSpec{
		ID:           item.ItemID(id),
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     "B1",
		Stimulus:     []item.StimulusPart{{Text: "next number?"}},
		AnswerFormat: item.FormatOpenNumeric,
		AnswerKey:    item.AnswerKey{Numeric: key, Tolerance: tol},
		Difficulty:   item.Difficulty{Band: band},
	})
	if err != nil {
		t.Fatalf("build numeric item %s: %v", id, err)
	}
	return it.Snapshot()
}

// --- fixed-policy end-to-end -------------------------------------------------

// TestDeliveredItemHidesAnswerKey proves the item handed to the taker in a
// Delivery carries neither the answer key nor the explanation: a taker must not be
// able to read the correct answer for the item they are about to answer. Grading
// still works because the executor reads the key from the bank, not from what it
// hands back.
func TestDeliveredItemHidesAnswerKey(t *testing.T) {
	ctx := context.Background()
	it, ierr := item.NewItem(item.ItemSpec{
		ID:           "log-1",
		Provenance:   item.Provenance{SourceID: "rulegen", Origin: item.OriginGenerated, Redistributable: shared.RedistYes},
		TestType:     "A2",
		Stimulus:     []item.StimulusPart{{Text: "which figure continues?"}},
		AnswerFormat: item.FormatMultipleChoice,
		Options: []item.Option{
			{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}, {ID: "d", Text: "D"},
		},
		AnswerKey:   item.AnswerKey{OptionID: "c"},
		Explanation: "the answer is C because the figure rotates 90 degrees",
		Difficulty:  item.Difficulty{Band: 1},
	})
	if ierr != nil {
		t.Fatalf("build item: %v", ierr)
	}
	bank := seededBank(t, it.Snapshot(), mcItem(t, "log-2", 2, "b"))
	svc := execution.NewService(clock.NewFake(epoch), bank, newFakeSessions(), counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "log-1", "log-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if d.Item == nil {
		t.Fatal("no item delivered")
	}
	if d.Item.AnswerKey != (item.AnswerKey{}) {
		t.Errorf("delivered item leaked the answer key: %+v", d.Item.AnswerKey)
	}
	if d.Item.Explanation != "" {
		t.Errorf("delivered item leaked the explanation: %q", d.Item.Explanation)
	}
	// The taker still receives everything needed to answer: stem + options.
	if len(d.Item.Options) != 4 || len(d.Item.Stimulus) == 0 {
		t.Errorf("delivered item is missing stem/options: %+v", d.Item)
	}
}

func TestStartDeliversFirstItemAndDeadline(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t, mcItem(t, "log-1", 1, "a"), mcItem(t, "log-2", 2, "b"))
	repo := newFakeSessions()
	clk := clock.NewFake(epoch)
	svc := execution.NewService(clk, bank, repo, counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "log-1", "log-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if d.Session.ID != "sess-1" {
		t.Fatalf("session id = %q, want sess-1", d.Session.ID)
	}
	if d.Session.State != session.StateInProgress {
		t.Fatalf("state = %q, want in-progress", d.Session.State)
	}
	if d.Session.Presented.ItemID != "log-1" {
		t.Fatalf("presented = %q, want log-1", d.Session.Presented.ItemID)
	}
	if d.Item == nil || d.Item.ID != "log-1" {
		t.Fatalf("delivered item = %+v, want log-1 content", d.Item)
	}
	// per-item cap 60s binds before the 20m global budget.
	if want := epoch.Add(60 * time.Second); !d.Deadline.Equal(want) {
		t.Fatalf("deadline = %s, want %s", d.Deadline, want)
	}
	if repo.saves != 1 {
		t.Fatalf("saves = %d, want 1 (started session persisted)", repo.saves)
	}
}

func TestFixedRunGradesAndCompletes(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t, mcItem(t, "log-1", 1, "a"), mcItem(t, "log-2", 2, "b"), mcItem(t, "log-3", 3, "c"))
	repo := newFakeSessions()
	clk := clock.NewFake(epoch)
	svc := execution.NewService(clk, bank, repo, counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "log-1", "log-2", "log-3"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := d.Session.ID

	// Correct, then wrong, then correct — 10s spent on each.
	clk.Advance(10 * time.Second)
	d = mustAnswer(t, svc, ctx, id, "log-1", session.Answer{OptionID: "a"})
	if d.Session.Presented.ItemID != "log-2" {
		t.Fatalf("after log-1: presented %q, want log-2", d.Session.Presented.ItemID)
	}
	clk.Advance(10 * time.Second)
	d = mustAnswer(t, svc, ctx, id, "log-2", session.Answer{OptionID: "z"})
	if d.Session.Presented.ItemID != "log-3" {
		t.Fatalf("after log-2: presented %q, want log-3", d.Session.Presented.ItemID)
	}
	clk.Advance(10 * time.Second)
	d = mustAnswer(t, svc, ctx, id, "log-3", session.Answer{OptionID: "c"})

	// Plan exhausted: no item presented, no advisory deadline, still in-progress.
	if d.Session.Presented.ItemID != "" {
		t.Fatalf("after last item: presented %q, want none", d.Session.Presented.ItemID)
	}
	if d.Item != nil {
		t.Fatalf("after last item: delivered item = %+v, want nil", d.Item)
	}
	if !d.Deadline.IsZero() {
		t.Fatalf("after last item: deadline = %s, want zero", d.Deadline)
	}
	if d.Session.State != session.StateInProgress {
		t.Fatalf("state = %q, want in-progress (executor must Complete explicitly)", d.Session.State)
	}

	snap, err := svc.Complete(ctx, id)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if snap.State != session.StateCompleted {
		t.Fatalf("final state = %q, want completed", snap.State)
	}
	if len(snap.Responses) != 3 {
		t.Fatalf("responses = %d, want 3", len(snap.Responses))
	}
	wantCorrect := []bool{true, false, true}
	for i, r := range snap.Responses {
		if r.Correct != wantCorrect[i] {
			t.Fatalf("response %d correct = %v, want %v", i, r.Correct, wantCorrect[i])
		}
		if r.Elapsed != 10*time.Second {
			t.Fatalf("response %d elapsed = %s, want 10s", i, r.Elapsed)
		}
	}
}

// TestAnswerGradesNumericWithinTolerance proves the executor honours an item's
// absolute grading tolerance: an answer on the epsilon boundary is correct
// (inclusive), one past it is wrong. Tolerance 0 keeps exact equality.
func TestAnswerGradesNumericWithinTolerance(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t,
		numItem(t, "num-1", 1, 10, 0.5),
		numItem(t, "num-2", 2, 20, 0.5),
	)
	repo := newFakeSessions()
	clk := clock.NewFake(epoch)
	svc := execution.NewService(clk, bank, repo, counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "num-1", "num-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := d.Session.ID
	// 10.5 lands exactly on the +0.5 boundary → correct (inclusive).
	mustAnswer(t, svc, ctx, id, "num-1", session.Answer{Numeric: 10.5})
	// 20.6 is 0.6 from 20, past the 0.5 epsilon → wrong.
	mustAnswer(t, svc, ctx, id, "num-2", session.Answer{Numeric: 20.6})

	snap, err := svc.Complete(ctx, id)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	want := []bool{true, false}
	for i, r := range snap.Responses {
		if r.Correct != want[i] {
			t.Fatalf("numeric response %d correct = %v, want %v", i, r.Correct, want[i])
		}
	}
}

// --- adaptive-policy end-to-end ----------------------------------------------
func TestAdaptiveRunFollowsStaircase(t *testing.T) {
	ctx := context.Background()
	// A pool spanning bands 1..3 with a duplicate band-2 item so the staircase
	// path differs from authored order.
	bank := seededBank(t, mcItem(t, "a1", 1, "a"), mcItem(t, "a2", 2, "a"), mcItem(t, "a3", 3, "a"), mcItem(t, "a4", 2, "a"))
	repo := newFakeSessions()
	clk := clock.NewFake(epoch)
	svc := execution.NewService(clk, bank, repo, counterIDs())

	d, err := svc.Start(ctx, adaptiveTest(t))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := d.Session.ID

	// Grade sequence: correct (climb), wrong (descend), correct (climb), correct.
	// Expected delivery order: a1 -> a2 -> a4 -> a3 (authored order is a1,a2,a3,a4).
	verdicts := map[string]string{"a1": "a", "a2": "z", "a4": "a", "a3": "a"}
	var order []string
	order = append(order, d.Session.Presented.ItemID)
	for range 3 {
		presented := d.Session.Presented.ItemID
		clk.Advance(5 * time.Second)
		d = mustAnswer(t, svc, ctx, id, presented, session.Answer{OptionID: verdicts[presented]})
		if d.Session.Presented.ItemID != "" {
			order = append(order, d.Session.Presented.ItemID)
		}
	}
	want := []string{"a1", "a2", "a4", "a3"}
	if len(order) != len(want) {
		t.Fatalf("delivered %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("delivery order = %v, want %v", order, want)
		}
	}
	if _, err := svc.Complete(ctx, id); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

// --- global timeout abandons -------------------------------------------------

func TestGlobalTimeoutAbandons(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t, mcItem(t, "log-1", 1, "a"), mcItem(t, "log-2", 2, "b"))
	repo := newFakeSessions()
	clk := clock.NewFake(epoch)
	svc := execution.NewService(clk, bank, repo, counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "log-1", "log-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := d.Session.ID

	// Jump past the 20-minute global budget, then answer.
	clk.Set(epoch.Add(21 * time.Minute))
	d, err = svc.Answer(ctx, id, "log-1", session.Answer{OptionID: "a"})
	if err != nil {
		t.Fatalf("answer after timeout: %v", err)
	}
	if d.Session.State != session.StateAbandoned {
		t.Fatalf("state = %q, want abandoned", d.Session.State)
	}
	if d.Item != nil {
		t.Fatalf("abandoned delivery still carries an item: %+v", d.Item)
	}
	if len(d.Session.Responses) != 0 {
		t.Fatalf("late answer was recorded: %d responses", len(d.Session.Responses))
	}
	// An abandoned session cannot be completed.
	if _, err := svc.Complete(ctx, id); !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("complete abandoned err = %v, want ErrInvalidSession", err)
	}
}

// --- resume across service instances (state lives only in the repo) ----------

func TestResumeAcrossServiceInstances(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t, mcItem(t, "log-1", 1, "a"), mcItem(t, "log-2", 2, "b"))
	repo := newFakeSessions()
	clk := clock.NewFake(epoch)

	starter := execution.NewService(clk, bank, repo, counterIDs())
	d, err := starter.Start(ctx, fixedTest(t, "log-1", "log-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := d.Session.ID

	// A different Service (own id generator, shared repo+bank) resumes the attempt
	// purely from persisted state.
	resumer := execution.NewService(clk, bank, repo, counterIDs())
	clk.Advance(5 * time.Second)
	d = mustAnswer(t, resumer, ctx, id, "log-1", session.Answer{OptionID: "a"})
	if d.Session.Presented.ItemID != "log-2" {
		t.Fatalf("resumed presented = %q, want log-2", d.Session.Presented.ItemID)
	}
	if len(d.Session.Responses) != 1 || !d.Session.Responses[0].Correct {
		t.Fatalf("resumed responses = %+v, want one correct", d.Session.Responses)
	}
}

// --- timeout enforcement on Complete + torn-write atomicity ------------------

func TestCompletePastDeadlineAbandons(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t, mcItem(t, "log-1", 1, "a"), mcItem(t, "log-2", 2, "b"))
	repo := newFakeSessions()
	clk := clock.NewFake(epoch)
	svc := execution.NewService(clk, bank, repo, counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "log-1", "log-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Blow past the 20-minute global budget, then Complete (not Answer): the
	// executor must abandon, never close it as a legitimate completion.
	clk.Set(epoch.Add(21 * time.Minute))
	snap, err := svc.Complete(ctx, d.Session.ID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if snap.State != session.StateAbandoned {
		t.Fatalf("state = %q, want abandoned (completion past the global budget)", snap.State)
	}
}

func TestStartMissingItemPersistsNothing(t *testing.T) {
	ctx := context.Background()
	repo := newFakeSessions()
	svc := execution.NewService(clock.NewFake(epoch), newFakeBank(), repo, counterIDs())

	// Structurally valid plan, but the bank holds none of the items.
	_, err := svc.Start(ctx, fixedTest(t, "ghost-1", "ghost-2"))
	if !errors.Is(err, item.ErrUnknownItem) {
		t.Fatalf("start missing item err = %v, want ErrUnknownItem", err)
	}
	if repo.saves != 0 {
		t.Fatalf("saves = %d, want 0 (a missing first item must not orphan a session)", repo.saves)
	}
}

func TestAnswerMissingNextItemDoesNotCommit(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t, mcItem(t, "log-1", 1, "a")) // log-2 intentionally absent
	repo := newFakeSessions()
	svc := execution.NewService(clock.NewFake(epoch), bank, repo, counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "log-1", "log-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := d.Session.ID

	// Answering log-1 advances to the (un-fetchable) log-2: the transition must
	// not commit, so the stored attempt still presents log-1 with no responses.
	if _, err := svc.Answer(ctx, id, "log-1", session.Answer{OptionID: "a"}); !errors.Is(err, item.ErrUnknownItem) {
		t.Fatalf("answer err = %v, want ErrUnknownItem", err)
	}
	got, gerr := repo.GetSession(ctx, id)
	if gerr != nil {
		t.Fatalf("get: %v", gerr)
	}
	if got.Presented.ItemID != "log-1" {
		t.Fatalf("presented = %q, want log-1 (torn write committed the advance)", got.Presented.ItemID)
	}
	if len(got.Responses) != 0 {
		t.Fatalf("responses = %d, want 0 (answer committed despite the fetch failure)", len(got.Responses))
	}
}

// --- error paths -------------------------------------------------------------

func TestAnswerUnknownSession(t *testing.T) {
	ctx := context.Background()
	svc := execution.NewService(clock.NewFake(epoch), newFakeBank(), newFakeSessions(), counterIDs())
	_, err := svc.Answer(ctx, "nope", "log-1", session.Answer{OptionID: "a"})
	if !errors.Is(err, session.ErrUnknownSession) {
		t.Fatalf("answer unknown err = %v, want ErrUnknownSession", err)
	}
	if _, err := svc.Complete(ctx, "nope"); !errors.Is(err, session.ErrUnknownSession) {
		t.Fatalf("complete unknown err = %v, want ErrUnknownSession", err)
	}
}

func TestAnswerWrongTargetItem(t *testing.T) {
	ctx := context.Background()
	bank := seededBank(t, mcItem(t, "log-1", 1, "a"), mcItem(t, "log-2", 2, "b"))
	repo := newFakeSessions()
	svc := execution.NewService(clock.NewFake(epoch), bank, repo, counterIDs())

	d, err := svc.Start(ctx, fixedTest(t, "log-1", "log-2"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// log-2 exists in the bank but log-1 is the presented item.
	_, err = svc.Answer(ctx, d.Session.ID, "log-2", session.Answer{OptionID: "b"})
	if !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("wrong-target err = %v, want ErrInvalidSession", err)
	}
}

func TestStartRejectsInvalidPlan(t *testing.T) {
	ctx := context.Background()
	repo := newFakeSessions()
	svc := execution.NewService(clock.NewFake(epoch), newFakeBank(), repo, counterIDs())

	// A test snapshot with no sections is an invalid plan.
	_, err := svc.Start(ctx, testset.TestSnapshot{ID: "empty", Title: "Empty", Policy: testset.PolicyFixedIncreasing})
	if !errors.Is(err, session.ErrInvalidSession) {
		t.Fatalf("invalid-plan err = %v, want ErrInvalidSession", err)
	}
	if repo.saves != 0 {
		t.Fatalf("saves = %d, want 0 (nothing persisted on invalid plan)", repo.saves)
	}
}

// --- helpers -----------------------------------------------------------------

func mustAnswer(
	t *testing.T, svc *execution.Service, ctx context.Context,
	id session.SessionID, itemID string, ans session.Answer,
) ports.Delivery {
	t.Helper()
	d, err := svc.Answer(ctx, id, itemID, ans)
	if err != nil {
		t.Fatalf("answer %s: %v", itemID, err)
	}
	return d
}

// fixedTest composes a fixed-increasing test over the given item ids in bands
// 1..n (one section), a 20-minute global budget and a 60s per-item cap.
func fixedTest(t *testing.T, ids ...string) testset.TestSnapshot {
	t.Helper()
	refs := make([]testset.ItemRef, len(ids))
	for i, id := range ids {
		refs[i] = testset.ItemRef{ItemID: id, Difficulty: i + 1}
	}
	tt, err := testset.NewTest(testset.TestSpec{
		ID:     "gia",
		Title:  "GIA",
		Policy: testset.PolicyFixedIncreasing,
		Timing: testset.Timing{Total: 20 * time.Minute, PerItem: 60 * time.Second},
		Sections: []testset.Section{{
			Title:  "Reasoning",
			Family: shared.FamilyLogical,
			Items:  refs,
		}},
	})
	if err != nil {
		t.Fatalf("build fixed test: %v", err)
	}
	return tt.Snapshot()
}

// adaptiveTest composes an adaptive test whose single section spans bands 1..3
// (a1=1, a2=2, a3=3, a4=2), an untimed budget so timing never interferes.
func adaptiveTest(t *testing.T) testset.TestSnapshot {
	t.Helper()
	tt, err := testset.NewTest(testset.TestSpec{
		ID:     "adaptigma",
		Title:  "Adaptigma",
		Policy: testset.PolicyAdaptive,
		Sections: []testset.Section{{
			Title:  "Matrices",
			Family: shared.FamilyLogical,
			Items: []testset.ItemRef{
				{ItemID: "a1", Difficulty: 1},
				{ItemID: "a2", Difficulty: 2},
				{ItemID: "a3", Difficulty: 3},
				{ItemID: "a4", Difficulty: 2},
			},
		}},
	})
	if err != nil {
		t.Fatalf("build adaptive test: %v", err)
	}
	return tt.Snapshot()
}
