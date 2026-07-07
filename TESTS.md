# Test Authoring Rules

The contract every new or modified test in testmaker follows. Goal:
deterministic, architecturally correct, fast enough that `make test` is a
trustworthy green-or-red signal on every save.

Tests use the **standard library `testing` package only** — no testify, no
mock frameworks. If a rule conflicts with an existing test, the rule wins;
rewrite the test when you touch it.

Architecture under test: [DDD.md](DDD.md), [UBIQUITOUS.md](UBIQUITOUS.md),
[ARCHITECTURE.md](ARCHITECTURE.md). Use those terms in test names and helpers.

## Categories

| Category | Lives in | Run with | Time budget |
|---|---|---|---|
| **Unit** (all tests today) | `foo_test.go` next to `foo.go` | `make test` (`-short -race -timeout 120s`) | < 100 ms / test |
| **Integration / long-running** (later) | added alongside the sqlite and fetch-adapter blocks | gated by `testing.Short()` | seconds+ |

Only unit tests exist right now — the implemented slice has no real I/O.
When a sqlite `SourceRepository` or a live fetch adapter lands, its
integration test skips under `testing.Short()` so `make test` stays green on
a machine without the backing dependency. Do not add a fast pure-fake test to
that category; a test that "feels integration-y" but uses only fakes is a
**unit** test.

## Style

- **Standard `testing` only.** Table-driven cases with `t.Run(name, …)`
  subtests; assert with plain `if` + `t.Fatalf`. No third-party assertion or
  mock library.
- One behaviour per top-level test function; `t.Run` groups the cases of that
  behaviour.
- Black-box package (`package foo_test`) unless you must touch unexported
  identifiers.
- Errors are compared with `errors.Is` / `errors.As`, never against
  `err.Error()` strings. For a `shared.TestmakerError`, matching is by `Code`
  — `errors.Is(err, source.ErrUnknownSource)`.

## The Conformance Suite (the key idiom)

Every implementation of a driven port is proven against **one shared suite**,
so all adapters (memory today, sqlite later) are guaranteed behavioural parity.
The suite is an exported function that takes `*testing.T` and a constructor for
a fresh, empty implementation. It lives next to the port under `ports/<x>test/`.

`ports/sourcetest/catalog.go` exposes:

```go
func RunSourceRepositoryTests(t *testing.T, newRepo func() ports.SourceRepository)
```

Each `SourceRepository` adapter runs it in one line:

```go
func TestSourceRepositoryConformance(t *testing.T) {
    sourcetest.RunSourceRepositoryTests(t, func() ports.SourceRepository {
        return memorycatalog.NewStore()
    })
}
```

The suite drives the whole contract — `PutThenGet`, `GetUnknownReturns
ErrUnknownSource`, `PutReplacesSameID`, list-filtering by category/family,
`Delete` (absent id is not an error), and `SnapshotIsolation` (a returned
snapshot must not share internal slices). **If the suite does not cover a
behaviour you need, extend the suite** — never write a one-off in the adapter
package. Every implementation gains the new check that way.

## Compile-Time Interface Assertions

Each adapter proves it satisfies its port with a `var _` assertion. These live
in the adapter's **`_test.go`** file (not the production file) so the
production package imports no `ports` package, keeping the arch graph clean:

```go
// in memorycatalog/store_test.go
var _ ports.SourceRepository = (*memorycatalog.Store)(nil)
```

The pattern is used by `memorycatalog` (`SourceRepository`), `filecatalog`
(`CatalogLoader`), and `stubfetcher` (`Fetcher`).

## Anti-Flake Rules

- **No `time.Now()`.** `forbidigo` bans the wall clock in production; inject a
  clock and drive it from the test. A test reading real time is a flake under
  CI load.
- **No `time.Sleep` for synchronisation.** Synchronise on channels, a
  `sync.WaitGroup`, or a started-signal — never on "100 ms ought to be enough".
- **Determinism over realism.** Map iteration order is undefined: build a
  sorted slice before asserting (the store `List` sorts by id for exactly this
  reason). Do not assert on unstable ordering or formatted messages.
- **Always `-race`.** It is on in `make test`. Register every goroutine, file,
  or other resource with `t.Cleanup`/`defer` at acquisition, so a leak fails
  its own test.

## Where Assertions Live

Test code — including the `var _` interface assertions and hand-rolled fakes —
lives in `*_test.go`. `*_test.go` files are excluded from the arch scope and
have the strict linters (`revive`, `wrapcheck`, `ireturn`, `forbidigo`,
`gochecknoglobals`) relaxed, so tests read naturally.

## Running

```bash
make test    # unit tests, -short -race -timeout 120s, over every module
make check   # build + lint + test (the CI aggregate)
```

`make test` is the contract: it must stay green on a fresh checkout with no
external services.

## Per-Test Checklist

- [ ] Standard-library `testing` only — no testify, no generated mocks.
- [ ] Errors compared with `errors.Is` / `errors.As`, not strings; a
      `TestmakerError` matched by its sentinel (`Code`).
- [ ] No `time.Now()` / `time.Sleep`; time and ordering are deterministic.
- [ ] If the code under test implements a port, the matching conformance suite
      from `ports/<x>test` is invoked (and extended if a case is missing).
- [ ] Compile-time `var _ ports.X = (*T)(nil)` assertion present in the
      adapter's `_test.go`.
- [ ] Runs green under `-race`; every resource cleaned up with `t.Cleanup`.
- [ ] No imports of a sibling adapter or another module's unexported types.
