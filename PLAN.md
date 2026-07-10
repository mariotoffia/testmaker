# Web App (Operator Console + Test Player) & Delivery-Surface Hardening — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the initiative formerly known as ROADMAP §1 — an embedded web application (operator console + test player) on top of a hardened `/api` delivery surface (roles/auth, rate + cost limits, pagination, error hygiene, async ingest jobs, catalogue upload) — as designed in [DESIGN.md §7](DESIGN.md), [ARCHITECTURE.md §9](ARCHITECTURE.md), [ADR-0005](docs/adr/0005-embedded-spa-web-ui-served-from-composition-root.md), [ADR-0006](docs/adr/0006-operator-token-and-hmac-capability-tokens.md), [ADR-0007](docs/adr/0007-async-ingest-jobs-in-memory-at-delivery-surface.md).

**Architecture:** Everything server-side lands in the **composition root** (`cmd/testmaker`) — middleware, auth, limits, jobs, SPA serving. **Zero new ports, zero domain changes** (the one adapter touch is extracting `filecatalog.ParseJSON`, a pure refactor of existing wire knowledge). The frontend is a Vite + React + TypeScript SPA in `web/` (not a Go module), built with Bun into `cmd/testmaker/webui/dist` and embedded via `go:embed`; the Go toolchain never depends on Bun (a committed `dist/.keep` placeholder + JSON-index fallback).

**Tech Stack:** Go 1.25 stdlib (`net/http`, `log/slog`, `crypto/hmac`, `embed`) + `golang.org/x/time/rate` (cmd module only). Web: Bun ≥ 1.1, Vite 6, React 18, TypeScript 5, react-router-dom 6, @tanstack/react-query 5, Tailwind CSS 4, Vitest 2 + React Testing Library.

## Global Constraints

These apply to **every** task below (they restate the repo's machine-enforced rules — see [.claude/CLAUDE.md](.claude/CLAUDE.md)):

- **Layering:** `domain ← ports ← app ← adapters ← cmd`. Everything in this plan lives in `cmd/testmaker` or `web/` except Task 16's `filecatalog.ParseJSON` extraction. Never import an adapter from `app`, never `app` from an adapter.
- **Done means:** `make lint` **and** `make test` green after every task (they are steps inside each task, not optional). `make check` is the CI aggregate.
- **Go tests:** stdlib `testing` only; no testify; errors via `errors.Is` on `shared.TestmakerError` sentinels; **no `time.Now`/`time.Sleep`** — inject `domain/clock` (`clock.Fake` in tests); `-race` is always on; per-file limit ≤ 500 lines (`wc -l` before finishing a file — split when close).
- **Web tests:** Vitest + RTL, jsdom, faked `fetch`, `vi.useFakeTimers()` for anything clocked. Run via `make webui-test`, never from `make test`.
- **Wire conventions (encode once, never drift):** domain snapshots marshal **PascalCase as-is** (no response DTOs); request bodies and cmd-local wire types (jobs, invites, pages, errors) are **camelCase**; `time.Time` = RFC3339 (zero value `0001-01-01T00:00:00Z` = "untimed"); `time.Duration` = **nanoseconds** (number).
- **Manually built binaries** get a `.out` suffix (`go build -o /tmp/testmaker.out ./cmd/testmaker`) so `.gitignore` catches them; never commit a binary (the pre-commit hook rejects them).
- **Commits:** one per task minimum, message prefix `Block 14:` (this initiative), imperative mood.
- **Branch:** all work on `block-14-web-app-hardening` off `main` (Task 1 creates it).
- **Config compatibility:** an existing `~/.testmaker/config/config.json` without the new sections must keep working (defaults fill in; secrets generated + persisted once).
- **Auth zero-value semantics:** a zero `Config` (what tests construct) means auth **off**, rate limit **off**, ingest gate **unbounded**, discard logger, system clock — so existing tests only need the `/api` path migration, nothing else.

---

## Design contract (implementation-grade)

[DESIGN.md §7](DESIGN.md) is the narrative; this section pins the exact shapes tasks code against. **If a task and this section disagree, this section wins; fix the task.**

### C1. Config file (complete target shape)

`$TESTMAKER_HOME/config/config.json` after first run in `token` mode:

```json
{
  "testdb": "/home/u/.testmaker/data/testmaker.db",
  "blobs": "/home/u/.testmaker/data/blobs",
  "catalog": "/home/u/.testmaker/data/catalog/sources.json",
  "prompts": "/home/u/.testmaker/data/prompts",
  "llm": {
    "baseURL": "",
    "apiKey": "",
    "model": "",
    "authScheme": "",
    "maxTokensCap": 4096,
    "allowedModels": []
  },
  "auth": {
    "mode": "token",
    "operatorToken": "<b64url 32 bytes>",
    "secret": "<b64url 32 bytes>",
    "inviteTTLSeconds": 86400
  },
  "limits": {
    "requestsPerSecond": 10,
    "burst": 20,
    "maxConcurrentIngests": 1,
    "ingestTimeoutSeconds": 600
  },
  "log": { "level": "info" }
}
```

Defaulting rules (applied on **every** load, so old files keep working):
`auth.mode` "" → `"token"`; `auth.inviteTTLSeconds` 0 → 86400; `limits.*` 0 →
the values above; `log.level` "" → `"info"`; `llm.maxTokensCap` 0 → 4096.
Secrets (`operatorToken`, `secret`) are generated (32 bytes,
`base64.RawURLEncoding`) **only when** `mode == "token"` and empty, then the
file is re-written (0600). `-auth <mode>` flag overrides `auth.mode` for a run.

### C2. Token formats (byte-exact)

```
invite  = "ti." + b64url(JSON{"tid": <testID>, "exp": <unix seconds>}) + "." + sig
session = "ts." + b64url(JSON{"sid": <sessionID>})                    + "." + sig
sig     = b64url( HMAC-SHA256(secret, prefix + "." + payloadB64) )
```

- `b64url` = `base64.RawURLEncoding` (no padding).
- Verification: split on `.` into exactly 3 parts; check prefix; recompute sig
  over `parts[0] + "." + parts[1]`; compare with `hmac.Equal`; then decode the
  payload; invites additionally require `now.Unix() < exp`.
- Operator token comparison: `subtle.ConstantTimeCompare` against the config
  value.
- Bearer extraction: `Authorization: Bearer <token>` only (case-insensitive
  scheme match); anything else = anonymous.

### C3. Roles → endpoints (authoritative matrix)

| Pattern | Role rule |
| --- | --- |
| `GET /api`, `GET /api/auth/whoami`, `GET /api/media/{ref}` | public |
| `GET /api/sources`, `GET /api/sources/{id}`, `POST /api/catalog`, `POST /api/catalog/sync`, `GET /api/items`, `GET /api/items/{id}`, `POST /api/sources/{id}/ingest`, `POST /api/sources/{id}/ingest-llm`, `GET /api/jobs`, `GET /api/jobs/{id}`, `POST /api/items/generate`, `POST /api/tests`, `GET /api/tests`, `GET /api/tests/{id}`, `POST /api/tests/{id}/sessions`, `POST /api/tests/{id}/invites` | operator only |
| `GET /api/invites/preview`, `POST /api/invites/start` | valid invite token (operator token NOT accepted — an operator starts via `/api/tests/{id}/sessions`) |
| `POST /api/sessions/{id}/answers`, `POST /api/sessions/{id}/complete`, `GET /api/sessions/{id}/score` | session token whose `sid` == path `{id}`, **or** operator |
| everything else (no `/api` prefix) | public → SPA handler |

Auth failures are transport-native JSON (never `TestmakerError`):
`401 {"error":"authentication required","code":"auth.required"}` (missing/invalid token, expired invite),
`403 {"error":"forbidden","code":"auth.forbidden"}` (valid token, wrong role/session).
`WWW-Authenticate: Bearer` header on 401.

### C4. Error body (every non-2xx from handlers)

```json
{ "error": "<TestmakerError.Message — safe>", "code": "<TestmakerError.Code>", "class": "<Class>" }
```

Non-`TestmakerError` → `500 {"error":"internal error","code":"internal"}` (no
class). The **full** error chain goes to `slog` at `Error` level with method +
path. Status map is unchanged: invalid→400, not_found→404, conflict→409,
unavailable→503, unsupported→501, else 500. Middleware-native statuses: 401,
403, 429.

### C5. Page envelope

```json
{ "items": [ … ], "total": 123, "limit": 50, "offset": 0 }
```

`limit` query param: ≤0 or absent → 50; > 500 → 500. `offset` < 0 → 0. `total`
= matches **before** slicing. Sort: items/sources/tests ascending by `ID`;
jobs descending by `createdAt`. Applies to `GET /api/items`, `GET /api/sources`,
`GET /api/tests`, `GET /api/jobs`.

### C6. Job wire shape (camelCase, cmd-local)

```json
{
  "id": "j-9f2c41d8a03b",
  "kind": "ingest-llm",
  "sourceId": "openpsych-viqt",
  "state": "running",
  "report": { "SourceID": "openpsych-viqt", "Fetched": 2, "Normalized": 40, "Saved": 38, "Skipped": 2, "Note": "…" },
  "error": "",
  "createdAt": "2026-07-09T12:00:00Z",
  "startedAt": "2026-07-09T12:00:00Z",
  "endedAt": "0001-01-01T00:00:00Z"
}
```

`state ∈ queued|running|done|failed`; `report` is the untouched PascalCase
`ingest.Report` (present when done); registry keeps the newest 100 jobs.

### C7. Invite wire shapes (camelCase)

```
POST /api/tests/{id}/invites  {"expiresInSeconds": 3600}          // 0/absent → config TTL
  → 201 {"token":"ti.…", "url":"/take#ti.…", "expiresAt":"2026-07-09T13:00:00Z"}

GET /api/invites/preview   (Bearer invite)
  → 200 {"testId":"t1","title":"Composite Aptitude","policy":"fixed-increasing",
         "totalSeconds":1200,"perItemSeconds":0,"itemCount":45,
         "sections":[{"title":"Logical","family":"logical","itemCount":20,"totalSeconds":600,"perItemSeconds":60}],
         "expiresAt":"2026-07-09T13:00:00Z"}     // NO item ids, NO difficulty bands

POST /api/invites/start    (Bearer invite)
  → 201 {"Session":{…},"Item":{…redacted…},"Deadline":"…","SessionToken":"ts.…"}
       // ports.Delivery embedded (PascalCase) + SessionToken
```

`POST /api/tests/{id}/sessions` (operator) returns the same start shape.

### C8. SPA serving rules

- Registered `/api` patterns always win (Go 1.22 ServeMux precedence); the SPA
  handler owns `GET /`.
- Build present (`webui.FS()` ok): exact embedded file if it exists —
  `/assets/*` with `Cache-Control: public, max-age=31536000, immutable` — else
  `index.html` with `Cache-Control: no-store` and CSP
  `default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'`.
- No build: `GET /` → the JSON service index (today's `handleIndex` body);
  any other non-`/api` path → 404 JSON.
- Global security headers (all responses): `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`. `GET /api/media/{ref}`
  keeps its stricter sandbox CSP (unchanged).

### C9. TypeScript wire types (the single source: `web/src/api/types.ts`)

Domain snapshots are PascalCase; nullable Go slices arrive as `null` — model as
`T[] | null`. Durations are nanoseconds. The full file is written in Task 20;
its shapes are quoted throughout Phases 7–8. Key aliases:

```ts
export type Ns = number;                       // Go time.Duration on the wire
export const NS_PER_MS = 1_000_000;
export type AnswerFormat = "multiple-choice" | "open-numeric" | "true-false-cannotsay";
export type SessionState = "created" | "in-progress" | "completed" | "abandoned";
```

### C10. Player timing rules

- Per-item deadline = `Delivery.Deadline` (RFC3339; zero time = untimed).
- Global deadline = `Session.StartedAt + Session.Timing.Total` (Total 0 = untimed).
- Client skew: `skewMs = Date.parse(response.headers.Date) - Date.now()`,
  captured on every API response; `remainingMs = deadlineMs - (Date.now() + skewMs)`.
- Per-item expiry → **auto-submit the current selection** (empty `{}` allowed —
  records as wrong; verified server-side by `graded()` semantics).
- 409 on answer = another writer won → "continue in your other tab" screen
  (client also disables double-submit while in flight). Refresh-resume uses the
  `sessionStorage`-persisted last Delivery + token (a server-side resume verb is
  deliberately out of scope — ROADMAP §6 territory).

### C11. Task → phase index

| Phase | Tasks | Delivers |
| --- | --- | --- |
| 0 Scaffolding | 1–3 | branch, `webui` embed package, `/api` re-base + SPA fallback, Makefile/lint/git plumbing |
| 1 Config & observability | 4–6 | config sections + secrets, slog + request log + safe errors, security headers |
| 2 Auth | 7–10 | authenticator, role middleware + whoami, invites/start, full-flow token test |
| 3 Limits | 11–13 | per-IP rate limit, ingest semaphore, LLM clamp hook |
| 4 Console API | 14–16 | pagination envelopes, `GET /api/tests`, `POST /api/catalog` |
| 5 Jobs | 17–18 | job registry, async ingest + poll endpoints |
| 6 Web foundation | 19–22 | `web/` scaffold + build/embed loop, API client + types, auth context + login, app shell |
| 7 Console UI | 23–29 | dashboard, sources+catalogue, ingest+jobs, items browser/preview, generate, compose+tests, invites |
| 8 Player UI | 30–35 | take flow, countdowns, item view + formats + keyboard, auto-submit, score view, edge states |
| 9 Finish | 36–38 | E2E smoke + serve-all, CI web job, doc status flips + final check |

---

# Phase 0 — Scaffolding

### Task 1: Branch + `webui` embed package (build-optional fallback) ✅

**Files:**
- Create: `cmd/testmaker/webui/doc.go`
- Create: `cmd/testmaker/webui/webui.go`
- Create: `cmd/testmaker/webui/webui_test.go`
- Create: `cmd/testmaker/webui/dist/.keep` (empty file, committed)
- Modify: `.gitignore` (root)

**Interfaces:**
- Consumes: nothing (leaf package; stdlib `embed`, `io/fs`).
- Produces: `webui.FS() (fs.FS, bool)` — the embedded `dist` filesystem and
  whether a real build (an `index.html`) is present. Task 2's SPA handler and
  Task 19's build integration consume exactly this signature.

- [ ] **Step 1: Create the branch**

```bash
git checkout main && git pull && git checkout -b block-14-web-app-hardening
```

- [ ] **Step 2: Write the failing test**

`cmd/testmaker/webui/webui_test.go`:

```go
package webui_test

import (
	"io/fs"
	"testing"

	"github.com/mariotoffia/testmaker/cmd/testmaker/webui"
)

func TestFSWithoutBuildReportsNotOK(t *testing.T) {
	sub, ok := webui.FS()
	if sub == nil {
		t.Fatal("FS() filesystem must never be nil")
	}
	// A checkout without `make webui` has only the committed placeholder, so
	// no index.html and ok must be false. (After a local build this test still
	// passes the nil/consistency checks below and skips the not-ok assertion.)
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		if ok {
			t.Fatal("ok must be false when dist has no index.html")
		}
	} else if !ok {
		t.Fatal("ok must be true when dist/index.html exists")
	}
}
```

- [ ] **Step 3: Run it to make sure it fails**

```bash
cd cmd/testmaker && go test ./webui/ -run TestFSWithoutBuild -v
```
Expected: FAIL — `no required module provides package .../webui` (package does not exist yet).

- [ ] **Step 4: Write the package**

`cmd/testmaker/webui/doc.go`:

```go
// Package webui embeds the built web application (operator console + test
// player; DESIGN.md §7.1, ADR-0005). The dist directory is produced by
// `make webui` (web/ source built by Vite); the committed dist/.keep
// placeholder keeps the go:embed pattern valid on a checkout with no UI
// build, in which case FS reports ok=false and the delivery surface falls
// back to the JSON index.
package webui
```

`cmd/testmaker/webui/webui.go`:

```go
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built UI rooted at dist and whether a real build is present
// (an index.html exists). Callers must treat ok=false as "no UI shipped" and
// fall back; the returned FS is still valid (it holds the placeholder).
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Unreachable: "dist" is a compiled-in directory. Return the root so
		// the contract (never-nil FS) holds even here.
		return dist, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return sub, false
	}
	return sub, true
}
```

Create the placeholder (empty file):

```bash
touch cmd/testmaker/webui/dist/.keep
```

- [ ] **Step 5: gitignore the built assets (placeholder stays tracked)**

Append to root `.gitignore`:

```gitignore
# Web app: source deps + built assets (the embed placeholder .keep is tracked)
/web/node_modules/
/web/dist/
/cmd/testmaker/webui/dist/*
!/cmd/testmaker/webui/dist/.keep
```

- [ ] **Step 6: Run the test to verify it passes**

```bash
cd cmd/testmaker && go test ./webui/ -v
```
Expected: PASS (`ok=false` branch — only `.keep` is embedded).

- [ ] **Step 7: Full gates**

```bash
make lint && make test
```
Expected: both green (`webui` is ordinary `cmd/**` for go-arch-lint; no new component needed).

- [ ] **Step 8: Commit**

```bash
git add cmd/testmaker/webui .gitignore
git commit -m "Block 14: webui go:embed package with build-optional fallback"
```

---

### Task 2: Re-base the JSON API under `/api` + SPA fallback handler ✅

The one deliberately breaking change (ADR-0005). After this task every JSON
endpoint lives under `/api`, `GET /api` serves the index, and `GET /` serves
the SPA when built / the JSON index when not.

**Files:**
- Modify: `cmd/testmaker/server.go` (routes, index, media stays as-is otherwise)
- Create: `cmd/testmaker/server_spa.go`
- Create: `cmd/testmaker/server_spa_test.go`
- Modify: `cmd/testmaker/server_test.go` (paths only)
- Modify: `cmd/testmaker/server_sourcing_test.go` (paths only)
- Modify: `cmd/testmaker/main_test.go`, `cmd/testmaker/extract_llm_test.go`, `cmd/testmaker/ingest_block13_test.go` (only if they hit HTTP paths — grep first)

**Interfaces:**
- Consumes: `webui.FS() (fs.FS, bool)` (Task 1).
- Produces: route table exactly as C3's pattern list (pre-auth: no role
  enforcement yet); `handleSPA(w, r)`; `handleIndex` now registered at
  `GET /api`. Every later server task registers new routes inside
  `(*server).routes()` using the `/api` prefix.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/testmaker/server_spa_test.go`:

```go
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSPATestServer stands up the surface with zero-value config: memory
// backends, no auth, no limits — the baseline every pre-auth test uses.
func newSPATestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, err := openBlobStore("memory")
	if err != nil {
		t.Fatalf("openBlobStore: %v", err)
	}
	ts := httptest.NewServer(newServer(serverDeps{db: db, blobs: blobs}).routes())
	t.Cleanup(ts.Close)
	return ts
}

