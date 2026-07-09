package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mariotoffia/testmaker/domain/shared"
)

// maxRequestBody caps a request body on this unauthenticated surface so a large
// or slow-drip POST cannot exhaust memory; the JSON payloads here are all small.
const maxRequestBody = 1 << 20 // 1 MiB

// writeJSON encodes v as the response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps a domain error class onto an HTTP status and writes a SAFE
// body — the TestmakerError's Message + Code + Class only — while logging the
// full cause chain (which may carry paths/backend URLs) to slog. An
// unclassified error becomes a generic 500 with no leaked message. This is the
// single translation point between shared.TestmakerError and the transport
// (DESIGN §7.6 / C4).
func (s *server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var terr *shared.TestmakerError
	if !errors.As(err, &terr) {
		s.logger().Error("unhandled error", "method", r.Method, "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal error", "code": "internal",
		})
		return
	}
	status := http.StatusInternalServerError
	switch terr.Class {
	case shared.ClassInvalid:
		status = http.StatusBadRequest
	case shared.ClassNotFound:
		status = http.StatusNotFound
	case shared.ClassConflict:
		status = http.StatusConflict
	case shared.ClassUnavailable:
		status = http.StatusServiceUnavailable
	case shared.ClassUnsupported:
		status = http.StatusNotImplemented
	}
	if status >= 500 {
		s.logger().Error("server error", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, map[string]string{
		"error": terr.Message, "code": string(terr.Code), "class": string(terr.Class),
	})
}

// logger returns the server's logger, or a discard logger if none was wired
// (zero-value server in a unit test).
func (s *server) logger() *slog.Logger {
	if s.log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return s.log
}

// decodeJSON reads the request body into dst, writing a 400 and returning false
// on malformed or over-large input so a handler can bail with a single guard.
func (s *server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		s.writeError(w, r, fmt.Errorf("%w: %s", shared.ErrInvalid, err))
		return false
	}
	return true
}

// decodeOptionalJSON decodes an optional JSON body: an empty body leaves dst at
// its zero value (ingest's limit/model are all optional, so a bodyless POST is
// valid). Malformed JSON is a 400.
func (s *server) decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		s.writeError(w, r, fmt.Errorf("%w: %s", shared.ErrInvalid, err))
		return false
	}
	return true
}

// intParam reads an optional integer query parameter: absent is (0, true); a
// non-integer value writes a 400 and returns (0, false) so the handler can bail.
func (s *server) intParam(w http.ResponseWriter, r *http.Request, q url.Values, key string) (int, bool) {
	v := q.Get(key)
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		s.writeError(w, r, shared.ErrInvalid.WithMessagef("query %q must be an integer, got %q", key, v))
		return 0, false
	}
	return n, true
}
