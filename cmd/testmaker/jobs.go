package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/clock"
)

// job is a delivery-surface view on one async ingest run (ADR-0007). It is a
// cmd-local wire type (camelCase) that embeds the PascalCase ingest.Report
// untouched. It carries no domain identity — the durable outcome is the bank
// the run writes; a job is lost on restart by design.
type job struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"` // "ingest" | "ingest-llm"
	SourceID  string         `json:"sourceId"`
	State     string         `json:"state"` // queued | running | done | failed
	Report    *ingest.Report `json:"report,omitempty"`
	Error     string         `json:"error,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	StartedAt time.Time      `json:"startedAt"`
	EndedAt   time.Time      `json:"endedAt"`
}

func (j job) terminal() bool { return j.State == "done" || j.State == "failed" }

// jobRegistry holds the recent async ingest jobs in memory, newest-bounded. The
// clock is injected so lifecycles are deterministic under test; all access is
// mutex-guarded and returns copies so a caller never races the background run
// mutating the same job.
type jobRegistry struct {
	mu    sync.Mutex
	jobs  map[string]*job
	order []string // create order (oldest first) for pruning + newest-first listing
	max   int
	clk   clock.Clock
	idFn  func() string
}

func newJobRegistry(clk clock.Clock, maxJobs int, idFn func() string) *jobRegistry {
	if idFn == nil {
		idFn = randomJobID
	}
	return &jobRegistry{jobs: make(map[string]*job), max: maxJobs, clk: clk, idFn: idFn}
}

func randomJobID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "j-" + hex.EncodeToString(b)
}

func (r *jobRegistry) create(kind, sourceID string) job {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := &job{ID: r.idFn(), Kind: kind, SourceID: sourceID, State: "queued", CreatedAt: r.clk.Now()}
	r.jobs[j.ID] = j
	r.order = append(r.order, j.ID)
	r.pruneLocked()
	return *j
}

func (r *jobRegistry) start(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j := r.jobs[id]; j != nil {
		j.State = "running"
		j.StartedAt = r.clk.Now()
	}
}

func (r *jobRegistry) finish(id string, rep *ingest.Report, runErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[id]
	if j == nil {
		return
	}
	j.EndedAt = r.clk.Now()
	if runErr != nil {
		j.State, j.Error = "failed", runErr.Error()
		return
	}
	j.State, j.Report = "done", rep
}

func (r *jobRegistry) get(id string) (job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j := r.jobs[id]; j != nil {
		return *j, true
	}
	return job{}, false
}

// list returns copies of the jobs, newest first.
func (r *jobRegistry) list() []job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]job, 0, len(r.order))
	for i := len(r.order) - 1; i >= 0; i-- {
		if j := r.jobs[r.order[i]]; j != nil {
			out = append(out, *j)
		}
	}
	return out
}

// pruneLocked evicts the oldest terminal jobs while over capacity, so an
// in-flight job is never dropped. ponytail: a running job pins a slot; at the
// default cap (100) that never bites, and durable job history is ROADMAP §2.
func (r *jobRegistry) pruneLocked() {
	for len(r.jobs) > r.max {
		evicted := false
		for i, id := range r.order {
			if j := r.jobs[id]; j != nil && j.terminal() {
				delete(r.jobs, id)
				r.order = append(r.order[:i], r.order[i+1:]...)
				evicted = true
				break
			}
		}
		if !evicted {
			return // all remaining jobs are in-flight; let the map grow briefly
		}
	}
}
