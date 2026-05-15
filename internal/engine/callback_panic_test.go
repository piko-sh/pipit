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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallbackPanicRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		evalError error
		value     any
		wantWrap  bool
		wantStack string
	}{
		{name: "panic with a recorded stack is wrapped", evalError: &uncaughtPanicError{value: "boom", prefix: "panic: ", stack: "f()\n\tx.go:1"}, value: "boom", wantWrap: true, wantStack: "f()\n\tx.go:1"},
		{name: "panic without a stack passes through", evalError: errors.New("plain"), value: "boom", wantWrap: false, wantStack: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			child := &VM{panicValue: tc.value, evalError: tc.evalError}
			raised := wrapCallbackPanic(child)
			_, wrapped := raised.(*callbackPanic)
			require.Equal(t, tc.wantWrap, wrapped)
			value, stack := unwrapCallbackPanic(raised)
			require.Equal(t, tc.value, value)
			require.Equal(t, tc.wantStack, stack)
		})
	}
}

func TestPanicStackWithCallbackPrependsCallbackFrames(t *testing.T) {
	t.Parallel()
	vm := newGCTestVM()
	vm.callbackPanicStack = "inner()\n\tinner.go:3"
	rendered := vm.panicStackWithCallback()
	require.True(t, len(rendered) > len(vm.callbackPanicStack))
	require.Contains(t, rendered, "inner()\n\tinner.go:3\n"+callbackBoundaryMarker)

	vm.callbackPanicStack = ""
	require.Equal(t, vm.panicStack(), vm.panicStackWithCallback())
}

func TestCallbackPanicErrorRendersValue(t *testing.T) {
	t.Parallel()
	nested := &callbackPanic{value: errors.New("index out of range"), stack: "x"}
	require.Equal(t, "index out of range", nested.Error())
}
