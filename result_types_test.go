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

package pipit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func TestEvalResultsKeepExactTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   any
	}{
		{name: "untyped integer", source: "1 + 2 * 3", want: 7},
		{name: "int64 variable", source: "var x int64 = 5\nx", want: int64(5)},
		{name: "int32 conversion", source: "int32(7)", want: int32(7)},
		{name: "uint8 variable", source: "var u uint8 = 200\nu", want: uint8(200)},
		{name: "float32 variable", source: "var f float32 = 1.5\nf", want: float32(1.5)},
		{name: "untyped float", source: "2.5", want: 2.5},
		{name: "string", source: "\"s\"", want: "s"},
		{name: "comparison", source: "1 < 2", want: true},
		{name: "int16 inside any", source: "var a any = int16(3)\na", want: int16(3)},
		{name: "slice literal", source: "[]int{1, 2}", want: []int{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			interpreter := pipit.NewInterpreter(pipit.WithMaxExecutionTime(5 * time.Second))
			result, err := interpreter.Eval(t.Context(), tt.source)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
