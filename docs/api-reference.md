---
title: "Embedding reference"
description: "Interpreter operations, state, errors, and configuration options."
section: "Reference"
order: 2
---

# Embedding reference

The public library evaluates source, compiles packages, and manages execution
state. Go syntax and standard-library APIs follow Go's documentation, subject to
Pipit's [compatibility limits](compatibility.md). For complete programs, see
[Embedding tasks](api.md).

## Evaluation and compilation

| Operation | Input | Result |
|---|---|---|
| `Eval` | Expression, statements, or imports/declarations followed by statements; a lone call with no result fails type-checking | Final expression as `any`, or `nil` |
| `EvalFile` | Complete source text and entrypoint name | Entrypoint result |
| `Compile` / `Execute` | Expression or statement source / compiled function | Compiled function / execution result |
| `CompileFileSet` / `ExecuteEntrypoint` | Named files in one package / compiled package and entrypoint | Compiled package / entrypoint result |
| `NewSession` / `Submit` | Interpreter / incremental source submission | Session / final expression result |

Compilation and execution return errors. Ordinary integer results have type `int`;
explicit numeric types retain their types. `EvalFile` takes source text, not a path.

## Imports

`NewInterpreter` registers no packages. `WithSymbolProvider` registers the host functions, types, and values
returned by a provider. Later providers take precedence. `NewInterpreterWithSymbols` adds a table whose
symbols override option-provided exports with the same package and name.

`stdlib.WithStandardLibrary()` registers the bundled standard library. Individual
bundles can reduce linked dependencies:

