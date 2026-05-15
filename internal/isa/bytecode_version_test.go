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

package isa

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBytecodeVersionMajorIsTwelve(t *testing.T) {
	t.Parallel()

	require.Equal(t, uint16(12), BytecodeVersionMajor,
		"the synthesised-struct sentinel field, the embedded-field rename prefix and the "+
			"cycle-broken tag key were renamed from piko to pipit; those names travel inside "+
			"serialised type descriptors, and an older payload read by this engine would have "+
			"its sentinel field go unrecognised and become visible to user code, so older "+
			"cached payloads must be rejected rather than mis-rendered")
}

func TestBytecodeVersionMinorIsZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, uint16(0), bytecodeVersionMinor,
		"BytecodeVersionMinor resets to 0 with each major bump")
}

func TestDrillMarkersAtIotaZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, Opcode(0), OpDrillTier1,
		"opDrillTier1 must occupy main-iota 0 so the dispatch loop "+
			"can treat byte == 0 as the descent into tier 1")
	require.Equal(t, SubOpcode(0), SubOpDrillTier2,
		"subOpDrillTier2 must occupy tier-1 iota 0 so operand A == 0 "+
			"means descend into tier 2")
	require.Equal(t, SubOpcodeTier2(0), SubOpTier2DrillTier3,
		"subOpTier2DrillTier3 must occupy tier-2 iota 0 so operand B == 0 "+
			"means descend into tier 3")
	require.Equal(t, SubOpcodeTier3(0), SubOpTier3Nop,
		"subOpTier3Nop must occupy tier-3 iota 0 so the all-drill word "+
			"{0,0,0,0} encodes the no-op")
}

func TestOpNopAliasesDrillTier1(t *testing.T) {
	t.Parallel()

	require.Equal(t, OpDrillTier1, OpNop,
		"after the Phase 4 migration opNop is a const alias for "+
			"opDrillTier1 (both are opcode value 0); the tier-3 no-op "+
			"is the all-drill word {opDrillTier1, subOpDrillTier2, "+
			"subOpTier2DrillTier3, subOpTier3Nop} = {0,0,0,0} and is "+
			"fast-path-dispatched by the flat dispatch macro inline")
	require.Equal(t, Opcode(0), OpNop,
		"opNop must remain at opcode value 0 so existing "+
			"makeInstruction(opNop, 0, 0, 0) padding sites continue "+
			"emitting the all-zero word")
}
