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
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func fillNonZero(t *testing.T, value reflect.Value) {
	t.Helper()
	if !value.CanSet() {
		value = reflect.NewAt(value.Type(), unsafe.Pointer(value.UnsafeAddr())).Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value.SetUint(7)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1.5)
	case reflect.Complex64, reflect.Complex128:
		value.SetComplex(1 + 2i)
	case reflect.String:
		value.SetString("stale")
	case reflect.Pointer:
		value.Set(reflect.New(value.Type().Elem()))
	case reflect.UnsafePointer:
		stale := new(int)
		value.SetPointer(unsafe.Pointer(stale))
	case reflect.Slice:
		slice := reflect.MakeSlice(value.Type(), 1, 1)
		fillNonZero(t, slice.Index(0))
		value.Set(slice)
	case reflect.Map:
		value.Set(reflect.MakeMap(value.Type()))
	case reflect.Chan:
		channel := reflect.MakeChan(reflect.ChanOf(reflect.BothDir, value.Type().Elem()), 0)
		value.Set(channel.Convert(value.Type()))
	case reflect.Func:
		value.Set(reflect.MakeFunc(value.Type(), func([]reflect.Value) []reflect.Value {
			panic("a stale function survived the reset")
		}))
	case reflect.Interface:
		for _, candidate := range []any{errors.New("stale"), t.Context(), new(bytes.Buffer), reflect.TypeFor[int](), 42} {
			if reflect.TypeOf(candidate).Implements(value.Type()) {
				value.Set(reflect.ValueOf(candidate))
				return
			}
		}

	case reflect.Struct:
		for _, field := range value.Fields() {
			fillNonZero(t, field)
		}
	case reflect.Array:
		for index := range value.Len() {
			fillNonZero(t, value.Index(index))
		}
	default:
		require.Failf(t, "unhandled kind", "fillNonZero: unhandled kind %s", value.Kind())
	}
}

func TestVMShellResetClearsEveryField(t *testing.T) {
	t.Parallel()
	vm := new(VM)
	fillNonZero(t, reflect.ValueOf(vm).Elem())
	cache := vm.callbackVMs
	cache.built.Store(3)
	cache.discarded.Store(2)
	cache.released.Store(true)
	cache.slot.Store(new(VM))
	snapshots := vm.rootSnapshots
	require.NotNil(t, snapshots[0])
	restores := vm.argumentRestores
	require.NotNil(t, restores)

	vm.resetShell()

	value := reflect.ValueOf(vm).Elem()
	for index := range value.NumField() {
		field := value.Type().Field(index)
		switch field.Name {
		case "callbackVMs", "rootSnapshots", "argumentRestores":
			continue
		}
		require.Truef(t, value.Field(index).IsZero(), "field %s survived the reset", field.Name)
	}
	require.Same(t, cache, vm.callbackVMs, "the VM keeps its own cache")
	require.True(t, reflect.ValueOf(cache).Elem().IsZero(), "the kept cache is emptied")
	require.Empty(t, vm.rootSnapshots)
	require.Nil(t, snapshots[0], "the kept snapshot table is cleared")
	require.Same(t, restores, vm.argumentRestores, "the VM keeps its argument-restore records")
	require.True(t, reflect.ValueOf(restores).Elem().IsZero(), "the kept records are emptied")
}

func TestVMShellRecycledNewVMMatchesFresh(t *testing.T) {
	globals, ctx := NewGlobalStore(), context.WithoutCancel(t.Context())
	shell := new(VM)
	fillNonZero(t, reflect.ValueOf(shell).Elem())
	shell.resetShell()
	var rebuilt *VM
	for range 64 {
		vmShellPool.Put(shell)
		if candidate := NewVM(ctx, globals, nil); candidate == shell {
			rebuilt = candidate
			break
		}
	}
	if rebuilt == nil {
		t.Skip("the pool never handed the shell back (the race detector drops pooled items)")
	}
	require.Empty(t, rebuilt.rootSnapshots)
	rebuilt.rootSnapshots = nil
	if rebuilt.argumentRestores != nil {
		require.True(t, reflect.ValueOf(rebuilt.argumentRestores).Elem().IsZero(), "the kept records are empty")
		rebuilt.argumentRestores = nil
	}
	fresh := NewVM(ctx, globals, nil)
	require.True(t, reflect.DeepEqual(fresh, rebuilt), "a recycled VM differs from a fresh one")
}

