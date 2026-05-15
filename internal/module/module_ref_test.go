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
)

func TestParseModuleRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected Ref
		wantErr  bool
	}{
		{"path only", "github.com/foo/bar", Ref{Path: "github.com/foo/bar"}, false},
		{"path and version", "github.com/foo/bar@v1.2.3", Ref{Path: "github.com/foo/bar", Version: "v1.2.3"}, false},
		{"path version pin", "github.com/foo/bar@v1.2.3#sha256:abc", Ref{Path: "github.com/foo/bar", Version: "v1.2.3", Pin: "sha256:abc"}, false},
		{"path with pin only", "github.com/foo/bar#sha256:abc", Ref{Path: "github.com/foo/bar", Pin: "sha256:abc"}, false},
		{"whitespace", "  github.com/foo/bar@v1.0.0  ", Ref{Path: "github.com/foo/bar", Version: "v1.0.0"}, false},
		{"empty input", "", Ref{}, true},
		{"whitespace only", "   ", Ref{}, true},
		{"no path", "@v1.0.0", Ref{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRef(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRef(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q) returned error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Fatalf("ParseRef(%q) = %+v, want %+v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestModuleRefString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    Ref
		expected string
	}{
		{Ref{Path: "foo"}, "foo"},
		{Ref{Path: "foo", Version: "v1"}, "foo@v1"},
		{Ref{Path: "foo", Version: "v1", Pin: "sha256:abc"}, "foo@v1#sha256:abc"},
		{Ref{Path: "foo", Pin: "sha256:abc"}, "foo#sha256:abc"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			if got := tt.input.String(); got != tt.expected {
				t.Fatalf("Ref.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestModuleRefIsZero(t *testing.T) {
	t.Parallel()
	if !(Ref{}).IsZero() {
		t.Fatalf("zero Ref should report IsZero=true")
	}
	if (Ref{Path: "x"}).IsZero() {
		t.Fatalf("populated Ref should report IsZero=false")
	}
}
