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

package cli

import (
	"context"
	"fmt"
	"runtime"

	"pipit.sh/pipit/cmd/pipit/internal/buildinfo"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// RunVersion prints CLI, library, bytecode and Go runtime versions.
//
// Takes streams (output.IO) which supplies the output writers.
//
// Returns int which is the process exit code, always 0.
func RunVersion(_ context.Context, _ []string, streams output.IO) int {
	fmt.Fprintf(streams.Stdout, "pipit            %s\n", buildinfo.Version)
	fmt.Fprintf(streams.Stdout, "library          %s\n", buildinfo.PipitLibraryVersion())
	fmt.Fprintf(streams.Stdout, "bytecode schema  %s\n", buildinfo.BytecodeVersion())
	fmt.Fprintf(streams.Stdout, "interpreter module %s\n", buildinfo.InterpreterModuleVersion())
	fmt.Fprintf(streams.Stdout, "go               %s (%s/%s)\n", buildinfo.GoVersion(), runtime.GOOS, runtime.GOARCH)
	return 0
}
