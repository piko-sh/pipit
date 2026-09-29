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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func TestAppendStructFastDecodesTheTypeIndexTheCompilerEmits(t *testing.T) {
	t.Parallel()

	for _, typeIndex := range []uint16{0, 1, 7, 0x0102, 0xFFFF} {
		compiledFunction := program.NewNamedFunction("f")
		program.EmitExtension(compiledFunction, typeIndex, 0)

		require.Equalf(t, int(typeIndex), appendStructFastTypeIndex(compiledFunction.Body[0]),
			"the handler must read the element type the compiler recorded (index %d)", typeIndex)
	}
}
