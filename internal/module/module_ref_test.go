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

func TestParseModuleRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    Ref
		wantErr bool
	}{
		{name: "path only", input: "github.com/foo/bar", want: Ref{Path: "github.com/foo/bar"}, wantErr: false},
		{name: "path and version", input: "github.com/foo/bar@v1.2.3", want: Ref{Path: "github.com/foo/bar", Version: "v1.2.3"}, wantErr: false},
		{name: "path version pin", input: "github.com/foo/bar@v1.2.3#sha256:abc", want: Ref{Path: "github.com/foo/bar", Version: "v1.2.3", Pin: "sha256:abc"}, wantErr: false},
		{name: "path with pin only", input: "github.com/foo/bar#sha256:abc", want: Ref{Path: "github.com/foo/bar", Pin: "sha256:abc"}, wantErr: false},
		{name: "whitespace", input: "  github.com/foo/bar@v1.0.0  ", want: Ref{Path: "github.com/foo/bar", Version: "v1.0.0"}, wantErr: false},
		{name: "empty input", input: "", want: Ref{}, wantErr: true},
		{name: "whitespace only", input: "   ", want: Ref{}, wantErr: true},
		{name: "no path", input: "@v1.0.0", want: Ref{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRef(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestModuleRefString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input Ref
		want  string
	}{
		{name: "path only", input: Ref{Path: "foo"}, want: "foo"},
		{name: "path and version", input: Ref{Path: "foo", Version: "v1"}, want: "foo@v1"},
		{name: "path, version and pin", input: Ref{Path: "foo", Version: "v1", Pin: "sha256:abc"}, want: "foo@v1#sha256:abc"},
		{name: "path and pin", input: Ref{Path: "foo", Pin: "sha256:abc"}, want: "foo#sha256:abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.input.String())
		})
	}
}

func TestModuleRefIsZero(t *testing.T) {
	t.Parallel()
	require.True(t, (Ref{}).IsZero(), "the zero Ref reports IsZero")
	require.False(t, (Ref{Path: "x"}).IsZero(), "a populated Ref is not zero")
}
