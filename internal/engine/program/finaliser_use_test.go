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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinaliserUseIsUnknownUntilRecorded(t *testing.T) {
	t.Parallel()
	root := &CompiledFunction{Name: "root"}
	sets, known := root.FinaliserUse(2)
	require.False(t, known)
	require.False(t, sets)

	root.RecordFinaliserUse(false, 2)
	sets, known = root.FinaliserUse(2)
	require.True(t, known, "a program that sets no finaliser is recorded too")
	require.False(t, sets)
	_, known = root.FinaliserUse(3)
	require.False(t, known, "but only for the table size it was scanned at")

	root.RecordFinaliserUse(true, 3)
	sets, known = root.FinaliserUse(5)
	require.True(t, known, "a program that sets a finaliser keeps doing so")
	require.True(t, sets)

	var absent *CompiledFunction
	absent.RecordFinaliserUse(true, 0)
	_, known = absent.FinaliserUse(0)
	require.False(t, known)
}

func TestARecordThatTheProgramSetsFinalisersIsNeverReplaced(t *testing.T) {
	t.Parallel()
	root := &CompiledFunction{Name: "root"}
	root.RecordFinaliserUse(true, 2)
	root.RecordFinaliserUse(false, 2)
	sets, known := root.FinaliserUse(2)
	require.True(t, known)
	require.True(t, sets, "a scan that found no call, finishing after the record, cannot undo it")
}

func TestFinaliserScanReportsTheTableSizeLastScanned(t *testing.T) {
	t.Parallel()
	root := &CompiledFunction{Name: "root"}
	sets, scanned := root.FinaliserScan()
	require.False(t, sets)
	require.Equal(t, -1, scanned, "no scan recorded")

	root.RecordFinaliserUse(false, 4)
	sets, scanned = root.FinaliserScan()
	require.False(t, sets)
	require.Equal(t, 4, scanned, "a later scan can start at the fifth function")

	root.RecordFinaliserUse(true, 6)
	sets, _ = root.FinaliserScan()
	require.True(t, sets)
}
