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

// Command pipit is the standalone CLI for the Pipit Go interpreter.
//
// Usage:
//
//	pipit <command> [flags] [args]
//
// Run `pipit --help` for the full command reference.
//
// To build a customised pipit binary with extra host symbols registered (e.g. net/http),
// depend on pipit.sh/pipit/cmd/pipit/cli and write your own main that calls cli.Main with
// the symbols you want. See examples/webserver for a worked example.
package main

import (
	"os"

	"pipit.sh/pipit/cmd/pipit/cli"
)

// main runs the pipit CLI and exits with the returned status code.
func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