func TestAPIIndexLivesUnderAPI(t *testing.T) {
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/api")
	if err != nil {
		t.Fatalf("GET /api: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api = %d, want 200", res.StatusCode)
	}
	var body struct {
		Service   string   `json:"service"`
		Endpoints []string `json:"endpoints"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Service != "testmaker" || len(body.Endpoints) == 0 {
		t.Fatalf("unexpected index body: %+v", body)
	}
	for _, e := range body.Endpoints {
		if !strings.Contains(e, "/api/") && e != "GET /api" {
			t.Fatalf("endpoint %q not under /api", e)
		}
	}
}

func TestRootFallsBackToJSONIndexWithoutUIBuild(t *testing.T) {
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	// A checkout with a locally built UI serves HTML here; both are legal.
	ct := res.Header.Get("Content-Type")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", res.StatusCode)
	}
	if !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / content-type = %q, want json (no build) or html (built)", ct)
	}
}

func TestUnknownNonAPIPathIs404WithoutUIBuild(t *testing.T) {
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/no/such/page")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	// Without a build: 404 JSON. With a locally built UI: 200 index.html
	// (client-side route). Assert the two legal outcomes only.
	if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusOK {
		t.Fatalf("GET /no/such/page = %d (%s), want 404 (no build) or 200 (built)", res.StatusCode, string(b))
	}
}

func TestOldRootEndpointsAreGone(t *testing.T) {
	ts := newSPATestServer(t)
	res, err := http.Get(ts.URL + "/sources")
	if err != nil {
		t.Fatalf("GET /sources: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		t.Fatal("GET /sources must no longer be served at the root (moved to /api/sources)")
	}
}
```

- [ ] **Step 2: Run them to make sure they fail**

```bash
cd cmd/testmaker && go test . -run 'TestAPIIndex|TestRootFallsBack|TestUnknownNonAPI|TestOldRoot' -v
```
Expected: FAIL — `/api` returns 404, `/sources` returns 200.

- [ ] **Step 3: Rewrite `routes()` and `handleIndex` in `server.go`**

Replace the whole `routes()` and `handleIndex` functions with:

```go
// routes maps the delivery verbs onto the use-cases, all under the /api
// prefix (ADR-0005); everything else falls through to the SPA handler. The
// patterns use the Go 1.22 method+path router — registered /api patterns are
// more specific than "GET /", so they always win over the SPA catch-all.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api", s.handleIndex)
	mux.HandleFunc("POST /api/items/generate", s.handleGenerate)
	mux.HandleFunc("POST /api/tests", s.handleCompose)
	mux.HandleFunc("GET /api/tests/{id}", s.handleGetTest)
	mux.HandleFunc("POST /api/tests/{id}/sessions", s.handleStartSession)
	mux.HandleFunc("POST /api/sessions/{id}/answers", s.handleAnswer)
	mux.HandleFunc("POST /api/sessions/{id}/complete", s.handleComplete)
	mux.HandleFunc("GET /api/sessions/{id}/score", s.handleScore)
	mux.HandleFunc("GET /api/media/{ref}", s.handleMedia)
	// Sourcing front half: catalogue, ingest, and item-bank query.
	mux.HandleFunc("GET /api/sources", s.handleListSources)
	mux.HandleFunc("GET /api/sources/{id}", s.handleGetSource)
	mux.HandleFunc("POST /api/catalog/sync", s.handleSyncCatalog)
	mux.HandleFunc("GET /api/items", s.handleListItems)
	mux.HandleFunc("GET /api/items/{id}", s.handleGetItem)
	mux.HandleFunc("POST /api/sources/{id}/ingest", s.handleIngest)
	mux.HandleFunc("POST /api/sources/{id}/ingest-llm", s.handleIngestLLM)
	// Everything that is not /api is the web app (or the JSON fallback).
	mux.HandleFunc("GET /", s.handleSPA)
	return mux
}

// handleIndex serves the JSON service index at GET /api: proof of life plus
// the endpoint list. It is also the GET / fallback body when no UI is built.
func (s *server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "testmaker",
		"status":  "ok",
		"endpoints": []string{
			"GET /api",
			"GET /api/sources", "GET /api/sources/{id}", "POST /api/catalog/sync",
			"GET /api/items", "GET /api/items/{id}",
			"POST /api/sources/{id}/ingest", "POST /api/sources/{id}/ingest-llm",
			"POST /api/items/generate", "POST /api/tests", "GET /api/tests/{id}",
			"POST /api/tests/{id}/sessions", "POST /api/sessions/{id}/answers",
			"POST /api/sessions/{id}/complete", "GET /api/sessions/{id}/score",
			"GET /api/media/{ref}",
		},
	})
}
```

(Later tasks append their routes and index entries here; the plan quotes each
addition in its own task.)

- [ ] **Step 4: Write the SPA handler**

`cmd/testmaker/server_spa.go`:

```go
package main

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/mariotoffia/testmaker/cmd/testmaker/webui"
)

// spaCSP locks the app down to same-origin resources; data: images cover the
// generator's inline SVG previews. The media endpoint keeps its own, stricter
// sandbox CSP (ADR-0003) — this policy is only for SPA documents.
const spaCSP = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'"

// handleSPA serves the embedded web app: the exact file when it exists
// (hashed /assets/* immutable), index.html for anything else so client-side
// routes deep-link (ADR-0005 / DESIGN §7.1). Without a UI build it degrades
// to the JSON index at "/" and JSON 404s elsewhere, keeping the Go toolchain
// independent of Bun.
func (s *server) handleSPA(w http.ResponseWriter, r *http.Request) {
	ui, ok := webui.FS()
	if !ok {
		if r.URL.Path == "/" {
			s.handleIndex(w, r)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "not found (web app not built; see `make webui`)",
			"code":  "server.not_found",
		})
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if _, err := fs.Stat(ui, name); err != nil {
		// Client-side route (e.g. /take, /items/123): serve the app shell.
		name = "index.html"
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", spaCSP)
	} else if strings.HasPrefix(name, "assets/") {
		// Vite emits content-hashed filenames under assets/ — safe to cache forever.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, ui, name)
}
```

- [ ] **Step 5: Migrate every test path (mechanical)**

```bash
cd cmd/testmaker
grep -rln 'ts.URL+"/\|ts.URL + "/' *_test.go
# For each hit, prefix the path with /api (media included):
#   "/sources" → "/api/sources"      "/items…" → "/api/items…"
#   "/catalog/sync" → "/api/catalog/sync"
#   "/tests…" → "/api/tests…"        "/sessions…" → "/api/sessions…"
#   "/media/" → "/api/media/"        "/items/generate" → "/api/items/generate"
# Do NOT touch httptest backends that fake EXTERNAL services (the canned LLM
# /v1/chat/completions server, the fake ASVAB site) — only testmaker's own URLs.
```

- [ ] **Step 6: Run the full package tests**

```bash
cd cmd/testmaker && go test . ./webui/ -short -race
```
Expected: PASS, including the four new tests.

- [ ] **Step 7: Full gates**

```bash
make lint && make test
```
Expected: green. `wc -l cmd/testmaker/server.go` — must stay ≤ 500 (it starts
~453 and this task adds ~10 net lines; Task 5 will carve helpers out into
`server_http.go` before the file can grow past the cap).

- [ ] **Step 8: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: re-base delivery API under /api and add SPA fallback handler"
```

---

### Task 3: Makefile web targets, arch-lint exclude, CLI `-auth` flag stub ✅

Build plumbing so every later task has its commands. (The `-auth` flag is
wired here as a no-op override slot; Task 4 gives it meaning.)

**Files:**
- Modify: `Makefile`
- Modify: `.go-arch-lint.yml` (exclude `web`)
- Modify: `cmd/testmaker/main.go` (flag + override plumbing only)

**Interfaces:**
- Consumes: nothing new.
- Produces: `make webui | webui-dev | webui-test | webui-lint | serve-all`;
  `-auth <mode>` flag reaching `serveWithConfig`'s override callback (Task 4
  reads `cfg.Auth.Mode`).

- [ ] **Step 1: Makefile targets** — append after the `serve` target:

```make
# Web app (operator console + test player). Bun is OPTIONAL: every Go target
# works without it; these targets are the only ones that need it.
WEB_DIR := web

## webui: build the web app into cmd/testmaker/webui/dist (requires bun)
.PHONY: webui
webui:
	cd $(WEB_DIR) && bun install --frozen-lockfile && bun run build
	@touch cmd/testmaker/webui/dist/.keep

## webui-dev: run the Vite dev server (HMR), proxying /api to localhost:8080
.PHONY: webui-dev
webui-dev:
	cd $(WEB_DIR) && bun install && bun run dev

## webui-test: run the web unit/component tests (Vitest)
.PHONY: webui-test
webui-test:
	cd $(WEB_DIR) && bun install --frozen-lockfile && bun run test:run

## webui-lint: typecheck the web app (tsc --noEmit)
.PHONY: webui-lint
webui-lint:
	cd $(WEB_DIR) && bun install --frozen-lockfile && bun run typecheck

## serve-all: build the web app, then serve the single binary (SPA + API)
.PHONY: serve-all
serve-all: webui serve
```

(`touch .keep` restores the placeholder Vite's `emptyOutDir` wipes, so a local
build never dirties git.)

- [ ] **Step 2: Exclude `web/` from the arch scan** — in `.go-arch-lint.yml`
`exclude:` list, after `- data`:

```yaml
  - web               # frontend source (Bun/Vite/TS) — no Go inside
```

- [ ] **Step 3: CLI flag** — in `cmd/testmaker/main.go`, next to the other
flag declarations:

```go
	authMode := flag.String("auth", "", `delivery-surface auth mode override: "token" or "none" (default: config)`)
```

and inside the `flag.Visit` switch in the `-serve` branch:

```go
				case "auth":
					cfg.Auth.Mode = *authMode
```

(This will not compile until Task 4 adds `Config.Auth` — that is expected;
finish Step 3 by **stubbing** the field now so the flag lands compilable:)

In `cmd/testmaker/config.go`, add to `Config`:

```go
	Auth AuthConfig `json:"auth"`
```

and below `LLMConfig`:

```go
// AuthConfig configures delivery-surface access control (ADR-0006). Zero value
// = auth off, which is what tests construct; loadOrCreateConfig defaults Mode
// to "token" for real deployments (Task 4 in PLAN.md).
type AuthConfig struct {
	Mode             string `json:"mode"`
	OperatorToken    string `json:"operatorToken"`
	Secret           string `json:"secret"`
	InviteTTLSeconds int    `json:"inviteTTLSeconds"`
}
```

- [ ] **Step 4: Verify**

```bash
make lint && make test && make help | grep webui
```
Expected: green; help lists the four webui targets + serve-all. (`make webui`
itself fails until Task 19 creates `web/` — that is fine and expected.)

- [ ] **Step 5: Commit**

```bash
git add Makefile .go-arch-lint.yml cmd/testmaker
git commit -m "Block 14: web build targets, arch-lint web exclude, -auth flag plumbing"
```

---

# Phase 1 — Config & observability

### Task 4: Config sections + secret generation on first run ✅

**Files:**
- Modify: `cmd/testmaker/config.go`
- Modify: `cmd/testmaker/config_test.go`

**Interfaces:**
- Consumes: `Config`, `AuthConfig` (Task 3 stub), `loadOrCreateConfig`, `writeConfig`.
- Produces: `LimitsConfig`, `LogConfig`, extended `LLMConfig`, `Config.Limits`,
  `Config.Log`; `applyConfigDefaults(*Config) (changed bool)`;
  `generateSecret() (string, error)`. Task 5 reads `cfg.Log`; Tasks 7–13 read
  `cfg.Auth` / `cfg.Limits` / `cfg.LLM`. Secrets are `base64.RawURLEncoding` of
  32 `crypto/rand` bytes.

- [ ] **Step 1: Write the failing tests** — append to `config_test.go`:

```go
// TestLoadOrCreateConfigGeneratesSecretsInTokenMode proves a first run in token
// mode mints an operator token + HMAC secret and persists them (0600), and that
// numeric limit/LLM defaults are filled.
func TestLoadOrCreateConfigGeneratesSecretsInTokenMode(t *testing.T) {
	home := t.TempDir()
	cfg, path, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("loadOrCreateConfig: %v", err)
	}
	if cfg.Auth.Mode != "token" {
		t.Fatalf("Auth.Mode = %q, want token (default)", cfg.Auth.Mode)
	}
	if cfg.Auth.OperatorToken == "" || cfg.Auth.Secret == "" {
		t.Fatal("token mode must generate operatorToken and secret")
	}
	if cfg.Auth.OperatorToken == cfg.Auth.Secret {
		t.Fatal("operatorToken and secret must be independently generated")
	}
	if cfg.Auth.InviteTTLSeconds != 86400 {
		t.Errorf("InviteTTLSeconds = %d, want 86400", cfg.Auth.InviteTTLSeconds)
	}
	if cfg.Limits.RequestsPerSecond != 10 || cfg.Limits.Burst != 20 ||
		cfg.Limits.MaxConcurrentIngests != 1 || cfg.Limits.IngestTimeoutSeconds != 600 {
		t.Errorf("limits defaults wrong: %+v", cfg.Limits)
	}
	if cfg.Log.Level != "info" || cfg.LLM.MaxTokensCap != 4096 {
		t.Errorf("log/llm defaults wrong: log=%+v llm.cap=%d", cfg.Log, cfg.LLM.MaxTokensCap)
	}
	// Reload must be stable — same secrets, no rewrite churn.
	cfg2, _, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg2.Auth.OperatorToken != cfg.Auth.OperatorToken || cfg2.Auth.Secret != cfg.Auth.Secret {
		t.Fatal("secrets changed on reload; they must be stable once written")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config perm = %o, want 600 (holds secrets)", perm)
	}
}

// TestApplyConfigDefaultsBackfillsOldFile proves a pre-Block-14 config (no auth/
// limits sections) still loads: defaults fill in and secrets are generated.
func TestApplyConfigDefaultsBackfillsOldFile(t *testing.T) {
	home := t.TempDir()
	old := `{"testdb":"/x.db","blobs":"/b","catalog":"/c.json","prompts":"/p"}`
	path := filepath.Join(home, "config", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("loadOrCreateConfig on old file: %v", err)
	}
	if cfg.TestDB != "/x.db" {
		t.Errorf("existing value lost: TestDB = %q", cfg.TestDB)
	}
	if cfg.Auth.Mode != "token" || cfg.Auth.OperatorToken == "" {
		t.Error("old file must gain token-mode defaults + generated secrets")
	}
}

// TestNoneModeGeneratesNoSecrets proves auth.mode:none neither needs nor mints
// secrets (the trusted-localhost / test posture).
func TestNoneModeGeneratesNoSecrets(t *testing.T) {
	home := t.TempDir()
	seed := Config{Auth: AuthConfig{Mode: "none"}}
	if err := writeConfig(filepath.Join(home, "config", "config.json"), seed); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.OperatorToken != "" || cfg.Auth.Secret != "" {
		t.Fatal("none mode must not generate secrets")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run 'TestLoadOrCreateConfigGenerates|TestApplyConfigDefaults|TestNoneMode' -v
```
Expected: FAIL — fields/functions undefined.

- [ ] **Step 3: Extend the config types** — in `config.go`, extend `LLMConfig`
and add the two structs:

```go
// LLMConfig configures the optional LLM backend used by the ingest-llm endpoint.
// An empty BaseURL means "use the TESTMAKER_LLM_* environment", so an API key can
// stay in the environment instead of on disk. MaxTokensCap/AllowedModels feed the
// server-side clamp hook (DESIGN §7.4).
type LLMConfig struct {
	BaseURL       string   `json:"baseURL"`
	APIKey        string   `json:"apiKey"`
	Model         string   `json:"model"`
	AuthScheme    string   `json:"authScheme"`
	MaxTokensCap  int      `json:"maxTokensCap"`
	AllowedModels []string `json:"allowedModels"`
}

// LimitsConfig bounds mutating cost on the delivery surface (DESIGN §7.4).
type LimitsConfig struct {
	RequestsPerSecond    float64 `json:"requestsPerSecond"`
	Burst                int     `json:"burst"`
	MaxConcurrentIngests int     `json:"maxConcurrentIngests"`
	IngestTimeoutSeconds int     `json:"ingestTimeoutSeconds"`
}

// LogConfig configures structured logging. Level is a slog level name.
type LogConfig struct {
	Level string `json:"level"`
}
```

and add to `Config` (after `LLM`):

```go
	Limits LimitsConfig `json:"limits"`
	Log    LogConfig    `json:"log"`
```

- [ ] **Step 4: Add defaulting + secret generation** — in `config.go`:

```go
import (
	"crypto/rand"
	"encoding/base64"
	// … existing imports …
)

// generateSecret returns 32 cryptographically-random bytes as an unpadded
// base64url string — the operator token and HMAC secret share this shape.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// applyConfigDefaults fills absent fields with their defaults and, in token mode,
// generates any missing secret. It reports whether it changed cfg, so the caller
// can persist a first-run or backfilled file. Existing non-zero values are never
// touched, so an old config keeps working and a user's overrides survive.
func applyConfigDefaults(cfg *Config) (bool, error) {
	changed := false
	set := func(cond bool, apply func()) {
		if cond {
			apply()
			changed = true
		}
	}
	set(cfg.Auth.Mode == "", func() { cfg.Auth.Mode = "token" })
	set(cfg.Auth.InviteTTLSeconds == 0, func() { cfg.Auth.InviteTTLSeconds = 86400 })
	set(cfg.Limits.RequestsPerSecond == 0, func() { cfg.Limits.RequestsPerSecond = 10 })
	set(cfg.Limits.Burst == 0, func() { cfg.Limits.Burst = 20 })
	set(cfg.Limits.MaxConcurrentIngests == 0, func() { cfg.Limits.MaxConcurrentIngests = 1 })
	set(cfg.Limits.IngestTimeoutSeconds == 0, func() { cfg.Limits.IngestTimeoutSeconds = 600 })
	set(cfg.Log.Level == "", func() { cfg.Log.Level = "info" })
	set(cfg.LLM.MaxTokensCap == 0, func() { cfg.LLM.MaxTokensCap = 4096 })

	if cfg.Auth.Mode == "token" {
		if cfg.Auth.OperatorToken == "" {
			tok, err := generateSecret()
			if err != nil {
				return changed, err
			}
			cfg.Auth.OperatorToken, changed = tok, true
		}
		if cfg.Auth.Secret == "" {
			sec, err := generateSecret()
			if err != nil {
				return changed, err
			}
			cfg.Auth.Secret, changed = sec, true
		}
	}
	return changed, nil
}
```

- [ ] **Step 5: Wire defaulting into `loadOrCreateConfig`** — replace the
`err == nil` case body and the `ErrNotExist` case so both run defaults and
persist when changed:

```go
	case err == nil:
		var cfg Config
		if uerr := json.Unmarshal(b, &cfg); uerr != nil {
			return Config{}, path, fmt.Errorf("parse config %s: %w", path, uerr)
		}
		changed, derr := applyConfigDefaults(&cfg)
		if derr != nil {
			return Config{}, path, derr
		}
		if changed {
			if werr := writeConfig(path, cfg); werr != nil {
				return Config{}, path, werr
			}
		}
		return cfg, path, nil
	case errors.Is(err, os.ErrNotExist):
		cfg := defaultConfig(home)
		if _, derr := applyConfigDefaults(&cfg); derr != nil {
			return Config{}, path, derr
		}
		if werr := writeConfig(path, cfg); werr != nil {
			return Config{}, path, werr
		}
		return cfg, path, nil
```

- [ ] **Step 6: Run the tests**

```bash
cd cmd/testmaker && go test . -run 'Config|None' -v
```
Expected: PASS. (`TestLoadOrCreateConfigReadsExisting` still passes — its seed
config has `Auth.Mode` unset, so it now gains token defaults; it only asserts
`TestDB`, so it is unaffected. Confirm.)

- [ ] **Step 7: Full gates** + size check

```bash
make lint && make test && wc -l cmd/testmaker/config.go
```
Expected: green; `config.go` ≤ 500 (≈ 200 lines).

- [ ] **Step 8: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: config auth/limits/log sections + first-run secret generation"
```

---

### Task 5: Structured logging, request-log middleware, safe error bodies ✅

**Files:**
- Create: `cmd/testmaker/middleware.go`
- Create: `cmd/testmaker/middleware_test.go`
- Modify: `cmd/testmaker/server.go` (`server` gains `log *slog.Logger`; `writeError` uses it; new `writeAuthError`)
- Create: `cmd/testmaker/server_http.go` (move `writeJSON`/`writeError`/`decodeJSON` helpers here to keep `server.go` ≤ 500)
- Modify: `cmd/testmaker/server_sourcing.go` (`decodeOptionalJSON`/`intParam` may move with helpers — grep + keep compiling)

**Interfaces:**
- Consumes: `newServer(serverDeps)`, `writeJSON`, `writeError`.
- Produces: `serverDeps.log *slog.Logger` (nil → discard);
  `withRequestLog(next, log, clk) http.Handler`; `responseRecorder`; `clientIP`;
  `(*server).writeError(w, r, err)` (takes `r` + logs full chain, wire body per
  C4); `writeAuthError(w, status, code, msg)`; `(*server).logger()`. Tasks 7–13
  wrap with more middleware; every handler keeps calling `s.writeError`.

- [ ] **Step 1: Write the failing tests** — `cmd/testmaker/middleware_test.go`:

```go
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
	}), log)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/ping", nil))
	if !strings.Contains(buf.String(), `"status":418`) || !strings.Contains(buf.String(), "/api/ping") {
		t.Fatalf("request log missing status/path: %s", buf.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run 'TestWriteError|TestRequestLog' -v
```
Expected: FAIL — `withRequestLog` undefined, `writeError` signature mismatch.

- [ ] **Step 3: Move HTTP helpers into `server_http.go`** — cut `writeJSON`,
`writeError`, `decodeJSON`, `maxRequestBody` from `server.go` and
`decodeOptionalJSON`, `intParam` from `server_sourcing.go` into a new
`cmd/testmaker/server_http.go` (package `main`). Update `writeError` to the new
signature (below). This keeps `server.go` under the 500-line cap as routes grow.

- [ ] **Step 4: Write the middleware + safe errors**

`cmd/testmaker/middleware.go`:

```go
package main

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// responseRecorder captures the status code an inner handler writes so the
// request-log middleware can report it. WriteHeader may be called at most once
// meaningfully; default is 200 if the handler writes a body without a status.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
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
```

No `.golangci.yml` change is needed: injecting `clock.Clock` keeps the wall
clock out of production exactly as the rest of the codebase requires.

- [ ] **Step 5: Rewrite `writeError` (in `server_http.go`)**

```go
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
		"error": terr.Message, "code": terr.Code, "class": string(terr.Class),
	})
}

// writeAuthError writes a transport-native auth/limit failure (401/403/429):
// these are not domain TestmakerErrors, so the closed Class vocabulary is not
// stretched to cover them (DESIGN §7.3).
func writeAuthError(w http.ResponseWriter, status int, code, msg string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// logger returns the server's logger, or a discard logger if none was wired
// (zero-value server in a unit test).
func (s *server) logger() *slog.Logger {
	if s.log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return s.log
}
```

- [ ] **Step 6: Update `server` struct + every `writeError` call**

Add `log *slog.Logger` to the `server` struct and `log *slog.Logger` to
`serverDeps`; set `log: d.log` in `newServer`. Then update **every** handler
call from `writeError(w, err)` to `s.writeError(w, r, err)`:

```bash
cd cmd/testmaker
grep -rn 'writeError(w, err)' *.go        # migrate each to s.writeError(w, r, err)
grep -rn 'writeError(w, ' *.go            # incl. the WithMessagef and errLLMUnconfigured call sites
```

Wire the logger in `runServer` (build from `cfg.Log.Level`) and wrap the mux
(the middleware clock is `clock.System()` — the same source the executor uses):

```go
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(cfg.Log.Level)) // unknown → Info
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	srv := newServer(serverDeps{ db: db, blobs: blobs, catalog: cat, ingest: ing, llm: llmSvc, llmModel: llmModel, log: logger })
	handler := withRequestLog(srv.routes(), logger, clock.System())
	httpSrv := &http.Server{ Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second }
```

- [ ] **Step 7: Run tests + gates**

```bash
cd cmd/testmaker && go test . -short -race
make lint && make test && wc -l cmd/testmaker/server.go cmd/testmaker/server_http.go cmd/testmaker/middleware.go
```
Expected: green; every file ≤ 500.

- [ ] **Step 8: Commit**

```bash
git add cmd/testmaker .golangci.yml
git commit -m "Block 14: structured logging, request-log middleware, safe error bodies"
```

---

### Task 6: Baseline security headers middleware ✅

**Files:**
- Modify: `cmd/testmaker/middleware.go`
- Modify: `cmd/testmaker/middleware_test.go`
- Modify: `cmd/testmaker/server.go` (`runServer` wraps with the new layer)

**Interfaces:**
- Consumes: `withRequestLog`.
- Produces: `withSecurityHeaders(next) http.Handler`. Media handler's stricter
  CSP is untouched (it sets its own headers after this runs).

- [ ] **Step 1: Failing test** — append to `middleware_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run TestSecurityHeaders -v
```
Expected: FAIL — `withSecurityHeaders` undefined.

- [ ] **Step 3: Implement** — append to `middleware.go`:

```go
// withSecurityHeaders sets baseline hardening headers on every response. The
// SPA handler adds its Content-Security-Policy per document and GET /api/media
// keeps its stricter sandbox CSP (ADR-0003); those set their own headers after
// this middleware runs, so they are not overridden here.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Wire it** — in `runServer`, nest inside the request log
(outermost log → security → mux):

```go
	handler := withRequestLog(withSecurityHeaders(srv.routes()), logger, clock.System())
```

- [ ] **Step 5: Run tests + gates**

```bash
cd cmd/testmaker && go test . -short -race && make lint && make test
```
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: baseline security-headers middleware"
```

---

# Phase 2 — Access control

### Task 7: The authenticator (token mint/verify — pure, injected clock) ✅

**Files:**
- Create: `cmd/testmaker/auth.go`
- Create: `cmd/testmaker/auth_test.go`

**Interfaces:**
- Consumes: `AuthConfig` (Task 4), `domain/clock`, `shared.ErrUnsupported`.
- Produces: `authenticator` + `newAuthenticator(AuthConfig, clock.Clock) *authenticator`;
  `(*authenticator).enforced() bool`; `verifyOperator(token) bool`;
  `mintInvite(tid, ttl) (string, time.Time, error)`, `verifyInvite(token) (tid string, ok bool)`;
  `mintSession(sid) (string, error)`, `verifySession(token) (sid string, ok bool)`;
  free `bearer(*http.Request) string`. Token formats are exactly C2. Tasks 8–10
  consume every method.

- [ ] **Step 1: Write the failing tests** — `cmd/testmaker/auth_test.go`:

```go
package main

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
)

func tokenAuth(t *testing.T, clk clock.Clock) *authenticator {
	t.Helper()
	return newAuthenticator(AuthConfig{
		Mode: "token", OperatorToken: "op-secret-token",
		Secret: "hmac-signing-secret", InviteTTLSeconds: 3600,
	}, clk)
}

func TestOperatorTokenVerify(t *testing.T) {
	a := tokenAuth(t, clock.System())
	if !a.verifyOperator("op-secret-token") {
		t.Fatal("correct operator token rejected")
	}
	if a.verifyOperator("wrong") || a.verifyOperator("") {
		t.Fatal("wrong/empty operator token accepted")
	}
}

func TestInviteRoundTripAndExpiry(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))
	a := tokenAuth(t, clk)
	tok, exp, err := a.mintInvite("test-1", 0) // 0 → config TTL (1h)
	if err != nil {
		t.Fatalf("mintInvite: %v", err)
	}
	if !exp.After(clk.Now()) {
		t.Fatal("expiry must be in the future")
	}
	tid, ok := a.verifyInvite(tok)
	if !ok || tid != "test-1" {
		t.Fatalf("verifyInvite = (%q, %v), want (test-1, true)", tid, ok)
	}
	clk.Advance(2 * time.Hour) // past the 1h TTL
	if _, ok := a.verifyInvite(tok); ok {
		t.Fatal("expired invite accepted")
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	a := tokenAuth(t, clock.System())
	tok, _, _ := a.mintInvite("test-1", time.Hour)
	bad := tok[:len(tok)-1] + "X" // flip the last sig char
	if _, ok := a.verifyInvite(bad); ok {
		t.Fatal("tampered signature accepted")
	}
	// A session token must not verify as an invite (prefix guard).
	stok, _ := a.mintSession("s-1")
	if _, ok := a.verifyInvite(stok); ok {
		t.Fatal("session token accepted as invite")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	a := tokenAuth(t, clock.System())
	tok, err := a.mintSession("sess-42")
	if err != nil {
		t.Fatal(err)
	}
	sid, ok := a.verifySession(tok)
	if !ok || sid != "sess-42" {
		t.Fatalf("verifySession = (%q, %v), want (sess-42, true)", sid, ok)
	}
}

func TestMintInviteRequiresSecret(t *testing.T) {
	a := newAuthenticator(AuthConfig{Mode: "none"}, clock.System())
	if a.enforced() {
		t.Fatal("none mode must not be enforced")
	}
	if _, _, err := a.mintInvite("t", time.Hour); err == nil {
		t.Fatal("mintInvite with no secret must error (invites need token mode)")
	}
}

func TestBearerExtraction(t *testing.T) {
	r := httptest.NewRequest("GET", "/api", nil)
	r.Header.Set("Authorization", "Bearer abc.def.ghi")
	if got := bearer(r); got != "abc.def.ghi" {
		t.Fatalf("bearer = %q", got)
	}
	r2 := httptest.NewRequest("GET", "/api", nil)
	r2.Header.Set("Authorization", "Basic xyz")
	if got := bearer(r2); got != "" {
		t.Fatalf("non-bearer scheme leaked: %q", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run 'TestOperator|TestInvite|TestTampered|TestSession|TestMintInvite|TestBearer' -v
```
Expected: FAIL — `newAuthenticator` undefined.

- [ ] **Step 3: Implement `auth.go`**

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// token type prefixes (C2). A prefix is part of the signed input, so a session
// token can never be replayed as an invite.
const (
	invitePrefix  = "ti"
	sessionPrefix = "ts"
)

// authenticator issues and verifies the three delivery-surface credentials
// (ADR-0006): a static operator token and stateless HMAC-SHA256 invite/session
// capability tokens. It holds no state beyond the config secrets and an
// injected clock (for invite expiry), so any instance verifies any token — the
// property that carries this design into a multi-instance deployment unchanged.
type authenticator struct {
	mode          string
	operatorToken string
	secret        []byte
	inviteTTL     time.Duration
	clk           clock.Clock
}

func newAuthenticator(cfg AuthConfig, clk clock.Clock) *authenticator {
	return &authenticator{
		mode:          cfg.Mode,
		operatorToken: cfg.OperatorToken,
		secret:        []byte(cfg.Secret),
		inviteTTL:     time.Duration(cfg.InviteTTLSeconds) * time.Second,
		clk:           clk,
	}
}

// enforced reports whether role checks apply. "none" mode (and a nil
// authenticator) leaves the surface open for trusted-localhost development.
func (a *authenticator) enforced() bool { return a != nil && a.mode == "token" }

func (a *authenticator) verifyOperator(token string) bool {
	if a.operatorToken == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.operatorToken)) == 1
}

type inviteClaims struct {
	TID string `json:"tid"`
	Exp int64  `json:"exp"`
}

type sessionClaims struct {
	SID string `json:"sid"`
}

func (a *authenticator) sign(prefix, payloadB64 string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(prefix + "." + payloadB64))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *authenticator) mint(prefix string, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", shared.ErrInvalid.Wrap(err).WithMessage("marshal token claims")
	}
	p64 := base64.RawURLEncoding.EncodeToString(payload)
	return prefix + "." + p64 + "." + a.sign(prefix, p64), nil
}

