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

package pipit

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{name: "no build info", info: nil, ok: false, want: "devel"},
		{
			name: "dependency",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/host", Version: "v1.0.0"},
				Deps: []*debug.Module{{Path: "pipit.sh/pipit", Version: "v0.1.0-alpha.2"}},
			},
			ok:   true,
			want: "0.1.0-alpha.2",
		},
		{
			name: "main module",
			info: &debug.BuildInfo{Main: debug.Module{Path: "pipit.sh/pipit", Version: "v0.2.0"}},
			ok:   true,
			want: "0.2.0",
		},
		{
			name: "workspace build",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "pipit.sh/pipit/cmd/pipit", Version: "(devel)"},
				Deps: []*debug.Module{{Path: "pipit.sh/pipit", Version: "(devel)"}},
			},
			ok:   true,
			want: "devel",
		},
		{
			name: "replaced by a directory",
			info: &debug.BuildInfo{Deps: []*debug.Module{{
				Path:    "pipit.sh/pipit",
				Version: "v0.1.0",
				Replace: &debug.Module{Path: "../pipit"},
			}}},
			ok:   true,
			want: "devel",
		},
		{
			name: "replaced by another version",
			info: &debug.BuildInfo{Deps: []*debug.Module{{
				Path:    "pipit.sh/pipit",
				Version: "v0.1.0",
				Replace: &debug.Module{Path: "example.com/fork", Version: "v0.1.1"},
			}}},
			ok:   true,
			want: "0.1.1",
		},
		{
			name: "not linked",
			info: &debug.BuildInfo{Main: debug.Module{Path: "example.com/host", Version: "v1.0.0"}},
			ok:   true,
			want: "devel",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, moduleVersion(tt.info, tt.ok))
		})
	}
}
