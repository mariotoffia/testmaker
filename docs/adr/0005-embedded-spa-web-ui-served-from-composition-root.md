# ADR-0005: Web UI is an embedded React SPA served by the composition root, with the JSON API re-based under `/api`

- **Status:** Accepted
- **Date:** 2026-07-09

## Context

The whole pipeline is exposed over HTTP (`testmaker -serve`) but is JSON-only —
`GET /` returns an endpoint index, nothing renders in a browser. ROADMAP §1
calls for one web application with two faces: an **operator console**
(catalogue, ingest, item bank, compose) and a **test player** (timed
administration and scoring). The player is genuinely client-heavy:
millisecond-visible global and per-item countdowns, instant advance in adaptive
delivery, and keyboard-first speeded navigation are client-state problems.

Alternatives considered:

1. **Server-rendered Go templates + htmx** — fits the CRUD console, but the
   strictly-speeded player is exactly the client-heavy, real-time case the
   htmx approach concedes to SPAs. Splitting the app across two UI stacks
   (templates for console, SPA for player) is worse than one SPA.
2. **Separately deployed frontend** (Node server / CDN) — breaks the
   single-binary deployment story and adds a second runtime to operate.
3. **SPA embedded in the Go binary via `go:embed`** — one binary, one origin
   (no CORS), the SPA is just another client of the JSON port.

A secondary question is where the API lives once a UI shares the origin: the
endpoints sit at the root today (`/sources`, `/items`, …), which collides with
SPA client-side routes and makes middleware targeting ("auth applies to the
API") pattern-fragile.

## Decision

- **Vite + React + TypeScript SPA, built with Bun, embedded via `go:embed`.**
  Frontend source lives in `web/` at the repo root (not a Go module); the
  production build lands in `cmd/testmaker/webui/dist`, embedded by the small
  `webui` package inside the `cmd/testmaker` module and served by the delivery
  surface. The composition root remains the only place the driving HTTP surface
  lives (it drives `app`, which no adapter may import) — the UI adds a static
  fallback route, not an adapter module.
- **A committed `dist/.keep` placeholder keeps `go build` green with no UI
  built.** When the embedded FS has no `index.html`, the server serves the JSON
  index at `/` exactly as today, so the Go toolchain never depends on Bun.
- **All JSON endpoints move under the `/api` prefix** (`/api/sources`,
  `/api/sessions/{id}/answers`, `/api/media/{ref}`, …). Everything not under
  `/api` falls through to the SPA handler (exact embedded file, else
  `index.html` for client-side routes). This is a breaking wire change made
  deliberately pre-1.0: the only clients are this repo's tests and demos.

## Consequences

- Deployment stays a single self-contained binary; the server gains one
  static-file handler and hashed-asset cache headers. Nothing inward of `cmd`
  changes.
- The repo gains a second toolchain (Bun) that is **optional**: `make check`
  stays pure Go; web build/test targets are separate and CI runs them in a
  separate job.
- API consumers must prefix `/api` — a one-time mechanical migration of tests
  and docs, bought once, and in exchange auth/rate-limit middleware and the SPA
  fallback become trivial prefix checks instead of route-list maintenance.
- React/TS is the ecosystem where LLM-assisted development is strongest (the
  stated ROADMAP rationale); the console and player share one stack.
- The SPA fallback must never shadow the API: Go 1.22 `ServeMux` precedence
  (most-specific pattern wins) guarantees registered `/api/...` patterns beat
  the `GET /` catch-all.