// verify splits a token, checks its prefix, recomputes the HMAC over
// prefix+"."+payload and compares it in constant time, then returns the raw
// payload bytes when the signature holds.
func (a *authenticator) verify(token, prefix string) ([]byte, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != prefix {
		return nil, false
	}
	if !hmac.Equal([]byte(a.sign(prefix, parts[1])), []byte(parts[2])) {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	return payload, true
}

func (a *authenticator) mintInvite(tid string, ttl time.Duration) (string, time.Time, error) {
	if len(a.secret) == 0 {
		return "", time.Time{}, shared.ErrUnsupported.WithMessage(`invites require auth mode "token"`)
	}
	if ttl <= 0 {
		ttl = a.inviteTTL
	}
	exp := a.clk.Now().Add(ttl)
	tok, err := a.mint(invitePrefix, inviteClaims{TID: tid, Exp: exp.Unix()})
	return tok, exp, err
}

func (a *authenticator) verifyInvite(token string) (string, bool) {
	payload, ok := a.verify(token, invitePrefix)
	if !ok {
		return "", false
	}
	var c inviteClaims
	if json.Unmarshal(payload, &c) != nil {
		return "", false
	}
	if a.clk.Now().Unix() >= c.Exp {
		return "", false
	}
	return c.TID, true
}

func (a *authenticator) mintSession(sid string) (string, error) {
	if len(a.secret) == 0 {
		return "", shared.ErrUnsupported.WithMessage(`session tokens require auth mode "token"`)
	}
	return a.mint(sessionPrefix, sessionClaims{SID: sid})
}

func (a *authenticator) verifySession(token string) (string, bool) {
	payload, ok := a.verify(token, sessionPrefix)
	if !ok {
		return "", false
	}
	var c sessionClaims
	if json.Unmarshal(payload, &c) != nil {
		return "", false
	}
	return c.SID, true
}

// bearer returns the token from an "Authorization: Bearer <token>" header, or
// "" for any other scheme / a missing header (→ anonymous).
func bearer(r *http.Request) string {
	const scheme = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) >= len(scheme) && strings.EqualFold(h[:len(scheme)], scheme) {
		return strings.TrimSpace(h[len(scheme):])
	}
	return ""
}
```

(Confirm `shared.TestmakerError` has `.Wrap(err)` and `.WithMessage` — it does,
per ARCHITECTURE §11's copy-on-write builders. If `Wrap` is spelled
differently, match the sentinel API in `domain/shared/errors.go`.)

- [ ] **Step 4: Run the tests**

```bash
cd cmd/testmaker && go test . -run 'TestOperator|TestInvite|TestTampered|TestSession|TestMintInvite|TestBearer' -v
```
Expected: PASS.

- [ ] **Step 5: Gates**

```bash
make lint && make test && wc -l cmd/testmaker/auth.go
```
Expected: green; `auth.go` ≤ 500 (≈ 190).

- [ ] **Step 6: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: HMAC authenticator (operator token + invite/session capability tokens)"
```

---

### Task 8: Role middleware, whoami, and route guards ✅

**Files:**
- Modify: `cmd/testmaker/auth.go` (guard methods) — or `cmd/testmaker/auth_middleware.go` if `auth.go` nears 500 (`wc -l` first)
- Modify: `cmd/testmaker/server.go` (`server`/`serverDeps` gain auth; `routes()` wraps handlers; register `GET /api/auth/whoami`)
- Create/modify: `cmd/testmaker/auth_middleware_test.go`

**Interfaces:**
- Consumes: `authenticator`, `bearer`, `writeAuthError`.
- Produces: `serverDeps.authCfg AuthConfig` + `serverDeps.clock clock.Clock`
  (nil → System); `server.auth *authenticator`; guards
  `(*server).requireOperator(http.HandlerFunc) http.HandlerFunc`,
  `requireSession(...)` (checks `sid == PathValue("id")` or operator),
  `requireInvite(func(w,r,tid)) http.HandlerFunc`; `handleWhoami`. Task 9's
  invite handlers plug into `requireInvite`.

- [ ] **Step 1: Write the failing tests** — `cmd/testmaker/auth_middleware_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// authTestServer builds a token-mode surface (memory backends) so role checks
// are live. Returns the server + the operator token for Authorization headers.
func authTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	cfg := AuthConfig{Mode: "token", OperatorToken: "op-tok", Secret: "sec", InviteTTLSeconds: 3600}
	ts := httptest.NewServer(newServer(serverDeps{
		db: db, blobs: blobs, authCfg: cfg, clock: clock.System(),
	}).routes())
	t.Cleanup(ts.Close)
	return ts, "op-tok"
}

func get(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return res
}

func TestOperatorEndpointRequiresToken(t *testing.T) {
	ts, op := authTestServer(t)
	if res := get(t, ts.URL+"/api/items", ""); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token → %d, want 401", res.StatusCode)
	}
	if res := get(t, ts.URL+"/api/items", "wrong"); res.StatusCode != http.StatusForbidden {
		t.Fatalf("bad token → %d, want 403", res.StatusCode)
	}
	if res := get(t, ts.URL+"/api/items", op); res.StatusCode != http.StatusOK {
		t.Fatalf("operator token → %d, want 200", res.StatusCode)
	}
}

func TestWhoami(t *testing.T) {
	ts, op := authTestServer(t)
	if res := get(t, ts.URL+"/api/auth/whoami", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("whoami anon → %d, want 200", res.StatusCode)
	}
	res := get(t, ts.URL+"/api/auth/whoami", op)
	var body struct{ Role, Mode string }
	decodeBody(t, res, &body)
	if body.Role != "operator" || body.Mode != "token" {
		t.Fatalf("whoami(operator) = %+v", body)
	}
}

func TestPublicEndpointsNeedNoToken(t *testing.T) {
	ts, _ := authTestServer(t)
	if res := get(t, ts.URL+"/api", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api anon → %d, want 200 (public)", res.StatusCode)
	}
}

func TestNoneModeDisablesEnforcement(t *testing.T) {
	db, _ := openTestDB("memory")
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	ts := httptest.NewServer(newServer(serverDeps{
		db: db, blobs: blobs, authCfg: AuthConfig{Mode: "none"},
	}).routes())
	t.Cleanup(ts.Close)
	if res := get(t, ts.URL+"/api/items", ""); res.StatusCode != http.StatusOK {
		t.Fatalf("none mode, no token → %d, want 200", res.StatusCode)
	}
}
```

Add a shared `decodeBody` helper to `server_test.go` (or a test helper file) if
not present:

```go
func decodeBody(t *testing.T, res *http.Response, dst any) {
	t.Helper()
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run 'TestOperatorEndpoint|TestWhoami|TestPublic|TestNoneMode' -v
```
Expected: FAIL — `authCfg` field / guards undefined.

- [ ] **Step 3: Add the guards + whoami** (append to `auth.go`; if it would
exceed 500 lines, put these in `auth_middleware.go`):

```go
// requireOperator gates an operator-only handler. Enforced mode: a missing
// token is 401, a present-but-non-operator token is 403. In none mode it is a
// pass-through.
func (s *server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.enforced() {
			next(w, r)
			return
		}
		tok := bearer(r)
		if tok == "" {
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "authentication required")
			return
		}
		if !s.auth.verifyOperator(tok) {
			writeAuthError(w, http.StatusForbidden, "auth.forbidden", "operator credentials required")
			return
		}
		next(w, r)
	}
}

// requireSession gates a session verb: the caller must hold that session's
// token (sid == path id) or the operator token. None mode passes through.
func (s *server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.enforced() {
			next(w, r)
			return
		}
		tok := bearer(r)
		if tok == "" {
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "authentication required")
			return
		}
		if s.auth.verifyOperator(tok) {
			next(w, r)
			return
		}
		sid, ok := s.auth.verifySession(tok)
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "invalid session token")
			return
		}
		if sid != r.PathValue("id") {
			writeAuthError(w, http.StatusForbidden, "auth.forbidden", "token does not match this session")
			return
		}
		next(w, r)
	}
}

// requireInvite gates the invite verbs. It ALWAYS verifies (even in none mode,
// where there is no secret, so it simply 401s — the invite flow is a token-mode
// feature; an operator in none mode starts sessions directly). The verified
// test id is handed to the wrapped handler so it need not re-parse the token.
func (s *server) requireInvite(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tid, ok := s.auth.verifyInvite(bearer(r))
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "a valid invite is required")
			return
		}
		next(w, r, tid)
	}
}

// handleWhoami resolves the presented bearer to a role for the SPA's login
// check. None mode reports everyone as operator (the surface is open).
func (s *server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	role := "anonymous"
	switch {
	case !s.auth.enforced():
		role = "operator"
	case s.auth.verifyOperator(bearer(r)):
		role = "operator"
	default:
		if _, ok := s.auth.verifySession(bearer(r)); ok {
			role = "taker"
		} else if _, ok := s.auth.verifyInvite(bearer(r)); ok {
			role = "taker"
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"role": role, "mode": s.auth.mode})
}
```

- [ ] **Step 4: Wire auth into the server + wrap routes**

In `server.go`: add `auth *authenticator` to `server`; add `authCfg AuthConfig`
and `clock clock.Clock` to `serverDeps`; in `newServer`:

```go
	clk := d.clock
	if clk == nil {
		clk = clock.System()
	}
	// … existing field assignments …, plus:
	auth: newAuthenticator(d.authCfg, clk),
```

Then wrap the operator routes in `routes()` (public + session/invite handled in
their own tasks). Update `routes()`:

```go
	mux.HandleFunc("GET /api", s.handleIndex)                       // public
	mux.HandleFunc("GET /api/auth/whoami", s.handleWhoami)          // public
	mux.HandleFunc("GET /api/media/{ref}", s.handleMedia)          // public
	mux.HandleFunc("POST /api/items/generate", s.requireOperator(s.handleGenerate))
	mux.HandleFunc("POST /api/tests", s.requireOperator(s.handleCompose))
	mux.HandleFunc("GET /api/tests/{id}", s.requireOperator(s.handleGetTest))
	mux.HandleFunc("POST /api/tests/{id}/sessions", s.requireOperator(s.handleStartSession))
	mux.HandleFunc("POST /api/sessions/{id}/answers", s.requireSession(s.handleAnswer))
	mux.HandleFunc("POST /api/sessions/{id}/complete", s.requireSession(s.handleComplete))
	mux.HandleFunc("GET /api/sessions/{id}/score", s.requireSession(s.handleScore))
	mux.HandleFunc("GET /api/sources", s.requireOperator(s.handleListSources))
	mux.HandleFunc("GET /api/sources/{id}", s.requireOperator(s.handleGetSource))
	mux.HandleFunc("POST /api/catalog/sync", s.requireOperator(s.handleSyncCatalog))
	mux.HandleFunc("GET /api/items", s.requireOperator(s.handleListItems))
	mux.HandleFunc("GET /api/items/{id}", s.requireOperator(s.handleGetItem))
	mux.HandleFunc("POST /api/sources/{id}/ingest", s.requireOperator(s.handleIngest))
	mux.HandleFunc("POST /api/sources/{id}/ingest-llm", s.requireOperator(s.handleIngestLLM))
	mux.HandleFunc("GET /", s.handleSPA)
```

Add `"GET /api/auth/whoami"` to the `handleIndex` endpoint list.

- [ ] **Step 5: Migrate existing pre-auth tests**

The Task 2 tests and the original surface tests construct `serverDeps` with no
`authCfg` → zero value → `Mode == ""` → `enforced()` false, so they keep
passing unauthenticated. Confirm:

```bash
cd cmd/testmaker && go test . -short -race
```
Expected: PASS (old tests unaffected; new auth tests green).

- [ ] **Step 6: Gates + size**

```bash
make lint && make test && wc -l cmd/testmaker/auth.go cmd/testmaker/server.go
```
Expected: green; both ≤ 500 (split `auth.go` → `auth_middleware.go` if over).

- [ ] **Step 7: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: role middleware, whoami, and per-route auth guards"
```

---

### Task 9: Invite minting, preview, and token-issuing session start ✅

**Files:**
- Create: `cmd/testmaker/server_invites.go`
- Create: `cmd/testmaker/server_invites_test.go`
- Modify: `cmd/testmaker/server.go` (register invite routes; `handleStartSession` returns a session token)

**Interfaces:**
- Consumes: `server.auth`, `server.tests` (`ports.TestRepository`),
  `server.exec` (`ports.Executor`), `requireOperator`, `requireInvite`.
- Produces: `handleMintInvite`, `handleInvitePreview(w,r,tid)`,
  `handleInviteStart(w,r,tid)`; `startResponse` wrapping `ports.Delivery` +
  `SessionToken`; invite preview/start wire shapes exactly C7. Task 10 exercises
  the whole chain; the player UI (Phase 8) consumes these bodies.

- [ ] **Step 1: Write the failing tests** — `cmd/testmaker/server_invites_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// seedTestForInvite composes a one-section fixed test via the surface so the
// invite flow has a real test id. Returns the server, operator token, test id.
func seedTestForInvite(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	cfg := AuthConfig{Mode: "token", OperatorToken: "op-tok", Secret: "sec", InviteTTLSeconds: 3600}
	srv := newServer(serverDeps{db: db, blobs: blobs, authCfg: cfg, clock: clock.System()})
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	// Seed one A2 item and compose a test — reuse the generator path.
	post(t, ts.URL+"/api/items/generate", "op-tok", generateReq{TestType: "A2", Difficulty: 1, Count: 4, Seed: 1})
	post(t, ts.URL+"/api/tests", "op-tok", composeReq{
		ID: "t1", Title: "Demo", Policy: "fixed-increasing",
		Sections: []sectionReq{{Title: "Logic", Family: "logical", MinDifficulty: 1, MaxDifficulty: 3, PerItemSeconds: 60}},
	})
	return ts, "op-tok", "t1"
}

