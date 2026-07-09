package main

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/app/ingest"
	"github.com/mariotoffia/testmaker/domain/clock"
)

func counterIDs() func() string {
	n := 0
	return func() string { n++; return "j-" + strconv.Itoa(n) }
}

func TestJobLifecycle(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	reg := newJobRegistry(clk, 100, counterIDs())

	j := reg.create("ingest-llm", "src-1")
	if j.ID != "j-1" || j.State != "queued" || j.SourceID != "src-1" {
		t.Fatalf("created job = %+v", j)
	}
	if !j.CreatedAt.Equal(clk.Now()) {
		t.Fatal("createdAt must be the injected clock reading")
	}

	clk.Advance(time.Second)
	reg.start("j-1")
	clk.Advance(2 * time.Second)
	reg.finish("j-1", &ingest.Report{Saved: 5}, nil)

	got, ok := reg.get("j-1")
	if !ok || got.State != "done" || got.Report == nil || got.Report.Saved != 5 {
		t.Fatalf("finished job = %+v (ok=%v)", got, ok)
	}
	if !got.StartedAt.After(got.CreatedAt) || !got.EndedAt.After(got.StartedAt) {
		t.Fatal("timestamps must advance created < started < ended")
	}
}

func TestJobFailureRecordsError(t *testing.T) {
	reg := newJobRegistry(clock.System(), 100, counterIDs())
	reg.create("ingest", "s")
	reg.start("j-1")
	reg.finish("j-1", nil, errors.New("fetch failed: boom"))
	got, _ := reg.get("j-1")
	if got.State != "failed" || got.Error == "" {
		t.Fatalf("failed job = %+v", got)
	}
}

func TestJobListNewestFirstAndBounded(t *testing.T) {
	reg := newJobRegistry(clock.System(), 3, counterIDs())
	for i := 0; i < 5; i++ {
		id := reg.create("ingest", "s").ID
		reg.finish(id, &ingest.Report{}, nil) // terminal so it is prunable
	}
	list := reg.list()
	if len(list) > 3 {
		t.Fatalf("registry kept %d jobs, want ≤ 3 (bounded)", len(list))
	}
	if len(list) >= 2 && list[0].ID <= list[1].ID {
		t.Fatal("list must be newest-first (descending create order)")
	}
}
