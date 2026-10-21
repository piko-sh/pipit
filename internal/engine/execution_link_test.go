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
	"errors"
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

func TestExecutionLinkRelaysCancellationButNotMainReturned(t *testing.T) {
	t.Parallel()

	errStop := errors.New("stop")
	newLink := func(t *testing.T) (*executionLink, context.Context, context.CancelCauseFunc) {
		t.Helper()
		ctx, cancel := context.WithCancelCause(t.Context())
		t.Cleanup(func() { cancel(nil) })
		return newExecutionLink(ctx, cancel), ctx, cancel
	}

	t.Run("a cancellation reaches a callback that started before it", func(t *testing.T) {
		t.Parallel()
		link, _, cancel := newLink(t)
		callbackCtx := link.callbackContext()

		cancel(errStop)

		require.Eventually(t, func() bool { return callbackCtx.Err() != nil }, 5*time.Second, time.Millisecond)
		require.ErrorIs(t, context.Cause(callbackCtx), errStop)
		require.True(t, link.live(), "a cancelled execution has not returned yet")
	})

	t.Run("a callback that starts after the cancellation stops at once", func(t *testing.T) {
		t.Parallel()
		link, _, cancel := newLink(t)
		cancel(errStop)

		callbackCtx := link.callbackContext()

		require.Error(t, callbackCtx.Err())
		require.ErrorIs(t, context.Cause(callbackCtx), errStop)
	})

	t.Run("main returning ends the link without stopping a running callback", func(t *testing.T) {
		t.Parallel()
		link, _, cancel := newLink(t)
		running := link.callbackContext()

		cancel(fault.ErrMainReturned)

		require.Eventually(t, func() bool { return !link.live() }, 5*time.Second, time.Millisecond)
		require.NoError(t, running.Err(), "a callback running when main returns finishes")
		require.Nil(t, link.callbackContext().Done(), "a callback that starts later runs detached")
	})

	t.Run("a callback started before the end keeps running after a later cancellation", func(t *testing.T) {
		t.Parallel()
		link, _, cancel := newLink(t)
		running := link.callbackContext()

		link.end()
		cancel(errStop)

		require.Never(t, func() bool { return running.Err() != nil }, 50*time.Millisecond, time.Millisecond)
		require.Nil(t, link.callbackContext().Done())
	})

	t.Run("fail cancels the running execution with its cause", func(t *testing.T) {
		t.Parallel()
		link, ctx, _ := newLink(t)

		require.True(t, link.fail(fault.ErrCallbackTimeout))

		require.ErrorIs(t, link.failedError(), fault.ErrCallbackTimeout)
		require.ErrorIs(t, link.failedError(), fault.ErrExecutionCancelled)
		require.Error(t, ctx.Err())
		require.ErrorIs(t, context.Cause(ctx), fault.ErrCallbackTimeout)
	})

	t.Run("fail leaves an ended execution alone", func(t *testing.T) {
		t.Parallel()
		link, ctx, _ := newLink(t)
		link.end()

		require.False(t, link.fail(fault.ErrCallbackTimeout))
		require.NoError(t, ctx.Err())
	})

	t.Run("fail without a cancel function is reported when the execution returns", func(t *testing.T) {
		t.Parallel()
		link := newExecutionLink(t.Context(), nil)
		require.NoError(t, link.failedError())

		require.True(t, link.fail(fault.ErrCallbackTimeout))

		require.ErrorIs(t, link.failedError(), fault.ErrCallbackTimeout)
		require.ErrorIs(t, link.failedError(), fault.ErrExecutionCancelled)
	})

	t.Run("goroutines follow the execution while it runs and run detached after", func(t *testing.T) {
		t.Parallel()
		link, ctx, _ := newLink(t)
		require.Equal(t, ctx, link.spawnContext())

		link.end()

		require.Nil(t, link.spawnContext().Done())
	})

	t.Run("a link outside any execution is ended", func(t *testing.T) {
		t.Parallel()
		link := endedExecutionLink(t.Context())

		require.False(t, link.live())
		require.Nil(t, link.callbackContext().Done())
	})
}

func TestCallbackSpecDoesNotPinParentVM(t *testing.T) {
	t.Parallel()

	vm := NewVM(context.WithoutCancel(t.Context()), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	vm.beginExecution(false)
	spec := newCallbackVMSpec(vm)
	parent := weak.Make(vm)
	vm = nil

	require.Eventually(t, func() bool {
		runtime.GC()
		return parent.Value() == nil
	}, 5*time.Second, 10*time.Millisecond, "a stored wrapper must not keep the VM that made it alive")
	runtime.KeepAlive(spec)
}

func TestExecuteWithBorrowedArenaEndsLink(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	vm.Arena = NewRegisterArena()
	vm.SetExecutionCancel(func(error) {})
	link := vm.currentLink()
	require.True(t, link.live(), "a wrapper made while the execution runs follows it")

	_, err := vm.Execute(returnIntProgram(1))

	require.NoError(t, err)
	require.False(t, link.live(), "an execution that borrowed its arena still ends its link")
	require.False(t, newCallbackVMSpec(vm).link.live(), "a wrapper made after the execution runs detached")
}

func TestExecutionWithoutWrappersBuildsNoLink(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)

	_, err := vm.Execute(returnIntProgram(1))

	require.NoError(t, err)
	require.Nil(t, vm.link, "an execution that starts nothing outside its loop allocates no link")
}
