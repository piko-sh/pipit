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

	cases := []struct {
		importPath string
		module     string
		sub        string
	}{
		{importPath: "github.com/tidwall/gjson", module: "github.com/tidwall/gjson", sub: ""},
		{importPath: "github.com/alecthomas/chroma/v2/formatters", module: "github.com/alecthomas/chroma/v2", sub: "formatters"},
		{importPath: "github.com/alecthomas/chroma/v2", module: "github.com/alecthomas/chroma/v2", sub: ""},
		{importPath: "github.com/org/repo/v1/sub", module: "github.com/org/repo", sub: "v1/sub"},
		{importPath: "github.com/org/repo/vendor/x", module: "github.com/org/repo", sub: "vendor/x"},
		{importPath: "gopkg.in/yaml.v3", module: "gopkg.in/yaml.v3", sub: ""},
	}
	for _, testCase := range cases {
		module, sub := splitModuleFromPackage(testCase.importPath)
		require.Equal(t, testCase.module, module, testCase.importPath)
		require.Equal(t, testCase.sub, sub, testCase.importPath)
	}
}
