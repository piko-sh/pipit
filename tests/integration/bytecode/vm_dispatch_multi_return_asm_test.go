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

//go:build integration && !safe && (amd64 || arm64)

package bytecode_test

import (
	"testing"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

const (
	asmMultiReturnIterations = 2000
)

func runASMMultiReturnScript(t *testing.T, source string) (any, *engine.VM) {
	t.Helper()
	compiled := compileExpression(t, source)
	vm := newTestVM(t)
	result, err := vm.Execute(compiled)
	require.NoError(t, err)
	return result, vm
}

func TestASMInlineCallTwoReturns(t *testing.T) {
	t.Parallel()
	result, vm := runASMMultiReturnScript(t, `
func divmod(a, b int) (int, int) { return a / b, a % b }
total := 0
for i := 1; i < 2000; i++ {
	q, r := divmod(i*7, 3)
	total += q + r
}
total`)

	expected := 0
	for i := 1; i < asmMultiReturnIterations; i++ {
		expected += (i*7)/3 + (i*7)%3
	}
	require.EqualValues(t, expected, result)
	require.Zero(t, vm.ReturnGoDispatches, "two scalar returns must be copied by the inline return handler")
}

func TestASMInlineCallMixedKindReturns(t *testing.T) {
	t.Parallel()
	result, vm := runASMMultiReturnScript(t, `
func split(i int) (int, float64, string, bool, uint) {
	return i * 2, float64(i) / 2, "x", i%2 == 0, uint(i) + 1
}
total := 0.0
evens := 0
acc := uint(0)
name := ""
for i := 0; i < 2000; i++ {
	n, f, s, even, u := split(i)
	total += float64(n) + f
	if even { evens++ }
	acc += u
	name = s
}
float64(evens) + total + float64(acc) + float64(len(name))`)

	expectedTotal := 0.0
	evens := 0
	var acc uint
	for i := range asmMultiReturnIterations {
		expectedTotal += float64(i*2) + float64(i)/2
		if i%2 == 0 {
			evens++
		}
		acc += uint(i) + 1
	}
	require.InDelta(t, float64(evens)+expectedTotal+float64(acc)+1, result, 1e-6)
	require.Positive(t, vm.ReturnGoDispatches, "five returns exceed the inline limit and must take the Go path")

	result, vm = runASMMultiReturnScript(t, `
func pair(i int) (int, float64) { return i, float64(i) * 0.5 }
total := 0.0
for i := 0; i < 2000; i++ {
	n, f := pair(i)
	total += float64(n) + f
}
total`)
	expected := 0.0
	for i := range asmMultiReturnIterations {
		expected += float64(i) + float64(i)*0.5
	}
	require.InDelta(t, expected, result, 1e-6)
	require.Zero(t, vm.ReturnGoDispatches, "int and float returns must be copied by the inline return handler")
}

func TestASMInlineCallStringAndBoolReturns(t *testing.T) {
	t.Parallel()
	result, vm := runASMMultiReturnScript(t, `
func label(i int) (string, bool, int) {
	if i%3 == 0 { return "fizz", true, i }
	return "n", false, -i
}
count := 0
length := 0
for i := 0; i < 2000; i++ {
	s, hit, n := label(i)
	length += len(s)
	if hit { count += n } else { count -= n }
}
count + length`)

	expected := 0
	for i := range asmMultiReturnIterations {
		if i%3 == 0 {
			expected += i + 4
		} else {
			expected += i + 1
		}
	}
	require.EqualValues(t, expected, result)
	require.Zero(t, vm.ReturnGoDispatches, "string, bool and int returns must be copied by the inline return handler")
}

func TestASMInlineCallFourReturnsAndDiscard(t *testing.T) {
	t.Parallel()
	result, vm := runASMMultiReturnScript(t, `
func quad(i int) (int, int, int, int) { return i, i + 1, i + 2, i + 3 }
total := 0
for i := 0; i < 2000; i++ {
	a, b, c, d := quad(i)
	total += a + b + c + d
	quad(i)
}
total`)

	expected := 0
	for i := range asmMultiReturnIterations {
		expected += 4*i + 6
	}
	require.EqualValues(t, expected, result)
	require.Zero(t, vm.ReturnGoDispatches, "four returns and a discarded call must stay on the inline return handler")
}

func TestConfigureASMReturnAcceptsUpToFourMatchingScalars(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{ResultKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterBool}}
	site := &program.CallSite{Returns: []program.VarLocation{
		{Kind: isa.RegisterInt, Register: 1}, {Kind: isa.RegisterFloat, Register: 2},
		{Kind: isa.RegisterString, Register: 3}, {Kind: isa.RegisterBool, Register: 4},
	}}
	var info engine.AsmCallInfo
	require.True(t, engine.ConfigureASMReturn(&info, site, callee))
	require.EqualValues(t, 4, info.ReturnCount)

	site.Returns = append(site.Returns, program.VarLocation{Kind: isa.RegisterUint, Register: 5})
	callee.ResultKinds = append(callee.ResultKinds, isa.RegisterUint)
	require.False(t, engine.ConfigureASMReturn(&engine.AsmCallInfo{}, site, callee), "five returns exceed the inline limit")

	mismatched := &program.CallSite{Returns: []program.VarLocation{{Kind: isa.RegisterInt, Register: 1}, {Kind: isa.RegisterInt, Register: 2}}}
	require.False(t, engine.ConfigureASMReturn(&engine.AsmCallInfo{}, mismatched, callee), "destination kinds must match the callee result kinds")

	general := &program.CompiledFunction{ResultKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterGeneral}}
	generalSite := &program.CallSite{Returns: []program.VarLocation{{Kind: isa.RegisterInt, Register: 1}, {Kind: isa.RegisterGeneral, Register: 2}}}
	require.False(t, engine.ConfigureASMReturn(&engine.AsmCallInfo{}, generalSite, general), "general-bank results stay on the Go path")
}
