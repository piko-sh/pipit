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

//go:build linux && (amd64 || arm64)

package main

import (
	"fmt"
	"os"

	"pipit.sh/pipit/internal/sandboxlinux"
)

// main accepts only the fixed native descriptor handoff, never script input.
func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "watchdog does not accept command-line input")
		os.Exit(1)
	}
	watchdog, err := sandboxlinux.BootstrapHostWatchdog()
	if err != nil {
		fmt.Fprintln(os.Stderr, "watchdog bootstrap failed:", err)
		os.Exit(1)
	}
	runErr := watchdog.Run()
	closeErr := watchdog.Close()
	if runErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "watchdog terminated:", runErr, closeErr)
		os.Exit(1)
	}
}
