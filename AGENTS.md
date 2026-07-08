# Testmaker Agent Rules

Navigation doc for AI agents working in this repo. testmaker is a Go 1.25+
multi-module workspace building a library of cognitive-aptitude / IQ tests and
the two systems around them (a test designer/generator and a renderer/executor),
architected as Clean Architecture + Hexagonal + DDD. Layering, vocabulary, and
lint rules are machine-enforced — this file routes you to the authoritative
doc, it does not restate the rules.

Only the **source catalogue** vertical slice is implemented end-to-end today
(`domain/source` → `ports` → `app/catalog` → `memorycatalog` / `filecatalog` /
`stubfetcher` → `cmd/testmaker`). The other bounded contexts (`item`,
`testset`, `session`, `scoring`) are `SCAFFOLD` — just enough types for the
workspace to compile until their block lands.

## Where to look — task → doc

| Doing this | Read |
|---|---|
| Local setup, build/test, adding a new adapter module | [DEVELOPMENT.md](DEVELOPMENT.md) |
| Writing or modifying any test (conformance suites, anti-flake) | [TESTS.md](TESTS.md) |
| Lint failed; which linter, why, how to fix | [LINT.md](LINT.md) |
| Changing layering, dependencies, or adding an arch component | `.go-arch-lint.yml` (source of truth) + [ARCHITECTURE.md](ARCHITECTURE.md) |
| Understanding bounded contexts and their relationships | [DDD.md](DDD.md) |
| Naming a type, field, constant, enum value, or concept | [UBIQUITOUS.md](UBIQUITOUS.md) |
| What is built vs. what comes next, and in what order | [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) |
| Implementing one design item / plan block end-to-end | [§ Implementing a design item](#implementing-a-design-item) below |

`ARCHITECTURE.md`, `DDD.md`, `UBIQUITOUS.md`, and `IMPLEMENTATION_PLAN.md` are
the design docs at the repo root (present or added as the project fills in).

## Implementing a design item

When building one component/block from the design, read in this order before
writing code:

1. This file — layering, conventions, definition of done.
2. The item's spec: its section in [DESIGN.md](DESIGN.md) and its block in
   [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) (scope + "done when").
3. [DEVELOPMENT.md](DEVELOPMENT.md) — the adapter-module checklist when the
   item is a new module.
4. [TESTS.md](TESTS.md) — every rule applies, including conformance suites
   and the real-backend rules.
5. The port it implements or consumes (read every field comment) and one
   existing sibling as the pattern (`stubfetcher` = simple adapter,
   `filecatalog` = file-backed adapter, `sqlitetestdb` = SQL-backed adapter with
   the driver isolated in `acl_*.go`).

Documentation is part of the item, in the same change: flip the item's
status markers (🚧 → ✅) in DESIGN.md / ARCHITECTURE.md /
IMPLEMENTATION_PLAN.md, write the package `doc.go`, and update any table row
the item completes. Docs trailing code = not done.

## Layer cheat-sheet

Dependencies point **inward only**. Enforced by `.go-arch-lint.yml` (packages)
and mirrored by `depguard` in `.golangci.yml` (files).

```
domain  <-  ports  <-  app  <-  adapters  <-  cmd
```

| Ring | May import | Rule |
|---|---|---|
| **domain** | stdlib only (contexts talk via `domain/shared`) | pure; no project or vendor deps |
| **ports** | `domain` | interfaces = the hexagon boundary; ≤ 6 methods each |
| **app** | `domain`, `ports` | use-cases; never an adapter |
| **adapters** | `domain`, `ports`, own vendor SDK | each is its own component; siblings cannot couple |
| **cmd** | anything | the only composition root |

## Conventions not fully caught by lint

- Adapter `_test.go` files carry a compile-time assertion:
  `var _ ports.SourceRepository = (*Store)(nil)` (kept out of production files).
- Aggregates keep state private, are built via `NewX(Spec)`, and cross ports
  only as a `Snapshot` DTO — never as the aggregate itself.
- Errors are `shared.TestmakerError`; each context declares its own sentinels
  and callers match by `Code` with `errors.Is`.
- Every package has a `doc.go`; scaffold packages label themselves `SCAFFOLD:`.

Everything else — layering, vendor opt-in, interface size, wall-clock ban,
gofmt/vet — is enforced by `make lint` and `make test`. Point at the failing
checker rather than restating the rule ([LINT.md](LINT.md)).

## How to know you're done

Two commands, both green on your branch:

```bash
make lint    # gofmt + go vet + go-arch-lint + golangci-lint
make test    # unit tests (-short -race -timeout 120s) across every module
```

Or the aggregate the CI runs:

```bash
make check   # build + lint + test
```

Pre-existing failures on the branch are still your problem — fix or revert
before declaring done. When `make lint` conflicts with the code, the **code**
is wrong: move types inward, introduce a port, or push wiring to `cmd`. Relax
`.go-arch-lint.yml` only when a generic architectural concept is genuinely
missing, and document why inline.

## Adding a new arch component

1. Give it a real layer and role; name adapters by role
   (`adapter_source_<tech>`, `adapter_fetch_<tech>`).
2. Add the component and precise `mayDependOn` + `canUse` to `.go-arch-lint.yml`
   (use the `_no_external_deps_` sentinel when it has no vendor).
3. Register the module in `go.work` and add its `replace` directive; wire it in
   `cmd/testmaker`.
4. `make check` must stay green.

Full steps are in [DEVELOPMENT.md](DEVELOPMENT.md#adding-a-new-adapter-module).
