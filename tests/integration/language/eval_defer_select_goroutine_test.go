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

func TestEdgeCaseDeferNamedReturns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "defer_multi_named_return",
			source: `package main

func helper() (a int, b int) {
	a = 10
	b = 100
	defer func() {
		a *= 2
		b *= 2
	}()
	return
}

func run() int {
	a, b := helper()
	return a + b
}

func main() {}
`,
			entrypoint: "run",
			want:       220,
		},
		{
			name: "defer_modify_explicit_return",
			source: `package main

func modified() (result int) {
	defer func() { result += 10 }()
	return 32
}

func run() int {
	return modified()
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
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

func TestEdgeCaseSelectChannel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "select_nil_channel_default",
			source: `package main

func run() int {
	var ch chan int
	select {
	case <-ch:
		return 1
	default:
		return 2
	}
}

func main() {}
`,
			entrypoint: "run",
			want:       2,
		},
		{
			name: "closure_in_select_body",
			source: `package main

func run() int {
	ch := make(chan int, 1)
	ch <- 42
	var f func() int
	select {
	case v := <-ch:
		f = func() int { return v }
	}
	return f()
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
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

func TestEdgeCaseGoroutines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "goroutine_loop_capture",
			source: `package main

func run() int {
	ch := make(chan int, 5)
	for i := range 5 {
		go func() {
			ch <- i
		}()
	}
	sum := 0
	for range 5 {
		sum += <-ch
	}
	return sum
}

func main() {}
`,
			entrypoint: "run",
			want:       10,
		},
		{
			name: "goroutine_panic_recover",
			source: `package main

func run() int {
	ch := make(chan int, 1)
	go func() {
		defer func() {
			recover()
			ch <- 42
		}()
		panic("boom")
	}()
	return <-ch
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
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
