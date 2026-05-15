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

package verify

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestVerificationErrorRendersOffsetAndReason(t *testing.T) {
	t.Parallel()
	err := verificationError{
		functionName: "calc",
		pc:           17,
		reason:       "uninitialised register read",
		operand:      "a",
		instruction:  isa.NewInstruction(isa.OpNop, 1, 2, 3),
		bank:         isa.RegisterInt,
		slot:         5,
	}
	got := err.Error()
	require.Contains(t, got, "calc")
	require.Contains(t, got, "pc=17")
	require.Contains(t, got, "slot=5")
	require.Contains(t, got, "uninitialised register read")
}

func TestVerificationReportFormatsEveryError(t *testing.T) {
	t.Parallel()

	empty := &VerificationReport{}
	require.False(t, empty.HasErrors())
	require.Empty(t, empty.Format())

	populated := &VerificationReport{
		errors: []verificationError{
			{functionName: "a", pc: 1, reason: "first"},
			{functionName: "b", pc: 2, reason: "second"},
		},
	}
	require.True(t, populated.HasErrors())
	formatted := populated.Format()
	require.Contains(t, formatted, "first")
	require.Contains(t, formatted, "second")
	require.Contains(t, formatted, "\n", "multiple errors should be newline-separated")
}

func TestCompiledFileSetListsFunctionNames(t *testing.T) {
	t.Parallel()

	var nilSet *program.CompiledFileSet
	require.Nil(t, nilSet.FunctionNames(),
		"nil receiver should return nil rather than panic")

	cfs := program.NewCompiledFileSet(nil, map[string]uint16{
		"run":   0,
		"setup": 1,
	}, nil, nil)
	names := cfs.FunctionNames()
	require.Len(t, names, 2)
	require.Contains(t, names, "run")
	require.Contains(t, names, "setup")
}
