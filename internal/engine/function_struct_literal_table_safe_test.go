//go:build safe || (js && wasm)

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

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func TestStructLiteralTablePublishesNoBaseInThePureGoLane(t *testing.T) {
	t.Parallel()

	compiledFunction := program.NewNamedFunction("literals")
	compiledFunction.TypeTable = []reflect.Type{reflect.TypeFor[literalPlainScalars]()}

	EnsureStructLiteralTable(compiledFunction)

	require.Len(t, compiledFunction.StructLiteralTable, 1, "the table itself is still built")
	require.Zero(t, compiledFunction.StructLiteralTableBase,
		"the pure-Go lane has no assembly to feed, so it publishes no base pointer")
}
