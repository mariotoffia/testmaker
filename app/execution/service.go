package execution

import (
	"context"
	"crypto/rand"

	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// Service administers test attempts: it implements ports.Executor over a clock,
// the item bank (read, for grading and content) and the session repository
// (read/write, for state). It is stateless — all attempt state lives in the
// persisted session snapshot — so one Service safely serves many concurrent
// takers.
type Service struct {
	clock clock.Clock
	bank  ports.ItemRepository
	repo  ports.SessionRepository
	newID func() session.SessionID
}

// NewService wires the clock, item bank, session repository and session-id
// generator. Use RandomIDs for production; tests inject a deterministic counter.
func NewService(
	clk clock.Clock, bank ports.ItemRepository, repo ports.SessionRepository, newID func() session.SessionID,
) *Service {
	return &Service{clock: clk, bank: bank, repo: repo, newID: newID}
}

// RandomIDs returns an id generator backed by crypto/rand (rand.Text panics only
// if the OS entropy source fails, which is unrecoverable), so session ids are
// unguessable and collision-free without any external dependency.
func RandomIDs() func() session.SessionID {
	return func() session.SessionID { return session.SessionID("sess-" + rand.Text()) }
}

// Start creates a session for the test, begins it (presenting the first item)
// and returns the opening delivery. An invalid plan surfaces as
// session.ErrInvalidSession and nothing is persisted.
func (s *Service) Start(ctx context.Context, test testset.TestSnapshot) (ports.Delivery, error) {
	sess, err := session.NewSession(specFromTest(s.newID(), test))
	if err != nil {
		return ports.Delivery{}, err
	}
	if err := sess.Begin(s.clock.Now()); err != nil {
		return ports.Delivery{}, err
	}
	return s.persistAndDeliver(ctx, sess)
}

// Answer grades the taker's answer to the presented item and advances the
// attempt. If the session's global time budget has already run out it is
// abandoned instead (the answer is not recorded). It fails if the session is
// unknown or the answer does not target the presented item.
func (s *Service) Answer(
	ctx context.Context, id session.SessionID, itemID string, ans session.Answer,
) (ports.Delivery, error) {
	sess, err := s.load(ctx, id)
	if err != nil {
		return ports.Delivery{}, err
	}
	now := s.clock.Now()
	if gd := sess.GlobalDeadline(); !gd.IsZero() && now.After(gd) {
		if err := sess.Abandon(now); err != nil {
			return ports.Delivery{}, err
		}
		return s.persistAndDeliver(ctx, sess)
	}
	presented, err := s.bank.GetItem(ctx, item.ItemID(itemID))
	if err != nil {
		return ports.Delivery{}, err
	}
	if err := sess.Record(itemID, ans, graded(presented, ans), now); err != nil {
		return ports.Delivery{}, err
	}
	return s.persistAndDeliver(ctx, sess)
}

// Complete ends the session normally and returns its final snapshot. A
// completion past the global budget is an abandonment, not a completion: the
// executor enforces the same hard stop here it does on Answer, so a timed-out
// attempt cannot be closed as if it finished in time.
func (s *Service) Complete(ctx context.Context, id session.SessionID) (session.SessionSnapshot, error) {
	sess, err := s.load(ctx, id)
	if err != nil {
		return session.SessionSnapshot{}, err
	}
	now := s.clock.Now()
	end := sess.Complete
	if gd := sess.GlobalDeadline(); !gd.IsZero() && now.After(gd) {
		end = sess.Abandon
	}
	if err := end(now); err != nil {
		return session.SessionSnapshot{}, err
	}
	snap := sess.Snapshot()
	if err := s.repo.SaveSession(ctx, snap); err != nil {
		return session.SessionSnapshot{}, err
	}
	return snap, nil
}

// load fetches and rehydrates the session for id.
func (s *Service) load(ctx context.Context, id session.SessionID) (*session.Session, error) {
	snap, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return session.RehydrateFromSnapshot(snap), nil
}

// persistAndDeliver builds the delivery for whatever item (if any) the session
// now presents, then persists the session. Content is fetched *before* the save
// so the transition is all-or-nothing: if the presented item cannot be fetched,
// nothing is committed and the attempt is neither orphaned (on Start) nor wedged
// presenting an un-fetchable item (on Answer) — a clean retry sees the prior
// state.
func (s *Service) persistAndDeliver(ctx context.Context, sess *session.Session) (ports.Delivery, error) {
	d := ports.Delivery{Session: sess.Snapshot(), Deadline: sess.Deadline()}
	if p := sess.Presented(); p.ItemID != "" {
		content, err := s.bank.GetItem(ctx, item.ItemID(p.ItemID))
		if err != nil {
			return ports.Delivery{}, err
		}
		d.Item = &content
	}
	if err := s.repo.SaveSession(ctx, d.Session); err != nil {
		return ports.Delivery{}, err
	}
	return d, nil
}

// graded scores the taker's answer against the item's key, interpreted by the
// item's answer format. A mismatched or malformed answer is simply wrong.
//
// ponytail: the key is read from the live bank, not frozen into the plan. No
// administered item type is open-numeric yet and the ItemRepository exposes no
// delete, so mid-attempt key drift cannot happen today; freezing the key (or a
// content hash) into the plan is a Block 9 fairness decision, deferred until
// scoring depends on it. Two known ceilings, both Block 9:
//   - open-numeric uses exact float equality (fine for the exact, finite keys
//     the bank stores; an epsilon belongs with the scoring model, not here);
//   - a zero-valued numeric answer matches a zero-valued key (Answer.Numeric has
//     no presence bit), so add an "answered" signal before numeric items ship.
func graded(it item.ItemSnapshot, ans session.Answer) bool {
	switch it.AnswerFormat {
	case item.FormatMultipleChoice:
		return ans.OptionID == it.AnswerKey.OptionID
	case item.FormatOpenNumeric:
		return ans.Numeric == it.AnswerKey.Numeric
	case item.FormatTrueFalseCannotSay:
		return ans.Verdict == string(it.AnswerKey.Verdict)
	default:
		return false
	}
}

// specFromTest maps a composed test's snapshot onto the session's plan value
// objects. This is the sanctioned bridge between the testset and session
// contexts (both are domain contexts the session cannot import, but the app
// layer can import both).
func specFromTest(id session.SessionID, test testset.TestSnapshot) session.SessionSpec {
	sections := make([]session.PlanSection, len(test.Sections))
	for i, sec := range test.Sections {
		items := make([]session.PlanItem, len(sec.Items))
		for j, ref := range sec.Items {
			items[j] = session.PlanItem{ItemID: ref.ItemID, Difficulty: ref.Difficulty}
		}
		sections[i] = session.PlanSection{
			Title:  sec.Title,
			Family: sec.Family,
			Timing: session.Timing{Total: sec.Timing.Total, PerItem: sec.Timing.PerItem},
			Items:  items,
		}
	}
	return session.SessionSpec{
		ID:       id,
		TestID:   string(test.ID),
		Policy:   session.Policy(test.Policy),
		Timing:   session.Timing{Total: test.Timing.Total, PerItem: test.Timing.PerItem},
		Sections: sections,
	}
}
