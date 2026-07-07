# testmaker

Build and administer cognitive-aptitude / IQ tests — logic-first (figure series,
matrices, Mensa-style figure reasoning, odd-one-out, syllogisms) plus numerical,
verbal, spatial and speed families. Testmaker catalogues **sources** of items,
**fetches or generates** them into an **item bank**, composes them into timed,
optionally adaptive **tests**, and **administers and scores** them.

Go 1.25 · multi-module `go.work` · DDD + Hexagonal + Clean Architecture, with the
layer graph enforced in CI by [`go-arch-lint`](.go-arch-lint.yml).

## Status

The **source-catalogue** slice is implemented end-to-end (domain → ports → app →
memory + file adapters → CLI) and seeded with an 81-source research catalogue.
Everything else is compiling scaffolding, built out block by block per
[IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md).

## Documentation

| Document | What it covers |
| --- | --- |
| [ARCHITECTURE.md](ARCHITECTURE.md) | System design: rings, bounded contexts, ports/adapters, mechanics |
| [DESIGN.md](DESIGN.md) | Model- and flow-level design decisions |
| [DDD.md](DDD.md) | Bounded contexts, aggregates, invariants, context map |
| [UBIQUITOUS.md](UBIQUITOUS.md) | Authoritative glossary |
| [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) | The work partitioned into blocks |
| [DEVELOPMENT.md](DEVELOPMENT.md) | Setup, build, adding modules, conventions |
| [LINT.md](LINT.md) | What the linters enforce and how |
| [TESTS.md](TESTS.md) | Test strategy + the conformance-suite pattern |
| [AGENTS.md](AGENTS.md) | Navigation for AI agents (task → doc) |
| [CLAUDE.md](CLAUDE.md) | Project mission, scope and test taxonomy |

## Project structure

```
domain/            pure model (stdlib only) — shared, source, item, testset, session, scoring
ports/             interfaces (the hexagon boundary) + conformance suites
app/               use-cases (catalog service today)
adapters/native/   edge implementations, one module each
  source/{memorycatalog,filecatalog}   in-memory repo + JSON/YAML loader
  fetch/stubfetcher                    placeholder Fetcher
cmd/testmaker/     composition root (CLI)
data/catalog/      seed source catalogue (sources.json / .yaml)
```

## Quick start

```bash
make install     # golangci-lint + go-arch-lint (pinned versions)
make check       # build + lint (gofmt, vet, go-arch-lint, golangci) + unit tests
go run ./cmd/testmaker --catalog data/catalog/sources.json
```

The CLI loads the catalogue into the in-memory repository and reports source
counts by category, the reusable set and the generator set, then exercises the
(stub) fetch boundary.

## Development

Dependencies point inward only: `domain ← ports ← app ← adapters ← cmd`. Each
adapter is its own module and lint component. See [DEVELOPMENT.md](DEVELOPMENT.md)
and [LINT.md](LINT.md).

## License

See [LICENSE](LICENSE).