func post(t *testing.T, url, token string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

func TestInviteMintPreviewStartFlow(t *testing.T) {
	ts, op, tid := seedTestForInvite(t)

	// 1. Operator mints an invite.
	res := post(t, ts.URL+"/api/tests/"+tid+"/invites", op, map[string]int{"expiresInSeconds": 600})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("mint invite → %d, want 201", res.StatusCode)
	}
	var inv struct{ Token, URL, ExpiresAt string }
	decodeBody(t, res, &inv)
	if inv.Token == "" || inv.URL == "" {
		t.Fatalf("empty invite: %+v", inv)
	}

	// 2. A non-operator cannot mint.
	if r := post(t, ts.URL+"/api/tests/"+tid+"/invites", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon mint → %d, want 401", r.StatusCode)
	}

	// 3. Preview with the invite token — no item ids leak.
	pres := get(t, ts.URL+"/api/invites/preview", inv.Token)
	if pres.StatusCode != http.StatusOK {
		t.Fatalf("preview → %d, want 200", pres.StatusCode)
	}
	var pv map[string]any
	decodeBody(t, pres, &pv)
	if pv["title"] != "Demo" {
		t.Fatalf("preview title = %v", pv["title"])
	}
	if _, leaked := pv["sections"].([]any)[0].(map[string]any)["items"]; leaked {
		t.Fatal("preview leaked item refs")
	}

	// 4. Start a session with the invite → get a session token.
	sres := post(t, ts.URL+"/api/invites/start", inv.Token, nil)
	if sres.StatusCode != http.StatusCreated {
		t.Fatalf("invite start → %d, want 201", sres.StatusCode)
	}
	var start struct {
		Session      struct{ ID string }
		SessionToken string
	}
	decodeBody(t, sres, &start)
	if start.Session.ID == "" || start.SessionToken == "" {
		t.Fatalf("start missing session/token: %+v", start)
	}

	// 5. The session token drives that session; a bare invite token cannot.
	ans := post(t, ts.URL+"/api/sessions/"+start.Session.ID+"/answers", start.SessionToken,
		answerReq{ItemID: "does-not-matter", OptionID: "a"})
	if ans.StatusCode == http.StatusUnauthorized || ans.StatusCode == http.StatusForbidden {
		t.Fatalf("session token rejected on its own session: %d", ans.StatusCode)
	}
	if bad := post(t, ts.URL+"/api/sessions/"+start.Session.ID+"/answers", inv.Token,
		answerReq{ItemID: "x", OptionID: "a"}); bad.StatusCode != http.StatusForbidden {
		t.Fatalf("invite token on session verb → %d, want 403", bad.StatusCode)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run TestInviteMintPreviewStart -v
```
Expected: FAIL — invite routes 404 / handlers undefined.

- [ ] **Step 3: Implement `server_invites.go`**

```go
package main

import (
	"net/http"
	"time"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// mintInviteReq optionally shortens the invite lifetime; 0/absent → config TTL.
type mintInviteReq struct {
	ExpiresInSeconds int `json:"expiresInSeconds"`
}

type inviteResponse struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// sectionSummary is the taker-safe view of a section: counts and timing only,
// never item ids or difficulty bands (those would hand a taker the test).
type sectionSummary struct {
	Title          string `json:"title"`
	Family         string `json:"family"`
	ItemCount      int    `json:"itemCount"`
	TotalSeconds   int    `json:"totalSeconds"`
	PerItemSeconds int    `json:"perItemSeconds"`
}

type invitePreview struct {
	TestID         string           `json:"testId"`
	Title          string           `json:"title"`
	Policy         string           `json:"policy"`
	TotalSeconds   int              `json:"totalSeconds"`
	PerItemSeconds int              `json:"perItemSeconds"`
	ItemCount      int              `json:"itemCount"`
	Sections       []sectionSummary `json:"sections"`
	ExpiresAt      time.Time        `json:"expiresAt"`
}

// startResponse embeds the executor's Delivery (PascalCase, marshalled as-is)
// and adds the session capability token the taker uses for the rest of the
// attempt. Operator direct-start returns the same shape.
type startResponse struct {
	ports.Delivery
	SessionToken string `json:"SessionToken"`
}

// handleMintInvite (operator) issues a signed, expiring invite for a test after
// confirming the test exists.
func (s *server) handleMintInvite(w http.ResponseWriter, r *http.Request) {
	id := testset.TestID(r.PathValue("id"))
	if _, err := s.tests.GetTest(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	var req mintInviteReq
	if !decodeOptionalJSON(w, r, &req) {
		return
	}
	tok, exp, err := s.auth.mintInvite(string(id), time.Duration(req.ExpiresInSeconds)*time.Second)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, inviteResponse{Token: tok, URL: "/take#" + tok, ExpiresAt: exp})
}

// handleInvitePreview (invite) returns the redacted test summary. tid comes from
// the verified invite, so a taker can only preview the invited test.
func (s *server) handleInvitePreview(w http.ResponseWriter, r *http.Request, tid string) {
	test, err := s.tests.GetTest(r.Context(), testset.TestID(tid))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	pv := invitePreview{
		TestID: tid, Title: test.Title, Policy: string(test.Policy),
		TotalSeconds: int(test.Timing.Total / time.Second), PerItemSeconds: int(test.Timing.PerItem / time.Second),
	}
	for _, sec := range test.Sections {
		pv.ItemCount += len(sec.Items)
		pv.Sections = append(pv.Sections, sectionSummary{
			Title: sec.Title, Family: string(sec.Family), ItemCount: len(sec.Items),
			TotalSeconds: int(sec.Timing.Total / time.Second), PerItemSeconds: int(sec.Timing.PerItem / time.Second),
		})
	}
	writeJSON(w, http.StatusOK, pv)
}

// handleInviteStart (invite) starts a session for the invited test and returns
// the opening Delivery plus a fresh session token.
func (s *server) handleInviteStart(w http.ResponseWriter, r *http.Request, tid string) {
	test, err := s.tests.GetTest(r.Context(), testset.TestID(tid))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.startAndRespond(w, r, test)
}

// startAndRespond runs the executor's Start and writes the token-augmented
// Delivery. Shared by invite start and operator direct start.
func (s *server) startAndRespond(w http.ResponseWriter, r *http.Request, test testset.TestSnapshot) {
	d, err := s.exec.Start(r.Context(), test)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tok, err := s.sessionToken(d.Session.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, startResponse{Delivery: d, SessionToken: tok})
}

// sessionToken mints a session capability, or returns "" in none mode (no
// secret) where the token is unused — the surface is open, so the player still
// works by presenting no token.
func (s *server) sessionToken(id interface{ String() string }) (string, error) {
	if !s.auth.enforced() {
		return "", nil
	}
	tok, err := s.auth.mintSession(id.String())
	if err != nil {
		return "", err
	}
	return tok, nil
}
```

Note: `session.SessionID` is a string type — confirm it has a `String()` method
or is a defined string; if it is `type SessionID string`, replace the
`interface{ String() string }` param with `session.SessionID` and call
`string(id)`. Adjust `d.Session.ID` accordingly (it is a `session.SessionID`).
Pick whichever compiles; the simplest is:

```go
func (s *server) sessionToken(id session.SessionID) (string, error) {
	if !s.auth.enforced() {
		return "", nil
	}
	return s.auth.mintSession(string(id))
}
```

- [ ] **Step 4: Register routes + reuse start in the operator handler**

In `server.go`, replace the direct-start registration and add the invite
routes:

```go
	mux.HandleFunc("POST /api/tests/{id}/sessions", s.requireOperator(s.handleStartSession))
	mux.HandleFunc("POST /api/tests/{id}/invites", s.requireOperator(s.handleMintInvite))
	mux.HandleFunc("GET /api/invites/preview", s.requireInvite(s.handleInvitePreview))
	mux.HandleFunc("POST /api/invites/start", s.requireInvite(s.handleInviteStart))
```

Rewrite `handleStartSession` to use the shared helper (so operator start also
returns a token):

```go
func (s *server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	test, err := s.tests.GetTest(r.Context(), testset.TestID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.startAndRespond(w, r, test)
}
```

Add the three new endpoints to the `handleIndex` list.

- [ ] **Step 5: Run tests**

```bash
cd cmd/testmaker && go test . -run 'TestInvite|TestStartSession|TestAnswer' -short -race -v
```
Expected: PASS. (If a pre-existing `handleStartSession` test asserted the bare
`Delivery` shape, it still decodes — `startResponse` embeds it; only the extra
`SessionToken` field is added.)

- [ ] **Step 6: Gates + size**

```bash
make lint && make test && wc -l cmd/testmaker/server_invites.go
```
Expected: green; ≤ 500.

- [ ] **Step 7: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: invite minting, redacted preview, token-issuing session start"
```

---

### Task 10: Full-flow auth integration test (operator → invite → taker → score) ✅

A single end-to-end test that proves the two roles compose across the whole
take path — the guarantee the player UI depends on.

**Files:**
- Create: `cmd/testmaker/auth_flow_test.go`

**Interfaces:**
- Consumes: everything from Tasks 7–9. Produces: nothing (a test).

- [ ] **Step 1: Write the test**

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// TestFullTakePathTwoRoles walks operator setup → invite → taker attempt →
// score, asserting the role boundary holds at every step. It is the executable
// contract behind DESIGN §7.3's sequence diagram.
func TestFullTakePathTwoRoles(t *testing.T) {
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	cfg := AuthConfig{Mode: "token", OperatorToken: "OP", Secret: "SECRET", InviteTTLSeconds: 3600}
	ts := httptest.NewServer(newServer(serverDeps{db: db, blobs: blobs, authCfg: cfg, clock: clock.System()}).routes())
	t.Cleanup(ts.Close)

	// Operator seeds bank + test.
	if r := post(t, ts.URL+"/api/items/generate", "OP", generateReq{TestType: "A2", Difficulty: 1, Count: 6, Seed: 7}); r.StatusCode != http.StatusOK {
		t.Fatalf("generate → %d", r.StatusCode)
	}
	if r := post(t, ts.URL+"/api/tests", "OP", composeReq{
		ID: "quiz", Title: "Quiz", Policy: "fixed-increasing",
		Sections: []sectionReq{{Title: "L", Family: "logical", MinDifficulty: 1, MaxDifficulty: 3, PerItemSeconds: 60}},
	}); r.StatusCode != http.StatusCreated {
		t.Fatalf("compose → %d", r.StatusCode)
	}

	// Operator mints an invite; taker previews + starts.
	var inv struct{ Token string }
	decodeBody(t, post(t, ts.URL+"/api/tests/quiz/invites", "OP", nil), &inv)
	if r := get(t, ts.URL+"/api/invites/preview", inv.Token); r.StatusCode != http.StatusOK {
		t.Fatalf("preview → %d", r.StatusCode)
	}
	var start struct {
		Session struct {
			ID        string
			Presented struct{ ItemID string }
		}
		Item         *struct{ AnswerKey struct{ OptionID string } }
		SessionToken string
	}
	decodeBody(t, post(t, ts.URL+"/api/invites/start", inv.Token, nil), &start)

	// The taker's presented item must be key-redacted (executor strips the key).
	if start.Item != nil && start.Item.AnswerKey.OptionID != "" {
		t.Fatal("presented item leaked its answer key to the taker")
	}

	// Taker answers every presented item with the session token, then completes.
	sid, stok := start.Session.ID, start.SessionToken
	presented := start.Session.Presented.ItemID
	for presented != "" {
		var d struct {
			Session struct {
				Presented struct{ ItemID string }
			}
		}
		decodeBody(t, post(t, ts.URL+"/api/sessions/"+sid+"/answers", stok,
			answerReq{ItemID: presented, OptionID: "a"}), &d)
		presented = d.Session.Presented.ItemID
	}
	if r := post(t, ts.URL+"/api/sessions/"+sid+"/complete", stok, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("complete → %d", r.StatusCode)
	}

	// Taker reads their score; an anonymous caller cannot.
	if r := get(t, ts.URL+"/api/sessions/"+sid+"/score", stok); r.StatusCode != http.StatusOK {
		t.Fatalf("score with session token → %d, want 200", r.StatusCode)
	}
	if r := get(t, ts.URL+"/api/sessions/"+sid+"/score", ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon score → %d, want 401", r.StatusCode)
	}

	// The operator bank view (keys) stays operator-only throughout.
	if r := get(t, ts.URL+"/api/items", stok); r.StatusCode != http.StatusForbidden {
		t.Fatalf("taker hitting operator bank → %d, want 403", r.StatusCode)
	}
}
```

- [ ] **Step 2: Run it**

```bash
cd cmd/testmaker && go test . -run TestFullTakePathTwoRoles -short -race -v
```
Expected: PASS.

- [ ] **Step 3: Gates**

```bash
make lint && make test
```
Expected: green.

- [ ] **Step 4: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: full two-role take-path integration test"
```

---

# Phase 3 — Limits

### Task 11: Per-IP rate limiter (hand-rolled token bucket, injected clock) ✅

Kept dependency-free on purpose: the token math is a handful of lines, the whole
module has only yaml + sqlite as vendors, and an injected clock makes it
deterministic under test — a vendored limiter would buy none of that.

**Files:**
- Create: `cmd/testmaker/ratelimit.go`
- Create: `cmd/testmaker/ratelimit_test.go`
- Modify: `cmd/testmaker/server.go` (`runServer` wraps with the limiter)

**Interfaces:**
- Consumes: `domain/clock`, `clientIP`, `writeAuthError`.
- Produces: `rateLimiter` + `newRateLimiter(rps float64, burst int, clk clock.Clock) *rateLimiter`;
  `(*rateLimiter).allow(key string) bool`; `withRateLimit(next http.Handler, l *rateLimiter) http.Handler`
  (nil `l` → pass-through). Rate limit applies to `/api*` only; over-limit → `429 code:limit.rate`.

- [ ] **Step 1: Failing test** — `cmd/testmaker/ratelimit_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
)

func TestRateLimiterRefills(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	l := newRateLimiter(1, 2, clk) // 1 rps, burst 2
	if !l.allow("ip") || !l.allow("ip") {
		t.Fatal("burst of 2 must allow the first two")
	}
	if l.allow("ip") {
		t.Fatal("third immediate request must be denied")
	}
	clk.Advance(time.Second) // refill one token
	if !l.allow("ip") {
		t.Fatal("after 1s a token should be available")
	}
	if l.allow("ip2") { // a different IP has its own full bucket
		// ip2's first call is allowed (own bucket), so this should be true — fix
		// the assertion sense: distinct IPs are independent.
	}
	if !l.allow("ip2-fresh") {
		t.Fatal("a fresh IP starts with a full bucket")
	}
}

func TestRateLimitMiddlewareOnlyGatesAPI(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	l := newRateLimiter(1, 1, clk)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := withRateLimit(ok, l)

	// First /api request from an IP passes; second is 429.
	req := func(path string) int {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "1.2.3.4:5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if req("/api/items") != http.StatusOK {
		t.Fatal("first /api request should pass")
	}
	if req("/api/items") != http.StatusTooManyRequests {
		t.Fatal("second /api request should be 429")
	}
	// Non-/api (SPA asset) is never gated, even when the bucket is empty.
	if req("/assets/app.js") != http.StatusOK {
		t.Fatal("SPA asset must not be rate-limited")
	}
}

func TestWithRateLimitNilIsPassthrough(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := withRateLimit(ok, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("nil limiter must pass through, got %d", rec.Code)
	}
}
```

(Fix the `ip2` block in `TestRateLimiterRefills` when implementing — it is left
as a note; the meaningful assertions are the burst, denial, refill, and
fresh-IP lines.)

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run 'TestRateLimiter|TestRateLimit|TestWithRateLimitNil' -v
```
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `ratelimit.go`**

```go
package main

import (
	"net/http"
	"strings"
	"sync"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// maxRateBuckets caps the per-IP bucket map. ponytail: a global lock + a lazy
// sweep of idle (refilled-to-full) buckets when the cap is hit — fine for a
// single node; a sharded map or an LRU is the upgrade if IP cardinality ever
// makes the lock hot.
const maxRateBuckets = 4096

type tokenBucket struct {
	tokens float64
	last   int64 // unix nanos of the last refill
}

// rateLimiter is a per-key token bucket. The clock is injected so refill is
// deterministic under test (the same discipline as the rest of the surface).
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64 // tokens per second
	burst   float64 // bucket capacity
	clk     clock.Clock
}

func newRateLimiter(rps float64, burst int, clk clock.Clock) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    rps,
		burst:   float64(burst),
		clk:     clk,
	}
}

// allow consumes one token for key, refilling by elapsed time first. It returns
// false when the bucket is empty (caller should 429).
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clk.Now().UnixNano()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxRateBuckets {
			l.sweepIdleLocked()
		}
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	elapsed := float64(now-b.last) / 1e9
	b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepIdleLocked drops buckets that have refilled to full (nobody is using
// them), reclaiming space without evicting an active limiter. Caller holds mu.
func (l *rateLimiter) sweepIdleLocked() {
	for k, b := range l.buckets {
		if b.tokens >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// withRateLimit gates /api* by client IP; a nil limiter (unconfigured, e.g.
// tests) is a pass-through. SPA assets and non-/api paths are never limited.
func withRateLimit(next http.Handler, l *rateLimiter) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") && !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeAuthError(w, http.StatusTooManyRequests, "limit.rate", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Wire into `runServer`** (limiter built from config, nested
inside security headers so a 429 still carries the baseline headers):

```go
	var limiter *rateLimiter
	if cfg.Limits.RequestsPerSecond > 0 {
		limiter = newRateLimiter(cfg.Limits.RequestsPerSecond, cfg.Limits.Burst, clock.System())
	}
	handler := withRequestLog(withSecurityHeaders(withRateLimit(srv.routes(), limiter)), logger, clock.System())
```

- [ ] **Step 5: Run tests + gates**

```bash
cd cmd/testmaker && go test . -short -race && make lint && make test
```
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: per-IP token-bucket rate limiter on /api"
```

---

### Task 12: Ingest concurrency semaphore (sync path) ✅

**Files:**
- Create: `cmd/testmaker/semaphore.go`
- Create: `cmd/testmaker/semaphore_test.go`
- Modify: `cmd/testmaker/server.go` (`server`/`serverDeps` gain the semaphore; `newServer` builds it)
- Modify: `cmd/testmaker/server_sourcing.go` (`handleIngest`/`handleIngestLLM` acquire it)

**Interfaces:**
- Consumes: nothing new.
- Produces: `semaphore` (`chan struct{}`) with `tryAcquire() bool`,
  `acquire(ctx) error`, `release()`; `serverDeps.maxIngest int`;
  `server.ingestSem semaphore` (nil when `maxIngest <= 0` → ungated). Task 18
  (async) reuses `acquire`/`release` off the same field.

- [ ] **Step 1: Failing test** — `cmd/testmaker/semaphore_test.go`:

```go
package main

import (
	"context"
	"testing"
)

func TestSemaphoreTryAcquire(t *testing.T) {
	sem := newSemaphore(1)
	if !sem.tryAcquire() {
		t.Fatal("first acquire on a free semaphore must succeed")
	}
	if sem.tryAcquire() {
		t.Fatal("second acquire must fail (capacity 1)")
	}
	sem.release()
	if !sem.tryAcquire() {
		t.Fatal("acquire after release must succeed")
	}
}

func TestSemaphoreAcquireRespectsContext(t *testing.T) {
	sem := newSemaphore(1)
	sem.tryAcquire() // fill it
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sem.acquire(ctx); err == nil {
		t.Fatal("acquire on a cancelled context must return its error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run TestSemaphore -v
```
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `semaphore.go`**

```go
package main

import "context"

// semaphore is a counting gate over concurrent ingest runs (sync and async
// share one instance) so outbound fetches and paid LLM calls stay bounded no
// matter the request path (DESIGN §7.4). A buffered channel is the whole
// implementation.
type semaphore chan struct{}

func newSemaphore(n int) semaphore { return make(semaphore, n) }

// tryAcquire takes a slot without blocking; false means the gate is full (the
// sync path turns this into a 429).
func (s semaphore) tryAcquire() bool {
	select {
	case s <- struct{}{}:
		return true
	default:
		return false
	}
}

// acquire blocks for a slot or until ctx is done (the async path waits here).
func (s semaphore) acquire(ctx context.Context) error {
	select {
	case s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s semaphore) release() { <-s }
```

- [ ] **Step 4: Wire it** — in `server.go` add `ingestSem semaphore` to
`server`, `maxIngest int` to `serverDeps`, and in `newServer`:

```go
	var sem semaphore
	if d.maxIngest > 0 {
		sem = newSemaphore(d.maxIngest)
	}
	// … field assignment: ingestSem: sem,
```

In `server_sourcing.go`, gate the sync ingest handlers. At the top of
`handleIngest` **and** `handleIngestLLM`, after the LLM-configured check but
before the fetch, add:

```go
	if s.ingestSem != nil {
		if !s.ingestSem.tryAcquire() {
			writeAuthError(w, http.StatusTooManyRequests, "limit.ingest", "another ingest is in progress")
			return
		}
		defer s.ingestSem.release()
	}
```

Wire `maxIngest: cfg.Limits.MaxConcurrentIngests` in `runServer`'s `newServer`
call.

- [ ] **Step 5: Test the gate end-to-end** — append to `server_sourcing_test.go`
a test that sets `maxIngest: 1`, holds the slot (a blocked fake fetcher on a
channel), fires a second ingest, and asserts `429 code:limit.ingest`. Use a
`sync.WaitGroup`/channel to synchronize — never `time.Sleep` (TESTS.md):

```go
func TestIngestSemaphoreRejectsConcurrent(t *testing.T) {
	// A normalizer/fetcher that blocks until released lets us hold the one slot
	// deterministically. Build the server with maxIngest:1 and a source wired to
	// the blocking fetcher; start one ingest in a goroutine, wait for it to
	// signal "in fetch", then assert a second ingest returns 429. Release, join.
	// (Construct via serverDeps with a stub blocking Fetcher injected through
	// ingest.NewService, mirroring the existing sourcing tests' wiring.)
	t.Skip("implement with the existing blocking-fetcher harness in this file")
}
```

Replace the `t.Skip` with the real body using this file's existing fetch-stub
pattern (the sourcing tests already stand up an `ingest.Service` with fakes).
The assertion is: first call occupies the slot, second returns 429
`limit.ingest`, and after release a third succeeds.

- [ ] **Step 6: Gates**

```bash
cd cmd/testmaker && go test . -short -race && make lint && make test
```
Expected: green.

- [ ] **Step 7: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: ingest concurrency semaphore (sync 429 when full)"
```

---

### Task 13: LLM cost clamp (BeforeGenerate hook) ✅

The first production consumer of the LLM service's designed `BeforeGenerate`
hook: cap tokens and gate models server-side, so a caller-supplied
`model`/`maxTokens` can't run up unbounded spend.

**Files:**
- Create: `cmd/testmaker/llm_clamp.go`
- Create: `cmd/testmaker/llm_clamp_test.go`
- Modify: `cmd/testmaker/server_sourcing.go` (`wireSourcing` registers the hook)

**Interfaces:**
- Consumes: `llmapp.WithBeforeGenerate`, `ports.LLMRequest`, `shared.ErrInvalid`,
  `LLMConfig`.
- Produces: `llmClampHook(maxTokens int, allowed []string) llmapp.BeforeGenerate`.
  Registered in `wireSourcing` when building `llmapp.NewService`.

- [ ] **Step 1: Failing test** — `cmd/testmaker/llm_clamp_test.go`:

```go
package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

func TestLLMClampCapsTokens(t *testing.T) {
	hook := llmClampHook(4096, nil)
	req := ports.LLMRequest{MaxTokens: 100000, Model: "anything"}
	if err := hook(context.Background(), &req); err != nil {
		t.Fatalf("hook errored: %v", err)
	}
	if req.MaxTokens != 4096 {
		t.Fatalf("MaxTokens = %d, want clamped to 4096", req.MaxTokens)
	}
	// Zero (backend default) is also clamped to the cap — a huge default is bounded.
	req2 := ports.LLMRequest{MaxTokens: 0, Model: "x"}
	_ = hook(context.Background(), &req2)
	if req2.MaxTokens != 4096 {
		t.Fatalf("zero MaxTokens should clamp to 4096, got %d", req2.MaxTokens)
	}
}

func TestLLMClampGatesModel(t *testing.T) {
	hook := llmClampHook(4096, []string{"gpt-ok", "local-ok"})
	if err := hook(context.Background(), &ports.LLMRequest{Model: "gpt-ok"}); err != nil {
		t.Fatalf("allowed model rejected: %v", err)
	}
	err := hook(context.Background(), &ports.LLMRequest{Model: "forbidden"})
	if !errors.Is(err, shared.ErrInvalid) {
		t.Fatalf("forbidden model err = %v, want ErrInvalid", err)
	}
}

func TestLLMClampEmptyAllowListPermitsAny(t *testing.T) {
	hook := llmClampHook(0, nil) // no cap, no allow-list
	if err := hook(context.Background(), &ports.LLMRequest{Model: "whatever", MaxTokens: 50}); err != nil {
		t.Fatalf("unconfigured clamp must permit: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run TestLLMClamp -v
```
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `llm_clamp.go`**

```go
package main

import (
	"context"

	llmapp "github.com/mariotoffia/testmaker/app/llm"
	"github.com/mariotoffia/testmaker/domain/shared"
	"github.com/mariotoffia/testmaker/ports"
)

// llmClampHook is a BeforeGenerate hook that bounds caller-controlled LLM spend
// on the delivery surface (DESIGN §7.4): it caps MaxTokens to maxTokens (a zero
// request value means "backend default", which is also clamped so a large
// default can't slip through) and, when allowed is non-empty, rejects any model
// not on the list. maxTokens <= 0 disables the cap; an empty allow-list permits
// any model. This is the composition root's use of the hook point the LLM
// service was built around — steps never register their own (DESIGN §6).
func llmClampHook(maxTokens int, allowed []string) llmapp.BeforeGenerate {
	allowset := make(map[string]struct{}, len(allowed))
	for _, m := range allowed {
		allowset[m] = struct{}{}
	}
	return func(_ context.Context, req *ports.LLMRequest) error {
		if maxTokens > 0 && (req.MaxTokens <= 0 || req.MaxTokens > maxTokens) {
			req.MaxTokens = maxTokens
		}
		if len(allowset) > 0 {
			if _, ok := allowset[req.Model]; !ok {
				return shared.ErrInvalid.WithMessagef("model %q is not in the allowed list", req.Model)
			}
		}
		return nil
	}
}
```

- [ ] **Step 4: Register the hook in `wireSourcing`** — where `llmSvc` is built:

```go
		llmSvc = llmapp.NewService(llmBackend, prompts,
			llmapp.WithBeforeGenerate(llmClampHook(llmCfg.MaxTokensCap, llmCfg.AllowedModels)))
```

(Confirm `NewService` is variadic on `...Option`; the grep in Task 0 showed
`WithBeforeGenerate` exists. If `NewService` is not variadic, use whatever
registration the service exposes — match `app/llm/service.go`.)

- [ ] **Step 5: Run tests + gates**

```bash
cd cmd/testmaker && go test . -short -race && make lint && make test
```
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: server-side LLM cost clamp via BeforeGenerate hook"
```

---

# Phase 4 — Console API

### Task 14: Pagination envelope on the list endpoints ✅

**Files:**
- Create: `cmd/testmaker/pagination.go`
- Create: `cmd/testmaker/pagination_test.go`
- Modify: `cmd/testmaker/server_sourcing.go` (`handleListSources`, `handleListItems` return envelopes)
- Modify: `cmd/testmaker/server_sourcing_test.go` (decode the envelope)

**Interfaces:**
- Consumes: `intParam` (Task 5 helpers).
- Produces: `pageEnvelope[T]{Items []T; Total, Limit, Offset int}`;
  `paginate[T any](all []T, limit, offset int) pageEnvelope[T]`;
  `pageParams(w, q) (limit, offset int, ok bool)` (defaults/clamps per C5).
  Tasks 15 & 17 reuse both for tests and jobs.

- [ ] **Step 1: Failing test** — `cmd/testmaker/pagination_test.go`:

```go
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
}

func TestPageParamsDefaults(t *testing.T) {
	q, _ := url.ParseQuery("") // no limit/offset
	limit, offset, ok := pageParams(httptest.NewRecorder(), q)
	if !ok || limit != 50 || offset != 0 {
		t.Fatalf("defaults = (%d,%d,%v)", limit, offset, ok)
	}
	q2, _ := url.ParseQuery("limit=10&offset=5")
	limit, offset, ok = pageParams(httptest.NewRecorder(), q2)
	if !ok || limit != 10 || offset != 5 {
		t.Fatalf("parsed = (%d,%d,%v)", limit, offset, ok)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run 'TestPaginate|TestPageParams' -v
```
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `pagination.go`**

```go
package main

import (
	"net/http"
	"net/url"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 500
)

// pageEnvelope is the paginated collection response (C5): the slice for this
// page plus the total match count and the applied window. A cmd-local wire type,
// so camelCase — distinct from the PascalCase domain snapshots it carries.
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
// writes a 400 via intParam and returns ok=false so the handler bails.
func pageParams(w http.ResponseWriter, q url.Values) (limit, offset int, ok bool) {
	limit, ok = intParam(w, q, "limit")
	if !ok {
		return 0, 0, false
	}
	offset, ok = intParam(w, q, "offset")
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
```

- [ ] **Step 4: Return envelopes** — in `server_sourcing.go`, end
`handleListSources` with:

```go
	limit, offset, ok := pageParams(w, q)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, paginate(sources, limit, offset))
```

and `handleListItems` similarly (`paginate(items, limit, offset)`). The
repositories already sort by id, so pages are stable.

- [ ] **Step 5: Migrate the list-endpoint tests** — the response shape changed
from a bare array to the envelope. In `server_sourcing_test.go`, update every
`/api/sources` and `/api/items` list assertion to decode
`struct{ Items []T; Total, Limit, Offset int }` and assert on `.Items` /
`.Total`. (`GET /…/{id}` single-item responses are unchanged.)

- [ ] **Step 6: Run tests + gates**

```bash
cd cmd/testmaker && go test . -short -race && make lint && make test
```
Expected: green.

- [ ] **Step 7: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: paginated envelope on /api/sources and /api/items"
```

---

### Task 15: `GET /api/tests` list endpoint ✅

The console's test list needs a paginated tests collection (only `GET
/api/tests/{id}` exists today).

**Files:**
- Modify: `cmd/testmaker/server.go` (register route + `handleListTests`; index entry)
- Modify: `cmd/testmaker/server_test.go` (test)

**Interfaces:**
- Consumes: `server.tests` (`ports.TestRepository.ListTests`), `paginate`, `pageParams`.
- Produces: `handleListTests`; `GET /api/tests` → `pageEnvelope[testset.TestSnapshot]`.

- [ ] **Step 1: Failing test** — append to `server_test.go`:

```go
func TestListTestsPaginated(t *testing.T) {
	ts := newSPATestServer(t) // zero-auth server from Task 2
	// Generate items + compose two tests so the list is non-empty.
	post(t, ts.URL+"/api/items/generate", "", generateReq{TestType: "A2", Difficulty: 1, Count: 4, Seed: 1})
	for _, id := range []string{"t-a", "t-b"} {
		post(t, ts.URL+"/api/tests", "", composeReq{
			ID: id, Title: id, Policy: "fixed-increasing",
			Sections: []sectionReq{{Title: "L", Family: "logical", MinDifficulty: 1, MaxDifficulty: 3, PerItemSeconds: 60}},
		})
	}
	res := get(t, ts.URL+"/api/tests", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/tests → %d", res.StatusCode)
	}
	var page struct {
		Items []struct{ ID string }
		Total int
	}
	decodeBody(t, res, &page)
	if page.Total < 2 || len(page.Items) < 2 {
		t.Fatalf("expected ≥2 tests, got %+v", page)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run TestListTestsPaginated -v
```
Expected: FAIL — `/api/tests` GET is 404 (only POST + `/{id}` registered).

- [ ] **Step 3: Implement** — add to `server.go`:

```go
// handleListTests returns the composed-test catalogue, paginated. TestFilter is
// currently empty (no criteria), so this lists all tests sorted by id.
func (s *server) handleListTests(w http.ResponseWriter, r *http.Request) {
	tests, err := s.tests.ListTests(r.Context(), testset.TestFilter{})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	limit, offset, ok := pageParams(w, r.URL.Query())
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, paginate(tests, limit, offset))
}
```

Register in `routes()` (operator-gated), next to the other `/api/tests` routes:

```go
	mux.HandleFunc("GET /api/tests", s.requireOperator(s.handleListTests))
```

Add `"GET /api/tests"` to the `handleIndex` list.

- [ ] **Step 4: Run tests + gates**

```bash
cd cmd/testmaker && go test . -short -race && make lint && make test && wc -l cmd/testmaker/server.go
```
Expected: green; `server.go` ≤ 500 (move `handleListTests` into
`server_sourcing.go` or a new `server_tests.go` if it pushes over).

- [ ] **Step 5: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: GET /api/tests paginated list endpoint"
```

---

### Task 16: `POST /api/catalog` upload + `filecatalog.ParseJSON` extraction ✅

The console edits the catalogue; today only `POST /api/catalog/sync` reloads the
deploy-time file. This adds a validated, atomic catalogue upload. The one
adapter touch in the whole plan: extract the JSON parse+validate out of
`filecatalog.Loader.Load` into a reusable pure function.

**Files:**
- Modify: `adapters/native/source/filecatalog/loader.go` (extract `ParseJSON`)
- Modify: `adapters/native/source/filecatalog/loader_test.go` (unit-test `ParseJSON`)
- Create: `cmd/testmaker/server_catalog.go`
- Create: `cmd/testmaker/server_catalog_test.go`
- Modify: `cmd/testmaker/server.go` / `server_sourcing.go` (`server`+`serverDeps` gain `catalogPath`; register route)

**Interfaces:**
- Consumes: `filecatalog.ParseJSON([]byte) ([]source.Snapshot, error)`, `server.cat`.
- Produces: `filecatalog.ParseJSON` (exported, pure — no file I/O);
  `handleUploadCatalog`; `serverDeps.catalogPath` → `server.catalogPath`;
  `POST /api/catalog` → validate → atomic write → sync → `{"synced": n}`.

- [ ] **Step 1: Failing test (adapter)** — append to
`adapters/native/source/filecatalog/loader_test.go`:

```go
func TestParseJSONValidatesRecords(t *testing.T) {
	good := []byte(`{"sources":[{"id":"s1","name":"S1","urls":["https://x"],"accessClasses":["downloadable-artifact"],"license":{"category":"public-domain","redistributable":"yes"}}]}`)
	snaps, err := filecatalog.ParseJSON(good)
	if err != nil {
		t.Fatalf("ParseJSON(good): %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "s1" {
		t.Fatalf("snaps = %+v", snaps)
	}
	// An invalid record (empty id) is rejected — same gate as Load.
	bad := []byte(`{"sources":[{"name":"no id","urls":["https://x"],"accessClasses":["downloadable-artifact"],"license":{"category":"public-domain","redistributable":"yes"}}]}`)
	if _, err := filecatalog.ParseJSON(bad); err == nil {
		t.Fatal("ParseJSON must reject an invalid record")
	}
}
```

(Adjust the fixture field names/enum values to the real wire schema — copy a
record from `data/catalog/sources.json` so the closed-set enums pass
`source.NewSource`.)

- [ ] **Step 2: Run to verify failure**

```bash
cd adapters/native/source/filecatalog && go test ./... -run TestParseJSON -v
```
Expected: FAIL — `ParseJSON` undefined.

- [ ] **Step 3: Extract `ParseJSON`** — in `loader.go`, pull the JSON branch's
parse+validate loop into an exported function and call it from `Load`:

```go
// ParseJSON parses a catalogue JSON document into validated source snapshots,
// running every record through source.NewSource — the same gate Load applies to
// a file. It does no I/O, so the delivery surface can validate an uploaded
// catalogue body before writing it (DESIGN §7.2, POST /api/catalog).
func ParseJSON(raw []byte) ([]source.Snapshot, error) {
	var wire wireCatalog
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("filecatalog: parse json: %w", err)
	}
	out := make([]source.Snapshot, 0, len(wire.Sources))
	for _, ws := range wire.Sources {
		src, verr := source.NewSource(ws.toSpec())
		if verr != nil {
			return nil, fmt.Errorf("filecatalog: source %q: %w", ws.ID, verr)
		}
		out = append(out, src.Snapshot())
	}
	return out, nil
}
```

Then in `Load`, the JSON branch becomes `return ParseJSON(raw)` (keep the YAML
branch as-is with its own inline loop, or extract a shared `snapshotsFrom(wire)`
helper both call — whichever keeps the file ≤ 500 and DRY). Confirm the existing
`filecatalog` conformance/loader tests still pass unchanged.

- [ ] **Step 4: Failing test (surface)** — `cmd/testmaker/server_catalog_test.go`:

```go
package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadCatalogValidatesWritesAndSyncs(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "sources.json")
	ts := serverWithCatalogPath(t, catPath) // helper below: zero-auth server, catalogPath set

	valid := `{"sources":[{"id":"s1","name":"S1","urls":["https://x"],"accessClasses":["downloadable-artifact"],"license":{"category":"public-domain","redistributable":"yes"}}]}`
	res := postRaw(t, ts.URL+"/api/catalog", "", []byte(valid))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("upload valid → %d, want 200", res.StatusCode)
	}
	// File was written…
	if _, err := os.Stat(catPath); err != nil {
		t.Fatalf("catalogue not written: %v", err)
	}
	// …and the catalogue is now queryable.
	list := get(t, ts.URL+"/api/sources", "")
	var page struct{ Total int }
	decodeBody(t, list, &page)
	if page.Total != 1 {
		t.Fatalf("synced total = %d, want 1", page.Total)
	}

	// Invalid body is a 400 and does NOT overwrite the file.
	bad := postRaw(t, ts.URL+"/api/catalog", "", []byte(`{"sources":[{"name":"no id"}]}`))
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("upload invalid → %d, want 400", bad.StatusCode)
	}
}
```

Add `serverWithCatalogPath` + `postRaw` helpers (a `serverDeps` with `catalog`
wired to a `filecatalog` loader on `catPath` and `catalogPath: catPath`; a POST
of raw bytes). Mirror the existing sourcing-test wiring.

- [ ] **Step 5: Implement `server_catalog.go`**

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// maxCatalogBody caps a catalogue upload (larger than the 1 MiB default: a full
// research catalogue is a few hundred KB, headroom for growth).
const maxCatalogBody = 4 << 20

// handleUploadCatalog validates an uploaded catalogue JSON body, writes it
// atomically to the configured catalogue path, then reloads it — so the console
// can edit the catalogue, not just re-sync the deploy-time file. A parse/
// validation failure is a 400 and never touches the file (DESIGN §7.2).
func (s *server) handleUploadCatalog(w http.ResponseWriter, r *http.Request) {
	if s.catalogPath == "" {
		s.writeError(w, r, shared.ErrUnsupported.WithMessage("no catalogue path configured"))
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCatalogBody))
	if err != nil {
		s.writeError(w, r, shared.ErrInvalid.WithMessage("catalogue body too large or unreadable"))
		return
	}
	if _, perr := filecatalog.ParseJSON(raw); perr != nil {
		s.writeError(w, r, fmt.Errorf("%w: %s", shared.ErrInvalid, perr))
		return
	}
	if werr := atomicWrite(s.catalogPath, raw); werr != nil {
		s.writeError(w, r, werr)
		return
	}
	n, serr := s.cat.Sync(r.Context())
	if serr != nil {
		s.writeError(w, r, serr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"synced": n})
}

// atomicWrite writes bytes to a temp file in the target's directory and renames
// it over the target, so a crashed or concurrent write never leaves a
// half-written catalogue on disk.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create catalogue dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".catalog-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp catalogue: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp catalogue: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp catalogue: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace catalogue: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Wire `catalogPath` + route**

Add `catalogPath string` to `server` and `serverDeps`; set it in `newServer`
(`catalogPath: d.catalogPath`). In `runServer`, pass `catalogPath: cfg.Catalog`.
Register the route (operator):

```go
	mux.HandleFunc("POST /api/catalog", s.requireOperator(s.handleUploadCatalog))
```

Add `"POST /api/catalog"` to the `handleIndex` list.

- [ ] **Step 7: Run tests + gates**

```bash
cd adapters/native/source/filecatalog && go test ./... -short -race
cd ../../../../cmd/testmaker && go test . -short -race
make lint && make test
```
Expected: green.

- [ ] **Step 8: Commit**

```bash
git add adapters/native/source/filecatalog cmd/testmaker
git commit -m "Block 14: POST /api/catalog upload + filecatalog.ParseJSON extraction"
```

---

# Phase 5 — Async ingest jobs

### Task 17: In-memory job registry (clock-injected, bounded) ✅

**Files:**
- Create: `cmd/testmaker/jobs.go`
- Create: `cmd/testmaker/jobs_test.go`

**Interfaces:**
- Consumes: `domain/clock`, `app/ingest.Report`.
- Produces: `job` (wire shape C6); `jobRegistry` +
  `newJobRegistry(clk clock.Clock, max int, idFn func() string) *jobRegistry`;
  `create(kind, sourceID string) job`, `start(id string)`,
  `finish(id string, rep *ingest.Report, runErr error)`, `get(id) (job, bool)`,
  `list() []job` (newest first). Task 18 drives all of them.

- [ ] **Step 1: Failing test** — `cmd/testmaker/jobs_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run TestJob -v
```
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `jobs.go`**

```go
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
```

- [ ] **Step 4: Run the tests**

```bash
cd cmd/testmaker && go test . -run TestJob -v
```
Expected: PASS.

- [ ] **Step 5: Gates + size**

```bash
make lint && make test && wc -l cmd/testmaker/jobs.go
```
Expected: green; ≤ 500.

- [ ] **Step 6: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: in-memory bounded ingest job registry (clock-injected)"
```

---

### Task 18: Async ingest endpoints (202 + poll) ✅

**Files:**
- Modify: `cmd/testmaker/server_sourcing.go` (`ingestReq`/`ingestLLMReq` gain `async`; handlers branch; add `runIngestJob`/`runIngestLLMJob`)
- Create: `cmd/testmaker/server_jobs.go` (`handleListJobs`, `handleGetJob`)
- Modify: `cmd/testmaker/server.go` (`server`/`serverDeps` gain `jobs`+`ingestTimeout`; register routes; index)
- Create: `cmd/testmaker/server_jobs_test.go`

**Interfaces:**
- Consumes: `jobRegistry`, `semaphore`, `ingest.Service`, `paginate`.
- Produces: `server.jobs *jobRegistry`, `server.ingestTimeout time.Duration`;
  `runIngestJob(id string, snap source.Snapshot, limit int)`,
  `runIngestLLMJob(id string, req ingest.LLMExtractRequest)`; `handleListJobs`,
  `handleGetJob`; `GET /api/jobs` (paginated, newest first), `GET /api/jobs/{id}`.
  `"async": true` on either ingest endpoint → `202` + the job.

- [ ] **Step 1: Failing test** — `cmd/testmaker/server_jobs_test.go`:

```go
package main

import (
	"net/http"
	"testing"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// Uses the deterministic ingest source path (VIQT/ASVAB) already exercised in
// server_sourcing_test.go; here we assert the async envelope + poll to done.
func TestAsyncIngestReturns202ThenCompletes(t *testing.T) {
	ts, sourceID := asyncIngestServer(t) // helper: server with a fake fetcher+normalizer wired, jobs registry, clock

	res := post(t, ts.URL+"/api/sources/"+sourceID+"/ingest", "", map[string]any{"async": true})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("async ingest → %d, want 202", res.StatusCode)
	}
	var j struct {
		ID    string
		State string
	}
	decodeBody(t, res, &j)
	if j.ID == "" || (j.State != "queued" && j.State != "running") {
		t.Fatalf("202 job = %+v", j)
	}

	// Poll GET /api/jobs/{id} until terminal (bounded loop; the fake fetcher is
	// synchronous so this converges immediately — no sleep, just retries).
	var final struct {
		State  string
		Report *struct{ Saved int }
	}
	for i := 0; i < 100; i++ {
		decodeBody(t, get(t, ts.URL+"/api/jobs/"+j.ID, ""), &final)
		if final.State == "done" || final.State == "failed" {
			break
		}
	}
	if final.State != "done" {
		t.Fatalf("job did not complete: state=%s", final.State)
	}

	// GET /api/jobs lists it.
	var page struct{ Total int }
	decodeBody(t, get(t, ts.URL+"/api/jobs", ""), &page)
	if page.Total < 1 {
		t.Fatal("job list empty after a run")
	}
	_ = clock.System
}
```

Write `asyncIngestServer` using this file's existing fetch-stub + normalizer
wiring, plus `jobs: newJobRegistry(clock.System(), 100, nil)` and
`ingestTimeout: 30 * time.Second` in `serverDeps`.

- [ ] **Step 2: Run to verify failure**

```bash
cd cmd/testmaker && go test . -run TestAsyncIngest -v
```
Expected: FAIL — 202 path / job routes absent.

- [ ] **Step 3: Add async fields + branch the ingest handlers**

In `server_sourcing.go`, extend the request bodies:

```go
type ingestReq struct {
	Limit int  `json:"limit"`
	Async bool `json:"async"`
}
```
```go
type ingestLLMReq struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"maxTokens"`
	Limit     int    `json:"limit"`
	TestType  string `json:"testType"`
	Async     bool   `json:"async"`
}
```

Rewrite `handleIngest` so async branches before the sync semaphore gate:

```go
func (s *server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var req ingestReq
	if !decodeOptionalJSON(w, r, &req) {
		return
	}
	snap, err := s.cat.Get(r.Context(), source.SourceID(r.PathValue("id")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Async && s.jobs != nil {
		j := s.jobs.create("ingest", string(snap.ID))
		go s.runIngestJob(j.ID, snap, req.Limit)
		writeJSON(w, http.StatusAccepted, j)
		return
	}
	if s.ingestSem != nil {
		if !s.ingestSem.tryAcquire() {
			writeAuthError(w, http.StatusTooManyRequests, "limit.ingest", "another ingest is in progress")
			return
		}
		defer s.ingestSem.release()
	}
	rep, err := s.ingestSvc.Ingest(r.Context(), snap, req.Limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
```

Add the background runner (uses `context.Background()` + the configured timeout,
never the request context, which is cancelled once the 202 is written):

```go
// runIngestJob executes a deterministic ingest on a background context and
// records the outcome on the job. It acquires the shared ingest semaphore
// (blocking, so a queued job waits its turn) and honours the configured
// timeout. The request context is gone by now, so it uses its own.
func (s *server) runIngestJob(id string, snap source.Snapshot, limit int) {
	ctx, cancel := context.WithTimeout(context.Background(), s.ingestTimeout)
	defer cancel()
	if s.ingestSem != nil {
		if aerr := s.ingestSem.acquire(ctx); aerr != nil {
			s.jobs.finish(id, nil, aerr)
			return
		}
		defer s.ingestSem.release()
	}
	s.jobs.start(id)
	rep, err := s.ingestSvc.Ingest(ctx, snap, limit)
	if err != nil {
		s.jobs.finish(id, nil, err)
		return
	}
	s.jobs.finish(id, &rep, nil)
}
```

Do the same for `handleIngestLLM`: async branch creates a `"ingest-llm"` job and
calls `go s.runIngestLLMJob(j.ID, extractReq)` where `extractReq` is the
`ingest.LLMExtractRequest` it already builds; the runner mirrors `runIngestJob`
but calls `s.ingestSvc.IngestLLM(ctx, req)`. Keep the sync path (incl. the
`s.llm == nil` 503 check and the semaphore gate) unchanged.

- [ ] **Step 4: Job endpoints** — `cmd/testmaker/server_jobs.go`:

```go
package main

import "net/http"

// handleListJobs returns the recent async ingest jobs, newest first, paginated.
func (s *server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		writeJSON(w, http.StatusOK, paginate([]job{}, 0, 0))
		return
	}
	limit, offset, ok := pageParams(w, r.URL.Query())
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
```

- [ ] **Step 5: Wire registry + routes** — in `server.go` add
`jobs *jobRegistry` and `ingestTimeout time.Duration` to `server`; add the same
to `serverDeps`; set them in `newServer`
(`jobs: d.jobs, ingestTimeout: d.ingestTimeout` — default the timeout to e.g.
`10 * time.Minute` when zero). Register (operator-gated):

```go
	mux.HandleFunc("GET /api/jobs", s.requireOperator(s.handleListJobs))
	mux.HandleFunc("GET /api/jobs/{id}", s.requireOperator(s.handleGetJob))
```

In `runServer` build the registry and timeout from config:

```go
	jobs := newJobRegistry(clock.System(), 100, nil)
	// serverDeps: jobs: jobs, ingestTimeout: time.Duration(cfg.Limits.IngestTimeoutSeconds) * time.Second,
```

Add `"GET /api/jobs"`, `"GET /api/jobs/{id}"` to `handleIndex`.

- [ ] **Step 6: Run tests + gates**

```bash
cd cmd/testmaker && go test . -short -race && make lint && make test && wc -l cmd/testmaker/server_sourcing.go
```
Expected: green; `server_sourcing.go` ≤ 500 (if over, move the two runners into
`server_jobs.go`).

- [ ] **Step 7: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: async ingest jobs (202 + GET /api/jobs poll)"
```

---

# Phase 6 — Web app foundation

> From here the deliverables are TypeScript in `web/`. The TDD rhythm holds
> where logic lives (API client, hooks, routing, answer controls): write the
> Vitest spec, watch it fail (`make webui-test`), implement, watch it pass.
> Pure-presentation components are create-then-render-check. `make webui-test`
> and `make webui-lint` are the web gates; `make lint`/`make test` (Go) must
> stay green too — they are unaffected by `web/` (arch-lint excludes it).

### Task 19: Scaffold `web/`, wire the build → embed loop ✅

**Files (all created):**
- `web/package.json`, `web/bunfig.toml`
- `web/vite.config.ts`, `web/tsconfig.json`, `web/tsconfig.node.json`
- `web/index.html`
- `web/src/main.tsx`, `web/src/App.tsx`, `web/src/index.css`
- `web/src/vite-env.d.ts`
- `web/.eslintrc.cjs` (or `eslint.config.js`)
- `web/src/smoke.test.ts` (proves the toolchain runs)

**Interfaces:**
- Consumes: `webui.FS()` (Task 1) — the build must land in
  `cmd/testmaker/webui/dist`.
- Produces: a runnable Vite app + Vitest; `make webui` produces
  `cmd/testmaker/webui/dist/index.html` so `webui.FS()` returns `ok=true` and
  the Go SPA tests serve HTML.

- [ ] **Step 1: `web/package.json`**

```json
{
  "name": "testmaker-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "typecheck": "tsc --noEmit && eslint .",
    "test": "vitest",
    "test:run": "vitest run"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.59.0",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.26.0"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4.0.0",
    "@testing-library/jest-dom": "^6.5.0",
    "@testing-library/react": "^16.0.1",
    "@testing-library/user-event": "^14.5.2",
    "@types/react": "^18.3.1",
    "@types/react-dom": "^18.3.1",
    "@typescript-eslint/eslint-plugin": "^8.8.0",
    "@typescript-eslint/parser": "^8.8.0",
    "@vitejs/plugin-react": "^4.3.0",
    "eslint": "^9.11.0",
    "eslint-plugin-react-hooks": "^5.0.0",
    "jsdom": "^25.0.0",
    "tailwindcss": "^4.0.0",
    "typescript": "^5.6.0",
    "vite": "^6.0.0",
    "vitest": "^2.1.0"
  }
}
```

- [ ] **Step 2: `web/vite.config.ts`** (build lands in the embed dir; dev proxies `/api`)

```ts
/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";

// The production build emits into the Go embed directory, so `make webui`
// followed by `go build` ships the SPA inside the binary (ADR-0005). The dev
// server proxies /api to a locally running `testmaker -serve` so the two-
// terminal dev loop (make serve + make webui-dev) needs no CORS.
export default defineConfig({
  plugins: [react(), tailwind()],
  build: {
    outDir: "../cmd/testmaker/webui/dist",
    emptyOutDir: true, // wipes dist (incl. .keep — the Makefile touches it back)
  },
  server: {
    port: 5173,
    proxy: { "/api": "http://localhost:8080" },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
  },
});
```

- [ ] **Step 3: `web/tsconfig.json` + `web/tsconfig.node.json`** — standard Vite
React strict config:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "useDefineForClassFields": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "types": ["vitest/globals", "@testing-library/jest-dom"]
  },
  "include": ["src"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

`tsconfig.node.json`:

```json
{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true,
    "strict": true
  },
  "include": ["vite.config.ts"]
}
```

- [ ] **Step 4: `web/index.html`**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Testmaker</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 5: entrypoints** — `web/src/index.css`:

```css
@import "tailwindcss";
```

`web/src/main.tsx`:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
```

`web/src/App.tsx` (placeholder — Task 22 fills the real shell):

```tsx
export default function App() {
  return <div className="p-8 text-lg">Testmaker web app — foundation OK</div>;
}
```

`web/src/vite-env.d.ts`: `/// <reference types="vite/client" />`

`web/src/test-setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 6: Smoke test** — `web/src/smoke.test.ts`:

```ts
import { describe, expect, it } from "vitest";

describe("toolchain", () => {
  it("runs vitest", () => {
    expect(1 + 1).toBe(2);
  });
});
```

- [ ] **Step 7: Install + run the web tests**

```bash
cd web && bun install
bun run test:run
```
Expected: PASS (1 test). Fixes any config wiring before proceeding.

- [ ] **Step 8: Build → embed, verify Go sees it**

```bash
cd /Users/mariotoffia/progs/github/testmaker && make webui
ls cmd/testmaker/webui/dist/index.html          # exists
cd cmd/testmaker && go test ./webui/ -run TestFSWithoutBuild -v
```
Expected: `webui.FS()` now returns `ok=true`; the SPA fallback tests from Task 2
that allowed either outcome now hit the HTML branch. Then **remove the built
assets** so the commit stays source-only (the placeholder returns):

```bash
cd /Users/mariotoffia/progs/github/testmaker
git status --short cmd/testmaker/webui/dist      # only .keep should be tracked
git checkout -- cmd/testmaker/webui/dist/.keep 2>/dev/null || true
```

- [ ] **Step 9: Go gates still green (Bun-independent)**

```bash
make lint && make test    # web/ is arch-lint-excluded; dist is placeholder again
```
Expected: green.

- [ ] **Step 10: Commit** (source only — never the built assets or node_modules)

```bash
git add web .gitignore
git commit -m "Block 14: scaffold web/ (Vite+React+TS+Tailwind) and build→embed loop"
```

---

### Task 20: API client + wire types ✅

**Files (created):**
- `web/src/api/types.ts`
- `web/src/api/client.ts`
- `web/src/api/client.test.ts`

**Interfaces:**
- Consumes: the `/api` contract (C4/C5/C6/C7/C9).
- Produces: `ApiError`; `apiFetch<T>(path, opts) : Promise<T>`; typed helpers
  `listItems`, `getItem`, `listSources`, `generate`, `compose`, `listTests`,
  `mintInvite`, `previewInvite`, `startInvite`, `answer`, `complete`, `score`,
  `listJobs`, `getJob`, `whoami`, `uploadCatalog`, `syncCatalog`, `ingest`; a
  `lastServerSkewMs` accessor (C10). Tokens are passed per call; Task 21 injects
  them from context.

- [ ] **Step 1: Failing tests** — `web/src/api/client.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, apiFetch, serverSkewMs } from "./client";

function mockFetch(status: number, body: unknown, headers: Record<string, string> = {}) {
  return vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json", ...headers },
    }),
  );
}

afterEach(() => vi.restoreAllMocks());

describe("apiFetch", () => {
  it("returns the decoded body on 2xx", async () => {
    vi.stubGlobal("fetch", mockFetch(200, { items: [{ ID: "a" }], total: 1, limit: 50, offset: 0 }));
    const page = await apiFetch<{ items: { ID: string }[]; total: number }>("/api/items");
    expect(page.total).toBe(1);
    expect(page.items[0].ID).toBe("a");
  });

  it("throws ApiError carrying code + status on non-2xx", async () => {
    vi.stubGlobal("fetch", mockFetch(404, { error: "item not found", code: "item.unknown", class: "not_found" }));
    await expect(apiFetch("/api/items/x")).rejects.toMatchObject({
      status: 404,
      code: "item.unknown",
    } satisfies Partial<ApiError>);
  });

  it("sends the bearer token when provided", async () => {
    const f = mockFetch(200, { role: "operator" });
    vi.stubGlobal("fetch", f);
    await apiFetch("/api/auth/whoami", { token: "OP" });
    const init = f.mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer OP");
  });

  it("captures server clock skew from the Date header", async () => {
    const serverNow = new Date("2030-01-01T00:00:10Z"); // 10s ahead of a fake local clock
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    vi.stubGlobal("fetch", mockFetch(200, {}, { Date: serverNow.toUTCString() }));
    await apiFetch("/api");
    expect(serverSkewMs()).toBeGreaterThanOrEqual(9000);
    vi.useRealTimers();
  });
});
```

- [ ] **Step 2: Run to verify failure**

```bash
cd web && bun run test:run src/api/client.test.ts
```
Expected: FAIL — module missing.

- [ ] **Step 3: `web/src/api/types.ts`** (the single wire-type source, C9)

```ts
// Wire types mirror the Go delivery API (DESIGN §7.1). Domain snapshots are
// PascalCase and marshalled as-is; cmd-local bodies (jobs, invites, pages) are
// camelCase. Nullable Go slices arrive as null → model as `T[] | null`.
// time.Duration is nanoseconds; time.Time is RFC3339.

export type Ns = number;
export const NS_PER_MS = 1_000_000;
export const NS_PER_SEC = 1_000_000_000;

export type AnswerFormat = "multiple-choice" | "open-numeric" | "true-false-cannotsay";
export type SessionState = "created" | "in-progress" | "completed" | "abandoned";
export type DeliveryPolicy = "fixed-increasing" | "adaptive";
export type MediaKind = "" | "image" | "svg" | "grid" | "figure";

export interface Page<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

export interface StimulusPart {
  Text: string;
  MediaKind: MediaKind;
  MediaRef: string;
}
export interface Option {
  ID: string;
  Text: string;
  MediaKind: MediaKind;
  MediaRef: string;
}
export interface AnswerKey {
  OptionID: string;
  Numeric: number;
  Verdict: string;
  Tolerance: number;
}
export interface Difficulty {
  Band: number;
}
export interface ItemSnapshot {
  ID: string;
  Provenance: { SourceID: string; Origin: string; Redistributable: string };
  TestType: string;
  Family: string;
  Stimulus: StimulusPart[] | null;
  AnswerFormat: AnswerFormat;
  Options: Option[] | null;
  AnswerKey: AnswerKey; // present in the operator bank view; absent-valued (zero) on the taker's presented item
  Explanation: string;
  Difficulty: Difficulty;
}

export interface Timing {
  Total: Ns;
  PerItem: Ns;
}
export interface Presented {
  ItemID: string;
  Difficulty: number;
  Section: number;
  DeliveredAt: string;
}
export interface SessionSnapshot {
  ID: string;
  TestID: string;
  Policy: DeliveryPolicy;
  State: SessionState;
  Timing: Timing;
  StartedAt: string;
  EndedAt: string;
  Presented: Presented;
  Responses: unknown[] | null;
  Version: number;
}
export interface Delivery {
  Session: SessionSnapshot;
  Item: ItemSnapshot | null;
  Deadline: string;
}
export interface StartResponse extends Delivery {
  SessionToken: string;
}

export interface SectionSnapshot {
  Title: string;
  Family: string;
  Timing: Timing;
  Items: { ItemID: string; Difficulty: number }[] | null;
}
export interface TestSnapshot {
  ID: string;
  Title: string;
  Policy: DeliveryPolicy;
  Timing: Timing;
  Families: string[] | null;
  Sections: SectionSnapshot[] | null;
}

export interface ItemFeedback {
  ItemID: string;
  Correct: boolean;
  Given: string;
  CorrectAnswer: string;
  Explanation: string;
  Elapsed: Ns;
}
export interface Score {
  Raw: number;
  Max: number;
  Ability: number;
  Normed: boolean;
  Percentile: number;
  ScaledIQ: number;
  Band: string;
  Speed: { Total: Ns; Mean: Ns; CorrectPerMinute: number };
  Items: ItemFeedback[] | null;
  DegradedFeedback: number;
}

export interface SourceSnapshot {
  ID: string;
  Name: string;
  Provider: string;
  Category: string;
  Families: string[] | null;
  TestTypes: string[] | null;
  License: { Category: string; Detail: string; Redistributable: string };
  Extraction: { Method: string; Auth: string; ItemsAs: string; Notes: string };
  ItemCount: number;
}

// cmd-local (camelCase)
export interface Job {
  id: string;
  kind: "ingest" | "ingest-llm";
  sourceId: string;
  state: "queued" | "running" | "done" | "failed";
  report?: IngestReport;
  error?: string;
  createdAt: string;
  startedAt: string;
  endedAt: string;
}
export interface IngestReport {
  SourceID: string;
  Fetched: number;
  Normalized: number;
  Saved: number;
  Skipped: number;
  Note: string;
}
export interface Invite {
  token: string;
  url: string;
  expiresAt: string;
}
export interface InvitePreview {
  testId: string;
  title: string;
  policy: DeliveryPolicy;
  totalSeconds: number;
  perItemSeconds: number;
  itemCount: number;
  sections: { title: string; family: string; itemCount: number; totalSeconds: number; perItemSeconds: number }[];
  expiresAt: string;
}
export interface Whoami {
  role: "operator" | "taker" | "anonymous";
  mode: "token" | "none";
}
```

- [ ] **Step 4: `web/src/api/client.ts`**

```ts
import type {
  Delivery, InvitePreview, Invite, ItemSnapshot, Job, Page, Score,
  SourceSnapshot, StartResponse, TestSnapshot, Whoami,
} from "./types";

// ApiError carries the transport status plus the server's error code/message so
// the UI can branch on 401/403/409 and show a safe message (C4).
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

let skewMs = 0;
// serverSkewMs is (server clock − local clock) in ms, refreshed from every
// response's Date header, so the player can render deadlines against the
// server's clock rather than a possibly-wrong local one (C10).
export function serverSkewMs(): number {
  return skewMs;
}

interface FetchOpts {
  method?: string;
  body?: unknown;
  token?: string;
  raw?: string; // raw text body (catalogue upload) — bypasses JSON.stringify
}

export async function apiFetch<T>(path: string, opts: FetchOpts = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
  let body: BodyInit | undefined;
  if (opts.raw !== undefined) {
    headers["Content-Type"] = "application/json";
    body = opts.raw;
  } else if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  const res = await fetch(path, { method: opts.method ?? (body ? "POST" : "GET"), headers, body });

  const date = res.headers.get("Date");
  if (date) skewMs = Date.parse(date) - Date.now();

  const text = await res.text();
  const parsed = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new ApiError(res.status, parsed.code ?? "error", parsed.error ?? res.statusText);
  }
  return parsed as T;
}

// --- typed endpoint helpers (token threaded in by the auth context, Task 21) ---
export const api = {
  whoami: (token?: string) => apiFetch<Whoami>("/api/auth/whoami", { token }),

  listSources: (token: string, q = "") => apiFetch<Page<SourceSnapshot>>(`/api/sources${q}`, { token }),
  getSource: (token: string, id: string) => apiFetch<SourceSnapshot>(`/api/sources/${id}`, { token }),
  syncCatalog: (token: string) => apiFetch<{ synced: number }>("/api/catalog/sync", { token, method: "POST" }),
  uploadCatalog: (token: string, json: string) => apiFetch<{ synced: number }>("/api/catalog", { token, raw: json }),

  listItems: (token: string, q = "") => apiFetch<Page<ItemSnapshot>>(`/api/items${q}`, { token }),
  getItem: (token: string, id: string) => apiFetch<ItemSnapshot>(`/api/items/${id}`, { token }),
  generate: (token: string, body: object) => apiFetch<unknown>("/api/items/generate", { token, body }),

  ingest: (token: string, id: string, body: object) => apiFetch<Job | IngestSync>(`/api/sources/${id}/ingest`, { token, body }),
  ingestLLM: (token: string, id: string, body: object) => apiFetch<Job | IngestSync>(`/api/sources/${id}/ingest-llm`, { token, body }),
  listJobs: (token: string, q = "") => apiFetch<Page<Job>>(`/api/jobs${q}`, { token }),
  getJob: (token: string, id: string) => apiFetch<Job>(`/api/jobs/${id}`, { token }),

  listTests: (token: string, q = "") => apiFetch<Page<TestSnapshot>>(`/api/tests${q}`, { token }),
  getTest: (token: string, id: string) => apiFetch<TestSnapshot>(`/api/tests/${id}`, { token }),
  compose: (token: string, body: object) => apiFetch<TestSnapshot>("/api/tests", { token, body }),
  mintInvite: (token: string, id: string, body: object) => apiFetch<Invite>(`/api/tests/${id}/invites`, { token, body }),

  previewInvite: (invite: string) => apiFetch<InvitePreview>("/api/invites/preview", { token: invite }),
  startInvite: (invite: string) => apiFetch<StartResponse>("/api/invites/start", { token: invite, method: "POST" }),
  answer: (token: string, sid: string, body: object) => apiFetch<Delivery>(`/api/sessions/${sid}/answers`, { token, body }),
  complete: (token: string, sid: string) => apiFetch<unknown>(`/api/sessions/${sid}/complete`, { token, method: "POST" }),
  score: (token: string, sid: string) => apiFetch<Score>(`/api/sessions/${sid}/score`, { token }),
};

type IngestSync = import("./types").IngestReport;
```

- [ ] **Step 5: Run the tests**

```bash
cd web && bun run test:run src/api/client.test.ts
```
Expected: PASS.

- [ ] **Step 6: Typecheck + Go gates**

```bash
cd web && bun run typecheck
cd .. && make lint && make test
```
Expected: green.

- [ ] **Step 7: Commit**

```bash
git add web
git commit -m "Block 14: web API client + wire types (skew capture, ApiError)"
```

---

### Task 21: Auth context, operator login, role routing ✅

**Files (created):**
- `web/src/auth/AuthContext.tsx`
- `web/src/auth/useAuth.ts`
- `web/src/auth/RequireOperator.tsx`
- `web/src/pages/Login.tsx`
- `web/src/auth/AuthContext.test.tsx`

**Interfaces:**
- Consumes: `api.whoami`.
- Produces: `AuthProvider`, `useAuth() : { role, mode, operatorToken, login(token), logout() }`;
  `<RequireOperator>` route guard (redirects to `/login` when not operator);
  operator token persisted in `localStorage` under `tm.operatorToken`. Player
  routes are token-in-fragment (Task 30), not this context.

- [ ] **Step 1: Failing test** — `web/src/auth/AuthContext.test.tsx`:

```tsx
import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "./AuthContext";
import { useAuth } from "./useAuth";

function Probe() {
  const { role, login } = useAuth();
  return (
    <div>
      <span>role:{role}</span>
      <button onClick={() => login("OP")}>login</button>
    </div>
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("AuthProvider", () => {
  it("resolves role via whoami after login", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ role: "operator", mode: "token" }), {
        status: 200, headers: { "Content-Type": "application/json" },
      })),
    );
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await userEvent.click(screen.getByText("login"));
    await waitFor(() => expect(screen.getByText("role:operator")).toBeInTheDocument());
    expect(localStorage.getItem("tm.operatorToken")).toBe("OP");
  });
});
```

- [ ] **Step 2: Run to verify failure**

```bash
cd web && bun run test:run src/auth/AuthContext.test.tsx
```
Expected: FAIL — modules missing.

- [ ] **Step 3: Implement the context**

`web/src/auth/AuthContext.tsx`:

```tsx
import { createContext, useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "../api/client";
import type { Whoami } from "../api/types";

interface AuthValue {
  role: Whoami["role"];
  mode: Whoami["mode"] | "unknown";
  operatorToken: string;
  login: (token: string) => Promise<void>;
  logout: () => void;
}

// eslint-disable-next-line react-refresh/only-export-components
export const AuthCtx = createContext<AuthValue | null>(null);

const KEY = "tm.operatorToken";

export function AuthProvider({ children }: { children: ReactNode }) {
  const [operatorToken, setToken] = useState(() => localStorage.getItem(KEY) ?? "");
  const [role, setRole] = useState<AuthValue["role"]>("anonymous");
  const [mode, setMode] = useState<AuthValue["mode"]>("unknown");

  const resolve = useCallback(async (token: string) => {
    const who = await api.whoami(token || undefined);
    setRole(who.role);
    setMode(who.mode);
  }, []);

  // On mount, learn the server's auth mode: none-mode makes everyone operator,
  // so the console is reachable without a login.
  useEffect(() => {
    void resolve(operatorToken).catch(() => setRole("anonymous"));
  }, [resolve, operatorToken]);

  const login = useCallback(
    async (token: string) => {
      await resolve(token); // throws on a bad token → caller shows the error
      localStorage.setItem(KEY, token);
      setToken(token);
    },
    [resolve],
  );

  const logout = useCallback(() => {
    localStorage.removeItem(KEY);
    setToken("");
    setRole("anonymous");
  }, []);

  const value = useMemo(
    () => ({ role, mode, operatorToken, login, logout }),
    [role, mode, operatorToken, login, logout],
  );
  return <AuthCtx.Provider value={value}>{children}</AuthCtx.Provider>;
}
```

`web/src/auth/useAuth.ts`:

```ts
import { useContext } from "react";
import { AuthCtx } from "./AuthContext";

export function useAuth() {
  const ctx = useContext(AuthCtx);
  if (!ctx) throw new Error("useAuth must be used within an AuthProvider");
  return ctx;
}
```

`web/src/auth/RequireOperator.tsx`:

```tsx
import { Navigate, Outlet } from "react-router-dom";
import { useAuth } from "./useAuth";

// RequireOperator guards the console. In none mode the server reports everyone
// as operator, so this is transparently open for local development.
export function RequireOperator() {
  const { role } = useAuth();
  if (role !== "operator") return <Navigate to="/login" replace />;
  return <Outlet />;
}
```

`web/src/pages/Login.tsx`:

```tsx
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../auth/useAuth";

// Login stores the operator token (from the server's config on first run) and
// verifies it via whoami. There is no password/account — single-tenant by
// design (ADR-0006); this is the operator credential, nothing more.
export default function Login() {
  const { login } = useAuth();
  const nav = useNavigate();
  const [token, setToken] = useState("");
  const [err, setErr] = useState("");
  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      await login(token.trim());
      nav("/");
    } catch {
      setErr("That operator token was not accepted.");
    }
  }
  return (
    <form onSubmit={submit} className="mx-auto mt-24 max-w-sm space-y-4 p-6">
      <h1 className="text-xl font-semibold">Operator sign-in</h1>
      <input
        type="password"
        value={token}
        onChange={(e) => setToken(e.target.value)}
        placeholder="operator token"
        className="w-full rounded border px-3 py-2"
        aria-label="operator token"
      />
      {err && <p className="text-sm text-red-600">{err}</p>}
      <button className="w-full rounded bg-slate-800 py-2 text-white">Sign in</button>
    </form>
  );
}
```

- [ ] **Step 4: Run the test**

```bash
cd web && bun run test:run src/auth/AuthContext.test.tsx
```
Expected: PASS.

- [ ] **Step 5: Typecheck + Go gates + commit**

```bash
cd web && bun run typecheck && cd .. && make lint && make test
git add web && git commit -m "Block 14: web auth context, operator login, role guard"
```

---

### Task 22: App shell + routing ✅

**Files:**
- Modify: `web/src/App.tsx`
- Create: `web/src/components/Layout.tsx`
- Create: `web/src/pages/NotFound.tsx`
- Create placeholder pages (filled in Phases 7–8): `web/src/pages/Dashboard.tsx`, `Sources.tsx`, `Items.tsx`, `Generate.tsx`, `Compose.tsx`, `Tests.tsx`, `Jobs.tsx`, `Take.tsx`
- Create: `web/src/App.test.tsx`

**Interfaces:**
- Consumes: `AuthProvider`, `RequireOperator`, all pages.
- Produces: the route table — `/login`, `/take` (public player), everything
  else operator-guarded under `Layout`. Phases 7–8 replace the page
  placeholders; the routes are stable from here.

- [ ] **Step 1: Failing test** — `web/src/App.test.tsx`:

```tsx
import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "./auth/AuthContext";
import { AppRoutes } from "./App";

