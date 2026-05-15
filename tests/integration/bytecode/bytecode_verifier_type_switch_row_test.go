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
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/verify"
)

func TestVerifierRejectsBackwardTypeSwitchRow(t *testing.T) {
	t.Parallel()
	builder := newBytecodeBuilder().generalRegisters(2).intRegisters(1)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpTypeSwitchJump), 1, 0)
	builder.EmitJump(isa.OpTypeSwitchCase, 0, -2)
	builder.EmitJump(isa.OpDrillTier1, uint8(isa.SubOpJump), 0)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 0, 1)
	builder.returnInt()
	compiledFunction := builder.build()
	report := &verify.VerificationReport{}
	require.NoError(t, verify.RunFunctionVerifier(context.Background(), compiledFunction, report))
	require.True(t, report.HasErrors())
	require.Contains(t, report.Format(), "type switch table row jumps backwards")
}
