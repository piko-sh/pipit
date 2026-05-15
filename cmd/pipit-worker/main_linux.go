//go:build linux && (amd64 || arm64)

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

package main

import (
	"fmt"
	"os"

	"pipit.sh/pipit/internal/sandboxlinux"
	"pipit.sh/pipit/internal/sandboxworker"
	"pipit.sh/pipit/sdk/stdlib"
)

// main starts only as a host-launched, confined worker with immutable reviewed policy.
func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "worker does not accept command-line input")
		os.Exit(1)
	}
	workerIO, err := sandboxlinux.BootstrapWorker()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	err = sandboxworker.Serve(workerIO.Stream(), sandboxworker.WithSymbols(stdlib.Providers()...))
	closeErr := workerIO.Stream().Close()
	if err != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "worker protocol failed:", err, closeErr)
		os.Exit(1)
	}
}
