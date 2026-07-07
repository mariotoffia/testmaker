# Development Guide

Everything you need to set up, build, test, and extend testmaker.

## Prerequisites

- **Go 1.25+** — testmaker is a multi-module Go workspace (`go.work`).
- **Make** — provides the build / test / lint entry points.
- **Dev tools** — run `make install` to install the pinned linters:
  `golangci-lint v2.12.2` and `go-arch-lint v1.15.0`.

## Repository Structure

testmaker is a multi-module `go.work` workspace. The **root module**
(`github.com/mariotoffia/testmaker`) holds the pure inner rings (domain,
ports, app) and depends on the standard library only. Each **adapter** is
its own module so technology-specific dependencies (yaml, a sqlite driver,
the AWS SDK, …) never leak into the core. `cmd/testmaker` is its own module
(the composition root).

```
testmaker/
├── go.work                 # Workspace: root + every adapter + cmd
├── go.mod                  # Root module (domain, ports, app) — stdlib only
├── Makefile                # build / test / lint entry points
├── .go-arch-lint.yml       # Layer graph — SINGLE SOURCE OF TRUTH
├── .golangci.yml           # golangci-lint v2 config (depguard mirrors the layers)
│
├── domain/                 # Innermost ring — pure, stdlib-only. Bounded contexts:
│   ├── shared/             #   shared kernel: TestmakerError + sentinels
│   ├── source/             #   source catalogue      (IMPLEMENTED)
│   ├── item/               #   item bank             (SCAFFOLD)
│   ├── testset/            #   test authoring        (SCAFFOLD)
│   ├── session/            #   test execution        (SCAFFOLD)
│   └── scoring/            #   scoring & feedback     (SCAFFOLD)
│
├── ports/                  # Hexagon boundary — interfaces; imports domain only
│   └── sourcetest/         #   conformance suite for SourceRepository adapters
│
├── app/                    # Application core (use-cases); imports domain + ports
│   └── catalog/            #   source-catalogue service
│
├── adapters/               # Port implementations (one go.mod per technology)
│   └── native/
│       ├── source/
│       │   ├── memorycatalog/   # in-memory SourceRepository
│       │   └── filecatalog/     # JSON/YAML CatalogLoader (yaml vendor)
│       └── fetch/
│           └── stubfetcher/     # no-op Fetcher (wires the fetch boundary)
│
├── cmd/testmaker/          # Composition root — wires adapters into the service
└── data/catalog/           # Research source catalogue (sources.json / sources.yaml)
```

Only the **source catalogue** vertical slice is implemented end-to-end today
(domain → ports → app → three adapters → cmd). Everything else is scaffolding
that keeps the workspace compiling until its implementation block lands.

## Getting Started

```bash
git clone https://github.com/mariotoffia/testmaker.git
cd testmaker
make install          # pinned golangci-lint + go-arch-lint
go work sync
make tidy
```

### Build and test

```bash
make build   # compile every module in the workspace
make test    # unit tests (-short -race -timeout 120s) across every module
```

Run the composition-root demo directly:

```bash
go run ./cmd/testmaker -catalog data/catalog/sources.json
```

## Layer / Dependency Rules

Dependencies point **inward only**. The rule is enforced twice: by
`.go-arch-lint.yml` at the package level and by the `depguard` block in
`.golangci.yml` at the file level. `.go-arch-lint.yml` is authoritative.

```
domain  <-  ports  <-  app  <-  adapters  <-  cmd
```

- **domain** — pure; standard library only, no project dependencies. Bounded
  contexts talk to each other only through `domain/shared`.
- **ports** — interfaces (the hexagon boundary). May import `domain` only.
- **app** — use-cases. May import `domain` + `ports`; never an adapter.
- **adapters** — implement ports. May import `domain` + `ports` + their own
  vendor SDK. Each adapter is its **own** arch-lint component, so siblings
  cannot couple to one another.
- **cmd** — the composition root. The only place allowed to import everything.

See [LINT.md](LINT.md) for how the two linters overlap and how to read a
failure. The bounded contexts and their relationships are narrated in
[DDD.md](DDD.md); the vocabulary in [UBIQUITOUS.md](UBIQUITOUS.md).

## Adding a New Adapter Module

Every adapter is a separate module. To add one (example: a sqlite-backed
`SourceRepository`):

1. Create the directory, `doc.go`, and `go.mod`:
   ```bash
   mkdir -p adapters/native/source/sqlitecatalog
   cd adapters/native/source/sqlitecatalog
   go mod init github.com/mariotoffia/testmaker/adapters/native/source/sqlitecatalog
   ```

2. Add the `replace` directive so the module resolves the root locally
   (path is relative to the new module — four levels up here):
   ```
   replace github.com/mariotoffia/testmaker => ../../../..
   ```

3. Register the module in the workspace `go.work`:
   ```
   use ./adapters/native/source/sqlitecatalog
   ```

4. Add a component and its dependency rule to `.go-arch-lint.yml`:
   ```yaml
   components:
     adapter_source_sqlitecatalog:
       in: [adapters/native/source/sqlitecatalog, adapters/native/source/sqlitecatalog/**]
   deps:
     adapter_source_sqlitecatalog:
       mayDependOn: [domain, domain_shared, ports]
       canUse: [sqlite]        # declare each vendor under `vendors:` and opt in here
   ```
   Adapters with no external dependency use `canUse: [_no_external_deps_]`.

5. Wire it at the composition root (`cmd/testmaker`) and add the matching
   `require` + `replace` to `cmd/testmaker/go.mod`.

6. Run `make tidy` then `make check`. Both must stay green.

## Code Conventions

- **`doc.go` per package** — every package opens with a `// Package <name> …`
  comment. It is the first thing a reader sees; scaffold packages say so
  (`SCAFFOLD: …`).
- **Aggregates are private, cross ports as DTOs** — the aggregate root keeps
  all state unexported and validated on construction (`NewSource(SourceSpec)`
  returns `*Source`). It accepts a **`Spec`** and emits a **`Snapshot`**;
  only the `Snapshot` crosses a port. `RehydrateFromSnapshot` rebuilds from a
  trusted snapshot without re-validating. See `domain/source/source.go`.
- **Ports are small interfaces** — keep them within the interface-size budget
  (ISP; `interfacebloat` caps at 6 methods). Split read/write only when a
  read-only consumer exists — don't pre-split on speculation.
- **Functional options** — if a constructor grows optional parameters, use the
  `WithXxx(value)` functional-options pattern rather than a config struct or
  positional booleans.
- **Errors via `shared.TestmakerError`** — the single structured error type.
  Match on `Code` with `errors.Is` against a sentinel; add detail with the
  copy-on-write builders `.WithMessage`, `.Wrap`, `.With(key, value)`. Each
  bounded context declares its own sentinels next to its model
  (`source.ErrInvalidSource`, `source.ErrUnknownSource`). Adapters wrap I/O
  errors with `%w` at the boundary (`wrapcheck`).
- **No wall clock in production** — `forbidigo` bans `time.Now/Sleep/After/…`;
  inject a clock. Determinism is a hard requirement (see [TESTS.md](TESTS.md)).

For testing conventions and the conformance-suite pattern, see
[TESTS.md](TESTS.md).
