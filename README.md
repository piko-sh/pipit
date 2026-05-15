<div align="center">

# Pipit

**Run Go as a script. Embed the interpreter. Contain what it runs.**

Pipit compiles Go source to bytecode and executes it on a register machine. No build step. No cgo. One import path.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/pipit-dark.svg">
  <img src="docs/images/pipit-light.svg" alt="Pipit" width="420"/>
</picture>

[![Go Version](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-Alpha-orange.svg)]()

[![Library Coverage](https://img.shields.io/badge/Library_Coverage-80%25-yellowgreen?logo=go)](hack/test/coverage.sh)
[![CLI Coverage](https://img.shields.io/badge/CLI_Coverage-60%25-orange?logo=go)](hack/test/coverage.sh)

[Getting Started](#getting-started) |
[Documentation](docs/) |
[Examples](examples/) |
[Contributing](CONTRIBUTING.md)

</div>

---

> **Alpha:** Pipit is under active development. Expect breaking changes between releases. Go language coverage is
> incomplete and actively expanding, so validate your own programs rather than assuming full compatibility.

> **Standalone:** Pipit began as the Go interpreter embedded inside the [Piko](https://github.com/piko-sh/piko) web
> framework. It is now its own project, and the engine lives in this repository.

---

## About

Running Go means compiling Go. That is the right trade for a service, and the wrong one for a build script, a
user-supplied rule, a config hook, or a plugin someone drops into your product at runtime. The usual answer is to embed
Lua, Starlark, or JavaScript, and now your team writes two languages, marshals values across the boundary, and loses the
type system exactly where the untrusted code lives.

Pipit removes the second language. You run Go source directly, from the command line or from inside your own Go program.
Values cross the boundary keeping their static Go type: an `int` expression returns an `int`, not a `float64` that used
to be one.

> Brings Go's famously fast runtime down to the speed of every other scripting language. You're welcome.

A Pipit embedding is an ordinary Go program:

```go
func main() {
  interpreter := pipit.NewInterpreter(
    stdlib.WithStandardLibrary(),
    pipit.WithMaxExecutionTime(5*time.Second),
    pipit.WithCostBudget(50_000_000),
    pipit.WithCapabilityHook(myHook),
  )

  value, err := interpreter.Eval(ctx, `import "strings"; strings.ToUpper("hello")`)
  if err != nil {
    log.Fatal(err)
  }
  fmt.Println(value) // HELLO
}
```

---

## How it works

### Go source compiles to bytecode

Pipit parses and type-checks with `go/parser` and `go/types`, compiles the typed AST to its own instruction set, and
executes it on a register machine with typed register banks: separate banks for ints, floats, strings, bools and slices,
rather than one boxed `any` stack.

Because compilation and execution are separate steps, a program can be compiled once and run many times, or written to
disk and reloaded. `pipit compile` produces a bytecode artefact; `pipit bytecode` inspects, disassembles and runs it.

### You choose what scripts can import

A symbol table registers real Go functions and types against import paths, so interpreted code calls into your program
without a serialisation layer. Nothing is registered until you ask, and the Go standard library is one option like any
other:

```go
pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithSymbolProvider(myapp.Provider()))
```

That matters for size and for containment. The standard-library tables are around 17 MiB of binary, because every
registered function is reachable through `reflect` and so survives the linker; an embedding that needs none of it pays
none of it. Bundles (`core`, `codec`, `crypto`, `net`, `system`, `text`, `image`, `gotool`) take part of the library,
and a bundle you leave out is absent rather than filtered, so `os/exec` cannot be reached even through a mistake in an
import policy. `pipit symbols list` shows what the CLI has registered.

### Three execution tiers

The same engine runs under three different boundaries, and you choose per call site:

| Tier           | Constructor                | Boundary                                                                   |
|----------------|----------------------------|----------------------------------------------------------------------------|
| **Trusted**    | `NewInterpreter`           | None against the host. Full speed, native helpers, the default             |
| **Restricted** | `NewRestrictedInterpreter` | Cooperative in-process limits, a reviewed import set, fresh state per call |
| **Isolated**   | `NewIsolatedWorker`        | A separate, hash-verified worker process under native OS controls          |

The trusted tier pays nothing for facilities it does not use: no policy construction, no cost metering, no workers
unless you ask.

The isolated tier is Linux amd64/arm64; other platforms return `ErrIsolatedUnavailable` without executing anything. It
is **experimental**: the native controls are implemented, but the full security release gates are not complete.
Read [docs/security.md](docs/security.md) before treating any tier as a boundary against hostile input.

---

## Getting started

### Prerequisites

- Go 1.27 (Pipit checks the Go minor version at runtime)

### Installation

```bash
# Install the CLI
go install pipit.sh/pipit/cmd/pipit@latest

# Or add the interpreter to an existing Go project
go get pipit.sh/pipit

# Or build from source
git clone https://github.com/piko-sh/pipit
cd pipit
go build ./cmd/pipit   # produces ./pipit
```

Prebuilt binaries for each platform are on the [Releases](https://github.com/piko-sh/pipit/releases) page. `go install`
puts the binary in your `GOBIN` (usually `~/go/bin`); make sure it is on your `PATH`.

---

## Usage

### Running and inspecting scripts

```bash
pipit eval -e '1 + 2 * 3'                      # evaluate an expression
pipit run examples/hello/main.go               # run a script
pipit repl                                     # interactive session
pipit doc strings.HasPrefix                    # documentation for a registered symbol
pipit symbols list                             # what the interpreter can call

pipit compile -o /tmp/hello.pbc examples/hello/main.go
pipit bytecode disasm /tmp/hello.pbc           # read the instructions
pipit bytecode run /tmp/hello.pbc              # execute the artefact
```

`pipit run` applies resource limits by default: execution time, allocation size, output size and goroutine count.
Instruction-cost metering is opt-in with `--cost-budget`.

### Embedding the interpreter

```go
package main

import (
	"context"
	"fmt"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func main() {
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary())
	value, err := interpreter.Eval(context.Background(), `
        import "strings"
        strings.ToUpper("hello, pipit")
    `)
	if err != nil {
		panic(err)
	}
	fmt.Println(value)
}
```

`Interpreter.Clone` shares the symbol registry but not execution state, which is how a service evaluates on many
goroutines at once. `NewSession` keeps declarations across submissions for REPL-style use.

The engine lives under `internal/`. Everything outside the module reaches it through the curated surface at
`pipit.sh/pipit`, so importing Pipit pulls in only the interpreter's dependencies. The optional pieces are separate
modules under `sdk/`, which depend on the interpreter and never the other way round.

### Capability gating

Network access is on by default and nothing else is gated unless you ask. `--gate=SPEC` requires approval before a
script touches the network, the disk, `exec`, or the environment:

```bash
pipit run --gate=network,disk,exec,env untrusted.go
```

Gating keys off a curated allow-list of standard-library entry points. That is meaningful containment for the common
risky calls, not a kernel-level sandbox. For that, use the isolated tier.

### Debugging in your editor

`pipit debug` steps through a script at the terminal. `pipit dap` speaks
the [Debug Adapter Protocol](https://microsoft.github.io/debug-adapter-protocol/) over stdio, so editors with a DAP
client can drive breakpoints, stepping and variable inspection directly:

```bash
pipit debug examples/fibonacci/main.go
pipit dap
```

The [DAP guide](docs/dap.md) has example setups for GoLand, Neovim and Helix; VS Code also needs an extension that
this repository does not supply.

---

## Documentation

- [Getting started](docs/getting-started.md) - install Pipit and run your first expressions and scripts.
- [CLI reference](docs/cli.md) - every subcommand and flag, including resource limits and `--gate`.
- [Embedding Pipit](docs/api.md) - use the interpreter as a Go library.
- [Execution security](docs/security.md) - the trusted, restricted and isolated tiers, and what each one does and does
  not promise.
- [Compatibility and limits](docs/compatibility.md) - Go language support, registered packages, and bytecode
  compatibility.
- [Debugging with DAP](docs/dap.md) - drive the step debugger from your editor.
- [Architecture](docs/architecture.md) - package layering, the compile-to-run lifecycle, and the generated artefacts.

---

## Contributing

Contributions are welcome. Read the [Contributing Guide](CONTRIBUTING.md) for details on the Pipit development process,
how to submit pull requests, and coding standards.

---

## License

Distributed under the Apache 2.0 License. See [LICENSE](LICENSE) for more information.
