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

	"pipit.sh/pipit/internal/schema"
)

func TestTheBytecodeVersionMatchesTheSchemaFormatVersion(t *testing.T) {
	t.Parallel()

	require.Equal(t, schema.BytecodeFormatVersion, BytecodeVersionMajor,
		"the instruction set's major version and the schema's format version move together, so a "+
			"payload from an older format is rejected by its schema hash")
}

func TestTheMinorVersionMatchesTheSchemaFormatRevision(t *testing.T) {
	t.Parallel()

	require.Equal(t, schema.BytecodeFormatRevision, bytecodeVersionMinor,
		"the minor version and the schema's format revision move together, so a build rejects "+
			"a payload holding opcodes that were appended after it")
}

func TestTheMinorVersionCountsTheAdditiveRevisionsOfThisMajor(t *testing.T) {
	t.Parallel()

	require.EqualValues(t, 1, bytecodeVersionMinor,
		"revision 1 appended opcodes to the ends of their tiers, leaving every earlier number as it was; "+
			"the minor resets to 0 with the next major bump")
}

func TestDrillMarkersAtIotaZero(t *testing.T) {
	t.Parallel()

	require.Zero(t, OpDrillTier1,
		"opDrillTier1 must occupy main-iota 0 so the dispatch loop "+
			"can treat byte == 0 as the descent into tier 1")
	require.Zero(t, SubOpDrillTier2,
		"subOpDrillTier2 must occupy tier-1 iota 0 so operand A == 0 "+
			"means descend into tier 2")
	require.Zero(t, SubOpTier2DrillTier3,
		"subOpTier2DrillTier3 must occupy tier-2 iota 0 so operand B == 0 "+
			"means descend into tier 3")
	require.Zero(t, SubOpTier3NoOperation,
		"subOpTier3NoOperation must occupy tier-3 iota 0 so the all-drill word "+
			"{0,0,0,0} encodes the no-op")
}

func TestOpNoOperationAliasesDrillTier1(t *testing.T) {
	t.Parallel()

	require.Equal(t, OpDrillTier1, OpNoOperation,
		"after the Phase 4 migration opNoOperation is a const alias for "+
			"opDrillTier1 (both are opcode value 0); the tier-3 no-op "+
			"is the all-drill word {opDrillTier1, subOpDrillTier2, "+
			"subOpTier2DrillTier3, subOpTier3NoOperation} = {0,0,0,0} and is "+
			"fast-path-dispatched by the flat dispatch macro inline")
	require.Zero(t, OpNoOperation,
		"opNoOperation must remain at opcode value 0 so existing "+
			"makeInstruction(opNoOperation, 0, 0, 0) padding sites continue "+
			"emitting the all-zero word")
}
