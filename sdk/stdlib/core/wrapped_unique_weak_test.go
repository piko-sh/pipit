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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type plain struct {
	A int
}

func TestAnInterpretedTypeIsNamedAsSuchWhenNoInstantiationExists(t *testing.T) {
	t.Parallel()

	interpreted := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeFor[int]()},
		{Name: "_pipitID_key", PkgPath: "main", Type: reflect.TypeFor[struct{}]()},
	})
	require.Equal(t, "core.plain", describeArgumentType(reflect.TypeFor[plain]()))
	require.Equal(t, "nil", describeArgumentType(nil))

	value := reflect.New(interpreted).Interface()
	require.PanicsWithError(t,
		"no instantiation of this generic function for the type: weak.Make of a pointer to a type declared in the interpreted program",
		func() { wrappedWeakMake(value) },
		"the synthesised struct's printed form would mislead, so the error names it as the program's own type")
	require.PanicsWithError(t,
		"no instantiation of this generic function for the type: unique.Make of a type declared in the interpreted program",
		func() { wrappedUniqueMake(reflect.New(interpreted).Elem().Interface()) })
}
