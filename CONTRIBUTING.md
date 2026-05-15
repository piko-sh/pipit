# Contributing to Pipit

## Requirements

Go 1.27+. `make help` lists every target, grouped by area. `golangci-lint`
2.13.2 for `make lint-go`; `shellcheck` for `make lint-scripts`;
`benchstat` for `make bench-stability`. `piko.sh/asmgen` is an ordinary
module dependency resolved from the proxy, so no sibling checkout is
needed to build or to run `make generate-asmgen`.

---

## Quick start

```bash
git clone https://pipit.sh/pipit.git
cd pipit
make check       # vet, lint, validators; what CI runs first
make test        # the unit and parity suites
```

---

## Code organisation

The engine lives under `internal/` and the root package `pipit.sh/pipit`
is the only way out of it. Layers import downwards: `app` over `compile`
over `engine` over `symtab` over the leaves (`isa`, `fault`, `policy`,
`safeconv`, `link`); the `depguard` rules in `.golangci.yml` enforce
that, so `make lint-go` is what fails on a bad import. The
[architecture page](docs/architecture.md) has the diagram, the lifecycle
and the generated-artefact table.

Big packages are split into sub-packages under themselves (`compile/{astpattern,appendlower,escape,fieldlayout,inline,isaselect,
liveness,passes,patterns,scope,typemap}`, `engine/{program,goversion,asm}`,
`symtab/{typemodel,descriptor}`). The rule for a new one: split under
the package it came from, and a leaf package imports nothing internal.

---

## Make targets

`make help` is the authoritative list; this table may lag it. Every target
is one line that delegates to a script under `hack/`, and the reasoning for
what a target does lives in that script's header comment.

| Target                                                          | What it does                                                                                                            |
|-----------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------|
| `make check`                                                    | vet, both lint passes, module verification, the prose check and every generated-file validator                          |
| `make check-all`                                                | `check` plus `test` and `test-golden`                                                                                   |
| `make test`, `test-short`                                       | the root facade and the CLI; the interpreter suites are integration tests and are not in this lane                     |
| `make test-integration`                                         | every suite under `tests/integration`: the parity corpus, language, bytecode, golden, apps, debug, session, selfhost   |
| `make test-safe`, `test-race`                                   | the pure-Go lane and the race detector, over `internal/` and the suites                                                 |
| `make test-torture*`                                            | the GC-torture lanes, with and without register-arena poisoning                                                         |
| `make test-golden`, `test-golden-update`                        | disassembly goldens and the hash manifest; update with `PIPIT_GOLDEN_UPDATE=1`                                          |
| `make test-isolation`                                           | the Linux sandbox tests with skips turned into failures                                                                 |
| `make test-gotoolchain`, `test-gotoolchain-update`              | run the Go toolchain's own `$GOROOT/test` run-mode tests through `bin/pipit` and ratchet the passing set per Go version |
| `make bench`, `bench-stability`                                 | run the benchmarks, and measure their run-to-run spread                                                                 |
| `make lint-go`, `lint-scripts`, `lint-vet`, `lint-vet-tags`     | golangci-lint (layering included), shellcheck, and vet on one or every build configuration                              |
| `make generate-all` and `generate-*` (each with `-validate`)    | regenerate or validate each generated artefact                                                                          |
| `make go-mod-tidy`, `go-mod-verify`                             | tidy the go.mod files that can be tidied; checksums and tidiness where it is meaningful                                 |

### tests/

The interpreter is proved by end-to-end suites, so they live under
`tests/integration`, one module each, behind the `integration` build tag:
`snippets` (the differential-parity corpus), `language`, `bytecode`,
`golden`, `apps`, `debug`, `session`, `limits`, `interop`, `gotoolchain`
and `selfhost`. `tests/bench` holds `cross_language/` (the sweep against
other runtimes) and `micro/` (Go microbenchmarks, behind the `bench` tag).

A plain `go test ./...` therefore covers the facade and the CLI only. The
lanes that exist to stress the engine, `test-safe`, `test-race` and
`test-torture*`, run the suites as well as `internal/`.

### CI

CI runs the gates a shared runner can actually finish: build and unit
tests, both lint passes, module verification, the generated-file
validators, the golden snapshots and the sandbox worker protocol. The
corpus-scale lanes are local: `make test-integration`, `make test-race`,
`make test-torture*` and `make test-gotoolchain`. Run them before anything
that touches the engine; nothing in CI will catch an interpreter
regression for you.

### hack/

Every Make target delegates to a script under `hack/`, organised by area:
`build/`, `test/`, `bench/`, `lint/`, `generate/`, `go/`, `verify/` and
`wasm/`. Each script sources `hack/lib/init.sh`, which sets the shell
options, exports `PIPIT_ROOT`, changes to the repository root and loads
the `pipit::log::`, `pipit::util::` and `pipit::go::` helpers. Scripts are
found rather than listed by `make lint-scripts`, so a new one cannot be
added without being linted.

---

## Working on the engine

Every change runs against the same nets. Before calling a step done:

1. `make test-golden`. State whether a golden diff is expected ("golden
   diff expected: none" or the list of snippets), inspect each listed
   snippet, then re-record with `make test-golden-update`.
2. `make test` (parity in the default lane) and `make test-safe` for the
   pure-Go lane. Engine files carry build tags; a symbol used from an
   untagged file must live in an untagged file, and `make lint-vet-tags`
   catches the ones that do not.
3. `make bench` for anything on a hot path, on a quiet machine, before and
   after. `make bench-stability` says which benchmarks are steady enough
   on this machine to read at all. There is no committed baseline and no
   automatic gate: one machine's numbers do not transfer to another.
4. `make lint-go`. The limits new code must meet: functions 90 lines or 60
   statements, cognitive complexity 15, cyclomatic 20, nesting depth 3,
   7 arguments, files 1000 lines, lines 200 characters.

---

## Conventions

- Minimise depth of nesting, prefer guard clauses.
- Prefer verbose code, no complicated boolean expressions.
- Pipit is exclusively written in British English.
- Always use field initialisation for structs (`Foo{bar: 1}`, not `Foo{1}`).
- Table-driven tests, with field initialisation for all test cases.
- No `goto`.

---

## Licence header

Every source code file needs this header.

```go
// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.
```

The anti-fascism statement is mandatory and non-negotiable.
