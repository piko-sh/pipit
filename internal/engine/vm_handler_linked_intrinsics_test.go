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
	"fmt"
	"io/fs"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/stdlink"
)

func TestOnlyTheSiblingsThatDependOnScriptTypesAreIntrinsics(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	site := &program.CallSite{LinkedTypeArgs: []reflect.Type{reflect.TypeFor[int]()}}
	_, _, handled := dispatchLinkedIntrinsic(vm, site, reflect.ValueOf(stdlink.RandN), []reflect.Value{reflect.ValueOf(3)})
	require.False(t, handled, "rand.N is the same for every program")

	results, panicValue, handled := dispatchLinkedIntrinsic(vm, site, reflect.ValueOf(stdlink.ReflectTypeFor), nil)
	require.True(t, handled)
	require.Nil(t, panicValue)
	require.Equal(t, reflect.TypeFor[int](), results[0].Interface())
}

func TestTheTypeAssertIntrinsicAssertsAsAProgramWould(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	results := linkedReflectTypeAssert(vm, reflect.TypeFor[fmt.Stringer](), reflect.ValueOf(reflect.ValueOf(time.Second)))
	require.True(t, results[1].Bool())
	require.Equal(t, "1s", results[0].Interface().(fmt.Stringer).String())

	results = linkedReflectTypeAssert(vm, reflect.TypeFor[string](), reflect.ValueOf(reflect.ValueOf(3)))
	require.False(t, results[1].Bool())
	require.Equal(t, "", results[0].String())

	site := &program.CallSite{LinkedTypeArgs: []reflect.Type{reflect.TypeFor[int]()}}
	_, panicValue, handled := dispatchLinkedIntrinsic(vm, site, reflect.ValueOf(stdlink.ReflectTypeAssert), []reflect.Value{reflect.ValueOf(reflect.Value{})})
	require.True(t, handled)
	require.NotNil(t, panicValue, "the zero Value panics in the program, as Value.Interface does")
}

func TestTheAsTypeIntrinsicSearchesTheChain(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	err := fmt.Errorf("outer: %w", &fs.PathError{Op: "open", Path: "/p", Err: fs.ErrNotExist})
	results := linkedErrorsAsType(vm, reflect.TypeFor[*fs.PathError](), reflect.ValueOf(&err).Elem())
	require.True(t, results[1].Bool())
	require.Equal(t, "/p", results[0].Interface().(*fs.PathError).Path)
}

func addCleanupSite() *program.CallSite {
	return &program.CallSite{LinkedTypeArgs: []reflect.Type{reflect.TypeFor[finaliserTestLinked](), reflect.TypeFor[int64]()}}
}

func TestACleanupRunsOnTheFinaliserWorker(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	ran := make(chan error, 1)
	before := guestFinalisers.ran.Load()
	func() {
		object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
		cleanup := reflect.ValueOf(func(argument any) {
			if argument != int64(9) {
				ran <- fmt.Errorf("cleanup argument %v", argument)
				return
			}
			ran <- nil
		})
		results, panicValue, handled := dispatchLinkedIntrinsic(vm, addCleanupSite(), reflect.ValueOf(stdlink.RuntimeAddCleanup),
			[]reflect.Value{object, cleanup, reflect.ValueOf(int64(9))})
		require.True(t, handled)
		require.Nil(t, panicValue)
		require.Len(t, results, 1)
	}()
	awaitFinaliser(t, ran)
	require.Greater(t, guestFinalisers.ran.Load(), before, "the worker ran the cleanup, not the runtime's goroutine")
}

func TestAPanickingCleanupIsRecovered(t *testing.T) {
	if !inChildProcessTest(t) {
		runChildProcessTest(t)
		return
	}
	vm := newFinaliserTestVM(t, true)
	panicked := guestFinalisers.panicked.Load()
	func() {
		object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()
		_, panicValue, _ := dispatchLinkedIntrinsic(vm, addCleanupSite(), reflect.ValueOf(stdlink.RuntimeAddCleanup),
			[]reflect.Value{object, reflect.ValueOf(func(any) { panic("cleanup panic") }), reflect.ValueOf(int64(1))})
		require.Nil(t, panicValue)
	}()
	require.Eventually(t, func() bool {
		runtime.GC()
		return guestFinalisers.panicked.Load() > panicked
	}, 10*time.Second, 10*time.Millisecond)
}

func TestAddCleanupRefusesWhatItCannotAttach(t *testing.T) {
	vm := newFinaliserTestVM(t, true)
	defer vm.Arena.Reset()
	sibling := reflect.ValueOf(stdlink.RuntimeAddCleanup)
	object := allocateStructLiteralValue(vm, reflect.TypeFor[finaliserTestLinked]()).Addr()

	_, panicValue, handled := dispatchLinkedIntrinsic(vm, addCleanupSite(), sibling,
		[]reflect.Value{object, reflect.ValueOf(42), reflect.ValueOf(int64(1))})
	require.True(t, handled)
	require.ErrorIs(t, panicValue.(error), errCleanupNotFunction, "a cleanup that is not a function has its own sentinel")

	placed := newFinaliserTestVM(t, false)
	defer placed.Arena.Reset()
	slot := placed.acquireBoundarySnapshot(reflect.TypeFor[finaliserTestLinked]())
	require.True(t, placed.ownsInterpreterStorage(slot.Addr().UnsafePointer()))
	_, panicValue, _ = dispatchLinkedIntrinsic(placed, addCleanupSite(), sibling,
		[]reflect.Value{slot.Addr(), reflect.ValueOf(func(any) {}), reflect.ValueOf(int64(1))})
	require.ErrorIs(t, panicValue.(error), errCleanupOnInterpreterStorage, "an arena may reuse the slot")
	require.True(t, placed.allocatesForFinalisers(), "and the program's later values are Go allocations")

	_, panicValue, handled = dispatchLinkedIntrinsic(vm, &program.CallSite{}, sibling, []reflect.Value{object})
	require.True(t, handled)
	require.ErrorIs(t, panicValue.(error), errLinkedIntrinsicShape, "a malformed call site is refused, not indexed past its end")
}
