---
title: "Getting started"
description: "Embed Pipit in a Go application or build the CLI and run your first script."
section: "Tutorials"
order: 1
---

# Getting started

Build a Go application that evaluates source with Pipit, then run a script through
the command-line interface (CLI). You need Git and Go 1.27. Pipit checks the Go
minor version at runtime, so use this version for the tutorial.

Get the source from the [Pipit repository](https://github.com/piko-sh/pipit):

```sh
git clone https://github.com/piko-sh/pipit.git
cd pipit
```

## Embed Pipit

Create an application beside the checkout. A local workspace lets the application
use both Pipit modules without requiring a published module release:

```sh
# From the pipit checkout:
cd ..
mkdir pipit-demo
cd pipit-demo
go mod init example.com/pipit-demo
go work init . ../pipit ../pipit/sdk/stdlib
```

Save this as `main.go`:

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
	interp := pipit.NewInterpreter(stdlib.WithStandardLibrary())
	value, err := interp.Eval(context.Background(), `
        import "strings"
        strings.ToUpper("hello, pipit")
    `)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(value)
}
```

Run it:

```sh
go run .
```

Expected output:

```text
HELLO, PIPIT
```

The root module supplies the interpreter. The separate `sdk/stdlib` module supplies
standard-library symbols. `stdlib.WithStandardLibrary()` registers those symbols;
without it, this example cannot import `strings`.

The evaluated code runs in your process and can call the functions you register.
Read [execution security](security.md) before accepting code from other people.

## Use the CLI

Return to the Pipit checkout (`cd ../pipit` if you followed the embedding example),
then build a local binary:

```sh
go build -o ./bin/pipit ./cmd/pipit
./bin/pipit version
```

To install the command on your `PATH`:

```sh
go install ./cmd/pipit
```

Go installs it into `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is unset. Add
that directory to your `PATH`. The commands below assume `pipit` is available there.
A built CLI can run scripts without a separate Go toolchain.

Create a separate directory for your scripts:

```sh
mkdir ../pipit-scripts
cd ../pipit-scripts
```

Save this as `hello.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("hello, pipit")
}
```

Run it:

```sh
pipit run hello.go
```

Expected output:

```text
hello, pipit
```

## Explore

```sh
pipit eval -e '1 + 2 * 3'        # prints 7
pipit symbols list              # registered host packages
pipit doc strings.HasPrefix      # symbol signature
```

To try an interactive session, run:

```sh
pipit repl
```

Enter `1 + 2` and press Enter. Pipit displays `3`. Enter `:q` to leave the
session and return to your shell.

You have now embedded Pipit in an application and run a standalone script. The
[CLI reference](cli.md) lists timeouts, resource limits, and approval options.

## Next steps

- [Embedding](api.md): host functions, state, concurrency, and errors.
- [CLI reference](cli.md): commands, flags, modules, and approvals.
- [Compatibility](compatibility.md): known differences from native Go.
- [Examples](../examples/README.md): programs to run from the checkout.
- [Debugging](api.md#attach-a-debugger): pause and inspect a script from Go.
