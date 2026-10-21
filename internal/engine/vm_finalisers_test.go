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
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

type finaliserTestNode struct {
	next *finaliserTestNode
	id   int64
}

type finaliserTestPlain struct {
	ID    int64
	Check int64
}

type finaliserTestLinked struct {
	Next *finaliserTestLinked
	ID   int64
}

type finaliserTestNamed struct {
	ID   int64
	Name string
}

func newFinaliserTestVM(tb testing.TB, sets bool) *VM {
	vm := newGCTestVM()
	callee := &program.CompiledFunction{GeneralConstants: []reflect.Value{reflect.ValueOf(runtime.GC)}}
	if sets {
		callee.GeneralConstants = append(callee.GeneralConstants, reflect.ValueOf(runtime.SetFinalizer))
	}
	vm.rootFunction = &program.CompiledFunction{Functions: []*program.CompiledFunction{callee}}
	vm.ctx = tb.Context()
	return vm
}

func TestAProgramThatSetsFinalisersAllocatesEachValueOnItsOwn(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	require.True(t, vm.allocatesForFinalisers())
	nodeType := reflect.TypeFor[finaliserTestNode]()
	plainType := reflect.TypeFor[finaliserTestPlain]()

	for name, value := range map[string]reflect.Value{
		"pointer-bearing literal": allocateStructLiteralValue(vm, nodeType),
		"pointer-free literal":    allocateStructLiteralValue(vm, plainType),
		"boundary snapshot":       vm.acquireBoundarySnapshot(nodeType),
		"struct literal slot":     vm.acquireStructLiteralSlot(nodeType),
	} {
		pointer := value.Addr().UnsafePointer()
		require.False(t, vm.ownsInterpreterStorage(pointer), "%s: not interpreter storage", name)
		require.False(t, vm.Arena.ownsBytePointer(pointer), "%s: not in the byte slab", name)
	}
	require.Empty(t, vm.boundarySnapshotChunks, "no boundary chunk was made")

	arenaValue := unsafeNewAt(reflectValueABIType(plainType), vm.Arena.AllocBytes(plainType.Size(), uintptr(plainType.Align())), reflect.Struct)
	arenaValue.Field(0).SetInt(7)
	granted := &CallFrame{Function: &program.CompiledFunction{ArenaSafeAllocPCs: map[int]bool{0: true}}, ProgramCounter: 1}
	promoted := promoteAddressedValue(vm, granted, arenaValue)
	require.False(t, vm.Arena.ownsBytePointer(promoted.Addr().UnsafePointer()), "an addressed arena value is promoted to the heap, even where the escape pass grants the arena")
	require.False(t, vm.ownsInterpreterStorage(promoted.Addr().UnsafePointer()))
	require.Equal(t, int64(7), promoted.Field(0).Int(), "the promoted copy holds the value")
}

func TestAProgramWithoutFinalisersKeepsItsPlacements(t *testing.T) {
	vm := newFinaliserTestVM(t, false)
	defer vm.Arena.Reset()
	require.False(t, vm.allocatesForFinalisers())
	slot := vm.acquireBoundarySnapshot(reflect.TypeFor[finaliserTestNode]())
	require.True(t, vm.ownsInterpreterStorage(slot.Addr().UnsafePointer()), "a boundary slot is interpreter storage")
	plain := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestPlain]())
	require.True(t, vm.Arena.ownsBytePointer(plain.Addr().UnsafePointer()), "a pointer-free literal takes the byte slab")
}

