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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"pipit.sh/pipit/internal/engine/program"
)

type rootCounter struct {
	stringsSeen       []string
	reflectValuesSeen int
	slicesIntSeen     int
	slicesFloatSeen   int
	slicesStringSeen  int
	slicesBoolSeen    int
	slicesUintSeen    int
	slicesByteSeen    int
	anysSeen          int
	upvalueCellsSeen  int
	closuresSeen      int
}

func (c *rootCounter) visitor() rootVisitor {
	return rootVisitor{
		visitString:         func(s string) { c.stringsSeen = append(c.stringsSeen, s) },
		visitReflectValue:   func(_ reflect.Value) { c.reflectValuesSeen++ },
		visitSliceInt:       func(_ []int64) { c.slicesIntSeen++ },
		visitSliceFloat:     func(_ []float64) { c.slicesFloatSeen++ },
		visitSliceString:    func(_ []string) { c.slicesStringSeen++ },
		visitSliceBool:      func(_ []bool) { c.slicesBoolSeen++ },
		visitSliceUint:      func(_ []uint64) { c.slicesUintSeen++ },
		visitSliceByte:      func(_ []byte) { c.slicesByteSeen++ },
		visitAny:            func(_ any) { c.anysSeen++ },
		visitUpvalueCell:    func(_ *program.UpvalueCell) { c.upvalueCellsSeen++ },
		visitRuntimeClosure: func(_ *RuntimeClosure) { c.closuresSeen++ },
	}
}

func newRootsTestVM(t *testing.T) *VM {
	t.Helper()
	return &VM{
		Globals:   &GlobalStore{},
		Arena:     NewRegisterArena(),
		CallStack: []CallFrame{},
	}
}

func TestWalkRoots_EmptyVM(t *testing.T) {
	vm := newRootsTestVM(t)
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Empty(t, counter.stringsSeen, "empty VM should visit zero string roots")
	assert.Equal(t, 0, counter.reflectValuesSeen, "empty VM should visit zero reflectValue roots")
}

func TestWalkRoots_GlobalStrings(t *testing.T) {
	vm := newRootsTestVM(t)
	vm.Globals.Strings = []string{"alpha", "beta", "gamma"}
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Len(t, counter.stringsSeen, 3, "expected 3 global strings visited")
}

func TestWalkRoots_GlobalGeneral(t *testing.T) {
	vm := newRootsTestVM(t)
	vm.Globals.general = []reflect.Value{
		reflect.ValueOf(42),
		reflect.ValueOf("hello"),
	}
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Equal(t, 2, counter.reflectValuesSeen, "expected 2 global generals visited")
}

func TestWalkRoots_FrameRegisters(t *testing.T) {
	vm := newRootsTestVM(t)
	vm.CallStack = []CallFrame{{
		Registers: Registers{
			Strings:    []string{"r0", "r1"},
			General:    []reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)},
			SlicesInt:  [][]int64{{1, 2, 3}, {4, 5}},
			slicesByte: [][]byte{[]byte("hi"), nil},
		},
	}}
	vm.FramePointer = 0
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Len(t, counter.stringsSeen, 2, "expected 2 frame strings")
	assert.Equal(t, 2, counter.reflectValuesSeen, "expected 2 frame generals")
	assert.Equal(t, 2, counter.slicesIntSeen, "expected 2 frame []int64 slices")
	assert.Equal(t, 2, counter.slicesByteSeen, "expected 2 frame []byte slices")
}

func TestWalkRoots_FrameUpvalues(t *testing.T) {
	vm := newRootsTestVM(t)
	cell1 := &program.UpvalueCell{}
	cell2 := &program.UpvalueCell{}
	vm.CallStack = []CallFrame{{
		upvalues: []upvalue{{Value: cell1}, {Value: cell2}},
	}}
	vm.FramePointer = 0
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Equal(t, 2, counter.upvalueCellsSeen, "expected 2 upvalue cells visited")
}

