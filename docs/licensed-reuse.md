---
title: "Licensed reuse"
description: "What Pipit's memory reuse guarantees and how embedders can configure it."
section: "Explanation"
order: 3
---

# Licensed reuse

Pipit reuses temporary working memory, called an arena, to reduce allocations
between executions. Values you keep after execution remain valid.

## What Pipit guarantees

A value that leaves an execution is never overwritten by a later one. That covers
results, errors, panic values, and values kept by host functions, variables,
channels or goroutines. It also covers function values handed to the host.

Storage these values still reference is left for Go's garbage collector instead
of being reused. It stays allocated as long as something refers to it.

## When storage is not reused

Common causes include returning large values, passing arguments or callbacks to
native code that may keep them, and executions that end with cancellation, a
budget limit or a panic. In the `safe` and browser (`js/wasm`) builds, a value that
leaves an execution prevents reuse of that execution's entire arena.

Less reuse means more allocations and garbage-collector work. Long-lived callbacks
and programs that use `runtime.SetFinalizer` or `runtime.AddCleanup` can also
increase memory use. If Pipit cannot establish that storage can be reused safely,
it allocates fresh storage.

## What an embedder can do

- Set `WithMaxArenaSizeBytes` to bound arena memory. Exceeding it fails execution
  with `ErrArenaBudgetExceeded`. This does not bound every Go allocation.
- Use `DeclareNativeBorrowClass` to declare how your native functions use their
  arguments. Declarations must be accurate. Keeping an argument after declaring
  that you would not keep it can let a later execution overwrite its storage.
- Use `DeclareTopLevelCalls` only when the host calls script functions outside
  native callbacks and their arguments cannot reach values of a running program.
  See the [declaration requirements](api-reference.md#licensed-reuse).
- Replace the interpreter after `ErrLockAbandoned`. A stopped execution has left
  a lock held, and later evaluations will fail immediately.

The abandoned-lock check covers locks taken directly by the script. It misses
locks taken through `sync.Locker` or inside host code, and `RWMutex` locks in the
`safe` and browser builds.

See the [embedding reference](api-reference.md#licensed-reuse) for borrow classes,
options and errors, and [execution security](security.md) for execution limits.
