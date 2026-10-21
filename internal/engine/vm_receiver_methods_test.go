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

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

type receiverMethodValue struct{ v int }

func TestReceiverMethodTypeRemovesPointersAndInterfaces(t *testing.T) {
	t.Parallel()

	value := &receiverMethodValue{v: 1}
	var boxed any = value
	holder := reflect.ValueOf(&boxed).Elem()

	require.Equal(t, reflect.TypeFor[receiverMethodValue](), receiverMethodType(reflect.ValueOf(value)))
	require.Equal(t, reflect.TypeFor[receiverMethodValue](), receiverMethodType(holder),
		"an interface-held receiver is keyed on its dynamic type")
	require.Nil(t, receiverMethodType(reflect.Value{}))
}

func TestReceiverMethodBodyRetargetsAndRefuses(t *testing.T) {
	t.Parallel()

	root := program.NewNamedFunction("<root>")
	root.Functions = []*program.CompiledFunction{
		program.NewNamedFunction("Box.Get"), program.NewNamedFunction("Box.Get[spec:int]"),
	}
	program.RegisterReceiverMethod(root, reflect.TypeFor[receiverMethodValue](), 0, 1)

	body, err := receiverMethodBody(root, reflect.ValueOf(&receiverMethodValue{}), root.Functions[0])
	require.NoError(t, err)
	require.Same(t, root.Functions[1], body)

	_, err = receiverMethodBody(root, reflect.ValueOf(struct{ w string }{}), root.Functions[0])
	require.ErrorIs(t, err, fault.ErrNoMethodBodyForReceiver)
	require.Contains(t, err.Error(), "Box.Get")

	body, err = receiverMethodBody(nil, reflect.ValueOf(1), root.Functions[0])
	require.NoError(t, err)
	require.Same(t, root.Functions[0], body, "with no table the method runs as found")
}
