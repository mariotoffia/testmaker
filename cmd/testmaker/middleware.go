package main

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// responseRecorder captures the status code an inner handler writes so the
// request-log middleware can report it. A handler that writes a body without an
// explicit WriteHeader leaves status 0; withRequestLog reports that as 200, the
// same default net/http sends the client — so no Write override is needed here.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// withRequestLog logs one line per request (method, path, status, duration,
// remote host) at Info. It wraps the whole mux, so it is the outermost handler.
// The clock is injected (domain/clock) rather than read from the wall clock —
// the same no-hidden-time discipline the executor follows, so request-duration
// stays deterministic under test and forbidigo needs no exception.
func withRequestLog(next http.Handler, log *slog.Logger, clk clock.Clock) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := clk.Now()
		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		log.Info("request",
			"method", r.Method, "path", r.URL.Path,
			"status", rec.status, "durationMs", clk.Now().Sub(start).Milliseconds(),
			"remote", clientIP(r))
	})
}

// clientIP extracts the connecting host (no port). It reads RemoteAddr only —
// proxy headers are untrusted on this surface; a trusted-proxy option is a
// documented later knob (DESIGN §7.4).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
