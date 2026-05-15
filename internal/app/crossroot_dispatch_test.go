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

func TestCrossRootDispatch(t *testing.T) {
	t.Parallel()

	libSources := map[string]map[string]string{
		"": {
			"lib.go": `package lib

type Checker struct {
	prefix string
}

func New(prefix string) *Checker {
	return &Checker{prefix: prefix}
}

func (c *Checker) label(kind string) string {
	return c.prefix + ":" + kind
}

func (c *Checker) Greater(a, b int) string {
	if a > b {
		return c.label("greater")
	}
	if a == b {
		return c.label("equal")
	}
	return c.label("less")
}

type Reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Name() string
}

// Report is testify's shape: the module calls back into a method of a type declared in the
// main program, through an interface, while the module itself is a separate bundle root.
func Report(r Reporter, a, b int) string {
	r.Helper()
	if a <= b {
		r.Errorf("%s: %d not greater than %d", r.Name(), a, b)
		return "reported"
	}
	return "ok"
}
`,
		},
	}

	builder := app.NewService()
	descriptor := module.Descriptor{
		SchemaVersion: module.DescriptorVersion,
		Ref:           module.Ref{Path: "example.com/lib", Version: "v0.0.0"},
	}
	bundle, err := builder.PackageModule(context.Background(),
		descriptor, "example.com/lib", libSources, adapters.PackCompiledFileSetToBytes)
	require.NoError(t, err)

	consumer := app.NewService()
	reference := module.Ref{Path: descriptor.Ref.Path, Version: descriptor.Ref.Version, Pin: bundle.Descriptor.Ref.Pin}
	_, err = consumer.LoadModule(context.Background(), bundle, reference, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err)

	mainSources := map[string]map[string]string{
		"": {
			"main.go": `package main

import "example.com/lib"

type recorder struct {
	name  string
	lines []string
}

// Helper is deliberately empty: it compiles to a zero-length body, which is the shape that
// used to poison the caller's dispatch context when the frame popped.
func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.lines = append(r.lines, format)
}

func (r *recorder) Name() string { return r.name }

func entrypoint() string {
	c := lib.New("probe")
	out := ""
	for i := 0; i < 8; i++ {
		out += c.Greater(i, 4) + "|"
	}
	rec := &recorder{name: "rec"}
	for i := 0; i < 4; i++ {
		out += lib.Report(rec, i, 2) + "|"
	}
	if len(rec.lines) != 3 {
		return out + "BADCOUNT"
	}
	out += rec.lines[0]
	return out
}

func main() {}
`,
		},
	}
	mainCfs, err := consumer.CompileProgram(context.Background(), "main", mainSources)
	require.NoError(t, err)

	result, err := consumer.ExecuteEntrypoint(context.Background(), mainCfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t,
		"probe:less|probe:less|probe:less|probe:less|probe:equal|probe:greater|probe:greater|probe:greater|"+
			"reported|reported|reported|ok|%s: %d not greater than %d",
		fmt.Sprint(result))
}
