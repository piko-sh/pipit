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

package app_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/module"
)

const crossRootLibrary = `package lib

func add(x, y int) int { return x + y }

// Sum3 chains two int-only inline calls and returns inline.
func Sum3(a, b, c int) int { return add(add(a, b), c) }

type Reporter interface {
	Helper()
	Name() string
}

func compare(a, b int) int {
	if a > b {
		return 1
	}
	if a < b {
		return -1
	}
	return 0
}

func label(sign int) string {
	switch sign {
	case 1:
		return "gt"
	case -1:
		return "lt"
	}
	return "eq"
}

// classify calls a helper that calls another, after Report has already returned inline
// from the main root's Helper.
func classify(a, b int) string { return label(compare(a, b)) }

// Report enters the main root through the interface (a void inline return) and then makes
// two levels of static inline calls inside its own root.
func Report(r Reporter, a, b int) string {
	r.Helper()
	return r.Name() + ":" + classify(a, b)
}

// Map calls a closure authored in the main root from this root, once per element.
func Map(values []int, f func(int) int) []int {
	out := make([]int, len(values))
	for i, v := range values {
		out[i] = f(v)
	}
	return out
}
`

const crossRootMain = `package main

import "example.com/lib"

type recorder struct{ name string }

func (r *recorder) Helper() {}
func (r *recorder) Name() string { return r.name }

func local(x int) int { return x * 2 }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func joinAll(parts ...string) string {
	out := ""
	for _, part := range parts {
		out += part
	}
	return out
}

func entrypoint() string {
	total := lib.Sum3(1, 2, 3)
	total += local(total)
	rec := &recorder{name: "rec"}
	shapeTwo := lib.Report(rec, 3, 1) + "," + lib.Report(rec, 1, 3) + "," + lib.Report(rec, 2, 2)
	text := "n=" + itoa(total)
	shapeThree := joinAll(text, "/", shapeTwo)
	values := []int{lib.Sum3(4, 5, 6), total, lib.Sum3(0, 0, 1)}
	shifted := lib.Map(values, func(v int) int { return v + total })
	return shapeThree + "/" + itoa(shifted[0]) + "," + itoa(shifted[1]) + "," + itoa(shifted[2])
}

func main() {}
`

func runCrossRootMain(t *testing.T, options ...app.Option) string {
	t.Helper()
	builder := app.NewService(options...)
	descriptor := module.Descriptor{
		SchemaVersion: module.DescriptorVersion,
		Ref:           module.Ref{Path: "example.com/lib", Version: "v0.0.0"},
	}
	bundle, err := builder.PackageModule(context.Background(),
		descriptor, "example.com/lib", map[string]map[string]string{"": {"lib.go": crossRootLibrary}}, adapters.PackCompiledFileSetToBytes)
	require.NoError(t, err)
	consumer := app.NewService(options...)
	reference := module.Ref{Path: descriptor.Ref.Path, Version: descriptor.Ref.Version, Pin: bundle.Descriptor.Ref.Pin}
	_, err = consumer.LoadModule(context.Background(), bundle, reference, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err)
	compiled, err := consumer.CompileProgram(context.Background(), "main", map[string]map[string]string{"": {"main.go": crossRootMain}})
	require.NoError(t, err)
	result, err := consumer.ExecuteEntrypoint(context.Background(), compiled, "entrypoint")
	require.NoError(t, err)
	return fmt.Sprint(result)
}

func TestCrossRootInlineReturnsOnBothDispatchers(t *testing.T) {
	t.Parallel()
	const want = "n=18/rec:gt,rec:lt,rec:eq/33,36,19"
	tests := []struct {
		name    string
		options []app.Option
	}{
		{name: "assembly dispatcher", options: nil},
		{name: "go dispatcher", options: []app.Option{app.WithForceGoDispatch()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, want, runCrossRootMain(t, tt.options...))
		})
	}
}
