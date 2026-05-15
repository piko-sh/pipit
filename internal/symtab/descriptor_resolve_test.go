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

package symtab

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestDescriptorToReflectTypeRefusesExcessiveNesting(t *testing.T) {
	t.Parallel()
	typeDescriptor := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	for range descriptor.MaxTypeDescriptorDepth + 16 {
		typeDescriptor = &descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: typeDescriptor}
	}
	reconstructed, err := ReflectTypeFor(*typeDescriptor, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)
	require.Nil(t, reconstructed)
}

func TestDescriptorToReflectTypeAllowsModerateNesting(t *testing.T) {
	t.Parallel()
	typeDescriptor := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	for range 8 {
		typeDescriptor = &descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: typeDescriptor}
	}
	reconstructed, err := ReflectTypeFor(*typeDescriptor, nil)
	require.NoError(t, err)
	require.Equal(t, reflect.Pointer, reconstructed.Kind())
}
