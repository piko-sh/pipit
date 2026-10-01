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

package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func buildGetMethodOnConst(receiver any, methodName string) *program.CompiledFunction {
	bb := newBytecodeBuilder()
	receiverConst := bb.addGeneralConst(reflect.ValueOf(receiver))
	nameConst := bb.addStringConst(methodName)
	bb.generalRegisters(2).returnGeneral()
	bb.Emit(isa.OpLoadGeneralConst, 1, receiverConst, 0)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpGetMethod), 0, 1)
	bb.emitExt(int32(nameConst))
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return bb.build()
}

func TestGetMethod_BindsHostNamedTypeMethod(t *testing.T) {
	t.Parallel()

	result, err := execSynthetic(t, buildGetMethodOnConst(2*time.Second, "String"))
	require.NoError(t, err)

	bound, ok := result.(func() string)
	require.True(t, ok, "expected a bound func() string, got %T", result)
	require.Equal(t, "2s", bound())
}

func TestGetMethod_MissOnValidReceiverIsInvariantError(t *testing.T) {
	t.Parallel()

	_, err := execSynthetic(t, buildGetMethodOnConst(int64(2_000_000_000), "String"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "undefined method: int64.String")
	require.NotContains(t, err.Error(), nilDereferenceMessage)
}
