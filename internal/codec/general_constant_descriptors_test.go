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

package codec

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestGeneralConstantDescriptorsRejectsAnInconsistentPool(t *testing.T) {
	t.Parallel()

	compositeZero := descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero}
	tests := []struct {
		name     string
		function *program.CompiledFunction
	}{
		{
			name:     "more descriptors than constants",
			function: &program.CompiledFunction{GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{compositeZero}},
		},
		{
			name:     "more constants than descriptors",
			function: &program.CompiledFunction{GeneralConstants: []reflect.Value{reflect.ValueOf(1)}},
		},
		{
			name: "a composite zero without a value",
			function: &program.CompiledFunction{
				GeneralConstants:           []reflect.Value{{}},
				GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{compositeZero},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			descriptors, err := GeneralConstantDescriptors(tt.function)

			require.ErrorIs(t, err, errInconsistentGeneralConstants)
			require.Nil(t, descriptors)
		})
	}
}

func TestGeneralConstantDescriptorsDescribesACompositeZero(t *testing.T) {
	t.Parallel()
	type point struct {
		X int
		Y int
	}

	descriptors, err := GeneralConstantDescriptors(&program.CompiledFunction{
		GeneralConstants:           []reflect.Value{reflect.New(reflect.TypeFor[point]()).Elem()},
		GeneralConstantDescriptors: []descriptor.GeneralConstantDescriptor{{Kind: descriptor.GeneralConstantCompositeZero}},
	})

	require.NoError(t, err)
	require.Len(t, descriptors, 1)
	require.Len(t, descriptors[0].TypeDescriptor.Fields, 2, "the descriptor rebuilds the struct type on load")
}
