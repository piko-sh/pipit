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
	"errors"
	"fmt"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// runIsolatedRecover reclaims what dead hosts left under the tenant's state directory:
// worker and session records always, filesystem records when the broker approval and the
// original root grants are given too.
//
// Takes args ([]string) which carries the worker flags the launches used.
// Takes streams (output.IO) which receives the report.
//
// Returns int which is zero only when every recoverable record was recovered.
func runIsolatedRecover(ctx context.Context, args []string, streams output.IO) int {
	flags, options := isolatedFlagSet("pipit isolated recover", streams)
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit isolated recover [worker flags] [-broker PATH -broker-sha256 HEX -root name=path:rights ...]")
		fmt.Fprintln(streams.Stderr, "Recovers the launch records of dead hosts for one tenant; live hosts are left alone.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if len(flags.Args()) != 0 {
		fmt.Fprintln(streams.Stderr, "pipit: isolated recover takes no positional arguments")
		return 1
	}
	config, err := isolatedConfiguration(options)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	filesystem, hasFilesystem, err := isolatedFilesystemConfiguration(options, config)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if ctx == nil || ctx.Err() != nil {
		fmt.Fprintln(streams.Stderr, "pipit: isolated recovery has no active context")
		return 1
	}
	if err := recoverIsolatedWorkers(ctx, config); err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if hasFilesystem {
		if err := recoverIsolatedFilesystems(ctx, &filesystem); err != nil {
			fmt.Fprintf(streams.Stderr, errorFormat, err)
			return 1
		}
	}
	if _, err := fmt.Fprintln(streams.Stdout, "Isolated recovery completed."); err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	return 0
}

// recoverIsolatedWorkers claims and recovers the tenant's worker and session records.
//
// Takes config (pipit.IsolatedConfig) which the launches used.
//
// Returns error when a record could not be claimed, recovered or released.
func recoverIsolatedWorkers(ctx context.Context, config pipit.IsolatedConfig) error {
	recovery, err := pipit.NewIsolatedRecovery(ctx, config)
	if err != nil {
		return err
	}
	return errors.Join(recovery.Recover(ctx), recovery.Close())
}

// recoverIsolatedFilesystems claims and recovers the tenant's filesystem records.
//
// Takes config (*pipit.IsolatedFilesystemConfig) which the launches used.
//
// Returns error when a record could not be claimed, recovered or released.
func recoverIsolatedFilesystems(ctx context.Context, config *pipit.IsolatedFilesystemConfig) error {
	recovery, err := pipit.NewIsolatedFilesystemRecovery(ctx, *config)
	if err != nil {
		return err
	}
	return errors.Join(recovery.Recover(ctx), recovery.Close())
}
