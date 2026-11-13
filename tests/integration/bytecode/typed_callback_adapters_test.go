// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

//go:build integration

package bytecode_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/sdk/stdlib"
)

type InspectFunc func(ast.Node) bool

var errSpinDeadline = errors.New("the spinning callback's deadline passed")

const typedAdapterSource = `package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"inspecthost"
	"strings"
)

func walk(node ast.Node, viaMakeFunc bool, f func(ast.Node) bool) {
	if viaMakeFunc {
		inspecthost.Inspect(node, f)
		return
	}
	ast.Inspect(node, f)
}

func parse(source string) ast.Node {
	expression, err := parser.ParseExpr(source)
	if err != nil {
		panic(err)
	}
	return expression
}

const program = "f(a+b*2, -c, [3]int{4, 5}[x], func(y int) bool { return y > z }(1))"

func Trace(viaMakeFunc bool) string {
	var trace strings.Builder
	walk(parse(program), viaMakeFunc, func(n ast.Node) bool {
		if n == nil {
			trace.WriteString(")")
			return true
		}
		fmt.Fprintf(&trace, "(%T", n)
		if identifier, ok := n.(*ast.Ident); ok {
			trace.WriteString(" " + identifier.Name)
		}
		_, literal := n.(*ast.CompositeLit)
		return !literal
	})
	return trace.String()
}

type collector struct {
	names []string
	nils  int
}

func (c *collector) visit(n ast.Node) bool {
	if n == nil {
		c.nils++
		return true
	}
	if identifier, ok := n.(*ast.Ident); ok {
		c.names = append(c.names, identifier.Name)
	}
	_, function := n.(*ast.FuncLit)
	return !function
}

func Collect(viaMakeFunc bool) string {
	c := &collector{}
	if viaMakeFunc {
		inspecthost.Inspect(parse(program), c.visit)
	} else {
		ast.Inspect(parse(program), c.visit)
	}
	return fmt.Sprint(c.names, c.nils)
}

func Panics(viaMakeFunc bool) (recovered string) {
	defer func() {
		r := recover()
		recovered = fmt.Sprintf("%T: %v", r, r)
	}()
	walk(parse(program), viaMakeFunc, func(n ast.Node) bool {
		if literal, ok := n.(*ast.BasicLit); ok && literal.Value == "2" {
			panic("found " + literal.Value)
		}
		return true
	})
	return "no panic"
}

func FailsAtRunTime(viaMakeFunc bool) (recovered string) {
	defer func() {
		r := recover()
		recovered = fmt.Sprintf("%T: %v", r, r)
	}()
	walk(parse(program), viaMakeFunc, func(n ast.Node) bool {
		if literal, ok := n.(*ast.BasicLit); ok && literal.Value == "5" {
			var nodes []ast.Node
			return nodes[len(literal.Value)] == nil
		}
		return true
	})
	return "no panic"
}

func Spins(viaMakeFunc bool) int {
	visits := 0
	walk(parse(program), viaMakeFunc, func(n ast.Node) bool {
		visits++
		if _, ok := n.(*ast.BinaryExpr); ok {
			for {
				visits++
			}
		}
		return true
	})
	return visits
}
`

func inspectHostSymbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"InspectFunc": reflect.ValueOf((*InspectFunc)(nil)),
		"Inspect": reflect.ValueOf(func(node ast.Node, f InspectFunc) {
			ast.Inspect(node, f)
		}),
	}
}

func typedAdapterService(t *testing.T) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	var options []app.Option
	if os.Getenv("PIPIT_TEST_GO_DISPATCH") == "1" {
		options = append(options, app.WithForceGoDispatch())
	}
	service := app.NewService(options...)
	service.UseSymbolProviders(stdlib.Providers()...)
	service.RegisterPackage("inspecthost", inspectHostSymbols())
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": typedAdapterSource})
	require.NoError(t, err)
	return service, compiled
}

func TestTypedInspectAdaptersBehaveAsMakeFuncWrappers(t *testing.T) {
	service, compiled := typedAdapterService(t)
	for _, entry := range []string{"Trace", "Collect", "Panics", "FailsAtRunTime"} {
		t.Run(entry, func(t *testing.T) {
			viaMakeFunc, errMakeFunc := service.CallFunction(t.Context(), compiled, entry, true)
			typed, errTyped := service.CallFunction(t.Context(), compiled, entry, false)
			require.Equal(t, fmt.Sprint(errMakeFunc), fmt.Sprint(errTyped))
			t.Logf("%s: %v", entry, typed)
			require.Equal(t, viaMakeFunc, typed)
			require.NotEqual(t, "no panic", typed)
		})
	}
}

func TestATypedInspectAdapterStopsAsAMakeFuncWrapperDoes(t *testing.T) {
	service, compiled := typedAdapterService(t)
	outcome := func(viaMakeFunc bool) string {
		ctx, cancel := context.WithTimeoutCause(t.Context(), 200*time.Millisecond, errSpinDeadline)
		defer cancel()
		type finished struct {
			result any

			err error
		}
		done := make(chan finished, 1)
		go func() {
			result, err := service.CallFunction(ctx, compiled, "Spins", viaMakeFunc)
			done <- finished{result: result, err: err}
		}()
		select {
		case got := <-done:
			return fmt.Sprintf("%v %v", got.result, got.err)
		case <-time.After(time.Minute):
			require.FailNow(t, "a callback that spins must stop at the execution's deadline")
			return ""
		}
	}
	viaMakeFunc, typed := outcome(true), outcome(false)
	t.Logf("Spins: %s", typed)
	require.Equal(t, viaMakeFunc, typed)
	require.Contains(t, typed, "deadline")
}
