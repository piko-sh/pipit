---
title: "Compatibility and limits"
description: "Go language support, known behavioural differences, and bytecode compatibility."
section: "Reference"
order: 1
---

# Compatibility and limits

Pipit has its own compiler and runtime. It type-checks source as Go 1.27 and
supports many Go language features, but does not claim complete compatibility.
Check your programs when adopting or upgrading it, especially code that relies
on reflection or native interfaces.

## Language and packages

The [language tests](https://github.com/piko-sh/pipit/tree/master/tests/integration/language) and
[parity corpus](https://github.com/piko-sh/pipit/tree/master/tests/integration/snippets) exercise functions, methods,
interfaces, generics, closures, slices, maps, channels, goroutines, defer, and
panic/recover. The corpus compares interpreted results with native Go. Passing
these cases does not imply every combination is supported. Restricted execution
also deliberately disables some features; see [execution security](security.md).

Host package registration makes native symbols available for imports.
`stdlib.WithStandardLibrary()` registers the bundled set; smaller bundles and
custom providers are described in [Embedding](api.md#choose-imports).

The standard-library registry excludes `syscall`, `plugin`, `runtime/cgo`,
`runtime/race`, `runtime/trace`, and `runtime/coverage`. The compiler handles
`unsafe` separately. `log/syslog` is available only on platforms that provide it.

Use `pipit symbols list` to inspect the CLI's actual registrations. Embedders can
inspect `stdlib.Exports()`; `pipit.StandardLibraryPaths()` provides package names
without linking the symbol tables.

## Known limitations

| Feature | Current behaviour and alternatives |
|---|---|
| `//go:embed` | Rejected in interpreted files. Use `//go:build !interp` for the embedding file. See [Embedded files](#embedded-files). |
| Promoted methods passed to native interfaces | Several built-in interfaces require methods declared directly on the script type. These include `error`, `fmt.Stringer`, `fmt.Formatter`, `fmt.Scanner`, `sort.Interface`, `heap.Interface`, `http.Handler`, JSON and text marshaling interfaces, and the `io` reader/writer interfaces. Add a forwarding method on your type. `fs.FS` and `fs.File` support promoted methods. |
| Script methods through `any` | Supported for selected standard-library calls. See [Native method support](#native-method-support). |
| Generic standard-library functions | `reflect.TypeFor`, `reflect.TypeAssert`, `errors.AsType`, `runtime.AddCleanup` and `math/rand/v2.N` support script types, including in saved bytecode. User-declared interface type arguments fail to compile. `unique.Make` and `weak.Make` support only built-in types and a few composites. Other types panic. |
| Defer inside a range-over-function loop | Fails with `ErrCompileDeferInRangeOverFunctionBody`. Move the defer to the enclosing function or put the body in a function literal. An iterator that recovers a body panic and calls the body again runs it, where Go would panic. |
| Loops in files targeting Go before 1.22 | Capturing a loop variable in a closure or taking its address fails with `ErrCompileSharedLoopVariables`. Use Go 1.22 or later loop semantics. |
| Script interfaces inside composite types | Reflection reports a user-declared interface field or element as `interface {}`, which every type implements. Use `reflect.TypeOf((*Namer)(nil)).Elem()` to inspect the interface itself. |
| Named structs in `encoding/xml` | `xml.Marshal` requires an `XMLName xml.Name` field. Without it, marshaling fails with `xml: unsupported type`. |
| Methods of generic types | A method supports up to 32 instantiations. Further instantiations fail compilation or return `ErrNoMethodBodyForReceiver` when called dynamically. |
| Method values through an embedded interface | `i.M` through a nil embedded interface panics when the method value is taken. Go panics when it is called. |
| `%p` of a script struct pointer | Prints as `%!p(...)`. Convert the pointer with `unsafe.Pointer` first. |
| `reflect.Type` method values | Taking `rt.Field` or `rt.FieldByName` as a function bypasses Pipit's field handling. Call the methods directly. |
| Native package variable writes | Mutate the host's variable. Restricted feature sets reject these operations. |
| `iter.Pull` / `iter.Pull2` | Use a parked goroutine. Call `stop` when abandoning iteration. |
| Finalisers and cleanups | Have time and queue limits, and may run later than in compiled Go. See [Finalisers and cleanups](#finalisers-and-cleanups). |
| Slice capacity | Growth can differ from the native compiler's allocation policy. |
| Recovered runtime errors | Message text follows Go, but `%T` reports Pipit's error type. |
| Deep recursion | Interpreted frames use more memory. Set `WithMaxCallDepth` for your workload. It includes closures, method values and callbacks from native code. |
| Very large functions | A statement with too many live values can fail with `ErrRegisterOverflow`. Split large statements or calls. Function types support at most 128 parameters and results combined, including the receiver. |

### Native method support

Passing a script value to a native `any` parameter preserves the value. Pipit also
makes script methods available to these standard-library operations.

| Operation | Supported methods |
|---|---|
| Printing with `fmt`, `log`, `testing`, `net/textproto`, and the `html/template` escapers | `String`, `Error`, `Format`, `GoString` |
| Scanning with `fmt` | `Scan` |
| Encoding with `encoding/json` and `encoding/json/v2` | `MarshalJSON`, `MarshalText`, including on nested values |
| Decoding with `encoding/json` and `encoding/json/v2` | `UnmarshalJSON`, `UnmarshalText` on the target itself |
| Encoding with `encoding/xml` | `MarshalText` |
| `log/slog` text and JSON handlers | `LogValue`, the printing methods above, `MarshalText`, `MarshalJSON` |

Other method-dependent operations can fail with `ErrUnsupportedInterfaceArgument`.
These include nested custom decoding, XML and gob marshaling methods, JSON streaming
methods, template execution, `net/rpc` receivers, `database/sql` values and scanners,
and `slog`'s `AppendText`. Method values bound by reflection outside the interpreter
also lack this support. Call the script method yourself and pass its result, or use
a native Go type.

Templates calling other script methods may report their own missing-field error.
Very large decoding targets may also be rejected with
`ErrUnsupportedInterfaceArgument`.

### Finalisers and cleanups

Scripts can use `runtime.SetFinalizer` and `runtime.AddCleanup`, with these limits.

- Each call has a 30-second timeout by default, capped by a shorter
  `WithCallbackTimeout`. Set `WithFinaliserTimeout` to change it.
- Panics are recovered and logged.
- Up to 4,096 calls can wait to run. Further calls are dropped while the queue is full.
- Finalisers may run later than in compiled Go, and using them increases allocations.
- Values created before a session's first use of finalisers may be ineligible.
  Pipit logs a warning when it cannot register one. `AddCleanup` rejects pointers
  into interpreter storage with a panic.

Closing the interpreter discards queued calls and prevents later ones from running.

### Named types and native interfaces

In standard native builds, named types remain distinct, including named arrays,
slices, maps, channels, functions and generic instantiations such as `Stack[int]`.
Self-referential types such as `type List []List` are supported too.
Recursive references through struct fields can appear as `any` in reflection.
Named array, slice, map, channel and function types require a supported Go
toolchain version. Unsupported versions fail compilation.

New named array, slice, map, channel and function types stay allocated for the life
of the process. The defaults allow 4,096 per interpreter and its clones, and 65,536
across the process. Recompiling or loading the same type does not use more of the
allowance. Exceeding a limit fails with `ErrNamedCompositeQuota` or
`ErrNamedCompositeLimit`. Configure these with `WithMaxNamedCompositeTypes` and
`WithNamedCompositeProcessLimit`. A new interpreter gets a fresh interpreter
allowance, but does not reset the process limit.

In `safe` and browser (`js/wasm`) builds, named array, slice, map, channel and
function types share their underlying type's runtime identity. Type switches,
assertions and formatting cannot distinguish them. Declaring methods on two such
types with the same underlying type fails compilation. Self-references use `any`.

Native parameters accept named values with a matching underlying type, such as a
named `[]string` passed to `strings.Join`. To call script methods from native code,
pass the value through a parameter of the required interface type, such as
`sort.Interface` or `http.Handler`.

Reflection within a script sees script methods, including promoted and generic
methods. Reflection inside native code does not see them, and native interface
assertions cannot discover them. The standard-library operations listed under
[Native method support](#native-method-support) are handled by Pipit.

Converting a function to a host's named function type, such as `http.HandlerFunc`,
gives it that type's native methods.

### Returned values

`Eval` returns ordinary integer expressions as `int`, and explicitly typed numeric
values as their declared types. Boxing into `any` preserves sized numeric types.
`float32` and `complex64` operations round back to single precision after each
operation. A value of a named type is returned with that type's runtime type, which
declares no methods of its own.

### Build constraints

The interpreter chooses a package's files as `go build` would for the host's operating
system and architecture, with the `interp` build tag set and cgo off. Guard a file with
`//go:build !interp` to leave it out of the interpreted build, or with
`//go:build interp` to use it only there. `WithBuildTags` adds further tags, and
`MatchFile` reports whether a file would be compiled.

### Embedded files

A `//go:embed` directive in a file the interpreter compiles fails with
`ErrEmbedUnsupported`. Keep the embedding in a file guarded by `//go:build !interp`,
and give the interpreted build a file guarded by `//go:build interp` that reads the
files at run time. Under the CLI, `PIPIT_SCRIPT_DIR` names the script's directory.

## Native and browser execution

Native functions may require facilities unavailable in a browser even if their
symbols are registered. Browser applications must provide their own worker
lifecycle and termination controls; Pipit's interpreter limits do not supply a
browser-wide memory limit.

## Bytecode compatibility

`pipit.BytecodeVersion` identifies the major format, `v13` in this release. Builds with
the same version can still have incompatible bytecode. Keep producer and consumer builds
and host symbols compatible, and recompile bytecode when it is rejected. A `safe` or
`js/wasm` build refuses saved bytecode that declares self-referential named array, slice,
map, channel or function
types, such as `type List []List`; compile such programs in that build instead.
Persisted bytecode is not a universal portable binary, and its schema marker does not
authenticate its contents. Recompile from source when upgrading across incompatible
versions.

## Call-stack inspection

`runtime.Caller`, `runtime.Callers`, `runtime.CallersFrames`, `runtime.FuncForPC`,
`runtime.Stack`, and `runtime/debug` stack helpers describe interpreted frames.
They report source positions, qualified Go function names, and synthetic program
counters that the stack helpers can decode.

Differences from native Go:

- Extra native runtime-error frames are absent, so fixed `Caller(n)` offsets can differ.
- A function value's `reflect.Value.Pointer()` does not map back through `FuncForPC`.
- Interpreted callbacks invoked by native functions see only their own frames.
- Programs loaded from bytecode have no source positions.

## Execution limits

See [execution security](security.md) for resource-limit coverage and native-call
limitations. See [concurrent use](api.md#concurrent-use) before evaluating source
from more than one goroutine.