func TestSetFinalizerOnInterpreterStorageIsCountedAndRecorded(t *testing.T) {
	vm := newFinaliserTestVM(t, false)
	defer vm.Arena.Reset()
	slot := vm.acquireBoundarySnapshot(reflect.TypeFor[finaliserTestNode]())
	pointer := slot.Addr()
	require.True(t, vm.ownsInterpreterStorage(pointer.UnsafePointer()))
	setFinaliser := reflect.ValueOf(runtime.SetFinalizer)
	finaliser := reflect.ValueOf(func(*finaliserTestNode) {})

	before := finaliserDrops.Load()
	_, consumed := dispatchSetFinalizerIntrinsic(vm, setFinaliser, []reflect.Value{pointer, reflect.Value{}})
	require.True(t, consumed, "a clearing call on interpreter storage is consumed")
	require.Equal(t, before, finaliserDrops.Load(), "clearing registers nothing, so nothing is dropped")

	_, consumed = dispatchSetFinalizerIntrinsic(vm, setFinaliser, []reflect.Value{pointer, finaliser})
	require.True(t, consumed, "the real function would terminate the process on a slot")
	require.Equal(t, before+1, finaliserDrops.Load(), "the drop is counted")
	require.True(t, finaliserDropLogged.Load(), "and logged")
	require.True(t, vm.allocatesForFinalisers(), "the program is recorded as setting finalisers")
	fresh := vm.acquireBoundarySnapshot(reflect.TypeFor[finaliserTestNode]())
	require.False(t, vm.ownsInterpreterStorage(fresh.Addr().UnsafePointer()), "later values are Go allocations")
}

func TestSetFinalizerOnAGoAllocationRegistersATrampoline(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	value := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestNode]())
	result, consumed := dispatchSetFinalizerIntrinsic(vm, reflect.ValueOf(runtime.SetFinalizer),
		[]reflect.Value{value.Addr(), reflect.ValueOf(func(*finaliserTestNode) {})})
	require.True(t, consumed, "the intrinsic registers the finaliser itself, through a trampoline")
	require.Equal(t, opContinue, result)
	require.NoError(t, vm.registerFinaliser(value.Addr(), reflect.Value{}), "and clears it")
	_, consumed = dispatchSetFinalizerIntrinsic(vm, reflect.ValueOf(runtime.GC), nil)
	require.False(t, consumed, "another callee is not the intrinsic's")
}

type finaliserMisuseCase struct {
	arguments func(vm *VM) (object reflect.Value, finaliser reflect.Value)
	name      string
	message   string
}

func finaliserMisuseCases() []finaliserMisuseCase {
	node := func(vm *VM) reflect.Value {
		return allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
	}
	noop := reflect.ValueOf(func(*finaliserTestLinked) {})
	return []finaliserMisuseCase{
		{name: "a nil first argument", message: "runtime.SetFinalizer: first argument is nil",
			arguments: func(*VM) (reflect.Value, reflect.Value) { return reflect.Value{}, reflect.Value{} }},
		{name: "a first argument that is not a pointer", message: "runtime.SetFinalizer: first argument is int, not pointer",
			arguments: func(*VM) (reflect.Value, reflect.Value) { return reflect.ValueOf(5), reflect.Value{} }},
		{name: "a nil pointer", message: "runtime.SetFinalizer: pointer not in allocated block",
			arguments: func(*VM) (reflect.Value, reflect.Value) { return reflect.ValueOf((*finaliserTestLinked)(nil)), noop }},
		{name: "a finaliser that is not a function", message: "runtime.SetFinalizer: second argument is int, not a function",
			arguments: func(vm *VM) (reflect.Value, reflect.Value) { return node(vm), reflect.ValueOf(42) }},
		{name: "a variadic finaliser", message: "runtime.SetFinalizer: cannot pass *engine.finaliserTestLinked to finalizer func(...*engine.finaliserTestLinked) because dotdotdot",
			arguments: func(vm *VM) (reflect.Value, reflect.Value) {
				return node(vm), reflect.ValueOf(func(...*finaliserTestLinked) {})
			}},
		{name: "a finaliser of two parameters", message: "runtime.SetFinalizer: cannot pass *engine.finaliserTestLinked to finalizer func(*engine.finaliserTestLinked, int)",
			arguments: func(vm *VM) (reflect.Value, reflect.Value) {
				return node(vm), reflect.ValueOf(func(*finaliserTestLinked, int) {})
			}},
		{name: "a finaliser whose parameter does not accept the object", message: "runtime.SetFinalizer: cannot pass *engine.finaliserTestLinked to finalizer func(*int)",
			arguments: func(vm *VM) (reflect.Value, reflect.Value) { return node(vm), reflect.ValueOf(func(*int) {}) }},
		{name: "a pointer to a field", message: "runtime.SetFinalizer: pointer not at beginning of allocated block",
			arguments: func(vm *VM) (reflect.Value, reflect.Value) {
				named := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestNamed]())
				return named.Field(1).Addr(), reflect.ValueOf(func(*string) {})
			}},
		{name: "a pointer the program did not allocate", message: "runtime.SetFinalizer: pointer not at beginning of allocated block",
			arguments: func(*VM) (reflect.Value, reflect.Value) { return reflect.ValueOf(new(finaliserTestLinked)), noop }},
		{name: "a second finaliser", message: "runtime.SetFinalizer: finalizer already set",
			arguments: func(vm *VM) (reflect.Value, reflect.Value) {
				object := node(vm)
				if err := vm.registerFinaliser(object, noop); err != nil {
					panic(err)
				}
				return object, noop
			}},
	}
}

