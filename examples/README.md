# pipit examples

Each directory is a standalone script you can run with the `pipit` binary (`go build ./cmd/pipit` from the repo root, or
`go install`). The first four
need nothing else. The `module_*` examples fetch a real dependency from
proxy.golang.org, so their first run needs `--allow-network`.

The module cache is off by default, so every run re-fetches and recompiles.
Add `--cache=home` to keep the compiled bundle; warm runs then need no
network at all. Capabilities are ungated unless you ask: `--gate=all` (or
`network`, `disk`, `exec`, `env`) prompts before a script reaches those
surfaces and records approvals in `script.lock` next to the script.

## hello/

The smallest valid pipit script: one import, one `fmt.Println`. Useful as a
first check that the binary and the standard library it registers are wired up.

```sh
pipit run examples/hello/main.go
```

## fibonacci/

Recursive `fib` evaluated by the bytecode VM, printing the first twelve
terms. Also the script the docs use for the debugger walkthrough.

```sh
pipit run examples/fibonacci/main.go
pipit debug examples/fibonacci/main.go
```

## strings_demo/

Calls the host `strings` package from interpreted code: `ToUpper`, `Count`,
and a `Split` result walked with `range`.

```sh
pipit run examples/strings_demo/main.go
```

## concurrent/

Five goroutines writing into a shared slice behind a `sync.WaitGroup`. Shows
that goroutines, closures, `defer`, and the scheduler all work under the
sandboxed VM.

```sh
pipit run examples/concurrent/main.go
```

## webserver/

A customised pipit CLI that compiles the host `net/http` package into its
symbol table, plus a script that serves three routes through it. This is the
template for extending pipit with any package your scripts need:
`pipit-symbols.yaml` lists the packages, `go generate` writes `symbols/` with
`pipit extract`, and `cli.WithSymbols` registers them with `cli.Main`. The
generated files are committed, so step 1 is only needed after editing the
manifest.

```sh
cd examples/webserver
go generate ./...                          # only after editing pipit-symbols.yaml
go build -o pipit-webserver .
./pipit-webserver run scripts/server.go &

curl http://localhost:8080/health          # ok
curl http://localhost:8080/hello/world     # hello, world!
curl http://localhost:8080/square/12       # 144
```

## module_uuid/

Fetches `github.com/google/uuid` and prints five random v4 UUIDs plus a
deterministic v5. A dependency-free module, so it is the shortest path
through fetch, compile, cache, and run.

```sh
pipit run --allow-network examples/module_uuid/main.go
```

## module_humanize/

`github.com/dustin/go-humanize` formatting IEC byte sizes, English ordinals,
"x ago" times, and comma-grouped numbers. Also dependency-free, and it claims
no capabilities, so the fetch is followed by a silent run.

```sh
pipit run --allow-network examples/module_humanize/main.go
```

## module_gjson/

Queries a JSON service descriptor with `github.com/tidwall/gjson`: field
paths, array indexing, and an `#(auth==false)#` wildcard filter. gjson pulls
in `tidwall/match` and `tidwall/pretty`, so this is the transitive case - the
resolver sorts all three modules topologically and bridges each one's exports
into the symbol registry before the next module type-checks.

```sh
pipit run --allow-network examples/module_gjson/main.go
```

## module_spew/

Dumps a nested order/customer/address struct with
`github.com/davecgh/go-spew`, via `Dump`, `Sdump`, and a `ConfigState` with
`MaxDepth: 1`. Known gap: `spew.Printf("%#+v")` prints spew's own formatter
internals instead of the value passed to it.

```sh
pipit run --allow-network examples/module_spew/main.go
```

## module_testify/

Runs twenty-odd `testify/assert` checks against a local `T` stub that
captures failures instead of aborting, ending with one deliberate failure so
the formatted diff is visible. Five modules are fetched in total (testify,
go-spew, go-difflib, objx, yaml.v3), the deepest graph in the examples.

```sh
pipit run --allow-network examples/module_testify/main.go
```

## module_decimal/

Exact invoice arithmetic with `github.com/shopspring/decimal`: line totals,
20% VAT, and the `0.1 + 0.2 == 0.3` check that floats get wrong. Known gap:
`Div` loses precision in the tail, so `1/3` at 20 dp prints
`0.33333333333333330000`.

```sh
pipit run --allow-network examples/module_decimal/main.go
```

## module_cast/

`github.com/spf13/cast` converting between strings, numbers, slices,
`map[any]any`, times, and durations. Known gap: `ToBool` returns false for
every input, including `"true"`, `"yes"`, and `1`; the other conversions
match native Go.

```sh
pipit run --allow-network examples/module_cast/main.go
```

## module_version/

Parses, sorts, and constraint-checks semver strings with
`github.com/hashicorp/go-version`, the version arithmetic from terraform and
vault. Known gap: sorting a `version.Collection` collapses it - all six
entries print as `1.10.0` afterwards.

```sh
pipit run --allow-network examples/module_version/main.go
```
