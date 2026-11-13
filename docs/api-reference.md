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
| `SaveCompiled` / `LoadCompiled` | Key and compiled package / key | Saved bytecode / loaded package |
| `UnloadCompiled` | Package that `LoadCompiled` returned | Its package variables released |
| `MatchFile` | File name and content | Whether `CompileFileSet` and `CompileProgram` compile the file |

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
clears accumulated evaluation state. Separate `Eval` calls do not retain imports
and declarations. Session submissions do.

`Session.Inspect` describes retained imports and declarations. `Session.Reset`
clears them. A failed submission may still have changed existing runtime values.

Interpreters and sessions must not be evaluated concurrently. `Clone` creates
fresh execution state while sharing the symbol registry and configuration.
Configure registrations before cloning, and protect any shared host objects with
normal Go concurrency controls.

`Close` releases interpreter resources. Later compilation and evaluation return
an error wrapping `ErrClosed`. Closing again does nothing. It does not wait for
in-flight evaluations. Each clone has its own lifecycle.

### Loaded programs

Call `UnloadCompiled` when you no longer need a program returned by `LoadCompiled`.
This releases storage for its package variables once active runs finish. With the
built-in directory store, loads of the same bytecode share this storage until all
of them are unloaded.

Running an unloaded program, or any loaded program after `Reset`, returns
`ErrProgramUnloaded`. Do not call function values from a program after unloading it.
Unloading twice does nothing. Unloading a program this interpreter did not load
returns `ErrProgramNotLoaded`. If loaded programs fill the available storage,
`LoadCompiled` returns `ErrGlobalSlotsExhausted`.

## Errors

Use `errors.Is` to check for these exported error values.

