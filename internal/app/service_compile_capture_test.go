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

package app

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

func TestCompiledScriptGivesEachIterationItsOwnCapturedVariable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "range over int",
			source: `out := []int{}
for i := range 3 { f := func() int { return i }; out = append(out, f()) }
out`,
			want: "[0 1 2]",
		},
		{
			name: "three-clause loop",
			source: `out := []int{}
for i := 0; i < 3; i++ { f := func() int { return i }; out = append(out, f()) }
out`,
			want: "[0 1 2]",
		},
		{
			name: "closures stored and called after the loop",
			source: `fs := []func() int{}
for i := range 3 { fs = append(fs, func() int { return i }) }
fs[0]()*100 + fs[1]()*10 + fs[2]()`,
			want: "12",
		},
		{
			name: "captured loop variable beside a captured accumulator",
			source: `sum := 0
for i := range 3 { f := func() { sum += i*10 + 1 }; f() }
sum`,
			want: "33",
		},
		{
			name: "captured loop variable indexes a captured map",
			source: `values := map[int]int{}
for i := range 3 { f := func() { values[i] = i }; f() }
len(values)`,
			want: "3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService()
			evaluated, err := service.Eval(t.Context(), tt.source)
			require.NoError(t, err)
			require.Equal(t, tt.want, fmt.Sprint(evaluated), "Eval")

			compiled, err := service.Compile(t.Context(), tt.source)
			require.NoError(t, err)
			executed, err := service.Execute(t.Context(), compiled)
			require.NoError(t, err)
			require.Equal(t, tt.want, fmt.Sprint(executed), "Compile then Execute")
		})
	}
}

func TestScriptVariablesWrittenByNativeCallbacksAreShared(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"captured accumulator": `import "host"
sum := 0
host.Call(func() { sum += 5 })
sum`,
		"captured accumulator and loop variable": `import "host"
sum := 0
for i := range 3 { host.Call(func() { sum += i*10 + 1 }) }
sum`,
	}
	wants := map[string]string{"captured accumulator": "5", "captured accumulator and loop variable": "33"}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			harness, vm := newCancellationVM(t, t.Context(), map[string]reflect.Value{
				"Call": reflect.ValueOf(func(fn func()) { fn() }),
			}, engine.VMLimits{})
			evaluated, err := harness.service.Eval(t.Context(), source)
			require.NoError(t, err)
			require.Equal(t, wants[name], fmt.Sprint(evaluated), "Eval")

			executed, err := vm.Execute(harness.compile(t, source))
			require.NoError(t, err)
			require.Equal(t, wants[name], fmt.Sprint(executed), "Compile then Execute")
		})
	}
}