func TestSetFinalizerMisuseRaisesGosMessageAsAPanic(t *testing.T) {
	for _, tc := range finaliserMisuseCases() {
		t.Run(tc.name, func(t *testing.T) {
			vm := newFinaliserTestVM(t, true)
			defer vm.Arena.Reset()
			object, finaliser := tc.arguments(vm)
			err := vm.registerFinaliser(object, finaliser)
			require.ErrorIs(t, err, errFinaliserMisuse)
			require.Equal(t, tc.message, err.Error(), "the program sees Go's own message")
		})
	}
}

func TestSetFinalizerMisuseNeverReachesTheRuntime(t *testing.T) {
	if !inChildProcessTest(t) {
		output := runChildProcessTest(t)
		require.NotContains(t, output, "fatal error", "runtime.SetFinalizer ends the process for each of these calls")
		return
	}
	setFinalizer := reflect.ValueOf(runtime.SetFinalizer)
	for _, tc := range finaliserMisuseCases() {
		vm := newFinaliserTestVM(t, true)
		object, finaliser := tc.arguments(vm)
		arguments := []reflect.Value{interfaceArgument(object), interfaceArgument(finaliser)}
		func() {
			defer func() { _ = recover() }()
			if _, consumed := dispatchSetFinalizerIntrinsic(vm, setFinalizer, arguments); !consumed {
				setFinalizer.Call(arguments)
			}
		}()
		vm.Arena.Reset()
	}
}

func interfaceArgument(value reflect.Value) reflect.Value {
	held := reflect.New(reflect.TypeFor[any]()).Elem()
	if value.IsValid() {
		held.Set(value)
	}
	return held
}

func TestASmallPointerFreeFieldTakesAFinaliserAsInGo(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	ran := make(chan error, 1)
	func() {
		object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Field(1).Addr()
		require.NoError(t, vm.registerFinaliser(object, reflect.ValueOf(func(*int64) { ran <- nil })),
			"Go accepts a pointer to a small pointer-free object anywhere in an allocation")
		require.ErrorIs(t, vm.registerFinaliser(object, reflect.ValueOf(func(*int64) {})), errFinaliserMisuse, "but only one finaliser")
	}()
	awaitFinaliser(t, ran)
}

func TestTheFirstElementOfAMadeSliceTakesAFinaliser(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	ran := make(chan error, 1)
	func() {
		slice := recordFinaliserBacking(vm, reflect.MakeSlice(reflect.TypeFor[[]*finaliserTestLinked](), 2, 2))
		require.NoError(t, vm.registerFinaliser(slice.Index(0).Addr(), reflect.ValueOf(func(**finaliserTestLinked) { ran <- nil })))
		err := vm.registerFinaliser(slice.Index(1).Addr(), reflect.ValueOf(func(**finaliserTestLinked) {}))
		require.ErrorIs(t, err, errFinaliserMisuse, "a later element is not the start of the backing array")
	}()
	awaitFinaliser(t, ran)
}

