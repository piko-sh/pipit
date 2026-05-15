---
title: "Run self-hosting checks"
description: "Compare bytecode from native and interpreted compilation."
section: "How-to guides"
order: 6
---

# Run self-hosting checks

Run Pipit's compiler under the interpreter and compare its bytecode with native
compilation. This checks whether Pipit can run its own compiler. It does not
execute the resulting bytecode. These checks run separately from `make test` and
`make check`.

The tests live in `tests/integration/selfhost`, a separate module enabled by the
`integration` build tag. They do not add dependencies to the public library.

## Run the checks

Run from the repository root:

```sh
PIPIT_SELFHOST_ARGS="-report /tmp/pipit-selfhost-report.md" \
  go test -tags integration -count=1 -run '^TestSelfhost$' ./tests/integration/selfhost/
```

The harness loads `internal/compile` from source and runs it under Pipit. Each
corpus program is compiled both natively and under interpretation, then the packed
bytecode is compared byte for byte. The command writes counts, timings, and
differences to `/tmp/pipit-selfhost-report.md`. Inspect that file for the result.
The broader `make test-integration` command updates the committed
[self-hosting report](selfhost-report.md).

Only the top-level `internal/compile` package runs interpreted. Its subpackages
and Pipit's other packages come natively through `sdk/selfhost`, whose symbol
tables are generated from `pipit-symbols-selfhost.yaml` with `make generate-selfhost-symbols`. Packages such as
`go/ast` and `go/types` come from the standard-library registry.

## Select programs and diagnostics

`TestSelfhost` runs the harness with its defaults. Without `-report`, it overwrites the committed
[self-hosting report](selfhost-report.md). Set `PIPIT_SELFHOST_ARGS` to pass these flags to the harness:

| Flag                   | Effect                                                                                  |
|------------------------|-----------------------------------------------------------------------------------------|
| `-corpus DIR`          | the directory whose `*/eval.go` files are compiled                                      |
| `-compiler DIR`        | the directory holding the compiler's sources (default `internal/compile`)               |
| `-filter A,B`          | compile only the programs whose directory name contains one of the fragments            |
| `-limit N`             | compile only the first N programs (after `-filter`)                                     |
| `-v`                   | one line per program                                                                    |
| `-go-dispatch=false`   | run the interpreted compiler on the assembly dispatcher (see below)                     |
| `-no-passes`           | compile the compiler itself without optimisation passes                                 |
| `-no-arena-promotion`  | compile the compiler itself without arena annotations, so every allocation is on the heap |
| `-no-gc`               | never run the register arena's minor collection while the interpreted compiler runs     |
| `-dump DIR`            | save mismatched bytecode, listings and failure stacks                                   |
| `-trace-lowerings FILE` | log lowering decisions; also set `PIPIT_TRACE_LOWERING=1`                              |
| `-disasm NAME`         | print the interpreted compiler's listing of one function, with source lines, and exit   |
| `-report FILE`         | the markdown report path                                                                |

For example, select programs whose directory names contain `0957` and save diagnostics:

```sh
mkdir -p /tmp/selfhost
PIPIT_SELFHOST_ARGS="-filter 0957 -dump /tmp/selfhost -report /tmp/selfhost/report.md" \
  go test -tags integration -count=1 -run '^TestSelfhost$' ./tests/integration/selfhost/
```

## Interpret the result

Inspect the report even when `go test` passes. `TestSelfhost` logs bytecode
mismatches without failing the test. It fails only when the harness could not run
or no report exists at the report path, so a report left by an earlier run still
passes. The harness itself returns 0 when every compared program compiles
identically; programs that fail natively or do not parse are not compared.

## Run additional checks

Run the compiler's own source check and the assembly-dispatch check:

```sh
go test -tags integration -count=1 \
  -run '^(TestSelfhostCompilesItself|TestSelfhostAssemblyDispatch)$' \
  ./tests/integration/selfhost/
```

The first test compares compilation of the compiler itself. The second runs a
subset of programs using assembly dispatch. Both are skipped under `-short`.

To compare two runs of the full corpus:

```sh
PIPIT_SELFHOST_DETERMINISM=2 go test -tags integration -count=1 -v \
  -run '^TestSelfhostIsDeterministic$' ./tests/integration/selfhost/
```

This checks whether each program has the same outcome on both runs. It does not
require every program to compile successfully. Use `PIPIT_SELFHOST_ARGS` to select
a smaller set, as shown above. The determinism test writes temporary reports.

To run the full corpus using assembly dispatch:

```sh
PIPIT_SELFHOST_ARGS="-go-dispatch=false -report /tmp/pipit-selfhost-assembly.md" \
  go test -tags integration -count=1 -run '^TestSelfhost$' ./tests/integration/selfhost/
```

Inspect `/tmp/pipit-selfhost-assembly.md` for mismatches. To investigate memory
reuse, add `pipit_arena_paranoid` to the build tags. This fills released
register slabs with a marker value, so accidental reuse returns obviously wrong
values.
