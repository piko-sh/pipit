---
title: "Compatibility and limits"
description: "Go language support, known behavioural differences, and bytecode compatibility."
section: "Reference"
order: 1
---

# Compatibility and limits

Pipit has its own compiler and runtime. It type-checks source as Go 1.27 and
supports many Go language features, but does not claim complete compatibility. Check your programs when adopting or
upgrading it, especially code that relies on reflection or native interfaces.

## Language and packages

The [language tests](../tests/integration/language) and
[parity corpus](../tests/integration/snippets) exercise functions, methods,
interfaces, generics, closures, slices, maps, channels, goroutines, defer, and
panic/recover. The corpus compares interpreted results with native Go. Passing
these cases does not imply every combination is supported. Restricted execution
also deliberately disables some features; see [execution security](security.md).

Host package registration makes native symbols available for imports. It does not
establish full compatibility with every program that uses those packages.
`stdlib.WithStandardLibrary()` registers the bundled set; smaller bundles and
custom providers are described in [Embedding](api.md#choose-imports).

The standard-library registry excludes `syscall`, `plugin`, `runtime/cgo`,
`runtime/race`, `runtime/trace`, and `runtime/coverage`. The compiler handles
`unsafe` separately. `log/syslog` is available only on platforms that provide it.
Generic functions that need native instantiation use wrappers, such as
`iter.Pull` and `iter.Pull2`. `crypto/hkdf` and `crypto/pbkdf2` are registered
instantiated at `hash.Hash`.

Use `pipit symbols list` to inspect the CLI's actual registrations. Embedders can
inspect `stdlib.Exports()`; `pipit.StandardLibraryPaths()` provides package names
without linking the symbol tables.

## Known limitations

| Feature | Current behaviour and alternatives |
|---|---|
| `//go:embed` | Rejected. Read data at run time with explicitly granted filesystem access. |
| Named array, slice, map, channel, or function types | Runtime identity is shared with the underlying type; interface dispatch and formatting through `any` can select or lose methods. See below. |
| `reflect.Type` method values | Taking `rt.Field` or `rt.FieldByName` as a function bypasses source-field interception. Call the methods directly. |
| Native package variable writes | Assignments can mutate the host's variable. Restricted feature sets reject these unsafe operations. |
| `iter.Pull` / `iter.Pull2` | Use a parked goroutine. Call `stop` when abandoning iteration. |
| `runtime.SetFinalizer` | Registers no finalizer for interpreted values. |
| Slice capacity | Growth can differ from the native compiler's allocation policy. |
| Recovered runtime errors | Message text follows Go, but `%T` reports Pipit's error type. |
| Deep recursion | Interpreted frames use more memory. Set call-depth limits for your workload. |

### Named types and native interfaces

Named structs and named basic types have distinct runtime identities. Named types
with array, slice, map, channel, or function underlying types do not. Direct calls
where the compiler knows the type can work, while operations on a runtime interface
value may not.

For example, `sort.Sort(ByAge(people))` can reach the declared methods through a
native adapter. A method call on an interface stored in script code can instead
reach the last declared type with that underlying runtime type. Similarly, storing
a value in `any` before formatting can lose its `String` method.

### Returned values

`Eval` returns ordinary integer expressions as `int`, and explicitly typed numeric
values as their declared types. Boxing into `any` preserves sized numeric types.
`float32` and `complex64` operations round back to single precision after each
operation. The named-type limitations above still apply.

## Native and browser execution

Native functions may require facilities unavailable in a browser even if their
symbols are registered. The WASM build uses the safe Go dispatcher. Browser
applications must provide their own worker lifecycle and termination controls;
Pipit's interpreter limits do not supply a browser-wide memory limit.

## Bytecode compatibility

`pipit.BytecodeVersion` identifies the schema. Keep producer and consumer versions
and host symbols compatible. Persisted bytecode is not a universal portable binary
and its schema marker does not authenticate its contents. Recompile from source
when upgrading across incompatible versions.

## Call-stack inspection

`runtime.Caller`, `runtime.Callers`, `runtime.CallersFrames`, `runtime.FuncForPC`,
`runtime.Stack`, and `runtime/debug` stack helpers describe interpreted frames.
They report source positions, qualified Go function names, and synthetic program
counters that the stack helpers can decode.

Differences from native Go:

- Extra native runtime-error frames are absent, so fixed `Caller(n)` offsets can differ.
- A function value's `reflect.Value.Pointer()` does not map back through `FuncForPC`.
- Interpreted callbacks invoked by native functions see only their own frames.
- Stack dumps use Pipit's goroutine numbers.
- The safe build returns nil for `FuncForPC` and `Frame.Func`.
- Programs loaded from bytecode have no source positions.

## Execution limits

See [execution security](security.md) for resource-limit coverage and native-call
limitations. See [concurrent use](api.md#concurrent-use) before evaluating source
from more than one goroutine.
