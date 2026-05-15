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

// Command pipit-worker is the process the experimental isolated tier runs source code in.
//
// The host starts it through NewIsolatedWorker, NewIsolatedSession or `pipit isolated`,
// after checking the binary against a SHA-256 digest the host approved. It takes no
// command-line arguments: its policy and protocol stream arrive as inherited descriptors.
// It confines itself with namespaces, privilege removal, a seccomp syscall filter and
// resource limits before it reads any input, then compiles and runs each submission
// against the standard library and returns the result to the host.
//
// It runs only on Linux amd64 and arm64. On other platforms it exits with an error
// without running anything. Build it statically with CGO_ENABLED=0.
package main
