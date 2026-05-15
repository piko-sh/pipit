---
title: "CLI reference"
description: "Commands and flags for running, inspecting, and debugging Go scripts."
section: "Reference"
order: 1
---

# CLI reference

Use `pipit <command> --help` to inspect a command's flags; `repl`, `debug`, and
`doc` take none. Global flags go before the command; command flags go before
positional arguments.

| Task | Commands |
|---|---|
| Run source | [run](#pipit-run-filedirectory), [eval](#pipit-eval--e-code--pipit-eval--) |
| Work interactively | [repl](#pipit-repl) |
| Run tests | [test](#pipit-test-flags-filedirectory) |
| Save or inspect bytecode | [compile](#pipit-compile--o-outpbc-filedirectory), [bytecode](#pipit-bytecode-subcommand-filepbc) |
| Inspect host packages | [symbols](#pipit-symbols), [doc](#pipit-doc-pkgsymbol) |
| Generate symbol tables | [extract](#pipit-extract) |
| Format source | [fmt](#pipit-fmt--w--d-filedir---) |
| Debug | [debug](#pipit-debug-file), [dap](#pipit-dap) |
| Inspect module records | [module](#pipit-module) |
| Inspect versions | [version](#pipit-version) |
| Experimental process isolation | [isolated](#pipit-isolated) |

Ordinary commands execute in the host process. See [execution security](security.md)
for the scope of resource limits and capability gates.

## Global flags

`--log-level` sets the minimum level for Pipit's diagnostics on stderr:
`debug`, `info`, `warn` (default), or `error`. Script output is unaffected.

```sh
pipit --log-level debug run script.go
```

## `pipit run <file|directory>`

Compile and execute Go source. With a single `.go` file it invokes the
named entrypoint (default `main`); with a directory it compiles every
`.go` file in that directory as one package and runs the entrypoint.

Arguments after `--` reach the script through `os.Args`, with the script target
as the first element. `flag.Parse()` reads those arguments as it does in Go.
Pipit also exposes them as `PIPIT_ARGC` and `PIPIT_ARG_<n>` environment variables.

`PIPIT_SCRIPT_DIR` holds the absolute script or package directory, so scripts can
find nearby files. The script inherits the host environment with these variables
added or replaced. `os.Setenv` and `os.Unsetenv` change only the script's view.

### Resource limits

These flags cap what a script may consume. Defaults differ by flag. `0` selects built-in limits where shown; for
`--cost-budget`, it disables metering.

| Flag | Unit | Default | Meaning of zero |
|---|---|---|---|
| `--timeout` | Duration per evaluation | `15m` | Use 15 minutes; disable the process exit backstop |
| `--max-alloc` | Elements in one allocation | 1,073,741,824 | Use the default |
| `--max-goroutines` | Concurrent goroutines | 10,000 | Use the default |
| `--max-output` | Bytes from `print` / `println` | 268,435,456 (256 MiB) | Use the default |
| `--cost-budget` | Instruction cost | No budget | Disable metering |
| `--max-call-depth` | Interpreted calls | 1,000,000 | Use the library default of 10,000 |

These limits cover interpreter operations. `--timeout` interrupts interpreted
loops, channel operations, `select`, and `time.Sleep`. A native call already in
progress, such as a blocking read, cannot be interrupted. If it continues for
more than two seconds past the deadline, Pipit exits with status 124.

A single large native allocation can exceed `--max-alloc`. See
[execution security](security.md) for the scope of these limits.

### Exit status

`pipit run` and `pipit eval` use these exit statuses:

| Status | Meaning                                                                                                                                                                                                                                                     |
|--------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 0      | The program returned from `main` (goroutines still running are abandoned, as Go does).                                                                                                                                                                      |
| 1      | The source failed to parse, type-check or compile, an import was refused, a gated capability was denied, or a resource limit ended the run.                                                                                                                 |
| 2      | The program panicked with no `recover`, a goroutine panicked, the call stack overflowed, or every goroutine was parked on channels the program made (`fatal error: all goroutines are asleep - deadlock!`). The panic report on stderr follows Go's layout. |
| 3      | pipit itself hit an internal invariant failure. Report this as a Pipit bug.                                                                                                                                                   |
| 124    | The `--timeout` deadline passed, or the first Ctrl-C or SIGTERM cancelled the run.                                                                                                                                                                          |
| 130    | A second Ctrl-C or SIGTERM forced the process to exit.                                                                                                                                                                                                      |

A script that calls `os.Exit(n)` exits with status `n`.

### Execution flags

- `--entrypoint main` - function name to invoke.
- `--emit-bytecode DIR` - create a bytecode store in DIR. `pipit run` does not
  write bytecode there yet; use [`pipit compile`](#pipit-compile--o-outpbc-filedirectory)
  to save it.

### Dispatcher selection

Any non-empty `PIPIT_GO_DISPATCH` value selects the portable Go instruction loop
for `pipit run`, `pipit eval`, `pipit test`, `pipit compile`, and `pipit bytecode run`. Use it to investigate failures that occur with
assembly dispatch. A difference between the two runs can help locate the fault.

### Local packages

A script beside a `go.mod` (or below one, as `go run` would find it) may
import packages of its own module: the `module` path the file declares
and any module a `replace` directive points at a directory (`replace
example.com/lib => ../lib`). Those packages are compiled from their
directories, honouring build constraints, and only the packages the
program reaches are loaded. `pipit run` accepts the script file or its
directory. A gated run (`--gate`) reads local packages from the approved
source snapshot, so a `replace` outside the script's directory is refused
there.

### Third-party module flags

Scripts that import a non-stdlib package (an import path containing a
`.` that no local module covers) are resolved through GOPROXY before
running, including such imports found inside local packages:

- `--allow-network` - allow fetching third-party modules from GOPROXY on
  demand (default `false`; without it, unknown imports fail).
- `--goproxy URL` - override the GOPROXY base URL (default
  `https://proxy.golang.org`).
- `--cache MODE` - module cache location: `off` (default) | `on` | `home`
  | `gopath` | an explicit filesystem path such as `./cache`, `/abs`,
  `~/foo`.

### Capability gating

By default nothing is gated: the script runs under the resource limits
above with native host access. Pass `--gate` to require approval for
selected sensitive entry points. This is not a security sandbox.

- `--gate SPEC` - gate the listed capability groups, comma-separated:
  `all`, `network`, `disk`, `exec`, `env` (for example
  `--gate=network,disk`). Aliases such as `net`, `fs`, and `process` are also
  accepted. Empty (the default) leaves everything ungated.
- `--autodeny` - with `--gate`, deny an unapproved capability outright
  rather than prompting, even on a terminal.
- `--lockfile PATH` - explicit capability approval file. The default is
  a per-script file under the user's configuration directory at
  `pipit/approvals`, outside the source directory.

On an interactive terminal, each unapproved claim prompts:

```
pipit: app.go wants capability: network.dial (tcp:proxy.golang.org:443)
[y/n]
```

Answer `y` or `yes` to approve and save the claim; other answers deny it.
Without an interactive terminal, or with `--autodeny`, unapproved claims are
rejected. Obtain approvals interactively before using them in a batch run.

Approvals are bound to the local source and invocation, including script path,
entrypoint, ordered arguments, selected gates, and host context. Relevant changes
require approval again. Gated execution uses the captured local source bytes.
Remote dependencies are outside that source snapshot. Keep approval files under
host control; a script with ambient host access can still attack same-user storage.

Gates cover selected native entry points. Filesystem gates reject `os.DirFS`,
`os.OpenRoot`, and `os.OpenInRoot`; process gates reject `os.Exit`. Unlisted
functions and returned native objects can bypass this model. See
[execution security](security.md).

## `pipit test [flags] <file|directory>`

Run a package's tests the way `go test` does. The directory's `.go`
files, `_test.go` files included, compile as one package (`package main`
or any other name); every top-level `func TestXxx(t *testing.T)` in a
`_test.go` file is a test. The tests run through the host's `testing`
package, so `t.Run`, `t.Parallel`, `t.Log`, `t.Skip`, `t.Cleanup` and
`testing.Short()` behave as in Go, and the process exits with the
suite's status: `0` when every test passes, `1` otherwise. Package
`init` functions and variable initialisers run once before the first
test.

- `-run REGEXP` - run only the tests whose name matches, subtests
  included, as `go test -run` does.
- `-v` - log every test as it runs.
- `-short` - make `testing.Short()` report true.

The resource-limit and third-party module flags of `pipit run` apply
unchanged, and a package that imports local or remote modules is
resolved as `pipit run` would. `--gate`, `--autodeny`, and `--lockfile` are
accepted but not applied, so tests run ungated. A file target compiles only
that file. `PIPIT_SCRIPT_DIR` names the package
directory, so tests can read fixtures beside them. Not supported:
external test packages (`package x_test`), `TestMain`, benchmarks,
examples and fuzz targets.

## `pipit eval -e "<code>"` / `pipit eval -`

One-shot evaluation. `-e` takes an expression or statement; the bare
`-` form reads source from stdin. Use it in shell pipelines:

```sh
printf 'import "fmt"\nfmt.Println("hi")\n' | pipit eval --print=false -
```

By default the result of the final expression is printed; pass
`--print=false` to silence. `--timeout` (default `1m`) caps evaluation
time.

## `pipit repl`

Start an interactive session that retains imports and declarations. When stdin
is a terminal, Pipit opens a terminal interface:

- Enter submits; Alt+Enter inserts a newline (multi-line input).
- Unbalanced `{`, `(`, `[` automatically continue to a new line.
- `Up` / `Down` walk through saved history.

In the terminal UI, meta commands begin with `:`:

- `:help` - list the meta commands.
- `:reset` - clear session state (declarations and imports).
- `:inspect` - show the current session's declarations and imports.
- `:code` - switch to the source transcript view (the default).
- `:bytecode` - switch to the compiled bytecode view.
- `:load <file>` - submit a Go source file to the session.
- `:symbols` - summarise the registered host packages.
- `:exit`, `:quit`, `:q` - quit (Ctrl-C and Ctrl-D also work).

Pipit saves history to `$XDG_STATE_HOME/pipit/history`, or
`~/.local/state/pipit/history` when `XDG_STATE_HOME` is unset. With piped input, the
REPL uses line mode: each non-empty line is a separate submission, and closing
stdin exits. Terminal UI meta commands are not available in that mode.

## `pipit compile -o <out.pbc> <file|directory>`

Compile a source file or package to a bytecode artefact:

```sh
pipit compile -o hello.pbc hello.go
pipit bytecode run hello.pbc
```

Keep the producer and consumer versions and host symbols compatible; see
[bytecode compatibility](compatibility.md#bytecode-compatibility).

## `pipit bytecode <subcommand> <file.pbc>`

- `inspect` - print schema version, function count, function names.
- `disasm` (or `disassemble`) - pretty-printed disassembly. Flags: `--func <name>`,
  `--no-colour`.
- `run` - execute a precompiled artefact. Flag: `--entrypoint <name>`
  (default `main`).

## `pipit symbols`

- `list [--filter substr]` - registered host packages and symbol counts.
- `show <pkg>` - full symbol listing for one package, with signatures.

## `pipit extract`

Generate the symbol tables that let scripts import native Go packages. A YAML
manifest, `pipit-symbols.yaml` by default, lists the packages; the generated
`gen_*.go` files are committed and registered with `cli.WithSymbols` or
`WithSymbolProvider`. The [webserver example](../examples/README.md#webserver)
shows the whole flow.

- `generate` - write the tables for the manifest. Flags: `--manifest`,
  `--output`, `--package`, `--select <prefixes>`, `--emit all|symbols|types|index`,
  `--list`, `--dry-run`.
- `discover` - print the packages the project's Go files import that are not
  yet provided. The standard library and the project's own module are left out.
  Flags: `--root`, `--output list|yaml|json`, `--ignore`.
- `init` - write a new manifest from `discover`. Flags: `--root`, `--output`,
  `--package`, `--dir`, `--force`.
- `check` - exit with status 1 when the manifest is missing a package the
  project imports; use it in CI. Flags: `--manifest`, `--root`.

The same command is available to other tools through the `pipit.sh/pipit/sdk/extract`
module.

## `pipit doc <pkg>.<symbol>`

Show the registered signature of a host symbol. Descriptive documentation bodies
are placeholders.

## `pipit fmt [-w] [-d] <file|dir...> | -`

`gofmt`-compatible formatter. It writes formatted source to stdout by
default, and reads stdin when given no arguments. `-w` overwrites each file in
place. `-d` prints the paths that differ and exits with status 2 when any file
differs, leaving the files untouched; a file that is already formatted is
printed in full. `-d` takes precedence over `-w`.

## `pipit debug <file>`

Terminal debugger. Running and quitting work, but line breakpoints did not pause
execution in the [Fibonacci example](../examples/fibonacci/main.go) during testing. Use the
[Go debugger example](api.md#attach-a-debugger) to pause and inspect a script.
The terminal controls are:


- `r` runs the script.
- `b <line>` sets a breakpoint.
- `c` continues, `n` step-over, `s` step-in, `o` step-out.
- `p` pauses a running script at its next instruction.
- `q` quits.

The status line shows the execution state and any pause reason.

## `pipit dap`

Start a Debug Adapter Protocol (DAP) server over stdin/stdout. Editors with a
configured DAP client can set breakpoints, step through scripts, and inspect
variables. The client's `launch` request supplies the script and its options.

Flag:

- `--debug-log <path>` - write every DAP message (in and out) to a file;
  useful when developing or troubleshooting the adapter.

See the [DAP guide](dap.md) for per-editor setup and the launch-args
schema.

## `pipit module`

Inspect the `script.lock` in the current directory:

- `list` lists recorded module entries.
- `inspect <path>[@<version>]` shows a recorded module and its capabilities.
- `verify` parses the lockfile and reports the number of pinned entries.

These commands do not fetch modules or verify cached payload hashes. The current
implementation always reads `./script.lock`, even though its help mentions a
lockfile override. This is separate from `run`'s default approval storage.

## `pipit version`

Print the CLI version, library version, bytecode schema, interpreter module build
version, and host Go toolchain. `pipit -v` and `pipit --version` are aliases.

## `pipit isolated`

Experimental Linux process isolation. The command requires independently approved
worker and watchdog executables, a delegated cgroup parent, and private state
storage. Use `--check` for a live probe, `--repl` for a session, or `recover` to
reclaim records after a host crash.

See [isolated execution](isolated-execution.md) for complete commands and
[filesystem isolation](filesystem-isolation.md) for broker and root flags. `--log`
writes the boundary's audit events to stderr, and `--print=false` silences the
result.

## Environment variables

| Variable | Purpose |
|---|---|
| `PIPIT_SCRIPT_DIR` | Script/package directory supplied by `run` and `test` |
| `PIPIT_ARGC`, `PIPIT_ARG_<n>` | Script arguments mirrored by `run`; scripts can also use `os.Args` |
| `PIPIT_GO_DISPATCH` | Any non-empty value uses Go dispatch for `run`, `eval`, `test`, `compile`, and `bytecode run` when diagnosing runtime differences |
| `PIPIT_UNSAFE_ALLOW_UNTESTED_GO=1` | Skip the Go minor-version check; the runtime layout probe still runs, and execution may be incorrect |
| `PIPIT_TRACE_LOWERING=1` | Log compiler lowering choices at debug level |
| `PIPIT_TRACE_POOL=1` | Log named scalar type pool assignments |

Contributor-only variables belong to the relevant test scripts under `hack/test`.

## See also

- [Getting started](getting-started.md): install and run a first script.
- [Embedding](api.md): use Pipit from a Go application.
- [Debugging with DAP](dap.md): editor setup.
