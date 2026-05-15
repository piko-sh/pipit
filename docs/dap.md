---
title: "Debugging with DAP"
description: "Connect a DAP client, set breakpoints, and inspect a running script."
section: "Reference"
order: 2
---

# Debugging with DAP

`pipit dap` runs a Debug Adapter Protocol server over stdin/stdout. An editor with
a configured DAP client can launch a script, set breakpoints, inspect variables,
and step through execution. Adapter registration depends on the editor.

## Connect a client

Build and install the CLI as described in [Getting started](getting-started.md#use-the-cli).
Configure your editor to launch the executable `pipit` with argument `dap`, then
send a launch request naming an absolute script path. Set a breakpoint on an
executable line and start the editor's debugger.

For a runnable Go example, see [Attach a debugger](api.md#attach-a-debugger).

The configurations below illustrate adapter setup. They have not been verified
end to end across editor versions; use the editor's current DAP configuration
instructions if its settings differ.

## Launch arguments

The DAP `launch` request body (the IDE wraps it in its Run/Debug config) accepts:

| Field          | Type     | Default          | Meaning                                                                                                              |
|----------------|----------|------------------|----------------------------------------------------------------------------------------------------------------------|
| `program`      | string   | *(required)*     | Path to a `.go` script file. Use an absolute path; a relative one resolves against the adapter's working directory.  |
| `args`         | string[] | `[]`             | Forwarded through `PIPIT_ARGC` and `PIPIT_ARG_<n>` environment values. |
| `allowNetwork` | bool     | `false`          | Allow GOPROXY fetches for third-party modules. Mirrors `pipit run --allow-network`.                                  |
| `goproxy`      | string   | proxy.golang.org | Override the GOPROXY base URL; the `GOPROXY` environment variable is not read. Mirrors `--goproxy`.                  |
| `cache`        | string   | `"off"`          | Module cache mode: `off \| on \| home \| gopath \| <path>`. Mirrors `--cache`.                                       |
| `entrypoint`   | string   | `"main"`         | Function to invoke once the program is compiled.                                                                     |
| `stopOnEntry`  | bool     | `false`          | Pause at the first instruction of the entrypoint so you can step from the start.                                        |

## Neovim (nvim-dap)

nvim-dap registers the adapter in Lua and attaches it to Go configurations.

```lua
local dap = require("dap")

dap.adapters.pipit = {
  type = "executable",
  command = "pipit",
  args = { "dap" },
}

dap.configurations.go = dap.configurations.go or {}
table.insert(dap.configurations.go, {
  type = "pipit",
  request = "launch",
  name = "pipit: current file",
  program = "${file}",
  allowNetwork = true,
  cache = "home",
})
```

## Helix

Helix declares the adapter in `languages.toml` under the Go language.

`~/.config/helix/languages.toml`:

```toml
[[language]]
name = "go"

[language.debugger]
name = "pipit"
transport = "stdio"
command = "pipit"
args = ["dap"]

[[language.debugger.templates]]
name = "pipit script"
request = "launch"
completion = [ { name = "program", completion = "filename" } ]
args = { program = "{0}" }
```

## GoLand and other generic DAP clients

Use a client that can launch an external adapter over stdio. Configure its command
as `pipit`, its arguments as `dap`, and its launch payload using the fields above.
Availability of generic DAP support and any required plugin depends on the editor
version. A typical payload is:

```json
{
  "request": "launch",
  "program": "/absolute/path/to/script.go",
  "stopOnEntry": true
}
```

## VS Code

VS Code requires an extension to register the `pipit` debug type and associate it
with `pipit dap`. A `launch.json` entry alone does not install an adapter.
This repository does not supply that extension or a verified VS Code setup.
The [VS Code debugger extension guide](https://code.visualstudio.com/api/extension-guides/debugger-extension)
describes adapter registration for extension authors.

## Capabilities

| Capability                    | Supported?                                                                          |
|-------------------------------|-------------------------------------------------------------------------------------|
| Launch                        | Yes                                                                                 |
| Attach                        | No - pipit owns the runtime; `launch` only                                          |
| Source breakpoints            | Yes, with `condition` and `hitCondition`                                            |
| Function breakpoints          | Yes, by runtime name (`main.(*T).M`, `main.run.func1`) or table name                |
| Exception breakpoints         | Yes: the `panic` filter pauses where `panic` is called, before deferred functions run |
| Data breakpoints              | No                                                                                  |
| Step over / in / out          | Yes, per thread; step-in enters a closure called from host code                     |
| Pause                         | Yes: the running program stops at its next instruction                              |
| Threads                       | Yes: one per interpreted goroutine, callback or method VM; a pause stops them all   |
| Stack trace                   | Yes, per thread                                                                     |
| Scopes / variables            | Yes: Locals, Closure (captured variables) and Globals, with expandable trees        |
| Set variable                  | Yes, for scalar and string locals                                                   |
| Evaluate (watch, hover, REPL) | Yes; hover evaluations may not call functions                                       |
| Exception info                | Yes, for a panic pause                                                              |
| Terminate / disconnect        | Yes                                                                                 |
| Step-back / reverse / restart | No                                                                                  |

## Inspect a paused script

Each interpreted goroutine or native-to-interpreted callback appears as a thread.
A pause stops the other threads at their next instruction. Continue resumes them
all; a step resumes execution until the selected thread reaches its target.
Breakpoints also work in package initialisers and `init` functions.

Watch expressions can use locals, captured variables, globals, and imports.
Evaluations have a two-second limit. Calls can mutate globals and heap data;
hover evaluations refuse function calls. Script stdout and stderr appear in the
client's debug console.

## Use conditional breakpoints

A `condition` must evaluate to a boolean. A failed evaluation pauses with an error
message. `hitCondition` counts hits for which the condition held:

| Form | Stops on |
|---|---|
| `N` or `== N` | The Nth hit |
| `>= N`, `> N`, `<= N`, `< N` | Hits satisfying the comparison |
| `% N` | Every Nth hit |

Function breakpoints use runtime names such as `main.run` or `main.(*T).M`.
They cannot stop on entry to a function that was inlined into its caller.

The `panic` exception filter is enabled by default. It pauses before deferred
functions and recovery run, including for panics that will be recovered. Disable
it in the client if those pauses are unwanted.

## Match source paths

Use the absolute path compiled by the adapter when setting breakpoints. For
imported modules, the editor must open that exact source file. Package registration
alone does not provide source for native functions.

A breakpoint on a line without code moves to the next executable line. Modules
loaded from bytecode without source positions cannot resolve source breakpoints;
the adapter reports them as unverified.

## Troubleshooting

To diagnose launch or breakpoint problems, enable the protocol log:

```sh
pipit dap --debug-log /tmp/pipit-dap.log
```

The log records requests and responses as JSON. Look for rejected requests and
their `ErrorResponse` messages.

For unresolved breakpoints, compare the request's `source.path` with the actual
compiled source path. `pipit symbols list` lists host registrations, not source paths.

## See also

- [CLI reference](cli.md) - resource limits, capability gating, and the `dap` command flags.
- [Getting started](getting-started.md) - install pipit and run your first script.
