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

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/sdk/extract"
)

// RunExtract handles `pipit extract`, which generates and checks the symbol tables that
// let scripts import native Go packages.
//
// Takes args ([]string) which holds the subcommand and its arguments.
// Takes streams (output.IO) which supplies the standard IO streams.
//
// Returns int which is the exit code: 0 on success, 1 on failure.
func RunExtract(ctx context.Context, args []string, streams output.IO) int {
	return extract.RunCommand(ctx, args, streams.Stdout, streams.Stderr, extractConfig())
}

// extractConfig adapts the shared extract command to pipit: pipit names the tool, the
// standard library is already provided, and a project's own packages are compiled from
// source rather than registered.
//
// Returns extract.Config which configures the command for pipit.
func extractConfig() extract.Config {
	return extract.Config{
		AlreadyProvided:     pipit.StandardLibraryPaths,
		ToolName:            "pipit extract",
		DefaultManifest:     "pipit-symbols.yaml",
		InitPackage:         "symbols",
		InitDirectory:       "symbols",
		SourceDirs:          nil,
		Scanners:            nil,
		IgnoreProjectModule: true,
	}
}
