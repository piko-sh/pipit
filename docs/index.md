---
title: "Documentation"
description: "Learn to embed Pipit, find task guides, and look up its behaviour."
section: "Get started"
order: 0
---

# Documentation

Pipit runs Go source. These docs cover embedding and operating the interpreter;
for the language itself, use the [Go documentation](https://go.dev/doc/).

## Tutorials

Start with [Getting started](getting-started.md): embed Pipit in a small Go
application, evaluate an expression, then try a script through the CLI.

## How-to guides

- [Embedding tasks](api.md): expose functions, select imports, reuse bytecode, and manage sessions.
- [Attach a debugger](api.md#attach-a-debugger): pause and inspect a script from Go.
- [Run isolated code](isolated-execution.md): provision experimental Linux workers and recover after a crash.
- [Grant file access](filesystem-isolation.md): use named roots with the isolated broker.
- [Verify a release](VERIFYING.md): check signatures and software bills of materials.
- [Run the self-hosting checks](selfhost.md): compare native and interpreted compiler output.

## Reference

- [Embedding reference](api-reference.md): operations, state, errors, and options.
- [CLI reference](cli.md): commands, flags, and defaults.
- [Debug Adapter Protocol](dap.md): launch fields, capabilities, and editor configuration examples.
- [Compatibility](compatibility.md): supported behaviour and known differences from Go.
- [Self-hosting report](selfhost-report.md): generated measurements and results.

## Explanation

- [Execution security](security.md): host access and what each execution mode means.
- [Architecture](architecture.md): how the compiler and runtime fit together.

For project policies, see [Contributing](../CONTRIBUTING.md) and
[Security policy](../SECURITY.md). Runnable scripts also live in [examples](../examples/).
