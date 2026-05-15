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

//go:build !safe && (amd64 || arm64)

package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

const (
	asmMethodCallIterations = 3000
)

func asmMethodCallShapeSource(typeCount int) string {
	var b strings.Builder
	b.WriteString(`

type adder interface{ add(x int) int }

type counter struct{ n int }

func (c *counter) add(x int) int { c.n += x; return c.n }

type doubler struct{ n int }

func (d doubler) add(x int) int { return d.n + 2*x }
`)
	for i := range typeCount {
		fmt.Fprintf(&b, "type shape%d struct{ n int }\nfunc (s *shape%d) add(x int) int { return s.n + x*%d }\n", i, i, i+1)
	}
	return b.String()
}

func runASMMethodCallScript(t *testing.T, source string) (any, *engine.VM) {
	t.Helper()
	compiled := compileExpression(t, source)
	vm := newTestVM(t)
	result, err := vm.Execute(compiled)
	require.NoError(t, err)
	return result, vm
}

func TestASMInlineMethodCallMonomorphic(t *testing.T) {
	t.Parallel()
	result, vm := runASMMethodCallScript(t, asmMethodCallShapeSource(0)+`
item := adder(&counter{})
total := 0
for i := 0; i < 3000; i++ { total += item.add(i) }
total`)

	expected, running := 0, 0
	for i := range asmMethodCallIterations {
		running += i
		expected += running
	}
	require.EqualValues(t, expected, result)
	require.Equal(t, 1, vm.MethodCallGoDispatches, "only the populating hit should reach the Go path")
}

func TestASMInlineMethodCallGuardSwitchesEntries(t *testing.T) {
	t.Parallel()
	result, vm := runASMMethodCallScript(t, asmMethodCallShapeSource(0)+`
items := []adder{&counter{}, doubler{n: 7}, &counter{n: 3}}
total := 0
for i := 0; i < 3000; i++ { total += items[i%3].add(i) }
total`)

	expected := 0
	first, third := 0, 3
	for i := range asmMethodCallIterations {
		switch i % 3 {
		case 0:
			first += i
			expected += first
		case 1:
			expected += 7 + 2*i
		default:
			third += i
			expected += third
		}
	}
	require.EqualValues(t, expected, result)
	require.Equal(t, 2, vm.MethodCallGoDispatches, "one populating hit per receiver type")
}

func TestASMInlineMethodCallNinthTypeFallsBack(t *testing.T) {
	t.Parallel()
	const typeCount = engine.MaxASMMethodEntries + 1
	var items strings.Builder
	for i := range typeCount {
		fmt.Fprintf(&items, "&shape%d{n: %d}, ", i, i)
	}
	result, vm := runASMMethodCallScript(t, asmMethodCallShapeSource(typeCount)+`
items := []adder{`+items.String()+`}
total := 0
for i := 0; i < 3000; i++ { total += items[i%9].add(i) }
total`)

	expected := 0
	ninthCalls := 0
	for i := range asmMethodCallIterations {
		k := i % typeCount
		expected += k + i*(k+1)
		if k == typeCount-1 {
			ninthCalls++
		}
	}
	require.EqualValues(t, expected, result)
	require.Equal(t, engine.MaxASMMethodEntries+ninthCalls-1, vm.MethodCallGoDispatches,
		"eight types populate once each; the ninth type is served by Go on every hit")
}

func TestASMInlineMethodCallGoroutineBails(t *testing.T) {
	t.Parallel()
	source := asmMethodCallShapeSource(0) + `
done := make(chan int)
go func() { done <- 1 }()
<-done
item := adder(&counter{})
total := 0
for i := 0; i < 3000; i++ { total += item.add(i) }
total`
	result, vm := runASMMethodCallScript(t, source)

	expected, running := 0, 0
	for i := range asmMethodCallIterations {
		running += i
		expected += running
	}
	require.EqualValues(t, expected, result)
	require.Equal(t, asmMethodCallIterations-1, vm.MethodCallGoDispatches,
		"with goroutines alive every inline-cache hit stays on the Go path")
}

func TestASMInlineMethodCallGoroutineSpawnMidLoopDisablesTable(t *testing.T) {
	t.Parallel()
	source := asmMethodCallShapeSource(0) + `
item := adder(&counter{})
total := 0
done := make(chan int, 1)
for i := 0; i < 3000; i++ {
	if i == 1500 {
		go func() { done <- 1 }()
		<-done
	}
	total += item.add(i)
}
total`
	result, vm := runASMMethodCallScript(t, source)

	expected, running := 0, 0
	for i := range asmMethodCallIterations {
		running += i
		expected += running
	}
	require.EqualValues(t, expected, result)
	require.Equal(t, 1+asmMethodCallIterations/2, vm.MethodCallGoDispatches,
		"one populating hit before the spawn, then every hit after it on the Go path")
}

func TestASMInlineMethodCallTablesDroppedAfterExecution(t *testing.T) {
	t.Parallel()
	_, vm := runASMMethodCallScript(t, asmMethodCallShapeSource(0)+`
item := adder(&counter{})
total := 0
for i := 0; i < 10; i++ { total += item.add(i) }
total`)
	require.Nil(t, vm.AsmMethodEntryTables)
	require.Nil(t, vm.PrivateACITables)
	require.Nil(t, vm.AsmCallInfoTables)
}