function renderAt(path: string) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify({ role: "operator", mode: "none" }), {
      status: 200, headers: { "Content-Type": "application/json" },
    })),
  );
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>
        <MemoryRouter initialEntries={[path]}>
          <AppRoutes />
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => vi.restoreAllMocks());

describe("routing", () => {
  it("renders the login page", () => {
    renderAt("/login");
    expect(screen.getByText(/operator sign-in/i)).toBeInTheDocument();
  });
  it("renders the dashboard under the guard in none mode", async () => {
    renderAt("/");
    await waitFor(() => expect(screen.getByRole("navigation")).toBeInTheDocument());
  });
});
```

- [ ] **Step 2: Run to verify failure**

```bash
cd web && bun run test:run src/App.test.tsx
```
Expected: FAIL — `AppRoutes`/pages missing.

- [ ] **Step 3: `web/src/components/Layout.tsx`**

```tsx
import { NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth/useAuth";

const links = [
  ["/", "Dashboard"],
  ["/sources", "Sources"],
  ["/items", "Item bank"],
  ["/generate", "Generate"],
  ["/tests", "Tests"],
  ["/jobs", "Jobs"],
] as const;

export function Layout() {
  const { mode, logout } = useAuth();
  return (
    <div className="min-h-screen">
      <nav className="flex items-center gap-1 border-b bg-slate-50 px-4 py-2" aria-label="main">
        <span className="mr-4 font-semibold">Testmaker</span>
        {links.map(([to, label]) => (
          <NavLink
            key={to}
            to={to}
            end={to === "/"}
            className={({ isActive }) =>
              `rounded px-3 py-1 text-sm ${isActive ? "bg-slate-800 text-white" : "hover:bg-slate-200"}`
            }
          >
            {label}
          </NavLink>
        ))}
        {mode === "token" && (
          <button onClick={logout} className="ml-auto text-sm text-slate-500 hover:text-slate-800">
            Sign out
          </button>
        )}
      </nav>
      <main className="p-6">
        <Outlet />
      </main>
    </div>
  );
}
```

- [ ] **Step 4: `web/src/App.tsx`** (routes; player is public, console guarded)

```tsx
import { Route, Routes } from "react-router-dom";
import { AuthProvider } from "./auth/AuthContext";
import { RequireOperator } from "./auth/RequireOperator";
import { Layout } from "./components/Layout";
import Login from "./pages/Login";
import NotFound from "./pages/NotFound";
import Dashboard from "./pages/Dashboard";
import Sources from "./pages/Sources";
import Items from "./pages/Items";
import Generate from "./pages/Generate";
import Compose from "./pages/Compose";
import Tests from "./pages/Tests";
import Jobs from "./pages/Jobs";
import Take from "./pages/Take";

// AppRoutes is separated from App so tests can mount it inside a MemoryRouter.
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/take" element={<Take />} />
      <Route element={<RequireOperator />}>
        <Route element={<Layout />}>
          <Route index element={<Dashboard />} />
          <Route path="sources" element={<Sources />} />
          <Route path="items" element={<Items />} />
          <Route path="generate" element={<Generate />} />
          <Route path="compose" element={<Compose />} />
          <Route path="tests" element={<Tests />} />
          <Route path="jobs" element={<Jobs />} />
        </Route>
      </Route>
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <AppRoutes />
    </AuthProvider>
  );
}
```

Since `App` now owns `AuthProvider`, remove it from `main.tsx` (keep the
QueryClient + BrowserRouter there). Update `main.tsx` accordingly.

- [ ] **Step 5: Placeholder pages** — each of `Dashboard.tsx`, `Sources.tsx`,
`Items.tsx`, `Generate.tsx`, `Compose.tsx`, `Tests.tsx`, `Jobs.tsx`, `Take.tsx`
is a stub returning its name; `NotFound.tsx` returns a 404 message. Example
`web/src/pages/Dashboard.tsx`:

```tsx
export default function Dashboard() {
  return <h1 className="text-xl font-semibold">Dashboard</h1>;
}
```

- [ ] **Step 6: Run tests + typecheck + Go gates + commit**

```bash
cd web && bun run test:run && bun run typecheck
cd .. && make lint && make test
git add web && git commit -m "Block 14: web app shell, routing, guarded console layout"
```

---

# Phase 7 — Operator console

> Each task ends with `make webui-test && make webui-lint` green and a commit.
> The Go gates are unaffected but re-run them at the phase's end (Task 29).

### Task 23: Data hooks + dashboard ✅

**Files:**
- Create: `web/src/api/hooks.ts`
- Modify: `web/src/pages/Dashboard.tsx`
- Create: `web/src/components/Async.tsx` (loading/error wrapper)
- Create: `web/src/api/hooks.test.tsx`

**Interfaces:**
- Consumes: `api`, `useAuth`, `@tanstack/react-query`.
- Produces: `useSources`, `useItems`, `useTests`, `useJobs`, `useJob`, `useSource`, `useItem`
  (react-query hooks that thread the operator token + build the `Page<T>` query
  key); `useApiToken()`; `<Async>` wrapper. Every console page consumes these.

- [ ] **Step 1: Failing test** — `web/src/api/hooks.test.tsx`:

```tsx
import { describe, expect, it, vi, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { AuthProvider } from "../auth/AuthContext";
import { useItems } from "./hooks";

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <AuthProvider>{children}</AuthProvider>
    </QueryClientProvider>
  );
}

