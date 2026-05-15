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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	tinyLeafMutateSource = `package main
type C struct {
	n int
	u uint
	f float64
	b bool
	w uint32
}
func (c *C) Inc() { c.n++ }
func (c *C) IncU() { c.u++ }
func (c *C) IncW() { c.w++ }
func (c *C) SetN(v int) { c.n = v }
func (c *C) SetU(v uint) { c.u = v }
func (c *C) SetF(v float64) { c.f = v }
func (c *C) SetB(v bool) { c.b = v }
func (c *C) Get() int { return c.n }
type Counter interface {
	Inc()
	IncU()
	SetN(v int)
	SetU(v uint)
	SetF(v float64)
	SetB(v bool)
	Get() int
}
func entrypoint() int {
	n := 7
	c := &C{}
	var i Counter = c
	for k := 0; k < n; k++ {
		i.Inc()
		i.IncU()
		i.SetN(k)
		i.SetU(uint(k))
		i.SetF(float64(k) + 0.5)
		i.SetB(k%2 == 0)
		c.Inc()
		c.SetN(c.Get() + 1)
	}
	total := c.n
	if c.u != uint(n-1) { total = -1 }
	if c.f != float64(n-1)+0.5 { total = -2 }
	if c.b != ((n-1)%2 == 0) { total = -3 }
	return total
}
`
)

func compileTinyLeafFixture(t *testing.T) (*program.CompiledFileSet, map[string]*program.CompiledFunction) {
	t.Helper()
	compiled, err := app.NewService().CompileFileSet(context.Background(), map[string]string{"main.go": tinyLeafMutateSource})
	require.NoError(t, err)
	byName := map[string]*program.CompiledFunction{}
	for _, f := range program.ExportFunctions(compiled.Root()) {
		byName[f.Name] = f
	}
	return compiled, byName
}

func TestClassifyTinyLeafIncIntField(t *testing.T) {
	t.Parallel()
	_, functions := compileTinyLeafFixture(t)
	require.Equal(t, program.TinyLeafIncIntField, functions["C.Inc"].TinyLeafShape)
	require.Equal(t, program.TinyLeafIncUintField, functions["C.IncU"].TinyLeafShape)
	require.Equal(t, program.TinyLeafNone, functions["C.IncW"].TinyLeafShape, "a narrow uint field does not fuse and stays unclassified")
	require.Equal(t, program.TinyLeafReturnIntField, functions["C.Get"].TinyLeafShape)
	require.Equal(t, uint8(1), functions["C.Inc"].TinyLeafLayout.PathLength)
}

func TestClassifyTinyLeafSetScalarFieldFromArg(t *testing.T) {
	t.Parallel()
	_, functions := compileTinyLeafFixture(t)
	for _, name := range []string{"C.SetN", "C.SetU", "C.SetF", "C.SetB"} {
		require.Equal(t, program.TinyLeafSetScalarFieldFromArg, functions[name].TinyLeafShape, name)
	}
}

func TestTinyLeafSetFieldFromArg(t *testing.T) {
	t.Parallel()
	compiled, _ := compileTinyLeafFixture(t)
	result, err := app.NewService().ExecuteEntrypoint(context.Background(), compiled, "entrypoint")
	require.NoError(t, err)
	require.EqualValues(t, 8, result, "n is set to k, incremented, then set to Get()+1 on every iteration")
}

func TestTinyLeafIncFieldNilReceiverPanics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := app.NewService().Eval(ctx, `type C struct{ n int }
func (c *C) Inc() { c.n++ }
type I interface{ Inc() }
var i I = (*C)(nil)
for k := 0; k < 3; k++ { i.Inc() }`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil pointer dereference")

	_, err = app.NewService().Eval(ctx, `type C struct{ n int }
func (c *C) SetN(v int) { c.n = v }
func run(c *C) { c.SetN(3) }
run(nil)`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil pointer dereference")
}
