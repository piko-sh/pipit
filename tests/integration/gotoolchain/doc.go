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

//go:build integration

// Package gotoolchain runs the Go toolchain's own test directory ($GOROOT/test) against
// the pipit binary.
//
// The run-mode tests (first line `// run`) are self-checking programs that panic or print
// a diagnostic when the result is wrong, making them a free adversarial corpus for the
// interpreter. The suite locates the local toolchain with `go env GOROOT`, filters to
// pipit-registered packages, runs each file through `pipit run`, and checks against the
// toolchain's own oracle.
//
// Opt-in: `make test-gotoolchain` runs the suite, `make test-gotoolchain-update` records
// the passing set under testdata/expected/<goMajorMinor>.txt. A file that stops passing
// fails the run; files that newly pass are reported for re-recording.
package gotoolchain
