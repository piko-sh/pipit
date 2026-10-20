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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/module"
)

func TestInferCapabilitiesDetectsNetworkImports(t *testing.T) {
	t.Parallel()
	sources := map[string]map[string]string{
		"pkg": {
			"main.go": `package pkg
import "net/http"
func Use() { _ = http.Get }`,
		},
	}
	got := InferCapabilities(sources)
	want := module.CapabilitySet{
		{Axis: "network.dial"},
		{Axis: "network.listen"},
	}
	require.Equal(t, want, got)
}

func TestInferCapabilitiesDetectsOSImports(t *testing.T) {
	t.Parallel()
	sources := map[string]map[string]string{
		"pkg": {
			"main.go": `package pkg
import "os"
func Use() { _ = os.Open }`,
		},
	}
	got := InferCapabilities(sources)
	axes := map[string]bool{}
	for _, capability := range got {
		axes[capability.Axis] = true
	}
	for _, want := range []string{"filesystem.read", "filesystem.write", "env.read", "env.write"} {
		assert.True(t, axes[want], "InferCapabilities finds %q", want)
	}
}

func TestInferCapabilitiesIgnoresHarmlessImports(t *testing.T) {
	t.Parallel()
	sources := map[string]map[string]string{
		"pkg": {
			"main.go": `package pkg
import (
    "path/filepath"
    "net/url"
)
func Use() { _ = filepath.Join; _ = url.Parse }`,
		},
	}
	got := InferCapabilities(sources)
	require.Empty(t, got, "path/filepath and net/url need no capabilities")
}

func TestInferCapabilitiesTolerantOfParseErrors(t *testing.T) {
	t.Parallel()
	sources := map[string]map[string]string{
		"pkg": {
			"broken.go": "this is not valid Go",
			"main.go": `package pkg
import "net/http"
func Use() { _ = http.Get }`,
		},
	}
	got := InferCapabilities(sources)
	require.NotEmpty(t, got, "capabilities are inferred from the valid file")
}

func TestMergeCapabilitiesDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()
	a := module.CapabilitySet{
		{Axis: "network.dial"},
		{Axis: "filesystem.read"},
	}
	b := module.CapabilitySet{
		{Axis: "network.dial"},
		{Axis: "exec"},
	}
	got := MergeCapabilities(a, b)
	want := module.CapabilitySet{
		{Axis: "exec"},
		{Axis: "filesystem.read"},
		{Axis: "network.dial"},
	}
	require.Equal(t, want, got)
}

func TestInferCapabilitiesEmptySources(t *testing.T) {
	t.Parallel()
	got := InferCapabilities(nil)
	require.Empty(t, got, "empty sources need no capabilities")
}