afterEach(() => vi.restoreAllMocks());

describe("useItems", () => {
  it("fetches a page of items", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          new Response(
            JSON.stringify(
              url.includes("whoami")
                ? { role: "operator", mode: "none" }
                : { items: [{ ID: "i1" }], total: 1, limit: 50, offset: 0 },
            ),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
      ),
    );
    const { result } = renderHook(() => useItems(""), { wrapper });
    await waitFor(() => expect(result.current.data?.total).toBe(1));
    expect(result.current.data?.items[0].ID).toBe("i1");
  });
});
```

- [ ] **Step 2: Run to verify failure**

```bash
cd web && bun run test:run src/api/hooks.test.tsx
```
Expected: FAIL — `useItems` missing.

- [ ] **Step 3: `web/src/api/hooks.ts`**

```ts
import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import { useAuth } from "../auth/useAuth";

// useApiToken returns the operator token (empty in none mode — the server
// ignores it there). Every operator query threads it so a token-mode deployment
// authorizes and a none-mode one is transparently open.
export function useApiToken(): string {
  return useAuth().operatorToken;
}

export function useSources(q = "") {
  const token = useApiToken();
  return useQuery({ queryKey: ["sources", q], queryFn: () => api.listSources(token, q) });
}
export function useSource(id: string) {
  const token = useApiToken();
  return useQuery({ queryKey: ["source", id], queryFn: () => api.getSource(token, id), enabled: !!id });
}
export function useItems(q = "") {
  const token = useApiToken();
  return useQuery({ queryKey: ["items", q], queryFn: () => api.listItems(token, q) });
}
export function useItem(id: string) {
  const token = useApiToken();
  return useQuery({ queryKey: ["item", id], queryFn: () => api.getItem(token, id), enabled: !!id });
}
export function useTests(q = "") {
  const token = useApiToken();
  return useQuery({ queryKey: ["tests", q], queryFn: () => api.listTests(token, q) });
}
export function useJobs(pollMs = 0) {
  const token = useApiToken();
  return useQuery({
    queryKey: ["jobs"],
    queryFn: () => api.listJobs(token),
    refetchInterval: pollMs || false,
  });
}
export function useJob(id: string, pollMs = 0) {
  const token = useApiToken();
  return useQuery({
    queryKey: ["job", id],
    queryFn: () => api.getJob(token, id),
    enabled: !!id,
    refetchInterval: (query) => {
      const s = query.state.data?.state;
      return pollMs && s !== "done" && s !== "failed" ? pollMs : false;
    },
  });
}
```

- [ ] **Step 4: `web/src/components/Async.tsx`**

```tsx
import type { ReactNode } from "react";
import { ApiError } from "../api/client";

// Async renders the standard loading / error / ready states for a react-query
// result so pages don't each reinvent them. A 401/403 surfaces as an auth hint.
export function Async<T>({
  query,
  children,
}: {
  query: { isLoading: boolean; error: unknown; data: T | undefined };
  children: (data: T) => ReactNode;
}) {
  if (query.isLoading) return <p className="text-slate-500">Loading…</p>;
  if (query.error) {
    const e = query.error;
    const msg = e instanceof ApiError ? `${e.status} ${e.message}` : "Something went wrong";
    return <p className="text-red-600">{msg}</p>;
  }
  if (query.data === undefined) return null;
  return <>{children(query.data)}</>;
}
```

- [ ] **Step 5: `web/src/pages/Dashboard.tsx`** (counts from the paginated totals)

```tsx
import { Link } from "react-router-dom";
import { useItems, useSources, useTests } from "../api/hooks";

function StatCard({ label, value, to }: { label: string; value: number | string; to: string }) {
  return (
    <Link to={to} className="rounded-lg border p-6 hover:bg-slate-50">
      <div className="text-3xl font-semibold">{value}</div>
      <div className="text-sm text-slate-500">{label}</div>
    </Link>
  );
}

export default function Dashboard() {
  const sources = useSources("?limit=1");
  const items = useItems("?limit=1");
  const tests = useTests("?limit=1");
  return (
    <div>
      <h1 className="mb-6 text-xl font-semibold">Dashboard</h1>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        <StatCard label="Sources" value={sources.data?.total ?? "…"} to="/sources" />
        <StatCard label="Bank items" value={items.data?.total ?? "…"} to="/items" />
        <StatCard label="Tests" value={tests.data?.total ?? "…"} to="/tests" />
      </div>
    </div>
  );
}
```

(`?limit=1` fetches one row but the envelope's `total` is the full count — cheap
dashboard stats without a dedicated endpoint.)

- [ ] **Step 6: Test + lint + commit**

```bash
cd web && bun run test:run && bun run typecheck
git add web && git commit -m "Block 14: web data hooks, Async wrapper, dashboard"
```

---

### Task 24: Sources browser + catalogue sync/upload ✅

**Files:**
- Modify: `web/src/pages/Sources.tsx`
- Create: `web/src/pages/SourceDetail.tsx` (route `/sources/:id`)
- Modify: `web/src/App.tsx` (add the detail route under the guarded layout)
- Create: `web/src/pages/Sources.test.tsx`

**Interfaces:**
- Consumes: `useSources`, `useSource`, `api.syncCatalog`, `api.uploadCatalog`, `Async`.
- Produces: sources table (filterable by family/category/redistributable via the
  query string), a source-detail panel with ingest actions (Task 25 adds the
  ingest buttons), and a catalogue sync button + JSON upload control.

- [ ] **Step 1: Failing test** — `web/src/pages/Sources.test.tsx`: mock `fetch`
to return a `Page<SourceSnapshot>` (+ whoami none-mode) and assert a source
`Name` renders in a table row. (Follow the `hooks.test.tsx` fetch-mock pattern.)

- [ ] **Step 2: Run to verify failure** (`bun run test:run src/pages/Sources.test.tsx`).

- [ ] **Step 3: Implement `Sources.tsx`**

```tsx
import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSources, useApiToken } from "../api/hooks";
import { Async } from "../components/Async";
import { api } from "../api/client";

