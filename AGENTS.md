# Testmaker Agent Rules

Navigation doc for AI agents working in this repo. testmaker is a Go 1.25+
multi-module workspace building a library of cognitive-aptitude / IQ tests and
the two systems around them (a test designer/generator and a renderer/executor),
architected as Clean Architecture + Hexagonal + DDD. Layering, vocabulary, and
lint rules are machine-enforced — this file routes you to the authoritative
doc, it does not restate the rules.

Every bounded context is implemented end-to-end today. The full pipeline runs
from cataloguing **sources**, through **fetching / generating** items into the
bank, **composing** them into timed (fixed or adaptive) **tests**, to
**administering and scoring** an attempt — exercised by the `cmd/testmaker` CLI
demo and exposed as an HTTP API (`testmaker -serve`). See [DESIGN.md](DESIGN.md)
for the current design and [ROADMAP.md](ROADMAP.md) for what is deliberately
deferred.

## Where to look — task → doc

| Doing this | Read |
|---|---|
| Local setup, build/test, adding a new adapter module | [DEVELOPMENT.md](DEVELOPMENT.md) |
| Writing or modifying any test (conformance suites, anti-flake) | [TESTS.md](TESTS.md) |
| Lint failed; which linter, why, how to fix | [LINT.md](LINT.md) |
| Changing layering, dependencies, or adding an arch component | `.go-arch-lint.yml` (source of truth) + [ARCHITECTURE.md](ARCHITECTURE.md) |
| Understanding bounded contexts and their relationships | [DDD.md](DDD.md) |
| Naming a type, field, constant, enum value, or concept | [UBIQUITOUS.md](UBIQUITOUS.md) |
| Why a specific design fork was taken (decision records) | [docs/adr/](docs/adr/README.md) |
| Component/model-level design, flows and mechanics | [DESIGN.md](DESIGN.md) |
| Implementing the web app + delivery-hardening initiative (task-by-task, with code) | [PLAN.md](PLAN.md) |
| Future directions / what is deliberately deferred | [ROADMAP.md](ROADMAP.md) |

`ARCHITECTURE.md`, `DDD.md`, `UBIQUITOUS.md`, `DESIGN.md` and `ROADMAP.md` are
the design docs at the repo root.

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
