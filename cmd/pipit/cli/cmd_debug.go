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

	"pipit.sh/pipit/cmd/pipit/internal/debug_tui"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// RunDebug handles `pipit debug <file>`.
//
// Takes args ([]string) which holds the positional command arguments.
// Takes streams (output.IO) which carries the standard I/O streams.
//
// Returns int which is the exit code: 0 on clean exit, 1 on error.
func RunDebug(ctx context.Context, args []string, streams output.IO) int {
	if len(args) < 1 {
		fmt.Fprintln(streams.Stderr, "Usage: pipit debug <file>")
		return 1
	}
	return debug_tui.Run(ctx, args[0], streams, ExtraSymbols(ctx), loggerOptions(ctx)...)
}
