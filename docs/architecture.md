---
title: "Architecture"
description: "How Pipit compiles Go source, runs bytecode, and connects to host packages."
section: "Explanation"
order: 1
---

# Architecture

Pipit compiles Go source into bytecode, then executes it in a virtual machine
(VM). The library and CLI use the same engine under `internal/`. The root package,
`pipit.sh/pipit`, exposes the public API.

## From source to execution

Pipit uses Go's parser and type checker to read source and resolve types. Its
compiler translates each function into bytecode. Further passes analyse and
optimise the result, then verify the bytecode before execution.

```text
Go source
  -> parse and type-check       internal/app and internal/frontend, using go/parser and go/types
  -> compile functions         internal/compile
  -> analyse, optimise, verify internal/compile and internal/verify, run by internal/app
  -> compiled package          internal/engine/program
  -> execute                   internal/engine
```

A compiled package can also be saved and loaded through `internal/codec` and
`internal/adapters`.
The saved format is versioned and requires compatible host symbols when loaded.
See [bytecode compatibility](compatibility.md#bytecode-compatibility).

The VM stores working values in registers. On amd64 and arm64, an assembly loop
selects and runs bytecode instructions. Other platforms use a Go loop. The `safe`
build tag also selects the Go loop and disables unsafe fast paths.

The [execution model](../internal/engine/doc_execution_model.go) describes call
frames and how the assembly and Go execution paths interact.

## Package boundaries

Internal dependencies point from the application layer towards the engine and its
support packages. The import rules in `.golangci.yml` enforce these boundaries.

| Package | Responsibility |
|---|---|
| `internal/app` | Interpreter operations and sessions; runs the compiler passes |
| `internal/frontend` | Configure parsing and type-checking |
| `internal/compile` | Translate typed Go source into bytecode and optimise it |
| `internal/engine` | Execute bytecode and call native functions |
| `internal/symtab` | Register host symbols and describe their types |
| `internal/isa` | Define bytecode instructions |
| `internal/codec`, `internal/adapters`, `internal/schema` | Serialise compiled programs |
| `internal/verify` | Check bytecode before execution |
| `internal/debug` | Debugging and disassembly |

The public API wraps these internal packages. Internal methods and types are not
part of the public compatibility contract.

## Why the standard library is separate

An application chooses which host packages its scripts can import. Registering a
package exposes its functions and types through symbol tables built with Go
reflection. Those tables keep references to native code, so the Go linker cannot
remove that code from the binary.

The standard-library tables therefore live in a separate module, `sdk/stdlib`.
Applications can register the whole library, select bundles, or supply only their
own functions. The interpreter keeps a list of standard-library import paths in
`internal/stdlibindex` without linking their symbols.

Three other modules supply optional host symbols, linking support and tooling:
`sdk/selfhost` exposes Pipit's own packages for the self-hosting checks,
`sdk/link` supplies the `//piko:link` types that bridge generic host functions into the interpreter, and `sdk/extract`
generates symbol tables for `pipit extract` and any other tool that embeds it. Each is a separate module, so importing
Pipit alone does not pull them in. `sdk/module`, the module bundle format, is part of
the root module.

## Generated code

Instruction definitions in `internal/isa/spec*.go` supply opcode names, costs,
handlers, and assembly details. Generators use these definitions to keep the
execution paths consistent.

| Generated files | Source or generator |
|---|---|
| Assembly dispatch and trampolines | `cmd/asmgen`, using `internal/engine/asm` |
| Opcode tables and Go dispatch | Generators in `internal/engine` |
| Typed handlers and native-call adapters | `internal/engine/gen_kind_families.go` |
| Bytecode serialisation bindings | `internal/schema/bytecode.fbs`, using `flatc` |
| Named-scalar pool tags | `internal/symtab/gen` |
| Standard-library symbol tables | `pipit-symbols-stdlib.yaml`, using `pipit extract` |
| Self-hosting symbol tables | `pipit-symbols-selfhost.yaml`, using `pipit extract` |

Generated-file checks compare committed output with these sources. See
[Contributing](../CONTRIBUTING.md) for generation and validation commands.

## Build configurations

| Tag or platform | Effect |
|---|---|
| Default on amd64 and arm64 | Assembly dispatch and unsafe fast paths |
| `safe`, also selected for `js/wasm` | Go dispatch with unsafe fast paths disabled |
| `pipit_arena_paranoid` | Fill released register slabs with a marker value so stale reads stand out |
| `pipit_bce_paranoid` | Recheck omitted bounds checks at runtime |
| `bench` | Include the benchmark suite |
| `fuzz` | Include fuzz targets |
| `integration` | Include the integration test suites |

The `safe` tag changes the engine implementation. For the limits of in-process
execution and the experimental process boundary, see
[execution security](security.md).

## Related documentation

- [Embedding reference](api-reference.md): public operations and configuration.
- [Run self-hosting checks](selfhost.md): run Pipit's compiler under its interpreter.