export default function Sources() {
  const token = useApiToken();
  const qc = useQueryClient();
  const [family, setFamily] = useState("");
  const q = family ? `?family=${family}` : "";
  const sources = useSources(q);
  const sync = useMutation({
    mutationFn: () => api.syncCatalog(token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sources"] }),
  });
  const upload = useMutation({
    mutationFn: (json: string) => api.uploadCatalog(token, json),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sources"] }),
  });

  return (
    <div>
      <div className="mb-4 flex items-center gap-2">
        <h1 className="text-xl font-semibold">Sources</h1>
        <button onClick={() => sync.mutate()} className="ml-auto rounded border px-3 py-1 text-sm">
          {sync.isPending ? "Syncing…" : "Sync catalogue"}
        </button>
        <label className="cursor-pointer rounded border px-3 py-1 text-sm">
          Upload JSON
          <input
            type="file"
            accept="application/json"
            hidden
            onChange={async (e) => {
              const f = e.target.files?.[0];
              if (f) upload.mutate(await f.text());
            }}
          />
        </label>
      </div>
      <select value={family} onChange={(e) => setFamily(e.target.value)} className="mb-3 rounded border px-2 py-1 text-sm">
        <option value="">all families</option>
        {["logical", "numerical", "verbal", "spatial", "speed"].map((f) => (
          <option key={f} value={f}>{f}</option>
        ))}
      </select>
      {upload.isError && <p className="mb-2 text-sm text-red-600">Upload rejected (invalid catalogue).</p>}
      <Async query={sources}>
        {(page) => (
          <table className="w-full text-sm">
            <thead className="text-left text-slate-500">
              <tr><th className="py-1">Name</th><th>Provider</th><th>Redistributable</th><th>Items</th></tr>
            </thead>
            <tbody>
              {page.items.map((s) => (
                <tr key={s.ID} className="border-t">
                  <td className="py-1"><Link className="text-blue-700 hover:underline" to={`/sources/${s.ID}`}>{s.Name}</Link></td>
                  <td>{s.Provider}</td>
                  <td>{s.License.Redistributable}</td>
                  <td>{s.ItemCount}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Async>
    </div>
  );
}
```

`SourceDetail.tsx` renders one `useSource(id)` (name, license, extraction
method, test types) and hosts the ingest actions Task 25 adds. Add the route
`<Route path="sources/:id" element={<SourceDetail />} />` inside the layout.

- [ ] **Step 4: Test + lint + commit**

```bash
cd web && bun run test:run && bun run typecheck
git add web && git commit -m "Block 14: web sources browser + catalogue sync/upload"
```

---

### Task 25: Ingest actions + live job progress ✅

**Files:**
- Modify: `web/src/pages/SourceDetail.tsx` (ingest / ingest-llm buttons, async toggle)
- Modify: `web/src/pages/Jobs.tsx` (job list, polling)
- Create: `web/src/components/JobBadge.tsx`
- Create: `web/src/pages/Jobs.test.tsx`

**Interfaces:**
- Consumes: `api.ingest`, `api.ingestLLM`, `useJobs`, `useJob`, `Job`.
- Produces: ingest triggers that, in async mode, start a job and navigate to it;
  a `Jobs` page polling `useJobs(1500)`; `<JobBadge state>` state pill. Proves the
  202-poll loop (ADR-0007) end to end in the UI.

- [ ] **Step 1: Failing test** — `Jobs.test.tsx`: mock `fetch` to return a
`Page<Job>` with one `running` and one `done` job (+ whoami); assert both rows
render with their state text.

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: `JobBadge.tsx`**

```tsx
import type { Job } from "../api/types";

const styles: Record<Job["state"], string> = {
  queued: "bg-slate-200 text-slate-700",
  running: "bg-blue-200 text-blue-800",
  done: "bg-green-200 text-green-800",
  failed: "bg-red-200 text-red-800",
};

export function JobBadge({ state }: { state: Job["state"] }) {
  return <span className={`rounded px-2 py-0.5 text-xs font-medium ${styles[state]}`}>{state}</span>;
}
```

- [ ] **Step 4: `Jobs.tsx`** (poll while any job is non-terminal)

```tsx
import { useJobs } from "../api/hooks";
import { Async } from "../components/Async";
import { JobBadge } from "../components/JobBadge";

export default function Jobs() {
  const jobs = useJobs(1500); // poll every 1.5s
  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Ingest jobs</h1>
      <Async query={jobs}>
        {(page) =>
          page.items.length === 0 ? (
            <p className="text-slate-500">No jobs yet. Trigger an ingest from a source.</p>
          ) : (
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500">
                <tr><th className="py-1">Source</th><th>Kind</th><th>State</th><th>Saved</th></tr>
              </thead>
              <tbody>
                {page.items.map((j) => (
                  <tr key={j.id} className="border-t">
                    <td className="py-1">{j.sourceId}</td>
                    <td>{j.kind}</td>
                    <td><JobBadge state={j.state} /></td>
                    <td>{j.report?.Saved ?? (j.error ? "—" : "")}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        }
      </Async>
    </div>
  );
}
```

- [ ] **Step 5: Ingest buttons in `SourceDetail.tsx`** — two mutations
(`api.ingest` / `api.ingestLLM`) with an "async" checkbox; on async success
(`202` returns a `Job`), navigate to `/jobs`. Show the sync `IngestReport`
counts inline when not async. Disable the LLM button UI hint when the server has
no LLM (a 503 surfaces via the mutation error).

- [ ] **Step 6: Test + lint + commit**

```bash
cd web && bun run test:run && bun run typecheck
git add web && git commit -m "Block 14: web ingest actions + live job progress"
```

---

### Task 26: Item bank browser + shared media renderer + item preview ✅

**Files:**
- Modify: `web/src/pages/Items.tsx`
- Create: `web/src/components/MediaRenderer.tsx` (shared with the player)
- Create: `web/src/components/ItemView.tsx` (stem + options; shared with the player)
- Create: `web/src/pages/ItemDetail.tsx` (route `/items/:id`)
- Create: `web/src/components/MediaRenderer.test.tsx`

**Interfaces:**
- Consumes: `useItems`, `useItem`, `ItemSnapshot`, `StimulusPart`, `Option`.
- Produces: `<MediaRenderer part>` (renders inline `data:` URIs directly and blob
  refs via `/api/media/{ref}`); `<ItemView item showKey>` (stem parts + options,
  answer key highlighted only when `showKey`); a filterable items table. The
  player (Phase 8) reuses `MediaRenderer` + `ItemView` with `showKey={false}`.

- [ ] **Step 1: Failing test** — `MediaRenderer.test.tsx`: a part with
`MediaRef: "data:image/svg+xml;base64,AAA"` renders an `<img>` whose `src` is that
data URI; a part with `MediaRef: "abc123"` (a blob ref) renders `src="/api/media/abc123"`;
a text-only part renders its text, no `<img>`.

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: `MediaRenderer.tsx`**

```tsx
import type { Option, StimulusPart } from "../api/types";

// mediaSrc resolves a media ref to a URL: an inline data: URI is used directly
// (the generator emits self-contained SVG), any other non-empty ref resolves
// through the content-addressed media endpoint (DESIGN §7.1 / ADR-0003).
export function mediaSrc(ref: string): string {
  return ref.startsWith("data:") ? ref : `/api/media/${ref}`;
}

export function MediaRenderer({ part }: { part: StimulusPart | Option }) {
  const text = "Text" in part ? part.Text : "";
  return (
    <span className="inline-flex items-center gap-2">
      {text && <span>{text}</span>}
      {part.MediaRef && (
        <img src={mediaSrc(part.MediaRef)} alt={text || "figure"} className="max-h-40 max-w-full" />
      )}
    </span>
  );
}
```

- [ ] **Step 4: `ItemView.tsx`** (shared stem + options; key highlight optional)

```tsx
import type { ItemSnapshot } from "../api/types";
import { MediaRenderer } from "./MediaRenderer";

// ItemView renders an item's stem and options. showKey highlights the correct
// option — operator preview only; the player passes showKey={false} and the
// server has already stripped the key from a taker's presented item anyway.
export function ItemView({ item, showKey }: { item: ItemSnapshot; showKey: boolean }) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        {(item.Stimulus ?? []).map((p, i) => (
          <div key={i}><MediaRenderer part={p} /></div>
        ))}
      </div>
      {item.AnswerFormat === "multiple-choice" && (
        <ul className="space-y-1">
          {(item.Options ?? []).map((o) => {
            const correct = showKey && o.ID === item.AnswerKey.OptionID;
            return (
              <li key={o.ID} className={`rounded border p-2 ${correct ? "border-green-500 bg-green-50" : ""}`}>
                <span className="mr-2 font-mono text-xs text-slate-500">{o.ID}</span>
                <MediaRenderer part={o} />
              </li>
            );
          })}
        </ul>
      )}
      {showKey && item.Explanation && (
        <p className="rounded bg-slate-50 p-2 text-sm text-slate-600">{item.Explanation}</p>
      )}
    </div>
  );
}
```

- [ ] **Step 5: `Items.tsx`** — filterable table (family / testType /
min-maxDifficulty query params) linking each row to `/items/:id`;
`ItemDetail.tsx` renders `<ItemView item showKey />` for the operator. Register
`items/:id` under the layout.

- [ ] **Step 6: Test + lint + commit**

```bash
cd web && bun run test:run && bun run typecheck
git add web && git commit -m "Block 14: web item bank browser, shared MediaRenderer + ItemView"
```

---

### Task 27: Generate items form ✅

**Files:**
- Modify: `web/src/pages/Generate.tsx`
- Create: `web/src/pages/Generate.test.tsx`

**Interfaces:**
- Consumes: `api.generate`, `useQueryClient`.
- Produces: a form (testType A1–A4, difficulty, count, seed) posting
  `/api/items/generate`, showing the returned counts and invalidating the items
  query so the bank browser refreshes.

- [ ] **Step 1: Failing test** — assert submitting the form calls `fetch` with
`POST /api/items/generate` and the chosen body; on success a result line renders.

- [ ] **Step 2–3: Implement** — a controlled form with a `useMutation`:

```tsx
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { useApiToken } from "../api/hooks";

export default function Generate() {
  const token = useApiToken();
  const qc = useQueryClient();
  const [form, setForm] = useState({ testType: "A2", difficulty: 2, count: 5, seed: 1 });
  const gen = useMutation({
    mutationFn: () => api.generate(token, form),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["items"] }),
  });
  return (
    <div className="max-w-md">
      <h1 className="mb-4 text-xl font-semibold">Generate figural items</h1>
      <form
        className="space-y-3"
        onSubmit={(e) => { e.preventDefault(); gen.mutate(); }}
      >
        <label className="block text-sm">Type
          <select className="mt-1 w-full rounded border px-2 py-1"
            value={form.testType} onChange={(e) => setForm({ ...form, testType: e.target.value })}>
            {["A1", "A2", "A3", "A4"].map((t) => <option key={t}>{t}</option>)}
          </select>
        </label>
        {(["difficulty", "count", "seed"] as const).map((k) => (
          <label key={k} className="block text-sm capitalize">{k}
            <input type="number" className="mt-1 w-full rounded border px-2 py-1"
              value={form[k]} onChange={(e) => setForm({ ...form, [k]: Number(e.target.value) })} />
          </label>
        ))}
        <button className="rounded bg-slate-800 px-4 py-2 text-white" disabled={gen.isPending}>
          {gen.isPending ? "Generating…" : "Generate"}
        </button>
      </form>
      {gen.isSuccess && <p className="mt-3 text-sm text-green-700">Generated. The bank has been updated.</p>}
      {gen.isError && <p className="mt-3 text-sm text-red-600">Generation failed.</p>}
    </div>
  );
}
```

- [ ] **Step 4: Test + lint + commit**

```bash
cd web && bun run test:run && bun run typecheck
git add web && git commit -m "Block 14: web generate-items form"
```

---

### Task 28: Compose test form + tests list/detail + invite minting ✅

**Files:**
- Modify: `web/src/pages/Compose.tsx`, `web/src/pages/Tests.tsx`
- Create: `web/src/pages/TestDetail.tsx` (route `/tests/:id`), `web/src/components/InviteButton.tsx`
- Modify: `web/src/App.tsx` (tests/:id route)
- Create: `web/src/pages/Compose.test.tsx`

**Interfaces:**
- Consumes: `api.compose`, `api.mintInvite`, `useTests`, `useTest`.
- Produces: a composer (id/title/policy + dynamic sections: title, family,
  difficulty range, timing), a paginated tests list, a test detail with an
  `<InviteButton>` that mints an invite and shows the copyable `/take#…` link.

- [ ] **Step 1: Failing test** — `Compose.test.tsx`: fill id/title, add a
section, submit → `fetch` called `POST /api/tests` with a `sections` array; the
composed test id renders.

- [ ] **Step 2–3: Implement `Compose.tsx`** — sections are an array in state
with add/remove; the submit maps them to the `composeReq` wire shape
(`totalSeconds`/`perItemSeconds`/`minDifficulty`/`maxDifficulty` per section).
Key body:

```tsx
const body = {
  id, title, policy,
  totalSeconds, perItemSeconds,
  sections: sections.map((s) => ({
    title: s.title, family: s.family,
    totalSeconds: s.totalSeconds, perItemSeconds: s.perItemSeconds,
    minDifficulty: s.minDifficulty, maxDifficulty: s.maxDifficulty,
  })),
};
await api.compose(token, body);
```

Validate client-side that an `adaptive` policy section spans ≥2 bands (mirrors
the server invariant so the user gets an inline message instead of a 400).

- [ ] **Step 4: `InviteButton.tsx`**

```tsx
import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api } from "../api/client";
import { useApiToken } from "../api/hooks";

// InviteButton mints an invite for a test and shows the shareable player link.
// The token is in the URL fragment (/take#…) so it never reaches server logs.
export function InviteButton({ testId }: { testId: string }) {
  const token = useApiToken();
  const [link, setLink] = useState("");
  const mint = useMutation({
    mutationFn: () => api.mintInvite(token, testId, {}),
    onSuccess: (inv) => setLink(new URL(inv.url, window.location.origin).toString()),
  });
  return (
    <div className="space-y-2">
      <button onClick={() => mint.mutate()} className="rounded bg-slate-800 px-4 py-2 text-white">
        {mint.isPending ? "Minting…" : "Create taker invite"}
      </button>
      {mint.isError && <p className="text-sm text-red-600">Minting failed (auth mode must be “token”).</p>}
      {link && (
        <div className="flex items-center gap-2">
          <input readOnly value={link} className="w-full rounded border px-2 py-1 text-sm" aria-label="invite link" />
          <button onClick={() => navigator.clipboard.writeText(link)} className="rounded border px-3 py-1 text-sm">Copy</button>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 5: `Tests.tsx` / `TestDetail.tsx`** — paginated list linking to
`/tests/:id`; detail shows sections/timing/policy and hosts `<InviteButton>` +
an operator "Start a session" button (direct start via
`POST /api/tests/:id/sessions`, useful for smoke-testing without an invite).

> **Deferred to Phase 8:** the operator direct-start button is intentionally
> omitted here. The player (`Take.tsx` / `useTakeSession`, Task 30) is
> invite-driven — it starts a session from an invite in the URL hash and has no
> landing for a pre-started session, and no `api.startSession` client method
> exists. Building the button now would start server-side session state with
> nowhere to navigate. Invite minting (`<InviteButton>`) already delivers the
> start capability; add the direct-start button in Phase 8 once `Take.tsx` can
> receive an operator-started session.

- [ ] **Step 6: Test + lint + commit**

```bash
cd web && bun run test:run && bun run typecheck
git add web && git commit -m "Block 14: web compose form, tests list/detail, invite minting"
```

---

### Task 29: Console polish + Go-gate checkpoint ✅

**Files:**
- Modify: assorted console pages (empty states, error copy, disabled-button
  states), `web/src/pages/NotFound.tsx`.

**Interfaces:** none new — a hardening/consistency pass.

- [ ] **Step 1** Add empty-state messaging to every list (no sources / no items /
no tests / no jobs) and consistent `disabled`/pending states on all mutation
buttons.

- [ ] **Step 2** Full web gate:

```bash
cd web && bun run test:run && bun run typecheck
```
Expected: green.

- [ ] **Step 3** Confirm the Go side is still untouched:

```bash
cd .. && make lint && make test
```
Expected: green (web/ excluded from arch scope; dist is the placeholder).

- [ ] **Step 4: Commit**

```bash
git add web && git commit -m "Block 14: console polish (empty states, pending/disabled)"
```

---

# Phase 8 — Test player

> The player is why this is an SPA (ADR-0005): millisecond countdowns, per-item
> deadlines with auto-submit, and keyboard-first speeded input are client-state
> problems. It is a **public** route (`/take`) — the taker's authority is the
> invite/session capability token carried in the URL fragment, not the operator
> auth context. All timing tests use `vi.useFakeTimers()` (TESTS.md's no-real-
> clock rule, in TypeScript).

### Task 30: Take entry — invite preview + session start

**Files:**
- Modify: `web/src/pages/Take.tsx`
- Create: `web/src/player/useTakeSession.ts`
- Create: `web/src/player/useTakeSession.test.tsx`

**Interfaces:**
- Consumes: `api.previewInvite`, `api.startInvite`, `serverSkewMs`.
- Produces: `useTakeSession(invite: string)` → a state machine
  `{ phase: "preview"|"in-test"|"complete"; preview; delivery; deadline; globalDeadline; error; start(); submit(answer); }`;
  Task 31 renders the countdowns, Task 32/33 the item + advance, Task 34 the
  score. The invite is read from `window.location.hash.slice(1)` in `Take.tsx`.

- [ ] **Step 1: Failing test** — `web/src/player/useTakeSession.test.tsx`:

```tsx
import { describe, expect, it, vi, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { useTakeSession } from "./useTakeSession";

function jsonResponse(body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json", ...headers } });
}

afterEach(() => vi.restoreAllMocks());

describe("useTakeSession", () => {
  it("previews then starts a session, moving to in-test", async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.includes("/invites/preview")) return Promise.resolve(jsonResponse({ testId: "t1", title: "Quiz", sections: [], itemCount: 3 }));
      if (url.includes("/invites/start")) {
        return Promise.resolve(jsonResponse({
          Session: { ID: "s1", Presented: { ItemID: "i1" }, StartedAt: "2030-01-01T00:00:00Z", Timing: { Total: 0, PerItem: 0 } },
          Item: { ID: "i1", AnswerFormat: "multiple-choice", Options: [{ ID: "a", Text: "A" }], Stimulus: [], AnswerKey: {} },
          Deadline: "0001-01-01T00:00:00Z",
          SessionToken: "ts.tok",
        }));
      }
      void init;
      return Promise.resolve(jsonResponse({}));
    });
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useTakeSession("ti.invite"));
    await waitFor(() => expect(result.current.preview?.title).toBe("Quiz"));
    expect(result.current.phase).toBe("preview");

    await act(async () => { await result.current.start(); });
    expect(result.current.phase).toBe("in-test");
    expect(result.current.delivery?.Session.Presented.ItemID).toBe("i1");
  });
});
```

- [ ] **Step 2: Run to verify failure** (`bun run test:run src/player/useTakeSession.test.tsx`).

- [ ] **Step 3: Implement `useTakeSession.ts`**

```ts
import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Answer } from "./answer";
import type { Delivery, InvitePreview, StartResponse } from "../api/types";

type Phase = "preview" | "in-test" | "complete";

// parseTime turns an RFC3339 stamp into a Date, treating Go's zero time
// (0001-01-01…) as "no deadline" (null).
function parseTime(s: string | undefined): Date | null {
  if (!s || s.startsWith("0001-01-01")) return null;
  const t = Date.parse(s);
  return Number.isNaN(t) ? null : new Date(t);
}

// useTakeSession is the player state machine: preview an invite, start a
// session (capturing the session token), then advance item-by-item until the
// plan is exhausted and the attempt completes. Time is never trusted from the
// client alone — deadlines come from the server's Delivery and are rendered
// against server-skew-corrected local time (Task 31).
export function useTakeSession(invite: string) {
  const [phase, setPhase] = useState<Phase>("preview");
  const [delivery, setDelivery] = useState<Delivery | null>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>("");

  const previewQ = useQuery({
    queryKey: ["invitePreview", invite],
    queryFn: () => api.previewInvite(invite),
    enabled: !!invite,
    retry: false,
  });

  const start = useCallback(async () => {
    const started: StartResponse = await api.startInvite(invite);
    setToken(started.SessionToken);
    setDelivery({ Session: started.Session, Item: started.Item, Deadline: started.Deadline });
    setPhase("in-test");
    sessionStorage.setItem("tm.session", JSON.stringify({ token: started.SessionToken, sid: started.Session.ID }));
  }, [invite]);

  const sid = delivery?.Session.ID ?? "";

  const submit = useCallback(
    async (answer: Answer) => {
      if (busy || !sid) return;
      setBusy(true);
      setError("");
      try {
        const next = await api.answer(token, sid, answer);
        if (next.Session.Presented.ItemID === "") {
          await api.complete(token, sid);
          setDelivery(next);
          setPhase("complete");
        } else {
          setDelivery(next);
        }
      } catch (e) {
        // 409 = a concurrent writer (another tab) already advanced this session.
        setError(e instanceof Error ? e.message : "answer failed");
      } finally {
        setBusy(false);
      }
    },
    [busy, sid, token],
  );

  const deadline = useMemo(() => parseTime(delivery?.Deadline), [delivery?.Deadline]);
  const globalDeadline = useMemo(() => {
    const s = delivery?.Session;
    if (!s) return null;
    const started = parseTime(s.StartedAt);
    if (!started || !s.Timing.Total) return null; // Total 0 = untimed
    return new Date(started.getTime() + s.Timing.Total / 1_000_000); // ns → ms
  }, [delivery?.Session]);

  return {
    phase, preview: previewQ.data as InvitePreview | undefined, previewError: previewQ.error,
    delivery, deadline, globalDeadline, sid, token, busy, error, start, submit,
  };
}
```

- [ ] **Step 4: `Take.tsx`** reads the invite and renders per phase:

```tsx
import { useTakeSession } from "../player/useTakeSession";
// … phase === "preview" → preview card + Start button;
//    phase === "in-test" → <PlayerItem> (Task 32) with countdowns (Task 31);
//    phase === "complete" → <ScoreView sid token> (Task 34).
export default function Take() {
  const invite = window.location.hash.slice(1);
  const s = useTakeSession(invite);
  if (!invite) return <p className="p-8">This link is missing its invite token.</p>;
  // … render by s.phase (filled across Tasks 31–34) …
  return <div className="mx-auto max-w-2xl p-6">{/* phase views */}</div>;
}
```

- [ ] **Step 5: stub `web/src/player/answer.ts`** (the `Answer` union used above):

```ts
import type { AnswerFormat } from "../api/types";

// Answer is the wire body for POST /api/sessions/{id}/answers, matching the Go
// answerReq (camelCase). Only the field for the item's format is meaningful.
export interface Answer {
  itemId: string;
  optionId?: string;
  numeric?: number;
  verdict?: string;
}

export function emptyAnswer(itemId: string, _format: AnswerFormat): Answer {
  return { itemId }; // a blank answer records as wrong (the speeded-timeout case)
}
```

- [ ] **Step 6: Run test + lint + commit**

```bash
cd web && bun run test:run src/player/useTakeSession.test.tsx && bun run typecheck
git add web && git commit -m "Block 14: player take-session state machine (preview → start → advance)"
```

---

### Task 31: Countdown hook + timer display

**Files:**
- Create: `web/src/player/useCountdown.ts`
- Create: `web/src/components/Countdown.tsx`
- Create: `web/src/player/useCountdown.test.ts`
- Modify: `web/src/pages/Take.tsx` (render global + per-item countdowns)

**Interfaces:**
- Consumes: `serverSkewMs`.
- Produces: `useCountdown(deadline: Date | null, onExpire?: () => void) : number | null`
  (remaining ms, `null` when untimed, fires `onExpire` once at zero);
  `<Countdown ms>` mm:ss display. Task 33 passes `onExpire` to auto-submit.

- [ ] **Step 1: Failing test** — `web/src/player/useCountdown.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useCountdown } from "./useCountdown";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useCountdown", () => {
  it("counts down and fires onExpire once at zero", () => {
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    const deadline = new Date("2030-01-01T00:00:05Z"); // 5s out
    const onExpire = vi.fn();
    const { result } = renderHook(() => useCountdown(deadline, onExpire));
    expect(result.current).toBeGreaterThan(4000);

    act(() => { vi.advanceTimersByTime(5100); });
    expect(result.current).toBe(0);
    expect(onExpire).toHaveBeenCalledTimes(1);

    act(() => { vi.advanceTimersByTime(1000); });
    expect(onExpire).toHaveBeenCalledTimes(1); // fires exactly once
  });

  it("returns null for an untimed (null) deadline", () => {
    const { result } = renderHook(() => useCountdown(null));
    expect(result.current).toBeNull();
  });
});
```

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: `useCountdown.ts`**

```ts
import { useEffect, useRef, useState } from "react";
import { serverSkewMs } from "../api/client";

