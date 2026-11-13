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

package program

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type (
	receiverOne struct{ v int }
	receiverTwo struct{ v string }
)

func receiverMethodRoot() *CompiledFunction {
	root := NewNamedFunction("<root>")
	root.Functions = []*CompiledFunction{
		NewNamedFunction("Box.Get"), NewNamedFunction("Box.Get[one]"), NewNamedFunction("Box.Get[two]"),
		NewNamedFunction("Other.Get"),
	}
	RegisterReceiverMethod(root, reflect.TypeFor[receiverOne](), 0, 1)
	RegisterReceiverMethod(root, reflect.TypeFor[receiverTwo](), 0, 2)
	return root
}

func TestReceiverMethodBodyNamesTheBodyForTheReceiver(t *testing.T) {
	t.Parallel()
	root := receiverMethodRoot()

	body, ok := root.ReceiverMethodBody(reflect.TypeFor[receiverOne](), root.Functions[0])
	require.True(t, ok)
	require.Same(t, root.Functions[1], body)

	body, ok = root.ReceiverMethodBody(reflect.TypeFor[*receiverTwo](), root.Functions[0])
	require.True(t, ok)
	require.Same(t, root.Functions[2], body, "a pointer receiver is keyed on its element type")
}

func TestReceiverMethodBodyRefusesAReceiverWithNoBody(t *testing.T) {
	t.Parallel()
	root := receiverMethodRoot()

	body, ok := root.ReceiverMethodBody(reflect.TypeFor[struct{ other bool }](), root.Functions[0])

	require.False(t, ok, "a method with bodies for other receivers has none for this one")
	require.Same(t, root.Functions[0], body)
}

func TestReceiverMethodBodyLeavesOtherMethodsAlone(t *testing.T) {
	t.Parallel()
	root := receiverMethodRoot()

	body, ok := root.ReceiverMethodBody(reflect.TypeFor[receiverOne](), root.Functions[3])
	require.True(t, ok)
	require.Same(t, root.Functions[3], body)
	require.False(t, root.HasReceiverBodies(root.Functions[3]))
	require.True(t, root.HasReceiverBodies(root.Functions[0]))

	empty := NewNamedFunction("<empty>")
	body, ok = empty.ReceiverMethodBody(reflect.TypeFor[receiverOne](), root.Functions[0])
	require.True(t, ok)
	require.Same(t, root.Functions[0], body)
}

func TestRegisterReceiverMethodIgnoresAnIndexOutsideTheFunctionTable(t *testing.T) {
	t.Parallel()
	root := NewNamedFunction("<root>")
	root.Functions = []*CompiledFunction{NewNamedFunction("Box.Get")}

	RegisterReceiverMethod(root, reflect.TypeFor[receiverOne](), 0, 5)

	require.Empty(t, root.ReceiverMethods())
	require.False(t, root.HasReceiverBodies(root.Functions[0]))
}

func TestSetReceiverMethodsRebuildsTheTable(t *testing.T) {
	t.Parallel()
	root := receiverMethodRoot()
	rebuilt := NewNamedFunction("<rebuilt>")
	rebuilt.Functions = root.Functions

	rebuilt.SetReceiverMethods(root.ReceiverMethods())

	require.Equal(t, root.ReceiverMethods(), rebuilt.ReceiverMethods())
	body, ok := rebuilt.ReceiverMethodBody(reflect.TypeFor[receiverTwo](), rebuilt.Functions[0])
	require.True(t, ok)
	require.Same(t, rebuilt.Functions[2], body)
}