| Failure | Errors and handling |
|---|---|
| Invalid source | `ErrParse`, `ErrTypeCheck`, `ErrCompilation`, `ErrCyclicImport`, `ErrEmbedUnsupported` |
| Function too large | `ErrRegisterOverflow` names the function and statement. Split large statements or calls. |
| Invalid option | `ErrInvalidOption` wraps `ErrInvalidConfig` and names the option. Every compilation and evaluation fails until the configuration is corrected. |
| Disallowed feature or import | `ErrFeatureNotAllowed`, `ErrPackageNotInRegistry`, `ErrCapabilityDenied`. An import outside `WithImportAllowlist` is reported as `ErrParse`. |
| Execution limit | `ErrCostBudgetExceeded`, `ErrAllocationLimit`, `ErrOutputLimit`, `ErrGoroutineLimit`, `ErrStackOverflow`, `ErrArenaBudgetExceeded` |
| Named-type limit | `ErrNamedCompositeQuota`, `ErrNamedCompositeLimit`. See [named-type limits](compatibility.md#named-types-and-native-interfaces). |
| Loaded programs | `ErrGlobalSlotsExhausted`, `ErrProgramUnloaded` (wraps `ErrClosed`), `ErrProgramNotLoaded`, `ErrNamedInterfaceLimit`, `ErrBytecodeVerification`. See [Loaded programs](#loaded-programs). |
| Borrow declaration | `ErrBorrowDeclarationsFrozen`, `ErrBorrowDeclarationRefused`. See [Licensed reuse](#licensed-reuse). |
| Cancellation | `ErrExecutionCancelled` keeps the context error in its chain. `ErrCallbackTimeout` also matches it. Deferred calls run within the [unwinding limits](#unwinding-limits). `ErrUnwindBlocked` reports one left blocked in native code. |
| Abandoned lock | `ErrLockAbandoned` means a stopped evaluation left a lock held in package state. Later evaluations fail immediately. Use a fresh interpreter. `AbandonedLocks` reports the count. |
| Too many blocked deferred calls | `ErrUnwindLeakLimit` means the limit set by `WithLeakedUnwinderCap` was reached. Later evaluations fail immediately. Use a fresh interpreter. |
| Runtime failure | `ErrUncaughtPanic`, `ErrDeadlock`, `ErrDivisionByZero`, `ErrIndexOutOfRange` |
| Interpreter defect | `ErrInterpreterInvariant`. Report it as a Pipit bug. |

`CompileProgram` can report failures from several packages at once. Use
`CompileProblems(err)` to inspect them. Each problem names its package.

The string-size and literal-element limits have no exported error value.
`UncaughtPanicValue(err)` retrieves the value of an unrecovered panic. The exported
errors are listed in [errors.go](https://github.com/piko-sh/pipit/blob/master/errors.go).

## Options

Pass these options to `NewInterpreter`. Limits cover interpreter operations, not
all native CPU or memory use. Cost metering is disabled by default. A cost budget
or a yield interval selects the Go dispatch loop. The public
[option declarations](https://github.com/piko-sh/pipit/blob/master/options.go) describe their individual semantics.

| Option | Unit | Default and meaning of zero |
|---|---|---|
| `WithMaxExecutionTime(d)` | Duration per evaluation | 15 minutes |
| `WithMaxAllocSize(n)` | Elements in one allocation | 1,073,741,824 |
| `WithMaxGoroutines(n)` | Concurrent goroutines | 10,000 |
| `WithMaxCallDepth(n)` | Call depth, including closures, method values and native callbacks | 10,000 |
| `WithMaxOutputSize(n)` | Bytes from `print` / `println` | 268,435,456 (256 MiB) |
| `WithMaxSourceSize(n)` | Source bytes per compilation | No source-size limit |
| `WithMaxStringSize(n)` | Bytes in one string built by concatenation, `strings.Repeat`, or rune appends | No string-size limit |
| `WithMaxLiteralElements(n)` | Elements in one composite literal | No literal-element limit |
| `WithCostBudget(n)` | Instruction cost | Metering disabled |
| `WithCallbackTimeout(d)` | Duration of a native call into an interpreted closure, including blocked time | No per-call limit. Negative also disables it. |
| `WithFinaliserTimeout(d)` | Duration of one finaliser or cleanup | 30 seconds, capped by a shorter `WithCallbackTimeout`. Must be positive. |

Finalisers and cleanups have additional [compatibility limits](compatibility.md#finalisers-and-cleanups).

With a debugger attached, the default execution timeout is disabled. Set an
explicit timeout or pass a context with a deadline when you need a time limit.

These are the defaults for ordinary embedding. Restricted and isolated execution
use separate limits. The CLI also sets some limits itself, including call depth.
See the [CLI defaults](cli.md#resource-limits).

Other options include:

- `WithYieldInterval(n)` - how often the dispatch loop checks for cancellation
- `WithBuildTags("integration", ...)` - tags set alongside `interp` when choosing
  files; see [build constraints](compatibility.md#build-constraints)
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
- `WithMaxLoadedNamedInterfaces(n)` - distinct interface names loaded bytecode may
  add, default 65,536. Exceeding it returns `ErrNamedInterfaceLimit`.
- `WithMaxNamedCompositeTypes(n)` - new named array, slice, map, channel and
  function types per interpreter and its clones, default 4,096.
- `WithNamedCompositeProcessLimit(n)` - the same types across the process,
  default 65,536. The last configured value applies to every interpreter.
- `WithAllowUnpinnedModules(true)` - accept module refs without an integrity pin (off by default; `LoadModule` returns
  `ErrUnpinnedModuleRef` otherwise)
- `WithLogger(logger)` - fallback `*slog.Logger` for diagnostics. A logger set with
  `ContextWithLogger(ctx, logger)` takes precedence for that call. Isolated
  constructors use `IsolatedConfig.Logger` instead.

Symbol-provider registration failures are logged and returned on the first call
that takes a context, using that call's logger.

### Unwinding limits

When an evaluation is cancelled or exceeds a time, cost or arena limit, Pipit
attempts its pending deferred calls. These options bound that work. Each must be
positive. Invalid values make compilation and evaluation fail with `ErrInvalidOption`.

| Option | Unit | Default |
|---|---|---|
| `WithAbortUnwindCost(n)` | Instruction cost all pending deferred calls may spend | 100,000 |
| `WithAbortUnwindWall(d)` | Wall time all pending deferred calls may spend | 1 second |
| `WithAbortUnwindDeferWallMax(d)` | Wall time one deferred call may spend | 500 ms |
| `WithAbortUnwindNativeGrace(d)` | Time a deferred call may stay in a native call past its deadline before it is left blocked (`ErrUnwindBlocked`) | 200 ms |
| `WithGoroutineJoinUnwindCap(d)` | Time, from its start, the wait for an evaluation's goroutines keeps extending while they unwind | 10 seconds |
| `WithLeakedUnwinderCap(n)` | Deferred calls left blocked in native code before every later evaluation fails with `ErrUnwindLeakLimit` | 64 |
| `WithLockWalkMaxValues(n)` | Values the abandoned-lock check visits from package variables; past it, unreached locks count as abandoned (`ErrLockAbandoned`) | 4,194,304 |

## Licensed reuse

Pipit reuses working storage only when no retained value can reach it. See
[licensed reuse](licensed-reuse.md) for the guarantees.
These declarations let more storage be reused. Make them before the first evaluation;
a later declaration returns `ErrBorrowDeclarationsFrozen`.

`DeclareNativeBorrowClass(function, class)` declares what one of your native
functions does with its arguments:

| Class | The function |
|---|---|
| `BorrowUnknown` | Declares nothing; its arguments are treated as kept (the default) |
| `BorrowBorrows` | Keeps no reference to an argument once it returns |
| `BorrowReadsOnly` | Keeps no reference, returns no alias and never writes through an argument |
| `BorrowResultAliases` | May return a result that aliases an argument, but keeps no other reference |
| `BorrowRetains` | May keep a reference to an argument |

Pipit returns `ErrBorrowDeclarationRefused` for method values, functions with
function or interface parameters, and functions whose Go escape information says
they retain arguments. The existing class stays in force. Accepted declarations
must be accurate to keep reuse safe.

`DeclareTopLevelCalls(program)` lets host calls to script functions use ordinary
execution reuse. Declare it only when both conditions hold.

- The host calls them directly, outside any native function called by a script.
- Their arguments cannot reach values belonging to a running program.

`WithMaxArenaSizeBytes(n)` bounds an execution's working arena. Exceeding it returns
`ErrArenaBudgetExceeded`. It does not bound other Go memory.

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
A logger in the context passed to `Eval` or `EvalFile` takes precedence over `Logger`,
and a nil `Logger` with no logger in the context uses `slog.Default()`. See the
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
