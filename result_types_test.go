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
	"context"
	"fmt"
	"testing"
	"time"

	"pipit.sh/pipit"
)

func TestEvalResultsKeepExactTypes(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(pipit.WithMaxExecutionTime(5 * time.Second))
	cases := []struct {
		source string
		want   any
	}{
		{source: "1 + 2 * 3", want: 7},
		{source: "var x int64 = 5\nx", want: int64(5)},
		{source: "int32(7)", want: int32(7)},
		{source: "var u uint8 = 200\nu", want: uint8(200)},
		{source: "var f float32 = 1.5\nf", want: float32(1.5)},
		{source: "2.5", want: 2.5},
		{source: "\"s\"", want: "s"},
		{source: "1 < 2", want: true},
		{source: "var a any = int16(3)\na", want: int16(3)},
		{source: "[]int{1, 2}", want: []int{1, 2}},
	}
	for _, tc := range cases {
		result, err := interpreter.Eval(context.Background(), tc.source)
		if err != nil {
			t.Fatalf("%q: %v", tc.source, err)
		}
		if fmt.Sprintf("%T %v", result, result) != fmt.Sprintf("%T %v", tc.want, tc.want) {
			t.Fatalf("%q: got %T %v, want %T %v", tc.source, result, result, tc.want, tc.want)
		}
	}
}
