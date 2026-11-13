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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func scriptStructType(fields ...reflect.StructField) reflect.Type {
	fields = append(fields, reflect.StructField{Name: isa.SynthesisedIDFieldPrefix + "S", PkgPath: "main", Type: reflect.TypeFor[struct{}]()})
	return reflect.StructOf(fields)
}

func TestFieldRoutesShowOnlyTheFieldsAScriptStructDeclares(t *testing.T) {
	t.Parallel()

	inner := scriptStructType(reflect.StructField{Name: "ID", Type: reflect.TypeFor[int]()})
	outer := scriptStructType(
		reflect.StructField{Name: isa.EmbeddedUnexportedPrefix + "base", PkgPath: "main", Type: inner},
		reflect.StructField{Name: "Price", Type: reflect.TypeFor[float64]()},
	)
	require.True(t, structNeedsFieldRewrite(outer))
	require.False(t, structNeedsFieldRewrite(reflect.TypeFor[struct{ A int }]()))

	names := []string{}
	for field := range pipitStructTypeFields(outer) {
		names = append(names, field.Name)
	}
	require.Equal(t, []string{"base", "Price"}, names, "the sentinel is hidden and the renamed embedded field restored")

	visible := make([]string, 0, outer.NumField())
	for _, field := range pipitVisibleFields(outer) {
		visible = append(visible, field.Name)
	}
	require.Equal(t, []string{"base", "ID", "Price"}, visible, "fields promoted through the embedded struct are visible; no sentinel at any depth")
	require.Equal(t, []int{0, 0}, pipitVisibleFields(outer)[1].Index)

	require.Equal(t, "ID", pipitStructFieldByIndex(outer, []int{0, 0}).Name)
	require.PanicsWithValue(t, reflectTypeFieldIndexOutOfBounds, func() { pipitStructFieldByIndex(outer, []int{2}) },
		"the sentinel's index is past the last declared field")

	value := reflect.New(outer).Elem()
	value.Field(1).SetFloat(2.5)
	values := []float64{}
	for field, fieldValue := range pipitStructValueFields(value) {
		if field.Name == "Price" {
			values = append(values, fieldValue.Float())
		}
	}
	require.Equal(t, []float64{2.5}, values)
	require.InDelta(t, 2.5, pipitStructValueFieldByIndex(value, []int{1}).Float(), 0)
	require.PanicsWithValue(t, reflectFieldIndexOutOfRange, func() { pipitStructValueFieldByIndex(value, []int{2}) })
}

func TestTwoFieldsOfOneNameAtOneDepthHideEachOtherFromVisibleFields(t *testing.T) {
	t.Parallel()

	left := scriptStructType(reflect.StructField{Name: "Name", Type: reflect.TypeFor[string]()})
	right := reflect.StructOf([]reflect.StructField{{Name: "Name", Type: reflect.TypeFor[string]()}, {Name: "Extra", Type: reflect.TypeFor[int]()}})
	outer := scriptStructType(
		reflect.StructField{Name: isa.EmbeddedUnexportedPrefix + "left", PkgPath: "main", Type: left},
		reflect.StructField{Name: "Right", Type: right, Anonymous: true},
	)
	names := make([]string, 0, outer.NumField())
	for _, field := range pipitVisibleFields(outer) {
		names = append(names, field.Name)
	}
	require.False(t, slices.Contains(names, "Name"), "an ambiguous promoted field is not visible, as in Go")
	require.Contains(t, names, "Extra")
}
