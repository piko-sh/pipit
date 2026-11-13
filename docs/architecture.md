---
title: "Architecture"
description: "How Pipit compiles Go source, runs bytecode, and connects to host packages."
section: "Explanation"
order: 1
---

# Architecture

Pipit compiles Go source into bytecode, then executes it in a virtual machine
(VM). The library and CLI use the same engine. Embedders use the public
`pipit.sh/pipit` package.

## From source to execution

Pipit parses and type-checks source, compiles each function, then optimises and
verifies the bytecode before running it.

```text
Go source -> parse and type-check -> compile -> optimise and verify -> execute
```

You can compile once and execute the result repeatedly. You can also save compiled
programs and load them later, provided the bytecode version and host symbols are
compatible. See [bytecode compatibility](compatibility.md#bytecode-compatibility).

The VM stores working values in registers. On amd64 and arm64, an assembly loop
runs bytecode instructions. Other platforms use a Go loop. The `safe` build tag
also selects the Go loop and disables unsafe fast paths.

## Runtime types

Scripts can declare types, attach methods and pass values to native Go functions.
Pipit preserves named-type identity, with limitations for reflection, native
interfaces, and the `safe` and browser builds. See
[named types and native interfaces](compatibility.md#named-types-and-native-interfaces)
before relying on these features at the host boundary.

Values returned to the host remain valid after execution. Pipit reuses temporary
working memory only when no retained value can reach it. See
[licensed reuse](licensed-reuse.md) for the memory guarantees and relevant options.

## Why the standard library is separate

An application chooses which host packages its scripts can import. Registering a
package keeps its native code in the application binary, even if no script uses it.

The standard library therefore lives in a separate module, `sdk/stdlib`.
Applications can register the whole library, select bundles, or supply only their
own functions. See [choosing imports](api.md#choose-imports) and
[exposing host functions](api.md#expose-a-host-function).

## Reading bytecode

The disassembler shows the instructions Pipit executes. Operation names start with
an action, followed by the value types and any modifiers. For example,
`LESS_THAN_INT64_THEN_JUMP_IF_FALSE` combines a comparison with a conditional jump.
A prefix such as `2:` identifies the instruction tier.

## Build configurations

| Tag or platform | Effect |
|---|---|
| Default on amd64 and arm64 | Assembly dispatch and unsafe fast paths |
| Other native platforms | Go dispatch |
| `safe`, also selected for `js/wasm` | Go dispatch with unsafe fast paths disabled. Named array, slice, map, channel and function types share their underlying type's runtime type. |

The `safe` tag changes how the engine runs. See
[execution security](security.md) when choosing how to run untrusted scripts.

## Related documentation

- [Embedding reference](api-reference.md) covers public operations and configuration.
- [Run self-hosting checks](selfhost.md) runs Pipit's compiler under its interpreter.
- [Compatibility and limits](compatibility.md) lists differences from native Go.
