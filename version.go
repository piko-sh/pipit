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
	"strings"
)

const (
	// BytecodeVersion is the on-disk bytecode schema version produced by the embedded
	// interpreter. Stored separately so consumers can communicate compatibility without
	// importing internal packages.
	BytecodeVersion = "v12"

	// modulePath is this module's path as it appears in a binary's build info.
	modulePath = "pipit.sh/pipit"

	// develVersion is the Version of a binary that carries no released pipit.
	develVersion = "devel"
)

// Version holds the released semver of the pipit library module, without the leading
// v. Release builds set it using: go build -ldflags "-X pipit.sh/pipit.Version=1.0.0".
var Version string

func init() {
	if Version == "" {
		info, ok := debug.ReadBuildInfo()
		Version = moduleVersion(info, ok)
	}
}

// moduleVersion finds this module's version in a binary's build info.
//
// Takes info (*debug.BuildInfo) which is the binary's build info.
// Takes ok (bool) which reports whether the build info was available.
//
// Returns string which is the version without its leading v, or "devel" when the
// module is absent, replaced by a directory, or built from a working copy.
func moduleVersion(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return develVersion
	}

	module := &info.Main
	if module.Path != modulePath {
		module = nil
		for _, dep := range info.Deps {
			if dep.Path == modulePath {
				module = dep
				break
			}
		}
	}
	if module == nil {
		return develVersion
	}
	if module.Replace != nil {
		module = module.Replace
	}

	if module.Version == "" || module.Version == "(devel)" {
		return develVersion
	}
	return strings.TrimPrefix(module.Version, "v")
}
