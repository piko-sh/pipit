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
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"

	"pipit.sh/pipit/sdk/module"
)

// capabilityRules is the import-path to capability-axis map used by InferCapabilities. It
// is conservative: a network-shaped import declares network, and touching os.* declares
// filesystem/exec axes.
var capabilityRules = map[string][]string{
	"net":           {"network.dial", "network.listen"},
	"net/http":      {"network.dial", "network.listen"},
	"net/url":       {},
	"net/rpc":       {"network.dial", "network.listen"},
	"net/smtp":      {"network.dial"},
	"net/textproto": {"network.dial"},
	"os":            {"filesystem.read", "filesystem.write", "env.read", "env.write"},
	"os/exec":       {"exec"},
	"io/fs":         {"filesystem.read"},
	"path/filepath": {},
	"crypto/rand":   {"random.system"},
	"syscall":       {"syscall"},
	"runtime":       {},
}

// inferredCapability mirrors module.Capability so this file stays self-contained. Values
// convert to module.Capability before use.
type inferredCapability struct {
	// axis names the capability axis such as "network.dial" or "filesystem.read".
	axis string

	// scope narrows the axis; empty means the whole axis.
	scope string
}

// InferCapabilities returns the capability set implied by the supplied module sources.
// Sources are the same map shape the GOPROXY fetch produces:
// map[packagePath]map[filename]source.
//
// Takes sources (map[string]map[string]string) which is the module's source tree.
//
// Returns the inferred capability set.
func InferCapabilities(sources map[string]map[string]string) module.CapabilitySet {
	seen := make(map[inferredCapability]struct{})
	for _, pkgSources := range sources {
		for filename, content := range pkgSources {
			collectFromFile(filename, content, seen)
		}
	}
	out := make(module.CapabilitySet, 0, len(seen))
	for capability := range seen {
		out = append(out, module.Capability{
			Axis:  capability.axis,
			Scope: capability.scope,
		})
	}
	slices.SortFunc(out, func(a, b module.Capability) int {
		return cmp.Or(cmp.Compare(a.Axis, b.Axis), cmp.Compare(a.Scope, b.Scope))
	})
	return out
}

// MergeCapabilities returns the union of two sets, deduplicated and sorted. Useful when
// combining inferred claims with operator-asserted claims at lockfile-generation time.
//
// Takes a (module.CapabilitySet) which holds the first claim set.
// Takes b (module.CapabilitySet) which holds the second claim set.
//
// Returns CapabilitySet which is the sorted, deduplicated union.
func MergeCapabilities(a, b module.CapabilitySet) module.CapabilitySet {
	seen := make(map[inferredCapability]struct{})
	for _, capability := range a {
		seen[inferredCapability{axis: capability.Axis, scope: capability.Scope}] = struct{}{}
	}
	for _, capability := range b {
		seen[inferredCapability{axis: capability.Axis, scope: capability.Scope}] = struct{}{}
	}
	out := make(module.CapabilitySet, 0, len(seen))
	for capability := range seen {
		out = append(out, module.Capability{
			Axis:  capability.axis,
			Scope: capability.scope,
		})
	}
	slices.SortFunc(out, func(a, b module.Capability) int {
		return cmp.Or(cmp.Compare(a.Axis, b.Axis), cmp.Compare(a.Scope, b.Scope))
	})
	return out
}

// collectFromFile parses one Go source file and records the capability claims implied by
// its import declarations. Parse errors are tolerated so the inferrer keeps running
// across clean and dirty sources.
//
// Takes filename (string) which labels the source for the parser.
// Takes content (string) which is the Go source text to scan.
// Takes seen (map[inferredCapability]struct{}) which accumulates claims.
func collectFromFile(filename, content string, seen map[inferredCapability]struct{}) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, content, parser.ImportsOnly)
	if err != nil {
		return
	}
	for _, importSpec := range file.Imports {
		path := importSpec.Path.Value
		if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
			path = path[1 : len(path)-1]
		}
		axes, ok := capabilityRules[path]
		if !ok {
			continue
		}
		for _, axis := range axes {
			seen[inferredCapability{axis: axis, scope: ""}] = struct{}{}
		}
	}

	_ = ast.Walk
}
