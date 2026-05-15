---
title: "Embedding tasks"
description: "Evaluate Go source, expose host functions, and manage imports, state, concurrency, and limits."
section: "How-to guides"
order: 1
---

# Embedding tasks

Pipit evaluates Go source inside your application. You choose the host functions
and packages scripts can call. For module setup and a complete first program,
start with [Getting started](getting-started.md#embed-pipit).

Each Go block below is a complete program. In the application workspace created by
the tutorial, replace `main.go` with the example and run `go run .`. Expected
values are shown beside the print calls.

## Expose a host function

Register functions under an import path with `NewInterpreterWithSymbols`. This
example exposes one function without registering the standard library:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"reflect"

	"pipit.sh/pipit"
)

func main() {
	symbols := pipit.SymbolExports{
		"example.com/host": {
			"Greet": reflect.ValueOf(func(name string) string {
				return "Hello, " + name
			}),
		},
	}
	interp := pipit.NewInterpreterWithSymbols(symbols)
	value, err := interp.Eval(context.Background(), `
        import "example.com/host"
        host.Greet("Pipit")
    `)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(value) // Hello, Pipit
}
```

Host functions execute native Go with access to your process. Interpreter
resource limits cannot forcibly interrupt a native function already running.
Review the operations and objects you expose; see [execution security](security.md).

For reusable registries, implement `Exports() pipit.SymbolExports` and pass the
provider to `WithSymbolProvider`. Later providers override earlier symbols with
the same package and name. Symbols passed to `NewInterpreterWithSymbols` override
all providers supplied through options.

For larger packages, the [webserver example](../examples/README.md#webserver)
shows symbol generation with [`pipit extract`](cli.md#pipit-extract). Generated
tables are committed, so running that example does not require the generator.

## Choose imports

`NewInterpreter()` registers no host packages. Register only the packages your
scripts need. This example registers two bundles:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib/codec"
	"pipit.sh/pipit/sdk/stdlib/core"
)

func main() {
	interp := pipit.NewInterpreter(core.WithCore(), codec.WithCodec())
	value, err := interp.Eval(context.Background(), `
        import "strings"
        strings.ToUpper("hello")
    `)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(value) // HELLO
}
```

See the [bundle reference](api-reference.md#imports) for the available groups.

## Compile and reuse a package

This program compiles two source files, saves the package in `build/program.pbc`,
reloads it, and runs its entrypoint twice. It prints `42` twice:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"pipit.sh/pipit"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	sources := map[string]string{
		"main.go":   "package main; func result() int { return answer() }",
		"answer.go": "package main; func answer() int { return 42 }",
	}
	store, err := pipit.NewDirectoryBytecodeStore("./build")
	if err != nil {
		return err
	}
	interp := pipit.NewInterpreter(pipit.WithBytecodeStore(store))
	program, err := interp.CompileFileSet(ctx, sources)
	if err != nil {
		return err
	}
	if err := pipit.SaveCompiledToFile(ctx, interp, "./build/program.pbc", program); err != nil {
		return err
	}
	loaded, err := pipit.LoadCompiledFromFile(ctx, interp, "./build/program.pbc")
	if err != nil {
		return err
	}
	for range 2 {
		value, err := interp.ExecuteEntrypoint(ctx, loaded, "result")
		if err != nil {
			return err
		}
		fmt.Println(value) // 42
	}

	return nil
}
```

The store must be rooted at the file's parent directory. Programs using host
packages need compatible registrations when loaded. The format is versioned;
see [bytecode compatibility](compatibility.md#bytecode-compatibility).

## Retain state between submissions

An interpreter owns mutable execution state, including package globals. Reuse it
for sequential work and call `Reset` to clear that state. Use a `Session` when
later snippets need earlier imports and declarations. Separate `Eval` calls do
not retain those declarations.

This program retains an import and variable across three submissions and prints
`PIPIT`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	interp := pipit.NewInterpreter(stdlib.WithStandardLibrary())
	session := interp.NewSession()
	for _, source := range []string{
		`import "strings"`,
		`name := "pipit"`,
		`strings.ToUpper(name)`,
	} {
		value, err := session.Submit(ctx, source)
		if err != nil {
			return err
		}
		if value != nil {
			fmt.Println(value)
		}
	}
	session.Reset()

	return nil
}
```