func TestVMShellRecycleRefusals(t *testing.T) {
	cases := []struct {
		name   string
		dirty  func(vm *VM)
		pinned bool
	}{
		{name: "pinned by an adapter", dirty: func(vm *VM) { vm.pinIdentity() }, pinned: true},
		{name: "arena attached", dirty: func(vm *VM) { vm.Arena = new(RegisterArena) }},
		{name: "call stack attached", dirty: func(vm *VM) { vm.CallStack = make([]CallFrame, 1) }},
		{name: "frame still pushed", dirty: func(vm *VM) { vm.FramePointer = 0 }},
		{name: "debugger attached", dirty: func(vm *VM) { vm.Limits.Debug = new(DebugSession) }},
		{name: "debug thread registered", dirty: func(vm *VM) { vm.debugThread = new(DebugThread) }},
		{name: "safe mode", dirty: func(vm *VM) { vm.Limits.SafeMode = true }},
		{name: "watcher live", dirty: func(vm *VM) { vm.stopWatcher = func() bool { return true } }},
		{name: "watcher fired", dirty: func(vm *VM) { vm.watcherFired = true }},
		{name: "goroutines leaked", dirty: func(vm *VM) { vm.goroutinesLeaked = true }},
		{name: "unwinding leaked", dirty: func(vm *VM) { vm.unwindLeaked.Store(true) }},
		{name: "unwinding", dirty: func(vm *VM) { vm.abortUnwinding = true }},
		{name: "inside an unwinding native call", dirty: func(vm *VM) { vm.unwindNative.Store(1) }},
		{name: "interpreter lock held", dirty: func(vm *VM) { vm.holdsInterpreterLock = true }},
		{name: "gil hand-off pending", dirty: func(vm *VM) { vm.pendingGilHandoff = new(gilHandoff) }},
		{name: "callback VM parked in its cache", dirty: func(vm *VM) { vm.callbackVMs.slot.Store(new(VM)) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			read := InstallVMShellProbe()
			vm := NewVM(context.WithoutCancel(t.Context()), NewGlobalStore(), nil)
			globals := vm.Globals
			testCase.dirty(vm)
			vm.recycle()
			counts := read()
			require.Zero(t, counts.Parked)
			if testCase.pinned {
				require.EqualValues(t, 1, counts.RefusedPinned)
			} else {
				require.EqualValues(t, 1, counts.RefusedState)
			}
			require.Same(t, globals, vm.Globals, "a refused VM is left untouched")
		})
	}
	t.Run("clean", func(t *testing.T) {
		read := InstallVMShellProbe()
		vm := NewVM(context.WithoutCancel(t.Context()), NewGlobalStore(), nil)
		vm.recycle()
		counts := read()
		require.EqualValues(t, 1, counts.Parked)
		require.Nil(t, vm.Globals, "a parked VM is zeroed")
	})
}

func TestFinishWatcherRecordsAFiredWatcher(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	vm := NewVM(ctx, NewGlobalStore(), nil)
	cancel()
	require.Eventually(t, func() bool { return vm.Cancelled.Load() == 1 }, 5*time.Second, time.Millisecond)
	vm.FinishWatcher()
	require.True(t, vm.watcherFired)
	require.False(t, vm.recyclable())

	live := t.Context()
	quiet := NewVM(live, NewGlobalStore(), nil)
	quiet.FinishWatcher()
	require.False(t, quiet.watcherFired)
	require.Nil(t, quiet.stopWatcher)
	require.True(t, quiet.recyclable())
}
