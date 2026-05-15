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

//go:build integration && !crossarch

package snippets_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/stdlib"
)

func TestNarrowIntNamedSliceDeferMatchesGo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		snippet string
		want    string
	}{
		{
			name: "int32_append",
			snippet: `package main
import "fmt"
func f() (r []int32) {
	defer func() { r = append(r, 99) }()
	r = []int32{1, 2}
	return r
}
func run() string { return fmt.Sprintf("%v", f()) }
`,
			want: "[1 2 99]",
		},
		{
			name: "int16_append",
			snippet: `package main
import "fmt"
func f() (r []int16) {
	defer func() { r = append(r, 99) }()
	r = []int16{1, 2}
	return r
}
func run() string { return fmt.Sprintf("%v", f()) }
`,
			want: "[1 2 99]",
		},
		{
			name: "uint32_append",
			snippet: `package main
import "fmt"
func f() (r []uint32) {
	defer func() { r = append(r, 99) }()
	r = []uint32{1, 2}
	return r
}
func run() string { return fmt.Sprintf("%v", f()) }
`,
			want: "[1 2 99]",
		},
		{
			name: "int32_elemwrite",
			snippet: `package main
import "fmt"
func f() (r []int32) {
	defer func() { r[0] = 42 }()
	r = []int32{1, 2}
	return r
}
func run() string { return fmt.Sprintf("%v", f()) }
`,
			want: "[42 2]",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := app.NewService()
			service.UseSymbolProviders(stdlib.Providers()...)

			got, err := service.EvalFile(context.Background(), testCase.snippet, "run")
			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}
