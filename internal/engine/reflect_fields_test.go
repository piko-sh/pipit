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

type reflectFieldsProbe struct {
	pointer *int
	text    string
	bytes   []byte
	number  int
}

func TestStructFieldValueAllocatesNothing(t *testing.T) {
	probe := reflectFieldsProbe{pointer: nil, text: "x", bytes: nil, number: 1}
	value := reflect.ValueOf(&probe).Elem()
	pointers := 0

	helper := testing.AllocsPerRun(100, func() {
		for index := range value.NumField() {
			if structFieldValue(value, index).Kind() == reflect.Pointer {
				pointers++
			}
		}
	})
	iterator := testing.AllocsPerRun(100, func() {
		for _, field := range value.Fields() {
			if field.Kind() == reflect.Pointer {
				pointers++
			}
		}
	})

	require.Zero(t, helper)
	require.LessOrEqual(t, helper, iterator)
	require.Positive(t, pointers)
	t.Logf("allocations per walk: helper %v, Value.Fields %v", helper, iterator)
}

func TestStructFieldTypeAllocatesNothing(t *testing.T) {
	probeType := reflect.TypeFor[reflectFieldsProbe]()
	pointers := 0

	helper := testing.AllocsPerRun(100, func() {
		for index := range probeType.NumField() {
			if structFieldType(probeType, index).Type.Kind() == reflect.Pointer {
				pointers++
			}
		}
	})
	iterator := testing.AllocsPerRun(100, func() {
		for field := range probeType.Fields() {
			if field.Type.Kind() == reflect.Pointer {
				pointers++
			}
		}
	})

	require.Zero(t, helper)
	require.LessOrEqual(t, helper, iterator)
	require.Positive(t, pointers)
	t.Logf("allocations per walk: helper %v, Type.Fields %v", helper, iterator)
}
