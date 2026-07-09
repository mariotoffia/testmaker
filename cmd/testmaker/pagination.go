package main

import (
	"net/http"
	"net/url"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 500
)

// pageEnvelope is the paginated collection response (DESIGN §7 / C5): the slice
// for this page plus the total match count and the applied window. A cmd-local
// wire type, so camelCase — distinct from the PascalCase domain snapshots it
// carries.
type pageEnvelope[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// paginate slices all to the requested window after clamping (limit → [1,500]
// default 50; offset → [0,total]) and guarantees a non-nil Items slice so the
// JSON is always [] not null.
func paginate[T any](all []T, limit, offset int) pageEnvelope[T] {
	total := len(all)
	switch {
	case limit <= 0:
		limit = defaultPageLimit
	case limit > maxPageLimit:
		limit = maxPageLimit
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := min(offset+limit, total)
	items := append([]T{}, all[offset:end]...)
	return pageEnvelope[T]{Items: items, Total: total, Limit: limit, Offset: offset}
}

// pageParams reads and clamps limit/offset from the query. A non-integer value
// writes a 400 via intParam (routed through the safe s.writeError) and returns
// ok=false so the handler bails. It is a *server method because intParam is —
// they share the same error path.
func (s *server) pageParams(w http.ResponseWriter, r *http.Request, q url.Values) (limit, offset int, ok bool) {
	limit, ok = s.intParam(w, r, q, "limit")
	if !ok {
		return 0, 0, false
	}
	offset, ok = s.intParam(w, r, q, "offset")
	if !ok {
		return 0, 0, false
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset, true
}
