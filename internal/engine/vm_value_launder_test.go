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
)

type launderProbe struct {
	boolField    bool
	intField     int
	uintField    uint32
	floatField   float64
	stringField  string
	sliceField   []int
	mapField     map[string]int
	pointerField *int
	structField  struct{ inner int }
}

func TestLaunderReadOnlyFieldValueStripsTaint(t *testing.T) {
	t.Parallel()
	pointee := 99
	probe := launderProbe{
		boolField:    true,
		intField:     -7,
		uintField:    5,
		floatField:   2.5,
		stringField:  "aliased",
		sliceField:   []int{1, 2, 3},
		mapField:     map[string]int{"k": 4},
		pointerField: &pointee,
		structField:  struct{ inner int }{inner: 11},
	}

	nonAddressable := reflect.ValueOf(probe)
	addressable := reflect.ValueOf(&probe).Elem()

	for _, source := range []struct {
		name  string
		value reflect.Value
	}{
		{name: "nonAddressable", value: nonAddressable},
		{name: "addressable", value: addressable},
	} {
		t.Run(source.name, func(t *testing.T) {
			for fieldIndex := range source.value.NumField() {
				field := source.value.Field(fieldIndex)
				require.Falsef(t, field.CanInterface(), "field %d must start tainted", fieldIndex)
				laundered := launderReadOnlyFieldValue(field)
				require.Truef(t, laundered.CanInterface(),
					"field %d must be laundered to a usable value", fieldIndex)
				require.NotPanics(t, func() { _ = laundered.Interface() })
			}
		})
	}

	launderedSlice := launderReadOnlyFieldValue(nonAddressable.Field(5))
	require.Equal(t, probe.sliceField, launderedSlice.Interface())
	launderedMap := launderReadOnlyFieldValue(nonAddressable.Field(6))
	require.Equal(t, probe.mapField, launderedMap.Interface())

	destination := reflect.New(reflect.TypeFor[int]()).Elem()
	require.NotPanics(t, func() {
		destination.Set(launderReadOnlyFieldValue(nonAddressable.Field(1)))
	})
	require.Equal(t, probe.intField, int(destination.Int()))
}
