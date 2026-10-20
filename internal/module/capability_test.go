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

package module

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Capability
	}{
		{name: "axis only", input: "filesystem.read", want: Capability{Axis: "filesystem.read"}},
		{name: "axis and scope", input: "filesystem.read(/etc/*)", want: Capability{Axis: "filesystem.read", Scope: "/etc/*"}},
		{name: "whitespace", input: "  exec ( ls ) ", want: Capability{Axis: "exec", Scope: "ls"}},
		{name: "empty scope parens", input: "network()", want: Capability{Axis: "network", Scope: ""}},
		{name: "unterminated paren", input: "exec(ls", want: Capability{Axis: "exec", Scope: "ls"}},
		{name: "empty input", input: "", want: Capability{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ParseCapability(tt.input))
		})
	}
}

func TestCapabilityString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input Capability
		want  string
	}{
		{name: "axis only", input: Capability{Axis: "exec"}, want: "exec"},
		{name: "axis and scope", input: Capability{Axis: "filesystem.read", Scope: "/etc/*"}, want: "filesystem.read(/etc/*)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.input.String())
		})
	}
}

func TestCapabilitySetNormalise(t *testing.T) {
	t.Parallel()
	input := CapabilitySet{
		{Axis: "network"},
		{Axis: "filesystem.read", Scope: "/var/*"},
		{},
		{Axis: "filesystem.read", Scope: "/etc/*"},
		{Axis: "exec", Scope: "ls"},
	}
	got := input.Normalise()
	want := CapabilitySet{
		{Axis: "exec", Scope: "ls"},
		{Axis: "filesystem.read", Scope: "/etc/*"},
		{Axis: "filesystem.read", Scope: "/var/*"},
		{Axis: "network"},
	}
	require.Equal(t, want, got)
}

func TestCapabilitySetContainsAndSubset(t *testing.T) {
	t.Parallel()
	full := CapabilitySet{
		{Axis: "filesystem.read", Scope: "/etc/*"},
		{Axis: "network"},
	}
	partial := CapabilitySet{
		{Axis: "network"},
	}
	require.True(t, partial.IsSubsetOf(full), "the partial set is a subset of the full set")
	require.False(t, full.IsSubsetOf(partial), "the full set is not a subset of the partial set")
	require.True(t, full.Contains(Capability{Axis: "network"}))
	require.False(t, full.Contains(Capability{Axis: "exec"}))
}
