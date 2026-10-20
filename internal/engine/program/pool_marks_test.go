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

func TestPoolMarksRestoreKeepsPoolsInStep(t *testing.T) {
	t.Parallel()
	anyType := reflect.TypeFor[any]()
	function := &CompiledFunction{}
	_, err := AddIntConstant(function, 1)
	require.NoError(t, err)
	_, err = AddTypeRef(function, anyType)
	require.NoError(t, err)
	before := CapturePoolMarks(function)

	_, err = AddIntConstant(function, 2)
	require.NoError(t, err)
	_, err = AddStringConstant(function, "spliced")
	require.NoError(t, err)
	_, err = AddGeneralConstant(function, reflect.ValueOf(struct{ X int }{}), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero})
	require.NoError(t, err)
	methodsIndex, err := AddTypeRefWithMethods(function, anyType, []string{"area"})
	require.NoError(t, err)

	before.Restore(function)

	require.Equal(t, before, CapturePoolMarks(function), "every pool returns to its recorded length")
	require.Len(t, function.TypeTableInterfaceMethods, len(function.TypeTable), "the interface method sets stay in step with the type table")
	require.Len(t, function.GeneralConstantDescriptors, len(function.GeneralConstants), "the descriptors stay in step with the general constants")
	readded, err := AddTypeRefWithMethods(function, anyType, []string{"area"})
	require.NoError(t, err)
	require.Equal(t, methodsIndex, readded, "the methods entry is appended afresh, not found through a stale index")
	require.Equal(t, []string{"area"}, function.TypeTableInterfaceMethods[readded])
	index, err := AddIntConstant(function, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), function.IntConstants[index], "a pruned dedup entry is not reused")
}
