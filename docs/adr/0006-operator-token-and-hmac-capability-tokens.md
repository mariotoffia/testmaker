# ADR-0006: Access control via a static operator token and stateless HMAC capability tokens (invite / session), enforced at the delivery surface

- **Status:** Accepted
- **Date:** 2026-07-09

## Context

The delivery surface ships unauthenticated and single-tenant: one mux serves
operator and taker. The taker's presented item is already key-redacted by the
executor, but `GET /items*` returns full `ItemSnapshot`s (answer keys and
explanations), and the ingest endpoints trigger outbound fetches and paid LLM
calls. Before the web UI exposes the surface beyond a trusted localhost
operator, requests must carry a role: **operator** (everything) or **taker**
(one session's verbs). Constraints: single-tenant, no user accounts, no
external identity provider, must keep working across restarts and across N
instances later (cloud roadmap), and must not leak into `domain`/`ports` — a
principal is a transport concern.

Alternatives considered:

1. **Cookie sessions + login database** — introduces stored credentials, CSRF
   handling, and a user store for what is a single-operator tool.
2. **OIDC / external IdP** — right for a multi-tenant SaaS deployment; far too
   heavy here, and deferred with cloud persistence (ROADMAP §2).
3. **Static bearer tokens for both roles** — a shared "taker password" cannot
   scope a taker to *their* session and leaks across takers.
4. **Static operator token + signed capability tokens for takers** — stateless,
   scoped, survives restarts, needs no storage.

## Decision

All enforcement is middleware in `cmd/testmaker` — no domain, port, or app
change. Two principals, three tokens, one secret:

- **Operator token** — a random 256-bit bearer credential generated into the
  config (`auth.operatorToken`, file mode 0600, same posture as the LLM API
  key) on first run. Grants every `/api` verb.
- **Auth secret** — a random 256-bit HMAC-SHA256 key (`auth.secret`, generated
  first run) that signs the two capability tokens.
- **Invite token** — minted by the operator for one test
  (`POST /api/tests/{id}/invites`): `ti.<b64url(JSON{tid,exp})>.<b64url(HMAC)>`.
  It grants exactly two verbs: preview the test summary and start a session for
  `tid` until `exp`. Stateless, therefore **not single-use** — a used invite
  can start further sessions until expiry (documented ceiling; the upgrade path
  is an invite store with a redemption flag).
- **Session token** — returned when a session starts:
  `ts.<b64url(JSON{sid})>.<b64url(HMAC)>`. It authorizes only that session's
  verbs (`answers`, `complete`, `score`). The operator token is also accepted
  on session verbs (operators may drive any attempt).
- **`auth.mode`** — `token` (default: roles enforced) or `none` (explicit
  opt-out for trusted-localhost development and most tests).
- **Media stays public**: `GET /api/media/{ref}` refs are sha256 content
  addresses — unguessable capability URLs carrying stimulus imagery only (never
  keys). `<img src>` cannot attach a bearer header, so this keeps the player
  dependency-free of cookies; signed media URLs are the upgrade path if media
  ever becomes sensitive.
- Verification uses constant-time comparison (`hmac.Equal` /
  `subtle.ConstantTimeCompare`); tokens are never logged; auth failures are
  transport-level responses (401/403 with `code: auth.*`), not
  `TestmakerError`s — the closed error-class vocabulary stays domain-only.
- Invite links put the token in the **URL fragment** (`/take#<token>`), which
  browsers never send to the server, so tokens stay out of request logs.

## Consequences

- Zero storage and zero coordination: any instance holding the secret verifies
  any token — the same "correct without a serialized authority" property the
  session CAS established (ADR-0001) — so this survives the multi-instance
  deployment unchanged.
- Revocation is coarse: rotating `auth.secret` invalidates every outstanding
  invite and session token; rotating the operator token is one config edit.
  Acceptable single-tenant; per-invite revocation arrives with the invite
  store, per-user identity/audit with cloud auth.
- The config file becomes secret-bearing on every deployment (it could already
  hold an LLM key); it is written 0600 and secrets can still live in the
  environment where preferred.
- Tests construct the server with `auth.mode: none` except the dedicated auth
  suite, so the existing surface tests stay focused on their own behaviour.
