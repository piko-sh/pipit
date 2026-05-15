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

// Package buildinfo carries CLI version information injected at build time via -ldflags.
// It is kept in a tiny dedicated package so the linker flag string stays short and
// stable.
package buildinfo

import (
	"runtime/debug"

	"pipit.sh/pipit"
)

// Version is the CLI release version. Build tooling sets it via -ldflags "-X
// 'pipit.sh/pipit/cmd/pipit/internal/buildinfo.Version=...'".
var Version = "dev"

// PipitLibraryVersion returns the version of the embedded pipit library.
//
// Returns string which is the version of the embedded pipit library.
func PipitLibraryVersion() string {
	return pipit.Version
}

// BytecodeVersion returns the on-disk bytecode schema version produced by the embedded
// interpreter.
//
// Returns string which is the on-disk bytecode schema version.
func BytecodeVersion() string {
	return pipit.BytecodeVersion
}

// InterpreterModuleVersion returns the version of the linked pipit.sh/pipit module.
//
// Falls back to "unknown" when build info is unavailable.
//
// Returns string which is the module version, or "unknown".
func InterpreterModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "pipit.sh/pipit" {
			if dep.Replace != nil {
				return dep.Replace.Version + " (replace)"
			}
			return dep.Version
		}
	}
	return "unknown"
}

// GoVersion returns the Go toolchain version that compiled pipit.
//
// Returns string which is the Go toolchain version.
func GoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return info.GoVersion
}
