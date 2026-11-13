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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCurrentGoroutineIDDiffersBetweenGoroutines(t *testing.T) {
	here := currentGoroutineID()
	require.NotZero(t, here)
	require.Equal(t, here, currentGoroutineID(), "the id is stable on one goroutine")
	other := make(chan uint64, 1)
	go func() { other <- currentGoroutineID() }()
	require.NotEqual(t, here, <-other)
}

func TestAbortUnwindersStackPerGoroutine(t *testing.T) {
	require.Nil(t, abortUnwinderOnThisGoroutine(), "nothing unwinds yet")
	outer, inner := new(VM), new(VM)

	removeOuter := registerAbortUnwinder(outer)
	require.Same(t, outer, abortUnwinderOnThisGoroutine())
	removeInner := registerAbortUnwinder(inner)
	require.Same(t, inner, abortUnwinderOnThisGoroutine(), "the innermost unwinder is found")

	seen := make(chan *VM, 1)
	go func() { seen <- abortUnwinderOnThisGoroutine() }()
	require.Nil(t, <-seen, "another goroutine sees none of this one's unwinders")

	removeInner()
	require.Same(t, outer, abortUnwinderOnThisGoroutine())
	removeOuter()
	require.Nil(t, abortUnwinderOnThisGoroutine())
	require.Zero(t, abortUnwindersActive.Load())
}

func TestAdoptAbortUnwindSliceIfNestedNeedsAnUnwindingParent(t *testing.T) {
	vm := newTestVM(t)
	limits := vm.Limits
	vm.adoptAbortUnwindSliceIfNested()()
	require.Equal(t, limits, vm.Limits, "nothing unwinds, so nothing is adopted")

	remove := registerAbortUnwinder(vm)
	defer remove()
	vm.adoptAbortUnwindSliceIfNested()()
	require.False(t, vm.abortUnwinding, "a VM never adopts its own slice")
}

func TestAdoptAbortUnwindSliceRunsOnTheParentsAllowance(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(t.Context())
	defer cancelParent()
	parent := NewVM(parentCtx, NewGlobalStore(), nil)
	parent.CostRemaining = 500
	parent.Arena = NewRegisterArena()
	parent.Arena.MaxArenaBytes = 1 << 20
	remove := registerAbortUnwinder(parent)
	defer remove()

	child := NewVM(context.WithoutCancel(t.Context()), NewGlobalStore(), nil)
	child.CostRemaining = 7
	child.callbackDeadline = time.Now().Add(time.Hour)
	child.Arena = NewRegisterArena()
	child.Arena.MaxArenaBytes = 1 << 10
	childCtx, savedLimits := child.ctx, child.Limits

	settle := child.adoptAbortUnwindSliceIfNested()
	require.True(t, child.abortUnwinding)
	require.True(t, child.Limits.ForceGoDispatch, "budget refills and go statements are off")
	require.NotNil(t, child.Limits.CostTable)
	require.EqualValues(t, 500, child.CostRemaining, "the child spends the parent's remaining cost")
	require.Equal(t, parentCtx, child.ctx, "the child stops with the parent")
	require.True(t, child.callbackDeadline.IsZero())
	require.EqualValues(t, 1<<20, child.Arena.MaxArenaBytes, "the child may use the parent's arena headroom")
	require.True(t, child.Arena.disableMinorGC)
	require.Same(t, child, abortUnwinderOnThisGoroutine(), "a callback unwinding inside the child finds it")

	child.CostRemaining = 120
	settle()
	require.EqualValues(t, 120, parent.CostRemaining, "the unspent cost goes back to the parent")
	require.EqualValues(t, 7, child.CostRemaining)
	require.Equal(t, childCtx, child.ctx)
	require.Equal(t, savedLimits, child.Limits)
	require.False(t, child.abortUnwinding)
	require.EqualValues(t, 1<<10, child.Arena.MaxArenaBytes)
	require.False(t, child.Arena.disableMinorGC)
	require.Same(t, parent, abortUnwinderOnThisGoroutine())
}

func TestAdoptAbortUnwindSliceSpendsTheParentWhenExhausted(t *testing.T) {
	parent := newTestVM(t)
	parent.CostRemaining = 500
	child := newTestVM(t)

	settle := child.adoptAbortUnwindSlice(parent)
	child.abortUnwindExhausted = true
	settle()
	require.True(t, parent.abortUnwindExhausted, "an exhausted child exhausts the slice")
	require.Zero(t, parent.CostRemaining)
	require.False(t, child.abortUnwindExhausted)
}
