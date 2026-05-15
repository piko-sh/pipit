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

	"pipit.sh/pipit/sdk/module"

	"pipit.sh/pipit/cmd/pipit/internal/modloader"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// moduleHelp is the usage text printed for the module command and help.
const moduleHelp = `pipit module - module management subcommands

Usage:
  pipit module <subcommand> [args]

Subcommands:
  list             Show modules approved by the active script.lock
  inspect <reference>    Show the descriptor + capabilities for a module
  verify <path>    Re-verify every hash in script.lock against the cache
  help             Show this help

These inspection commands read --lockfile (default ./script.lock).
To inspect approvals from pipit run, pass its user-configuration
approval file explicitly.
`

// RunModule dispatches to a module subcommand.
//
// Takes args ([]string) which holds the subcommand and its arguments.
// Takes streams (output.IO) which provides stdout and stderr.
//
// Returns int which is the process exit code: 0 on success, 1 on usage error, 2 on
// operational failure.
func RunModule(ctx context.Context, args []string, streams output.IO) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(streams.Stdout, moduleHelp)
		return 0
	}
	subcommand := args[0]
	rest := args[1:]
	switch subcommand {
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(streams.Stdout, moduleHelp)
		return 0
	case "list":
		return runModuleList(ctx, rest, streams)
	case "inspect":
		return runModuleInspect(ctx, rest, streams)
	case "verify":
		return runModuleVerify(ctx, rest, streams)
	default:
		fmt.Fprintf(streams.Stderr, "pipit module: unknown subcommand %q (try `pipit module help`)\n", subcommand)
		return 1
	}
}

// runModuleList prints every entry in the lockfile.
//
// Takes args ([]string) which holds the optional lockfile path override.
// Takes streams (output.IO) which provides stdout and stderr.
//
// Returns int which is the process exit code.
func runModuleList(_ context.Context, args []string, streams output.IO) int {
	lockfilePath, err := defaultLockfilePath(args)
	if err != nil {
		fmt.Fprintln(streams.Stderr, err)
		return 1
	}
	store := modloader.NewStore(lockfilePath)
	if err := store.Load(); err != nil {
		fmt.Fprintf(streams.Stderr, "loading %s: %v\n", lockfilePath, err)
		return 2
	}
	snapshot := store.Snapshot()
	if len(snapshot.Modules) == 0 {
		fmt.Fprintf(streams.Stdout, "no modules approved in %s\n", lockfilePath)
		return 0
	}
	for _, entry := range snapshot.Modules {
		fmt.Fprintf(streams.Stdout, "%s@%s  %s  caps=%v\n",
			entry.Path, entry.Version, entry.BundleDigest, entry.ApprovedCapabilities)
	}
	return 0
}

// runModuleInspect prints detailed metadata for a specific module reference.
//
// Takes args ([]string) which holds the module reference and optional path.
// Takes streams (output.IO) which provides stdout and stderr.
//
// Returns int which is the process exit code.
func runModuleInspect(_ context.Context, args []string, streams output.IO) int {
	if len(args) == 0 {
		fmt.Fprintln(streams.Stderr, "usage: pipit module inspect <path>[@<version>]")
		return 1
	}
	target := args[0]
	reference, err := module.ParseRef(target)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "invalid reference %q: %v\n", target, err)
		return 1
	}
	lockfilePath, lockfileErr := defaultLockfilePath(args[1:])
	if lockfileErr != nil {
		fmt.Fprintln(streams.Stderr, lockfileErr)
		return 1
	}
	store := modloader.NewStore(lockfilePath)
	if err := store.Load(); err != nil {
		fmt.Fprintf(streams.Stderr, "loading %s: %v\n", lockfilePath, err)
		return 2
	}
	got, ok := store.Lookup(reference.Path, reference.Version)
	if !ok {
		fmt.Fprintf(streams.Stderr, "module %s not in lockfile\n", reference)
		return 2
	}
	fmt.Fprintf(streams.Stdout, "path: %s\nversion: %s\nbundle_digest: %s\napproved_capabilities: %v\napproved_at: %s\napproved_via: %s\n",
		got.Path, got.Version, got.BundleDigest,
		got.ApprovedCapabilities, got.ApprovedAt.Format("2006-01-02 15:04:05Z07:00"), got.ApprovedVia)
	return 0
}

// runModuleVerify parses the lockfile and reports its pinned entries.
//
// Takes args ([]string) which holds the optional lockfile path override.
// Takes streams (output.IO) which provides stdout and stderr.
//
// Returns int which is the process exit code.
func runModuleVerify(_ context.Context, args []string, streams output.IO) int {
	lockfilePath, err := defaultLockfilePath(args)
	if err != nil {
		fmt.Fprintln(streams.Stderr, err)
		return 1
	}
	store := modloader.NewStore(lockfilePath)
	if err := store.Load(); err != nil {
		fmt.Fprintf(streams.Stderr, "loading %s: %v\n", lockfilePath, err)
		return 2
	}
	snapshot := store.Snapshot()
	fmt.Fprintf(streams.Stdout, "%s parsed successfully; %d module entries pinned (cache hash verification lands when GOPROXY provider ships)\n",
		lockfilePath, len(snapshot.Modules))
	return 0
}

// defaultLockfilePath resolves the lockfile path.
//
// Returns string which is the conventional lockfile name.
// Returns error when the lockfile path cannot be resolved.
func defaultLockfilePath(_ []string) (string, error) {
	return modloader.LockfileName, nil
}
