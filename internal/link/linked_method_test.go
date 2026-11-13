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

package link_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/link"
)

func TestWrapMethodInfersTheShapeFromTheSibling(t *testing.T) {
	t.Parallel()

	sibling := func(typeArg reflect.Type, receiver any, value int) int { return value + 1 }
	linked := link.WrapMethod(1, sibling)

	require.Equal(t, 1, linked.TypeArgCount)
	require.Nil(t, linked.Params, "a method wrapped without descriptors takes its shape from the sibling")
	require.Nil(t, linked.Results)
	require.False(t, linked.Variadic)
	results := linked.Target.Call([]reflect.Value{reflect.ValueOf(reflect.TypeFor[int]()), reflect.ValueOf(struct{}{}), reflect.ValueOf(2)})
	require.Equal(t, 3, int(results[0].Int()))
}

func TestWrapMethodFuncRecordsTheDeclaredShape(t *testing.T) {
	t.Parallel()

	sibling := func(typeArg reflect.Type, receiver any, values ...int) error { return nil }
	params := []link.GenericFieldType{{Kind: link.FieldKindSlice, Element: &link.GenericFieldType{Kind: link.FieldKindTypeArg, TypeArgIndex: 0}}}
	results := []link.GenericFieldType{{Kind: link.FieldKindError}}
	linked := link.WrapMethodFunc(1, sibling, params, results, true)

	require.Equal(t, 1, linked.TypeArgCount)
	require.Equal(t, params, linked.Params)
	require.Equal(t, results, linked.Results)
	require.True(t, linked.Variadic)
	require.Equal(t, reflect.Func, linked.Target.Kind())
}

func TestMethodSymbolKeyJoinsTypeAndMethod(t *testing.T) {
	t.Parallel()

	require.Equal(t, "List.Map", link.MethodSymbolKey("List", "Map"))
	require.Equal(t, "List"+link.MethodSymbolSeparator+"Map", link.MethodSymbolKey("List", "Map"))
}
