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

//go:build !safe && (amd64 || arm64)

package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/asm"
	"pipit.sh/pipit/internal/engine/program"
)

func TestABITypeOffsetsMatchASMDefines(t *testing.T) {
	t.Parallel()
	require.EqualValues(t, program.AbiTypeKindByteOffset, asm.ABITypeKindByteOffset)
	require.EqualValues(t, program.AbiTypePointerElemOffset, asm.ABITypePointerElemOffset)
}

func TestCallSiteIndexWithinRejectsForeignSites(t *testing.T) {
	t.Parallel()
	function := &program.CompiledFunction{CallSites: make([]program.CallSite, 3)}
	index, ok := callSiteIndexWithin(function, &function.CallSites[2])
	require.True(t, ok)
	require.Equal(t, 2, index)
	_, ok = callSiteIndexWithin(function, &program.CallSite{})
	require.False(t, ok)
	_, ok = callSiteIndexWithin(&program.CompiledFunction{}, &function.CallSites[0])
	require.False(t, ok)
}