// useCountdown returns milliseconds remaining until deadline, corrected for
// server clock skew, ticking ~4×/sec. It fires onExpire exactly once when it
// first hits zero. A null deadline (untimed section/test) returns null and does
// nothing. Skew (server − local) keeps a wrong local clock from mis-timing a
// speeded test (C10).
export function useCountdown(deadline: Date | null, onExpire?: () => void): number | null {
  const [remaining, setRemaining] = useState<number | null>(() =>
    deadline ? Math.max(0, deadline.getTime() - (Date.now() + serverSkewMs())) : null,
  );
  const fired = useRef(false);

  useEffect(() => {
    fired.current = false;
    if (!deadline) {
      setRemaining(null);
      return;
    }
    const tick = () => {
      const left = Math.max(0, deadline.getTime() - (Date.now() + serverSkewMs()));
      setRemaining(left);
      if (left === 0 && !fired.current) {
        fired.current = true;
        onExpire?.();
      }
    };
    tick();
    const h = setInterval(tick, 250);
    return () => clearInterval(h);
    // onExpire is intentionally excluded: it is captured per item; a new
    // deadline (new item) resets the effect and the fired guard.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deadline]);

  return remaining;
}
```

- [ ] **Step 4: `Countdown.tsx`**

```tsx
// Countdown renders mm:ss, turning red under 10s. An untimed slot (null) shows
// nothing.
export function Countdown({ ms, label }: { ms: number | null; label: string }) {
  if (ms === null) return null;
  const total = Math.ceil(ms / 1000);
  const mm = String(Math.floor(total / 60)).padStart(2, "0");
  const ss = String(total % 60).padStart(2, "0");
  return (
    <span className={`font-mono text-sm ${total <= 10 ? "text-red-600" : "text-slate-600"}`}>
      {label} {mm}:{ss}
    </span>
  );
}
```

- [ ] **Step 5: Wire the timers into `Take.tsx`'s in-test header** — a global
countdown from `s.globalDeadline` and a per-item countdown from `s.deadline`
(the `onExpire` is added in Task 33).

- [ ] **Step 6: Run test + lint + commit**

```bash
cd web && bun run test:run src/player/useCountdown.test.ts && bun run typecheck
git add web && git commit -m "Block 14: player countdown hook + timer display (skew-corrected)"
```

---

### Task 32: Item presentation + answer-format controls + keyboard

**Files:**
- Create: `web/src/player/AnswerControl.tsx`
- Create: `web/src/components/PlayerItem.tsx`
- Create: `web/src/player/AnswerControl.test.tsx`
- Modify: `web/src/pages/Take.tsx`

**Interfaces:**
- Consumes: `ItemView`, `MediaRenderer`, `ItemSnapshot`, `Answer`.
- Produces: `<AnswerControl item value onChange onSubmit>` covering all three
  formats with keyboard support (digits 1–6 select an option, `Enter` submits,
  `T`/`F`/`C` choose a verdict); `<PlayerItem>` (stem via `ItemView showKey=false`
  + the control). Task 33 drives submission.

- [ ] **Step 1: Failing test** — `web/src/player/AnswerControl.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AnswerControl } from "./AnswerControl";
import type { ItemSnapshot } from "../api/types";

const mcItem = {
  ID: "i1", AnswerFormat: "multiple-choice",
  Options: [{ ID: "a", Text: "Alpha" }, { ID: "b", Text: "Beta" }, { ID: "c", Text: "Gamma" }],
  Stimulus: [], AnswerKey: {}, Difficulty: { Band: 1 },
} as unknown as ItemSnapshot;

describe("AnswerControl (multiple choice)", () => {
  it("selects an option by digit key and submits on Enter", async () => {
    const onChange = vi.fn();
    const onSubmit = vi.fn();
    render(<AnswerControl item={mcItem} value={{ itemId: "i1" }} onChange={onChange} onSubmit={onSubmit} />);
    await userEvent.keyboard("2"); // selects option b (2nd)
    expect(onChange).toHaveBeenLastCalledWith({ itemId: "i1", optionId: "b" });
    await userEvent.keyboard("{Enter}");
    expect(onSubmit).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: `AnswerControl.tsx`**

```tsx
import { useEffect } from "react";
import type { ItemSnapshot } from "../api/types";
import type { Answer } from "./answer";
import { MediaRenderer } from "../components/MediaRenderer";

// AnswerControl renders the input for an item's answer format and wires
// keyboard-first entry — speeded tests live or die on input latency (ADR-0005):
// digits 1–6 pick a multiple-choice option, Enter submits, T/F/C pick a verdict.
export function AnswerControl({
  item, value, onChange, onSubmit,
}: {
  item: ItemSnapshot;
  value: Answer;
  onChange: (a: Answer) => void;
  onSubmit: () => void;
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Enter") { onSubmit(); return; }
      if (item.AnswerFormat === "multiple-choice") {
        const n = Number(e.key);
        const opts = item.Options ?? [];
        if (n >= 1 && n <= opts.length) onChange({ itemId: item.ID, optionId: opts[n - 1].ID });
      } else if (item.AnswerFormat === "true-false-cannotsay") {
        const v = { t: "true", f: "false", c: "cannot-say" }[e.key.toLowerCase()];
        if (v) onChange({ itemId: item.ID, verdict: v });
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [item, onChange, onSubmit]);

  if (item.AnswerFormat === "multiple-choice") {
    return (
      <ul className="space-y-2">
        {(item.Options ?? []).map((o, i) => (
          <li key={o.ID}>
            <button
              onClick={() => onChange({ itemId: item.ID, optionId: o.ID })}
              className={`flex w-full items-center gap-2 rounded border p-3 text-left ${
                value.optionId === o.ID ? "border-slate-800 bg-slate-100" : "hover:bg-slate-50"
              }`}
            >
              <kbd className="rounded bg-slate-200 px-1.5 text-xs">{i + 1}</kbd>
              <MediaRenderer part={o} />
            </button>
          </li>
        ))}
      </ul>
    );
  }
  if (item.AnswerFormat === "open-numeric") {
    return (
      <input
        type="number"
        autoFocus
        value={value.numeric ?? ""}
        onChange={(e) => onChange({ itemId: item.ID, numeric: Number(e.target.value) })}
        className="w-40 rounded border px-3 py-2"
        aria-label="numeric answer"
      />
    );
  }
  return (
    <div className="flex gap-2">
      {[["true", "True (T)"], ["false", "False (F)"], ["cannot-say", "Cannot say (C)"]].map(([v, label]) => (
        <button
          key={v}
          onClick={() => onChange({ itemId: item.ID, verdict: v })}
          className={`rounded border px-4 py-2 ${value.verdict === v ? "border-slate-800 bg-slate-100" : "hover:bg-slate-50"}`}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: `PlayerItem.tsx`** composes the redacted stem + the control:

```tsx
import type { ItemSnapshot } from "../api/types";
import type { Answer } from "../player/answer";
import { ItemView } from "./ItemView";
import { AnswerControl } from "../player/AnswerControl";

export function PlayerItem({
  item, value, onChange, onSubmit, busy,
}: {
  item: ItemSnapshot; value: Answer; onChange: (a: Answer) => void; onSubmit: () => void; busy: boolean;
}) {
  return (
    <div className="space-y-6">
      <ItemView item={item} showKey={false} />
      <AnswerControl item={item} value={value} onChange={onChange} onSubmit={onSubmit} />
      <button onClick={onSubmit} disabled={busy} className="rounded bg-slate-800 px-5 py-2 text-white disabled:opacity-50">
        {busy ? "Submitting…" : "Submit (Enter)"}
      </button>
    </div>
  );
}
```

- [ ] **Step 5: Run test + lint + commit**

```bash
cd web && bun run test:run src/player/AnswerControl.test.tsx && bun run typecheck
git add web && git commit -m "Block 14: player item view + keyboard-first answer controls"
```

---

### Task 33: Wire the advance loop + per-item auto-submit

**Files:**
- Modify: `web/src/pages/Take.tsx` (compose item + countdowns + current-answer state)
- Create: `web/src/pages/Take.test.tsx`

**Interfaces:**
- Consumes: `useTakeSession`, `useCountdown`, `PlayerItem`, `Countdown`,
  `emptyAnswer`.
- Produces: the full in-test view: current-answer state resets per item, the
  per-item countdown's `onExpire` auto-submits the current selection (empty if
  none — records wrong, the speeded convention, C10), and the loop advances
  until `phase === "complete"`.

- [ ] **Step 1: Failing test** — `web/src/pages/Take.test.tsx` (fake timers):
mock `fetch` so preview + start return item `i1` with a per-item deadline 30s
out, and `answers` returns a Delivery with no presented item (plan of one). Start
the attempt, **advance fake time past the deadline**, assert `POST
/api/sessions/s1/answers` was called (auto-submit fired) and the view moved to
the score phase. This is the executable proof of the auto-submit design.

```tsx
// skeleton — fill the fetch mock per the pattern in useTakeSession.test.tsx
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());
describe("Take auto-submit", () => {
  it("auto-submits the current answer when the per-item deadline lapses", async () => {
    // render <Take/> at /take#ti.invite, start, advance timers 31_000ms,
    // assert the answers endpoint was hit and the score view renders.
  });
});
```

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: Implement the in-test view in `Take.tsx`**

```tsx
// inside Take(), when s.phase === "in-test" and s.delivery?.Item:
const item = s.delivery.Item;
const [answer, setAnswer] = useState<Answer>(() => ({ itemId: item.ID }));

// Reset the working answer whenever the presented item changes.
useEffect(() => { setAnswer({ itemId: item.ID }); }, [item.ID]);

// Per-item deadline auto-submits the current selection (empty ⇒ wrong).
const perItem = useCountdown(s.deadline, () => s.submit(answer));
const global = useCountdown(s.globalDeadline);

return (
  <div className="mx-auto max-w-2xl p-6">
    <header className="mb-4 flex justify-between">
      <Countdown ms={global} label="Total" />
      <Countdown ms={perItem} label="This item" />
    </header>
    {s.error && <p className="mb-3 rounded bg-amber-50 p-2 text-sm text-amber-800">{s.error}</p>}
    <PlayerItem item={item} value={answer} onChange={setAnswer} onSubmit={() => s.submit(answer)} busy={s.busy} />
  </div>
);
```

Import `useState`, `useEffect`, `Answer`, `useCountdown`, `Countdown`,
`PlayerItem`.

- [ ] **Step 4: Run test + lint + commit**

```bash
cd web && bun run test:run src/pages/Take.test.tsx && bun run typecheck
git add web && git commit -m "Block 14: player advance loop with per-item auto-submit"
```

---

### Task 34: Score report view

**Files:**
- Create: `web/src/player/ScoreView.tsx`
- Create: `web/src/player/ScoreView.test.tsx`
- Modify: `web/src/pages/Take.tsx` (render on `phase === "complete"`)

**Interfaces:**
- Consumes: `api.score`, `Score`, `ItemFeedback`.
- Produces: `<ScoreView sid token>` — fetches the score and renders raw/max,
  speed, the normed band/IQ/percentile (or a raw-only note when unnormed), and
  the per-item feedback list (given vs correct + explanation).

- [ ] **Step 1: Failing test** — `ScoreView.test.tsx`: mock `api.score` to return
a normed `Score`; assert the IQ, band, and a per-item explanation render; then a
`Normed:false` score renders the "raw only" note and no IQ.

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: `ScoreView.tsx`**

```tsx
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Score } from "../api/types";

function ms(ns: number) { return Math.round(ns / 1_000_000); }

export function ScoreView({ sid, token }: { sid: string; token: string }) {
  const q = useQuery({ queryKey: ["score", sid], queryFn: () => api.score(token, sid), retry: false });
  if (q.isLoading) return <p>Scoring…</p>;
  if (q.error || !q.data) return <p className="text-red-600">Could not load your score.</p>;
  const s: Score = q.data;
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Your result</h1>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        <div className="rounded border p-4"><div className="text-3xl font-semibold">{s.Raw}/{s.Max}</div><div className="text-sm text-slate-500">Correct</div></div>
        {s.Normed ? (
          <>
            <div className="rounded border p-4"><div className="text-3xl font-semibold">{s.ScaledIQ}</div><div className="text-sm text-slate-500">Scaled IQ</div></div>
            <div className="rounded border p-4"><div className="text-3xl font-semibold">{s.Percentile.toFixed(1)}</div><div className="text-sm text-slate-500">Percentile · {s.Band}</div></div>
          </>
        ) : (
          <div className="col-span-2 rounded border p-4 text-sm text-slate-500">Raw score only — this test carries no norms.</div>
        )}
      </div>
      <p className="text-sm text-slate-600">
        Speed: {ms(s.Speed.Total)} ms total, {ms(s.Speed.Mean)} ms/item, {s.Speed.CorrectPerMinute.toFixed(1)} correct/min.
      </p>
      <div className="space-y-2">
        <h2 className="font-semibold">Review</h2>
        {(s.Items ?? []).map((f) => (
          <div key={f.ItemID} className={`rounded border p-3 ${f.Correct ? "border-green-300" : "border-red-300"}`}>
            <div className="text-sm">{f.Correct ? "✓ Correct" : `✗ You: ${f.Given || "—"} · Correct: ${f.CorrectAnswer}`}</div>
            {f.Explanation && <div className="mt-1 text-sm text-slate-600">{f.Explanation}</div>}
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Render it** — in `Take.tsx`, `phase === "complete"` →
`<ScoreView sid={s.sid} token={s.token} />`.

- [ ] **Step 5: Run test + lint + commit**

```bash
cd web && bun run test:run src/player/ScoreView.test.tsx && bun run typecheck
git add web && git commit -m "Block 14: player score report view"
```

---

### Task 35: Player edge states (conflict, resume, untimed, bad invite)

**Files:**
- Modify: `web/src/pages/Take.tsx`, `web/src/player/useTakeSession.ts`
- Create: `web/src/pages/TakeEdge.test.tsx`

**Interfaces:** none new — hardening the player against the real failure modes.

- [ ] **Step 1: Failing tests** — `TakeEdge.test.tsx`:
  - an expired/invalid invite (`previewInvite` → 401) renders a clear "this
    invite is no longer valid" screen, not a crash;
  - a `409` from `api.answer` sets `error` and renders a "continue in your other
    tab" notice while keeping the current item (no double-advance);
  - an untimed test (deadlines null) renders no `Countdown` and never
    auto-submits.

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: Implement**
  - In `Take.tsx`, branch on `s.previewError` (from the preview query) →
    invalid-invite screen; gate the whole flow on a present preview.
  - In `useTakeSession`, when `submit` catches an `ApiError` with `status === 409`,
    set a specific message and **do not** change `delivery` (the server already
    advanced; the taker's other tab owns the attempt). Optionally refetch the
    session view is out of scope — the message suffices.
  - Untimed already works (null deadlines → `useCountdown` returns null,
    `onExpire` never fires) — the test just locks the behaviour in.
  - Refresh-resume: on mount, if `sessionStorage["tm.session"]` exists and the
    hash invite matches, offer a "resume" affordance. A **server-side** resume
    verb (re-presenting the current item to a reloaded page) is explicitly out of
    scope — that is ROADMAP §6 (client-supplied `If-Match`, read surfaces); note
    it inline with a `// ponytail:` comment so the ceiling is visible.

- [ ] **Step 4: Run tests + full web gate + commit**

```bash
cd web && bun run test:run && bun run typecheck
git add web && git commit -m "Block 14: player edge states (invalid invite, 409 conflict, untimed)"
```

---

# Phase 9 — Integration, CI, and finish

### Task 36: Embedded-SPA integration test + `serve-all` smoke

**Files:**
- Create: `cmd/testmaker/server_spa_embedded_test.go`
- Modify: `cmd/testmaker/server_spa.go` (only if the smoke reveals a header/route gap)

**Interfaces:**
- Consumes: `webui.FS()`, the SPA handler.
- Produces: a Go test that, **when a UI build is embedded**, asserts the server
  serves `index.html` at `/` and cache-controls hashed assets; it skips cleanly
  without a build so `make test` stays green on a Bun-free checkout.

- [ ] **Step 1: Write the test** — `cmd/testmaker/server_spa_embedded_test.go`:

```go
package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/cmd/testmaker/webui"
)

// TestServesEmbeddedSPAWhenBuilt asserts the SPA-serving contract, but only when
// a build is embedded (make webui). Without one it skips — the Go toolchain must
// stay green with no Bun, so this can never be a hard failure on CI's Go job.
func TestServesEmbeddedSPAWhenBuilt(t *testing.T) {
	if _, ok := webui.FS(); !ok {
		t.Skip("no embedded UI build (run `make webui`); Go tests stay green without Bun")
	}
	ts := newSPATestServer(t)

	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / content-type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `id="root"`) {
		t.Fatal("index.html did not contain the React root div")
	}

	// A client-side route (no such file) also serves the shell, not a 404.
	deep, err := http.Get(ts.URL + "/take")
	if err != nil {
		t.Fatalf("GET /take: %v", err)
	}
	defer deep.Body.Close()
	if deep.StatusCode != http.StatusOK {
		t.Fatalf("GET /take = %d, want 200 (SPA shell)", deep.StatusCode)
	}
}
```

- [ ] **Step 2: Run without a build (skips)**

```bash
cd cmd/testmaker && go test . -run TestServesEmbeddedSPAWhenBuilt -v
```
Expected: SKIP (no build embedded).

- [ ] **Step 3: Run with a build (asserts)**

```bash
cd /Users/mariotoffia/progs/github/testmaker && make webui
cd cmd/testmaker && go test . -run TestServesEmbeddedSPAWhenBuilt -v
```
Expected: PASS.

- [ ] **Step 4: Manual `serve-all` smoke** (token mode — the real posture)

```bash
cd /Users/mariotoffia/progs/github/testmaker
make serve-all TESTMAKER_HOME=/tmp/tm-smoke &     # builds UI, serves :8080
sleep 2
OP=$(python3 -c "import json;print(json.load(open('/tmp/tm-smoke/config/config.json'))['auth']['operatorToken'])")
curl -s localhost:8080/ | grep -q 'id="root"' && echo "SPA served: OK"
curl -s -o /dev/null -w "%{http_code}\n" localhost:8080/api/items                       # 401 (no token)
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $OP" localhost:8080/api/items  # 200
kill %1
```
Expected: `id="root"` present, `/api/items` → 401 without token / 200 with the
operator token.

- [ ] **Step 5: Restore the placeholder + Go gates**

```bash
git checkout -- cmd/testmaker/webui/dist/.keep 2>/dev/null || true
git clean -fdq cmd/testmaker/webui/dist
make lint && make test
```
Expected: green (embedded test skips again).

- [ ] **Step 6: Commit**

```bash
git add cmd/testmaker
git commit -m "Block 14: embedded-SPA integration test (skips without a build)"
```

---

### Task 37: CI — separate web job

**Files:**
- Modify: `.github/workflows/check.yml`

**Interfaces:** none — CI wiring. The Go job is unchanged (still `make check`,
Bun-free); a second job runs the web toolchain.

- [ ] **Step 1: Add a `web` job** — append to `.github/workflows/check.yml`
under `jobs:` (the existing `check` job stays exactly as-is):

```yaml
  web:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: oven-sh/setup-bun@v2
        with:
          bun-version: latest
      - name: Install web deps
        run: cd web && bun install --frozen-lockfile
      - name: Typecheck + lint
        run: cd web && bun run typecheck
      - name: Unit tests
        run: cd web && bun run test:run
      - name: Production build
        run: cd web && bun run build
```

- [ ] **Step 2: Add a build-embed CI check to the Go job (optional but cheap)** —
after the web build proves the SPA compiles, a follow-on job can `make webui`
then `go build ./cmd/testmaker` to prove the embed path. Keep it a separate job
so the pure-Go `check` job stays Bun-free:

```yaml
  embed:
    runs-on: ubuntu-latest
    needs: [check, web]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25", check-latest: true }
      - uses: oven-sh/setup-bun@v2
        with: { bun-version: latest }
      - name: Build UI then Go binary
        run: |
          make webui
          cd cmd/testmaker && go build -o /tmp/testmaker.out ./...
```

- [ ] **Step 3: Validate the workflow locally** (yaml lint, or push a branch and
watch Actions). Confirm the `check` job definition is byte-identical to before.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/check.yml
git commit -m "Block 14: CI web job (bun test/typecheck/build) + embed build check"
```

---

### Task 38: Documentation status flip + final gates

The design docs were written describing the target as designed, with
implementation tracked here. With the plan complete, flip the remaining
status markers to shipped and drop the "designed; see PLAN.md" hedges.

**Files:**
- Modify: `ROADMAP.md` (§1 now fully shipped), `ARCHITECTURE.md`, `DESIGN.md`,
  `README.md`, `DEVELOPMENT.md` (status verbs), `docs/adr/*` unchanged
  (ADRs are immutable once Accepted).

- [ ] **Step 1** In `DESIGN.md §7` / `ARCHITECTURE.md §9`, change the framing
lines that point implementation at PLAN.md (e.g. "implemented step-by-step in
PLAN.md", "the step-by-step implementation is PLAN.md") to a shipped-status
statement, and add ✅ where the doc uses the legend. Keep the ADR references.

- [ ] **Step 2** In `ROADMAP.md`, move §1 from "designed; in PLAN.md" to a short
"shipped" note (one paragraph) linking DESIGN §7 + the ADRs, and confirm §2
(cloud persistence) is called out as the next recommended step. Leave §§2–6
intact.

- [ ] **Step 3** In `README.md`, change "The current initiative is the web app…"
to describe the web app as available (how to run it: `make serve-all`), keeping
the endpoint table and posture note.

- [ ] **Step 4: Final full gate — every module + both toolchains**

```bash
make check                       # Go: build + lint + test
cd web && bun run test:run && bun run typecheck && bun run build && cd ..
make webui && cd cmd/testmaker && go build -o /tmp/testmaker.out ./... && cd ..
git checkout -- cmd/testmaker/webui/dist/.keep 2>/dev/null || true
git clean -fdq cmd/testmaker/webui/dist
make check                       # green again with the placeholder restored
```
Expected: all green.

- [ ] **Step 5** Mark this plan complete: tick every checkbox and add a final
"Status: shipped on `<date>`" line at the top of PLAN.md (or delete PLAN.md and
fold a one-line pointer into ROADMAP if the team prefers not to keep completed
plans — team choice).

- [ ] **Step 6: Commit + open the PR**

```bash
git add -A
git commit -m "Block 14: flip web-app + hardening docs to shipped status"
git push -u origin block-14-web-app-hardening
gh pr create --title "Block 14: web app (console + player) + delivery-surface hardening" \
  --body "Implements ROADMAP §1 per DESIGN §7 / ADR-0005..0007 and PLAN.md."
```

---

## Self-review checklist (run before declaring the plan done)

Spec coverage — every ROADMAP §1 requirement maps to a task:

| §1 requirement | Task(s) |
| --- | --- |
| Operator console (catalogue, ingest, bank, generate, compose) | 23–28 |
| Test player (timed, one-item-at-a-time, figural, keyboard, score) | 30–35 |
| Embedded SPA via `go:embed`, single-binary deploy | 1, 19, 36 |
| Auth + roles (operator vs taker; keep keys operator-only) | 4, 7–10 |
| Rate + cost limits (request limiter, LLM param clamp) | 11–13 |
| Pagination on `/items` + `/sources` (+ tests) | 14–15 |
| Error hygiene (code + safe message, log detail) | 5 |
| Async ingest (`202` + job poll) | 17–18 |
| `POST /catalog` (upload a catalogue body) | 16 |

Cross-cutting invariants to re-verify while executing:

- **Zero new ports / zero domain change** — every server file added lives in
  `cmd/testmaker` except `filecatalog.ParseJSON` (Task 16, a pure refactor).
  `make arch-lint` proves it.
- **`make lint` + `make test` green with no Bun** — the `dist/.keep` placeholder
  + `webui.FS()` fallback (Tasks 1, 2, 19) keep the Go toolchain independent.
- **No wall clock in production** — the request-log middleware, rate limiter and
  job registry all take `domain/clock`; player timing uses `serverSkewMs` +
  `vi.useFakeTimers()` (no real-clock test).
- **File-size cap (≤ 500)** — `wc -l` after each server task; the plan pre-splits
  `server_http.go`, `auth_middleware.go`, `server_jobs.go`, `server_catalog.go`,
  `server_invites.go` to stay under it.
- **Wire conventions consistent** — PascalCase domain snapshots vs camelCase
  cmd-local bodies; nanosecond durations; RFC3339 zero = untimed. Encoded once in
  `web/src/api/types.ts` (C9), asserted in `client.test.ts`.
- **Answer keys never reach a taker** — executor redaction (pre-existing) +
  operator gate on `/api/items*` (Task 8) + the two-role flow test (Task 10) +
  `ItemView showKey={false}` in the player (Tasks 26, 32).

## Type/name consistency (used across tasks — keep these exact)

| Name | Defined | Used by |
| --- | --- | --- |
| `webui.FS() (fs.FS, bool)` | Task 1 | Tasks 2, 19, 36 |
| `(*server).writeError(w, r, err)` | Task 5 | every handler task |
| `writeAuthError(w, status, code, msg)` | Task 5 | Tasks 8, 11, 12 |
| `authenticator` + `mintInvite/verifyInvite/mintSession/verifySession` | Task 7 | Tasks 8, 9, 10 |
| `requireOperator/requireSession/requireInvite` | Task 8 | Tasks 8, 9, 15, 16, 18 |
| `startResponse{ports.Delivery; SessionToken}` | Task 9 | Tasks 10, 30 |
| `paginate[T]` / `pageEnvelope[T]` / `pageParams` | Task 14 | Tasks 15, 18 |
| `semaphore` (`tryAcquire/acquire/release`) | Task 12 | Task 18 |
| `jobRegistry` (`create/start/finish/get/list`) | Task 17 | Task 18 |
| `filecatalog.ParseJSON([]byte)` | Task 16 | Task 16 handler |
| `apiFetch` / `api.*` / `serverSkewMs` | Task 20 | every web task |
| `useAuth` / `RequireOperator` | Task 21 | Tasks 22–29 |
| `useCountdown(deadline, onExpire?)` | Task 31 | Task 33 |
| `useTakeSession(invite)` | Task 30 | Tasks 31, 33, 34 |
| `Answer{itemId, optionId?, numeric?, verdict?}` | Task 30 | Tasks 32, 33 |

## Execution order & parallelism

Tasks are numbered in dependency order. Safe parallel tracks once Phase 2
(auth) lands: the **console** (Tasks 23–29) and the **player** (Tasks 30–35)
share only `MediaRenderer`/`ItemView` (Task 26) — build Task 26 before starting
the player, then the two tracks are independent. Server Phases 3–5 (limits,
console API, jobs) are independent of each other and can interleave with the web
foundation (Phase 6). Do **not** start any web task before Task 2 (the `/api`
re-base) — the client assumes the prefix throughout.