func TestAZeroSizeObjectTakesNoFinaliser(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	object := recordFinaliserPointer(vm, reflect.New(reflect.TypeFor[struct{}]()))
	finaliser := reflect.ValueOf(func(*struct{}) {})
	require.NoError(t, vm.registerFinaliser(object, finaliser))
	require.NoError(t, vm.registerFinaliser(object, finaliser), "Go ignores both calls, so neither is refused")
}

func TestAFinaliserRunsOnTheWorkerAndMayBeSetAgain(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	ran := make(chan error, 1)
	register := func() {
		object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
		object.Elem().Field(1).SetInt(7)
		require.NoError(t, vm.registerFinaliser(object, reflect.ValueOf(func(node *finaliserTestLinked) {
			ran <- vm.registerFinaliser(reflect.ValueOf(node), reflect.ValueOf(func(*finaliserTestLinked) {}))
		})))
	}
	register()
	before := guestFinalisers.ran.Load()
	awaitFinaliser(t, ran)
	require.Greater(t, guestFinalisers.ran.Load(), before, "the worker ran it")
}

func awaitFinaliser(t *testing.T, ran <-chan error) {
	t.Helper()
	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case err := <-ran:
			require.NoError(t, err, "a finaliser may set the object's finaliser again, as in Go")
			return true
		default:
			return false
		}
	}, 10*time.Second, 10*time.Millisecond)
}

func TestAClearedFinaliserDoesNotRun(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	ran := make(chan error, 2)
	func() {
		cleared := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
		require.NoError(t, vm.registerFinaliser(cleared, reflect.ValueOf(func(*finaliserTestLinked) { ran <- errors.New("a cleared finaliser ran") })))
		require.NoError(t, vm.registerFinaliser(cleared, reflect.Value{}))
		require.NoError(t, vm.registerFinaliser(cleared, reflect.ValueOf(func(*finaliserTestLinked) { ran <- nil })),
			"a cleared object takes a new finaliser")
	}()
	awaitFinaliser(t, ran)
}

func TestAPanickingFinaliserEndsNeitherTheProcessNorOtherFinalisers(t *testing.T) {
	if !inChildProcessTest(t) {
		runChildProcessTest(t)
		return
	}
	vm := newFinaliserTestVM(t, true)
	ran := make(chan error, 1)
	panicked := guestFinalisers.panicked.Load()
	func() {
		object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
		require.NoError(t, vm.registerFinaliser(object, reflect.ValueOf(func(*finaliserTestLinked) { panic("finaliser panic") })))
	}()
	require.Eventually(t, func() bool {
		runtime.GC()
		return guestFinalisers.panicked.Load() > panicked
	}, 10*time.Second, 10*time.Millisecond, "the panic was recovered on the worker")
	func() {
		object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
		require.NoError(t, vm.registerFinaliser(object, reflect.ValueOf(func(*finaliserTestLinked) { ran <- nil })))
	}()
	awaitFinaliser(t, ran)
}

func TestABlockedFinaliserDoesNotStallTheRuntimesFinalisers(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	release := make(chan struct{})
	entered := make(chan struct{})
	func() {
		object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
		require.NoError(t, vm.registerFinaliser(object, reflect.ValueOf(func(*finaliserTestLinked) {
			close(entered)
			<-release
		})))
	}()
	defer close(release)
	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case <-entered:
			return true
		default:
			return false
		}
	}, 10*time.Second, 10*time.Millisecond)

	hostRan := make(chan struct{})
	func() {
		host := new(finaliserTestLinked)
		runtime.SetFinalizer(host, func(*finaliserTestLinked) { close(hostRan) })
	}()
	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case <-hostRan:
			return true
		default:
			return false
		}
	}, 10*time.Second, 10*time.Millisecond, "the host's finalisers run while a guest finaliser blocks")
}

func TestASliceHeaderOfAProgramThatSetsFinalisersTakesAFinaliser(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	vm.bindFinaliserPlacement()
	header := vm.Arena.allocSliceHeader()
	pointer := reflect.ValueOf(header)

	finaliserAllocations.mu.Lock()
	state := finaliserAllocations.stateLocked(pointer)
	finaliserAllocations.mu.Unlock()
	require.Equal(t, finaliserAllocationFree, state, "the header is recorded as a whole allocation")
}
