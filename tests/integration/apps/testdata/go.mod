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

// Module appsdata makes the parity programs under this directory standalone.
//
// The harness runs each one with `go run .` and GOWORK=off to get the Go toolchain's
// own answer to compare the interpreter against. Without a go.mod here that walk-up
// finds the suite's module, which requires pipit.sh/pipit at an unpublished version and
// so cannot be verified. These programs use the standard library only, so an empty
// module is all they need. The directory is testdata, which the go tool ignores.

module appsdata

go 1.27.0
