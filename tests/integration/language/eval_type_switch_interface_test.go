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

package language_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func TestEdgeCaseTypeSwitch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "multi_types_per_case",
			source: `package main

func run() string {
	var x any = 42
	switch x.(type) {
	case int, float64:
		return "numeric"
	case string:
		return "text"
	default:
		return "other"
	}
}

func main() {}
`,
			entrypoint: "run",
			want:       "numeric",
		},
		{
			name: "nil_case",
			source: `package main

func run() string {
	var x any
	switch x.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	default:
		return "other"
	}
}

func main() {}
`,
			entrypoint: "run",
			want:       "nil",
		},
		{
			name: "binding_multi_type",
			source: `package main

func run() int {
	var x any = 3.14
	switch v := x.(type) {
	case int, float64:
		_ = v
		return 1
	case string:
		return 2
	default:
		return 3
	}
}

func main() {}
`,
			entrypoint: "run",
			want:       1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestEdgeCaseInterfaceMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "interface_reassignment",
			source: `package main

type Stringer interface{ String() string }
type A struct{}
func (A) String() string { return "A" }
type B struct{}
func (B) String() string { return "B" }

func run() string {
	var s Stringer
	s = A{}
	r1 := s.String()
	s = B{}
	r2 := s.String()
	return r1 + r2
}

func main() {}
`,
			entrypoint: "run",
			want:       "AB",
		},
		{
			name: "method_value_receiver_copy",
			source: `package main

type Box struct{ V int }
func (b Box) Get() int { return b.V }

func run() int {
	b := Box{V: 10}
	f := b.Get
	b.V = 99
	return f()
}

func main() {}
`,
			entrypoint: "run",
			want:       10,
		},
		{
			name: "field_shadow_embedding",
			source: `package main

type Base struct{ V int }
type Derived struct {
	Base
	V int
}

func run() int {
	d := Derived{Base: Base{V: 1}, V: 2}
	return d.V + d.Base.V
}

func main() {}
`,
			entrypoint: "run",
			want:       3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
