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

//go:build integration

package bytecode_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"
)

func TestRestoreSpliceSnapshotDropsStaleConstantIndex(t *testing.T) {
	t.Parallel()
	caller := program.NewNamedFunction("caller")

	snapshot := inline.CaptureSpliceSnapshot(caller)

	abandoned, err := program.AddIntConstant(caller, 9001)
	require.NoError(t, err)
	require.Equal(t, uint16(0), abandoned)

	inline.RestoreSpliceSnapshot(caller, snapshot)
	require.Empty(t, caller.IntConstants, "rollback must truncate the pool")

	survivor, err := program.AddIntConstant(caller, 4242)
	require.NoError(t, err)
	require.Equal(t, uint16(0), survivor)

	reinterned, err := program.AddIntConstant(caller, 9001)
	require.NoError(t, err)
	require.NotEqual(t, survivor, reinterned,
		"rolled-back constant resolved to the slot a different constant now occupies")
	require.Equal(t, int64(9001), caller.IntConstants[reinterned])
	require.Equal(t, int64(4242), caller.IntConstants[survivor])
}
