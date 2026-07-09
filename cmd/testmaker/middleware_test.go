package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/shared"
)

func TestWriteErrorBodyIsSafeAndClassMapped(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/items/x", nil)
	srv := &server{log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	err := shared.ErrNotFound.WithMessage("item \"x\" not found").With("backendURL", "sqlite:///secret/path.db")
	srv.writeError(rec, req, err)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] == "" || body["class"] != string(shared.ClassNotFound) {
		t.Fatalf("body missing code/class: %+v", body)
	}
	if strings.Contains(rec.Body.String(), "secret/path.db") {
		t.Fatal("wire body leaked the error Context (backend path)")
	}
}

func TestWriteErrorUnclassifiedIsGeneric500(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/x", nil)
	srv := &server{log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	srv.writeError(rec, req, errors.New("raw boom with /internal/detail"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "internal/detail") {
		t.Fatal("unclassified error leaked its message to the client")
	}
}

func TestRequestLogMiddlewareLogsStatus(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	h := withRequestLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), log, clock.NewFake(time.Unix(0, 0)))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/ping", nil))
	if !strings.Contains(buf.String(), `"status":418`) || !strings.Contains(buf.String(), "/api/ping") {
		t.Fatalf("request log missing status/path: %s", buf.String())
	}
}

func TestSecurityHeadersOnAPIResponses(t *testing.T) {
	h := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api", nil))
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