func TestWalkRoots_FrameSharedCells(t *testing.T) {
	vm := newRootsTestVM(t)
	cell1 := &program.UpvalueCell{}
	cell2 := &program.UpvalueCell{}
	cell3 := &program.UpvalueCell{}
	scm := &sharedCellMap{
		inlineLen: 2,
		overflow:  []sharedCellEntry{{cell: cell3, key: 99}},
	}
	scm.inlineCells[0] = cell1
	scm.inlineCells[1] = cell2
	vm.CallStack = []CallFrame{{
		sharedCells: scm,
	}}
	vm.FramePointer = 0
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Equal(t, 3, counter.upvalueCellsSeen, "expected 3 cells (2 inline + 1 overflow)")
}

func TestWalkRoots_DeferStack(t *testing.T) {
	vm := newRootsTestVM(t)
	closure := &RuntimeClosure{}
	vm.deferStack = []deferredCall{{
		Function:  closure,
		arguments: []reflect.Value{reflect.ValueOf("a"), reflect.ValueOf("b")},
	}}
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Equal(t, 1, counter.closuresSeen, "expected 1 defer closure")
	assert.Equal(t, 2, counter.reflectValuesSeen, "expected 2 defer args as reflectValues")
}

func TestWalkRoots_VMState(t *testing.T) {
	vm := newRootsTestVM(t)
	vm.panicValue = "oops"
	vm.evalResult = 42
	vm.EvalAllResults = []any{"x", "y", "z"}
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Equal(t, 5, counter.anysSeen, "expected 5 any roots (panic + result + 3 results)")
}

func TestWalkRoots_ClosureCache(t *testing.T) {
	vm := newRootsTestVM(t)
	closure := &RuntimeClosure{}
	vm.closureCache = []reflect.Value{
		reflect.ValueOf(closure),
		{},
		reflect.ValueOf(closure),
	}
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Equal(t, 2, counter.reflectValuesSeen, "expected 2 valid cached closures")
}

func TestWalkRoots_AllSourcesCombined(t *testing.T) {
	vm := newRootsTestVM(t)
	cell := &program.UpvalueCell{}
	closure := &RuntimeClosure{}
	vm.CallStack = []CallFrame{{
		Registers: Registers{
			Strings: []string{"frame-string"},
			General: []reflect.Value{reflect.ValueOf("g")},
		},
		upvalues: []upvalue{{Value: cell}},
	}}
	vm.FramePointer = 0
	vm.Globals.Strings = []string{"global-string"}
	vm.Globals.general = []reflect.Value{reflect.ValueOf(1.5)}
	vm.deferStack = []deferredCall{{Function: closure}}
	vm.panicValue = "panic"
	vm.closureCache = []reflect.Value{reflect.ValueOf(closure)}
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Len(t, counter.stringsSeen, 2, "expected 2 strings (1 frame + 1 global)")
	assert.Equal(t, 3, counter.reflectValuesSeen, "expected 3 reflect.Values (1 frame + 1 global + 1 cache)")
	assert.Equal(t, 1, counter.upvalueCellsSeen, "expected 1 upvalue cell")
	assert.Equal(t, 1, counter.closuresSeen, "expected 1 defer closure")
	assert.Equal(t, 1, counter.anysSeen, "expected 1 panic any")
}

func TestWalkRoots_OnlyActiveFrames(t *testing.T) {
	vm := newRootsTestVM(t)
	vm.CallStack = []CallFrame{
		{Registers: Registers{Strings: []string{"active-frame-0"}}},
		{Registers: Registers{Strings: []string{"active-frame-1"}}},
		{Registers: Registers{Strings: []string{"DEAD-frame-2"}}},
	}
	vm.FramePointer = 1
	counter := &rootCounter{}
	vm.walkRoots(counter.visitor())
	assert.Len(t, counter.stringsSeen, 2, "expected 2 active-frame strings (framePointer=1)")
	assert.NotContains(t, counter.stringsSeen, "DEAD-frame-2", "walked dead frame above framePointer")
}

func TestWalkRoots_NilCallbacks(t *testing.T) {
	vm := newRootsTestVM(t)
	vm.Globals.Strings = []string{"a", "b"}
	vm.Globals.general = []reflect.Value{reflect.ValueOf(1)}
	vm.walkRoots(rootVisitor{})
}