`Inspect` returns the session's known declarations and imports. A failed
submission does not guarantee rollback of mutations to existing runtime values.

## Concurrent use

An interpreter or session must not be evaluated concurrently. Configure a base
interpreter, then clone it for each worker or request:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	// Configure base before starting concurrent work.
	base := pipit.NewInterpreter(stdlib.WithStandardLibrary())

	// Each worker uses its own clone.
	worker := base.Clone()
	value, err := worker.Eval(ctx, "1 + 1")
	if err != nil {
		return err
	}
	fmt.Println(value) // 2

	return nil
}
```

Clones have fresh execution state and share the symbol registry and configuration.
Protect any objects shared by registered host functions with the usual Go
concurrency controls. Configure registrations before cloning.

## Set limits and logging

Pass `WithLogger(logger)` to receive interpreter diagnostics. Without it, records
go to `slog.Default()`. This program sets a one-second timeout and a cost budget.
Any diagnostics go to stderr; this short program produces none and prints `2` on
stdout:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"pipit.sh/pipit"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	interp := pipit.NewInterpreter(
		pipit.WithLogger(logger),
		pipit.WithMaxExecutionTime(time.Second),
		pipit.WithCostBudget(1000),
	)

	value, err := interp.Eval(ctx, "1 + 1")
	if err != nil {
		return err
	}
	fmt.Println(value) // 2

	return nil
}
```

## Restrict imports and capabilities

The public restricted constructor requires a symbol provider. In the workspace
from [Getting started](getting-started.md#embed-pipit), save this as `main.go` and
run `go run .`. It prints `7`:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func main() {
	interp, err := pipit.NewRestrictedInterpreter(pipit.RestrictedConfig{
		Imports: []string{"math"},
	}, stdlib.Providers()...)
	if err != nil {
		log.Fatal(err)
	}
	result, err := interp.Eval(context.Background(), `
        import "math"
        math.Sqrt(49)
    `)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(result.Value)) // 7
}
```

## Control native calls with a capability hook

A capability hook lets the host allow or deny native calls. This example registers
two host functions, allows `Greet`, and rejects `Farewell`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"runtime"

	"pipit.sh/pipit"
)

type selectedFunction struct {
	pipit.DenyCapabilityHook
	name string
}

func (hook selectedFunction) CheckFunctionCall(_ context.Context, _, name string, _ []reflect.Value) error {
	if name == hook.name {
		return nil
	}
	return pipit.ErrCapabilityDenied
}

func greet(name string) string    { return "Hello, " + name }
func farewell(name string) string { return "Goodbye, " + name }

func main() {
	allowed := reflect.ValueOf(greet)
	hook := selectedFunction{name: runtime.FuncForPC(allowed.Pointer()).Name()}
	interp := pipit.NewInterpreterWithSymbols(pipit.SymbolExports{
		"example.com/host": {
			"Greet":    allowed,
			"Farewell": reflect.ValueOf(farewell),
		},
	}, pipit.WithCapabilityHook(hook))
	value, err := interp.Eval(context.Background(), `import "example.com/host"
host.Greet("Pipit")`)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(value) // Hello, Pipit
	_, err = interp.Eval(context.Background(), `import "example.com/host"
host.Farewell("Pipit")`)
	fmt.Println(errors.Is(err, pipit.ErrCapabilityDenied)) // true
}
```

The hook receives the native function's name, which can differ from its script
import name. The example gets that name from the registered Go function.
`errors.Is` checks whether the second call failed with `ErrCapabilityDenied`.

`DenyCapabilityHook` denies the typed file, network, process, and environment
checks. Its `CheckFunctionCall` method allows calls by default, so this example
overrides that method to select one function. `PermissiveCapabilityHook` allows
all checks by default. Embed it only when you want to deny specific operations.

