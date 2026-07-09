package main

import "net/http"

// handleListJobs returns the recent async ingest jobs, newest first, paginated.
func (s *server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		writeJSON(w, http.StatusOK, paginate([]job{}, 0, 0))
		return
	}
	limit, offset, ok := s.pageParams(w, r, r.URL.Query())
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, paginate(s.jobs.list(), limit, offset))
}

// handleGetJob returns one job by id, or 404 when the registry never held it (or
// pruned it — jobs are ephemeral by design, ADR-0007).
func (s *server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.jobs != nil {
		if j, ok := s.jobs.get(id); ok {
			writeJSON(w, http.StatusOK, j)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{
		"error": "job not found (jobs are in-memory and lost on restart)",
		"code":  "server.job_not_found",
	})
}
