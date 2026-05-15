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

// Command pipit-watchdog is the lifetime supervisor for an isolated pipit worker.
//
// The host starts it next to each worker, after checking the binary against a SHA-256
// digest the host approved. It takes no command-line arguments: it receives handles to
// the host, the worker's cgroup and a kill control as inherited descriptors, confines
// itself, and signals that it is ready. It then waits until the host process dies or the
// fixed deadline passes, and in either case kills the worker, so a crashed host cannot
// leave a script running. It never runs script code.
//
// It runs only on Linux amd64 and arm64. On other platforms it exits with an error
// without running anything. Build it statically with CGO_ENABLED=0.
package main
