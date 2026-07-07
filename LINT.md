# Testmaker Lint

Two linters guard the codebase, and they overlap on purpose:

- **`go-arch-lint`** (`.go-arch-lint.yml`) — enforces the layer graph at the
  **package** level. This file is the **single source of truth** for layering.
- **`golangci-lint`** (`.golangci.yml`) — standard static analysis, plus a
  `depguard` block that mirrors the load-bearing layer edges at the **file**
  level so the IDE flags a boundary violation without a full arch-lint run.

`make lint` runs both (and gofmt + go vet). It is the static gate; `make test`
is the test-side gate. `make check` = `build + lint + test` (the CI aggregate).

```bash
make lint        # gofmt check + go vet + arch-lint + golangci-lint
make arch-lint   # just the layer graph
make golangci    # just golangci-lint (looped over every module)
```

## What `make lint` runs

`make lint` executes four stages in order; the first failure stops the build.

| # | Stage | Tool | Catches |
|---|---|---|---|
| 1 | Format | `gofmt -l` on tracked `*.go` | Files not run through `gofmt`. Auto-fix: `make fmt`. |
| 2 | Vet | `go vet` per workspace module | Stdlib correctness issues (printf, shadow, unreachable, …). |
| 3 | Architecture | `go-arch-lint check` | Outward dependency edges and un-opted-in vendor imports (see below). |
| 4 | Lint | `golangci-lint run` per module | The full `.golangci.yml` ruleset (`depguard`, `forbidigo`, `wrapcheck`, `interfacebloat`, `ireturn`, `revive`, `gochecknoglobals`, `gochecknoinits`). |

## What `go-arch-lint` enforces

`.go-arch-lint.yml` (schema v3) declares one **component** per package group
and, for each, a `mayDependOn` allow-list and a `canUse` vendor allow-list.
The dependency rule is Clean Architecture — imports point inward only:

```
domain  <-  ports  <-  app  <-  adapters  <-  cmd
```

Two settings make it strict:

- **`depOnAnyVendor: false`** — every external dependency must be opted into
  explicitly via `canUse`. A component with none uses the `_no_external_deps_`
  sentinel, which turns "this component has zero vendors" into a machine-checked
  claim. `domain_shared`, `domain`, `ports`, `ports_conformance`, and `app` all
  carry it; `adapter_source_filecatalog` is the only component allowed a vendor
  (`gopkg.in/yaml.v3`).
- **`deepScan: true`** — catches structural / method-call leaks where a type
  from one component flows into another even without a direct import.

The domain is split into two components — `domain_shared` (the shared kernel,
`domain/shared`) and `domain` (every bounded context). Contexts may depend on
the kernel but not on each other, so the DDD rule "contexts talk only through
`domain/shared`" is machine-checked, not just narrated. Conformance suites
(`ports/<x>test`) are their own component (`ports_conformance`) because they
exercise the port interfaces and so depend on `ports` itself.

Each adapter is its **own** component (`adapter_source_memorycatalog`,
`adapter_source_filecatalog`, `adapter_fetch_stubfetcher`, …), so cross-adapter
coupling fails lint the moment it is introduced. `cmd` is the only component
with `anyProjectDeps` + `anyVendorDeps`. Test files (`*_test.go`) and the
`data/` directory are excluded from the arch scope.

## How `depguard` mirrors the architecture

`golangci-lint`'s `depguard` rules restate the inward-only edges at the file
level, scoped by path:

- files under `**/domain/**` may not import `ports`, `app`, or `adapters`;
- files under `**/ports/**` may not import `app` or `adapters`;
- files under `**/app/**` may not import `adapters`.

This is a fast, editor-visible mirror — not a second source of truth. When
`depguard` and `go-arch-lint` disagree, `.go-arch-lint.yml` wins; fix
`.golangci.yml` to match.

Other `.golangci.yml` rules worth knowing:

- **`forbidigo`** bans `time.Now/Sleep/After/Tick/NewTimer/NewTicker/Since/Until`
  in production code — inject a clock instead (determinism, see [TESTS.md](TESTS.md)).
- **`interfacebloat`** caps port interfaces at 6 methods (ISP).
- **`ireturn`** — accept interfaces, return concrete types.
- **`wrapcheck`** — wrap errors crossing a boundary with `%w`; internal
  `testmaker` packages and `ports.*` interface returns are exempt.
- **`gochecknoglobals` / `gochecknoinits`** — no package globals or `init`;
  `domain/` is exempted because its enum sentinels and closed-set validation
  tables are idiomatic. Test files relax the strict rules too.

## Adding a Vendor

A new adapter that needs an external dependency must declare it in
`.go-arch-lint.yml`, or arch-lint fails on the un-opted-in import:

1. Register the vendor under `vendors:`:
   ```yaml
   vendors:
     sqlite: { in: ["modernc.org/sqlite"] }
   ```
2. Opt the owning component into it via `canUse`:
   ```yaml
   deps:
     adapter_source_sqlitecatalog:
       mayDependOn: [domain, ports]
       canUse: [sqlite]
   ```

Only the adapter that actually imports the SDK gets the `canUse` entry. The
core rings (`domain`, `ports`, `app`) stay stdlib-only.

## Escape hatch

```bash
make fmt   # gofmt -w on every tracked Go file (also aliased as make lint-fix)
```

Formatting is the only auto-fix. Every other failure requires a code change:
move the offending type inward, introduce a port, or wire it at the
composition root.

## Authoritative sources

- `.go-arch-lint.yml` — component dependency map; **single source of truth**
  for architecture. `ARCHITECTURE.md` narrates it.
- `.golangci.yml` — golangci-lint v2 ruleset (`depguard` mirrors the layers).
- `Makefile` — the `lint`, `arch-lint`, `golangci`, and `fmt` targets.
