# ADR-0003: Content-addressed blob store, authoring-time media offload, and hardened media serving

- **Status:** Accepted
- **Date:** 2026-07-09

## Context

Block 11 gives figural items a home for their media. A generated matrix or
figure-series item carries an SVG per stimulus grid and per option; rulegen emits
each as a self-contained `data:image/svg+xml;base64,…` URI so an item is viewable
with no external dependency (see [DESIGN.md](../../DESIGN.md) media decision).
Persisting those inline blobs bloats every item and duplicates identical figures
across items. Block 11 must move the bytes out of the item aggregate, behind a
port, and let the renderer resolve them back — without inverting the dependency
rule (domain ← ports ← app ← adapters ← cmd) or coupling sibling adapters.

Three forks in the road are hard to reverse once items are persisted with the
resulting `MediaRef`, so they are recorded here rather than left as code:

1. **How a blob is addressed** — an opaque content hash vs. an item-scoped or
   random key.
2. **Where inline→ref rewriting happens** — the authoring use-case, a persistence
   decorator, or the generator emitting refs directly.
3. **How stored media is served** — figural media is SVG, which is
   script-executable in a browser.

## Decision

**1. Content addressing.** `ports.BlobStore` is a two-method port
(`Put(Blob) (ref, err)` / `Get(ref) (Blob, err)`) over a
`Blob{Bytes, ContentType}`. The ref is `hex(sha256(contentType + 0x00 + bytes))`.
Binding the content type *and* the bytes (NUL-separated — a MIME type contains no
NUL, so the delimiter is injective) means identical media dedupe to one ref, and
the same bytes served under two content types never collide. It is an
infrastructure port owning no domain aggregate (like `ports.RawItem` at the fetch
boundary), so it reuses the shared sentinels: unknown ref → `shared.ErrNotFound`,
empty input → `shared.ErrInvalid`. Native `memoryblob` (runtime default) and
`fsblob` ("local FS first") back it; an `adapters/aws/blob/*` S3 store is the
documented upgrade path. Cross-adapter ref equality is **not** part of the
contract — each backend mints and validates its own ref shape (fsblob accepts
only 64-lowercase-hex refs, which keeps `/media/{ref}` non-traversable). All
backends are proven by the `ports/blobtest` conformance suite.

**2. Offload at the authoring use-case.** `app/authoring.Service` takes an
optional `ports.BlobStore`. `Generate`/`Author` call `offloadMedia` before
persisting: any `MediaRef` that parses as an inline base64 `data:` URI is `Put`
and rewritten to the returned content ref; a non-`data:` ref (an external URL, an
already-offloaded ref) passes through untouched, so offload is idempotent. A nil
store is a no-op, keeping items self-contained. This puts the inline→ref rewrite
in the composition-owned use-case, not in the generator (which stays a pure
figure emitter) nor hidden in a persistence decorator.

**3. Hardened same-origin serving.** The renderer resolves refs via
`GET /media/{ref}`, writing the stored bytes with their content type plus
`X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src
'none'; sandbox`. The blob store can hold arbitrary producer-declared content
types, and SVG runs script, so the media endpoint is served pinned and sandboxed
so it cannot become a stored-XSS vector on the assessment origin.

`fsblob.Put` writes through a temp file renamed into place (atomic on POSIX same
directory), so a crash mid-write can never leave a truncated file under a content
ref that a "content-addressed" read would then serve as complete.

## Consequences

- **Dedup for free, at the cost of opaque refs.** Identical figures store once.
  Refs are undiscoverable hashes, so a client must distinguish an inline `data:`
  ref from a bare content ref (and turn the latter into `/media/{ref}`) by
  convention; items generated before a store was wired stay inline and are never
  offloaded retroactively, so the renderer must handle both forms. The resolution
  rule lives in [DESIGN.md](../../DESIGN.md) and [UBIQUITOUS.md](../../UBIQUITOUS.md)
  ("Media ref").
- **No read-time verification, no GC — accepted gaps.** `Get` does not re-hash to
  verify bytes against the ref, and the port has no `Delete`/`List`, so a blob
  `Put` for an item whose save later fails is an orphan (a space leak, not a
  correctness bug — content addressing makes retries converge). A sweep/`Delete`
  is deferred until fsblob or S3 runs at volume.
- **The authoring layer knows one wire detail (`data:` URIs).** The use-case
  depends on rulegen's emission format. The trade accepted: the generator stays
  pure and the composition root owns the media policy. If a second producer emits
  a different inline shape, the parse helper (or a shared encode/decode contract)
  is the seam to revisit.
- **The port stays thin (`Put`/`Get`).** No `Exists`/`List`/`Delete` until a
  concrete need (GC, admin) appears — within the interface-size budget and the
  YAGNI default.
- **S3 later drops in behind the same port** with its own ref shape and its own
  conformance-suite run; no app or renderer change.
