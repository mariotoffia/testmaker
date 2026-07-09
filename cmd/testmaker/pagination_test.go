package main

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPaginateClampsAndSlices(t *testing.T) {
	all := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	p := paginate(all, 3, 2)
	if p.Total != 10 || p.Limit != 3 || p.Offset != 2 {
		t.Fatalf("meta = %+v", p)
	}
	if len(p.Items) != 3 || p.Items[0] != 2 || p.Items[2] != 4 {
		t.Fatalf("items = %v", p.Items)
	}
	// Offset past the end yields an empty (non-nil) slice, total preserved.
	e := paginate(all, 5, 100)
	if e.Total != 10 || len(e.Items) != 0 || e.Items == nil {
		t.Fatalf("past-end page = %+v (items must be [] not null)", e)
	}
	// limit<=0 → 50; limit>500 → 500.
	if paginate(all, 0, 0).Limit != 50 || paginate(all, 9999, 0).Limit != 500 {
		t.Fatal("limit clamp wrong")
	}
	// Negative offset clamps to 0.
	if paginate(all, 2, -5).Offset != 0 {
		t.Fatal("negative offset must clamp to 0")
	}
}

func TestPageParamsDefaults(t *testing.T) {
	srv := &server{} // zero value: intParam's error path uses a discard logger
	rec := httptest.NewRecorder()

	q, _ := url.ParseQuery("") // no limit/offset
	limit, offset, ok := srv.pageParams(rec, httptest.NewRequest("GET", "/api/items", nil), q)
	if !ok || limit != 50 || offset != 0 {
		t.Fatalf("defaults = (%d,%d,%v)", limit, offset, ok)
	}

	q2, _ := url.ParseQuery("limit=10&offset=5")
	limit, offset, ok = srv.pageParams(rec, httptest.NewRequest("GET", "/api/items", nil), q2)
	if !ok || limit != 10 || offset != 5 {
		t.Fatalf("parsed = (%d,%d,%v)", limit, offset, ok)
	}

	// A non-integer limit is a 400 and bails (ok=false), same gate as intParam.
	q3, _ := url.ParseQuery("limit=abc")
	if _, _, ok := srv.pageParams(rec, httptest.NewRequest("GET", "/api/items", nil), q3); ok {
		t.Fatal("non-integer limit must return ok=false")
	}
}
