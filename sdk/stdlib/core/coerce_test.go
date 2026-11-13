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

package core

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoerceDirectMatch(t *testing.T) {
	t.Parallel()

	require.Equal(t, 42, coerce[int](42))
	require.Equal(t, "hello", coerce[string]("hello"))
	require.True(t, coerce[bool](true))
	require.InDelta(t, 3.14, coerce[float64](3.14), 0)
}

func TestCoerceInt64ToInt(t *testing.T) {
	t.Parallel()

	require.Equal(t, 42, coerce[int](int64(42)))
}

func TestCoerceUint64ToByte(t *testing.T) {
	t.Parallel()

	require.Equal(t, byte(255), coerce[byte](uint64(255)))
}

func TestCoerceIntToFloat64(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 3, coerce[float64](int(3)), 0)
}

func TestCoerceInt64ToFloat64(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 7, coerce[float64](int64(7)), 0)
}

func TestValueAsGivesAYieldedValueTheTargetType(t *testing.T) {
	t.Parallel()

	stringerType := reflect.TypeFor[fmt.Stringer]()
	held := reflect.ValueOf([]any{customStringer("x")}).Index(0)
	require.True(t, valueAs(held, stringerType).Type().AssignableTo(stringerType))
	require.Equal(t, customStringer("x"), valueAs(held, stringerType).Interface())

	nilHeld := reflect.ValueOf([]any{nil}).Index(0)
	require.True(t, valueAs(nilHeld, stringerType).IsNil())
	require.Equal(t, 0, valueAs(nilHeld, reflect.TypeFor[int]()).Interface())
	require.Equal(t, customString("k"), valueAs(reflect.ValueOf("k"), reflect.TypeFor[customString]()).Interface())
	require.Equal(t, int64(3), valueAs(reflect.ValueOf(int64(3)), reflect.TypeFor[string]()).Interface(),
		"an int is not converted to a string")
}
