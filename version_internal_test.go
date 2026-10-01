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
)

func TestModuleVersion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"no build info", nil, false, "devel"},
		{
			"dependency",
			&debug.BuildInfo{
				Main: debug.Module{Path: "example.com/host", Version: "v1.0.0"},
				Deps: []*debug.Module{{Path: "pipit.sh/pipit", Version: "v0.1.0-alpha.2"}},
			},
			true,
			"0.1.0-alpha.2",
		},
		{
			"main module",
			&debug.BuildInfo{Main: debug.Module{Path: "pipit.sh/pipit", Version: "v0.2.0"}},
			true,
			"0.2.0",
		},
		{
			"workspace build",
			&debug.BuildInfo{
				Main: debug.Module{Path: "pipit.sh/pipit/cmd/pipit", Version: "(devel)"},
				Deps: []*debug.Module{{Path: "pipit.sh/pipit", Version: "(devel)"}},
			},
			true,
			"devel",
		},
		{
			"replaced by a directory",
			&debug.BuildInfo{Deps: []*debug.Module{{
				Path:    "pipit.sh/pipit",
				Version: "v0.1.0",
				Replace: &debug.Module{Path: "../pipit"},
			}}},
			true,
			"devel",
		},
		{
			"replaced by another version",
			&debug.BuildInfo{Deps: []*debug.Module{{
				Path:    "pipit.sh/pipit",
				Version: "v0.1.0",
				Replace: &debug.Module{Path: "example.com/fork", Version: "v0.1.1"},
			}}},
			true,
			"0.1.1",
		},
		{
			"not linked",
			&debug.BuildInfo{Main: debug.Module{Path: "example.com/host", Version: "v1.0.0"}},
			true,
			"devel",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := moduleVersion(test.info, test.ok); got != test.want {
				t.Fatalf("moduleVersion = %q, want %q", got, test.want)
			}
		})
	}
}