Use `WithCapabilityHook` for ordinary embedding, or set `RestrictedConfig.Hook`
for restricted execution. Typed checks such as `CheckFileOpen` can inspect a path
or arguments, but only cover operations the interpreter recognises. Review the
functions you register: work inside a native function does not become a separate
script call. Hooks do not provide process isolation.

## Attach a debugger

Run evaluation in a goroutine so the host can respond while the script is paused.
This program stops before `value++`, reads a local variable, evaluates
`value * 2`, then steps over the increment and continues:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"pipit.sh/pipit"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	debugger := pipit.NewDebugger()

	interp := pipit.NewInterpreter(pipit.WithDebugger(debugger), pipit.WithDebugInfo())
	program, err := interp.CompileFileSet(ctx, map[string]string{
		"script.go": `package main
func answer() int {
    value := 21
    value++
    return value
}`,
	})
	if err != nil {
		return err
	}
	breakpoint := debugger.SetBreakpoint("script.go", 4)
	if !breakpoint.Verified {
		return fmt.Errorf("breakpoint: %s", breakpoint.Message)
	}
	done := make(chan error, 1)
	go func() {
		_, err := interp.ExecuteEntrypoint(ctx, program, "answer")
		done <- err
	}()
	defer func() { cancel(); <-done }()
	event, err := debugger.WaitForPause(ctx)
	if err != nil {
		return err
	}
	frames, err := debugger.StackTrace(event.ThreadID)
	if err != nil {
		return err
	}
	fmt.Println("paused in", frames[0].Function)
	locals, err := debugger.Variables(event.ThreadID, 0, pipit.ScopeLocals)
	if err != nil {
		return err
	}
	for _, local := range locals {
		if local.Name == "value" {
			fmt.Println("value:", local.Value)
		}
	}
	result, err := debugger.Evaluate(ctx, event.ThreadID, 0, "value * 2", pipit.EvalOptions{})
	if err != nil {
		return err
	}
	fmt.Println("value * 2:", result.Value)
	if err := debugger.StepOver(event.ThreadID); err != nil {
		return err
	}
	event, err = debugger.WaitForPause(ctx)
	if err != nil {
		return err
	}
	result, err = debugger.Evaluate(ctx, event.ThreadID, 0, "value", pipit.EvalOptions{})
	if err != nil {
		return err
	}
	fmt.Println("after step:", result.Value)

	if err := debugger.Continue(); err != nil {
		return err
	}
	for {
		event, err := debugger.WaitForEvent(ctx)
		if err != nil {
			return err
		}
		if event.Kind == pipit.DebugEventExited {
			if event.Err != nil {
				return event.Err
			}
			fmt.Println("finished")
			return nil
		}
	}
}
```

Expected output:

```text
paused in main.answer
value: 21
value * 2: 42
after step: 22
finished
```

The context limits the whole example to five seconds. The host waits for the
evaluation goroutine before returning, including when inspection fails.
`CompileFileSet` gives the source a stable filename for breakpoints, and
`WithDebugInfo` keeps the information needed to inspect variables.

Most debugger methods return errors. `WaitForPause` returns `ErrDebugExited` if the
script ends before it pauses; `Eval` does not report its end, so use the other run
methods with a debugger. Inspection and stepping require a paused script;
otherwise they return `ErrDebugNotPaused`. `WaitForEvent` also reports thread
starts, thread exits, and the final execution result. See the
[debugger reference](api-reference.md#debugger-operations).

See [debugging.go](../debugging.go) for the public API and [the DAP guide](dap.md)
for editor usage and supported behaviour.

See the [option and error reference](api-reference.md) for configuration details.

## Related guides

- [Compatibility](compatibility.md): language and native interoperability limitations.
- [Execution security](security.md): restricted embedding and experimental isolation.
- [CLI reference](cli.md): commands for running and inspecting scripts.
