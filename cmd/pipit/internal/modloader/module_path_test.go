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

package modloader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitModuleFromPackageKeepsMajorVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		importPath string
		wantModule string
		wantSub    string
	}{
		{name: "module root", importPath: "github.com/tidwall/gjson", wantModule: "github.com/tidwall/gjson"},
		{name: "package under a major version", importPath: "github.com/alecthomas/chroma/v2/formatters", wantModule: "github.com/alecthomas/chroma/v2", wantSub: "formatters"},
		{name: "major version root", importPath: "github.com/alecthomas/chroma/v2", wantModule: "github.com/alecthomas/chroma/v2"},
		{name: "v1 is a directory", importPath: "github.com/org/repo/v1/sub", wantModule: "github.com/org/repo", wantSub: "v1/sub"},
		{name: "vendor is a directory", importPath: "github.com/org/repo/vendor/x", wantModule: "github.com/org/repo", wantSub: "vendor/x"},
		{name: "gopkg.in version", importPath: "gopkg.in/yaml.v3", wantModule: "gopkg.in/yaml.v3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			module, sub := splitModuleFromPackage(tt.importPath)
			require.Equal(t, tt.wantModule, module)
			require.Equal(t, tt.wantSub, sub)
		})
	}
}
