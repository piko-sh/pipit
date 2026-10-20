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

package inline

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestRestoreSpliceSnapshotKeepsDescriptorsInStep(t *testing.T) {
	t.Parallel()
	caller := &program.CompiledFunction{}
	snapshot := CaptureSpliceSnapshot(caller)
	_, err := program.AddGeneralConstant(caller, reflect.ValueOf(struct{ X int }{}), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero})
	require.NoError(t, err)
	_, err = program.AddTypeRefWithMethods(caller, reflect.TypeFor[any](), []string{"area"})
	require.NoError(t, err)

	RestoreSpliceSnapshot(caller, snapshot)

	require.Empty(t, caller.GeneralConstants)
	require.Empty(t, caller.GeneralConstantDescriptors, "a failed splice must not leave a descriptor without its constant")
	require.Empty(t, caller.TypeTable)
	require.Empty(t, caller.TypeTableInterfaceMethods, "nor a method set without its type")
	index, err := program.AddTypeRefWithMethods(caller, reflect.TypeFor[any](), []string{"area"})
	require.NoError(t, err)
	require.Equal(t, uint16(0), index, "the methods index must not point past the rolled-back table")
}
