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
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestAddTypeRefLeavesDescriptorsToTheCodec(t *testing.T) {
	function := &CompiledFunction{}
	index, err := AddTypeRef(function, reflect.TypeFor[[]int]())
	require.NoError(t, err)
	require.Equal(t, uint16(0), index)
	require.Empty(t, function.TypeTableDescriptors, "compiling must not materialise descriptors")

	derived := typeTableDescriptorsOf(function)
	require.Len(t, derived, 1)
	require.Equal(t, descriptor.KindSlice, derived[0].Kind)
	require.Empty(t, function.TypeTableDescriptors, "deriving must not cache on the function")
}

func TestTypeTableDescriptorAtPrefersStoredEntries(t *testing.T) {
	stored := descriptor.ReflectTypeToDescriptor(reflect.TypeFor[string]())
	tests := []struct {
		name  string
		table []reflect.Type
		kept  []descriptor.TypeDescriptor
		index int
		want  descriptor.TypeDescriptorKind
	}{
		{name: "stored entry wins over the reflect type", table: []reflect.Type{reflect.TypeFor[int]()}, kept: []descriptor.TypeDescriptor{stored}, index: 0, want: descriptor.KindBasic},
		{name: "entry past the stored tail is derived", table: []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[map[string]int]()}, kept: []descriptor.TypeDescriptor{stored}, index: 1, want: descriptor.KindMap},
		{name: "nothing stored derives everything", table: []reflect.Type{reflect.TypeFor[*int]()}, kept: nil, index: 0, want: descriptor.KindPtr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			function := &CompiledFunction{TypeTable: tt.table, TypeTableDescriptors: tt.kept}
			got := TypeTableDescriptorAt(function, tt.index)
			require.Equal(t, tt.want, got.Kind)
			if tt.index < len(tt.kept) {
				require.Equal(t, tt.kept[tt.index], got)
			}
		})
	}
}