| Bundle | Main packages |
|---|---|
| `core` | strings, bytes, fmt, math, time, sort, slices, maps, sync, runtime |
| `codec` | encoding, hashing, compression, archives |
| `crypto` | cryptography, TLS, X.509 |
| `net` | networking, HTTP, URLs, MIME |
| `system` | os, os/exec, io, paths, database/sql |
| `text` | templates, regular expressions, text scanning |
| `image` | images and decoders |
| `gotool` | go/*, debug/*, testing/* |

`WithImportAllowlist` further limits imports. `StandardLibraryPaths()` lists package
names without linking symbols. Registered functions execute native Go with host
access; import selection is not a process boundary.

## State and concurrency

An interpreter owns mutable execution state, including package globals. `Reset`
clears accumulated evaluation state. Separate `Eval` calls do not retain imports and declarations. Session submissions do.

`Session.Inspect` describes retained imports and declarations; `Session.Reset`
clears them. A failed submission does not guarantee rollback of mutations to
existing runtime values.

Interpreters and sessions must not be evaluated concurrently. `Clone` shares the
symbol registry and configuration but creates fresh execution state. Clones may still share
host objects. Protect those objects with normal Go concurrency controls.
Configure registrations before cloning.

## Errors

Use `errors.Is` to check for these exported error values:

| Failure | Examples |
|---|---|
| Invalid source | `ErrParse`, `ErrTypeCheck`, `ErrCompilation` |
| Disallowed feature/import | `ErrFeatureNotAllowed`, `ErrPackageNotInRegistry`, `ErrCapabilityDenied`; an import outside `WithImportAllowlist` is reported as `ErrParse` |
| Resource limit | `ErrCostBudgetExceeded`, `ErrAllocationLimit`, `ErrOutputLimit`, `ErrGoroutineLimit`, `ErrStackOverflow` |
| Cancellation | `ErrExecutionCancelled`; the context error remains in the chain |
| Runtime failure | `ErrUncaughtPanic`, `ErrDeadlock`, `ErrDivisionByZero`, `ErrIndexOutOfRange` |
| Interpreter defect | `ErrInterpreterInvariant`; report it as a Pipit bug |

The string-size and literal-element limits have no exported error value.
`UncaughtPanicValue(err)` retrieves the value of an unrecovered panic. The exported
errors and their descriptions are listed in [errors.go](../errors.go).

## Options

Pass these options to `NewInterpreter`. Limits cover interpreter operations, not
all native CPU or memory use. Cost metering is disabled by default. A cost budget
or a yield interval selects the Go dispatch loop. The public
[option declarations](../options.go) describe their individual semantics.

| Option | Unit | Default and meaning of zero |
|---|---|---|
| `WithMaxExecutionTime(d)` | Duration per evaluation | 15 minutes |
| `WithMaxAllocSize(n)` | Elements in one allocation | 1,073,741,824 |
| `WithMaxGoroutines(n)` | Concurrent goroutines | 10,000 |
| `WithMaxCallDepth(n)` | Interpreted calls | 10,000 |
| `WithMaxOutputSize(n)` | Bytes from `print` / `println` | 268,435,456 (256 MiB) |
| `WithMaxSourceSize(n)` | Source bytes per compilation | No source-size limit |
| `WithMaxStringSize(n)` | Bytes in one string built by concatenation, `strings.Repeat`, or rune appends | No string-size limit |
| `WithMaxLiteralElements(n)` | Elements in one composite literal | No literal-element limit |
| `WithCostBudget(n)` | Instruction cost | Metering disabled |

With a debugger attached, the default execution timeout is disabled. Set an
explicit timeout or pass a context with a deadline when you need a time limit.

These are the defaults for ordinary embedding. Restricted and isolated execution
use separate limits. The CLI also sets some limits itself, including call depth.
See the [CLI defaults](cli.md#resource-limits).

Other options include:

- `WithYieldInterval(n)` - how often the dispatch loop checks for cancellation
- `WithBuildTags("integration", ...)` - //go:build evaluation
- `WithEnv(map[string]string{...})` - variables overlaid on the host environment
  for `os.Getenv`, `os.LookupEnv`, `os.ExpandEnv` and `os.Environ`; a script's
  `os.Setenv` and `os.Unsetenv` change only its own view
- `WithSealedEnv(map[string]string{...})` - the whole environment; the host's
  variables are never visible
- `WithArgs([]string{"prog", "-v", "x"})` - what `os.Args` and the `flag`
  package's command line see; a script's `flag.Parse()` reads these
- `WithDeadlockGrace(d)` - how long every goroutine may sit parked on
  program channels before the run fails with `ErrDeadlock` (default one
  second; negative disables the check)
- `WithForceGoDispatch()` - force the Go dispatch loop, disabling ASM
- `WithDebugInfo()` - retain source maps and variable tables for debugging
- `WithDeniedImports(paths...)` - refuse the listed imports; a denial wins over
  `WithImportAllowlist`
- `WithStderr(w)` - where `print` and `println` write, instead of `os.Stderr`
- `WithMaxArenaSizeBytes(n)` - bound the register arena, not all Go memory
- `WithAllowUnpinnedModules(true)` - accept module refs without an integrity pin (off by default; `LoadModule` returns
  `ErrUnpinnedModuleRef` otherwise)
- `WithLogger(logger)` - the `*slog.Logger` receiving interpreter diagnostics;
  isolated constructors take their logger in `IsolatedConfig` instead

## Restricted and isolated APIs

`NewRestrictedInterpreter` requires one or more symbol providers. An empty import
list selects the available packages in `PureSurface()`. A nil capability hook
installs `DenyCapabilityHook`.
Submissions have fresh state and return captured output plus an optional JSON
scalar. Concurrent submissions are rejected. Zero limits select the defaults
below; negative limits are invalid.
See [execution security](security.md) for the boundary's limitations.

### Restricted limits

Set these fields on `RestrictedConfig`:

| Field | Default | Measures |
|---|---|---|
| `Timeout` | 2 seconds | Cooperative compilation and execution time per submission |
| `MaxSourceBytes` | 1 MiB | Source bytes per submission |
| `MaxOutputBytes` | 64 KiB | Captured `print` and `println` output |
| `MaxReturnBytes` | 256 KiB | Encoded JSON result |
| `MaxStringBytes` | 1 MiB | Interpreter string operations |
| `MaxAllocationElements` | 65,536 | Elements in one allocation |
| `MaxCallDepth` | 256 | Interpreted calls |
| `CostBudget` | 10,000,000 | Metered instruction cost |
| `MaxArenaBytes` | 64 MiB | Memory for the interpreter's working registers |

Fixed compiler limits allow 256 levels of expression nesting, 65,536 constant-pool
entries, 1,024 type specialisations, and 1,024 method resolutions. These are not
fields on `RestrictedConfig`.

These limits do not cap total process memory. The register limit excludes other
Go allocations, and instruction cost excludes compilation and native work.
Cancellation is cooperative: it cannot forcibly stop a parser, compiler, or native
call that does not check for it.

Set `Hook` to customise capability checks and `Logger` to receive diagnostics.
A nil logger uses `slog.Default()`. See the
[capability-hook example](api.md#control-native-calls-with-a-capability-hook).

### Isolated execution

The experimental isolated APIs run in a separate process and require host-selected
executable approvals, tenant identity, delegated native controls, and private state
storage. Their setup and lifecycle are documented in [isolated execution](isolated-execution.md).

## Debugger operations

Create a debugger with `NewDebugger()` and attach it using `WithDebugger`.
Use `WithDebugInfo()` when inspecting variables or evaluating expressions.

| Operation | Purpose |
|---|---|
| `SetPauseOnEntry(true)` | Pause at the first entrypoint instruction |
| `SetBreakpoints(file, points)` | Set line breakpoints; check each result's `Verified` and `Message` fields |
| `SetFunctionBreakpoints(points)` | Set breakpoints using runtime function names |
| `SetExceptionBreakpoints(filters)` | Choose exception filters; `ExceptionFilterPanic` is the only one |
| `WaitForPause(ctx)` | Wait for a pause; return `ErrDebugExited` if execution ends first (`Eval` does not report its end) |
| `WaitForEvent(ctx)` | Read pause, thread, and execution events |
| `StackTrace(threadID)` | Read a paused thread's stack |
| `Variables(threadID, frame, scope)` | Read locals, captured variables, or globals |
| `Evaluate(ctx, threadID, frame, expression, options)` | Evaluate an expression in a paused frame |
| `StepIn`, `StepOver`, `StepOut` | Resume until the selected thread reaches the step target |
| `Continue()` | Resume paused threads |
| `Pause()` / `Stop()` | Request a pause / end execution |

Frame index `0` selects the current function. Scopes are `ScopeLocals`,
`ScopeClosure`, and `ScopeGlobals`. Inspection and stepping return
`ErrDebugNotPaused` when execution is not paused. Handle errors from every call that
returns one.

Expression evaluation uses the paused frame's variables and imports. Function
calls need `EvalOptions{AllowCalls: true}` and can change shared data. Keep calls
disabled when you only need to inspect a value. For a complete program, see
[Attach a debugger](api.md#attach-a-debugger).
