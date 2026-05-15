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

// Command pipit-filesystem-broker gives an isolated pipit worker controlled access to
// host directories.
//
// The host starts it next to the worker after verifying the binary's SHA-256 digest. It
// receives host-granted roots and their rights as inherited descriptors, then seals
// itself with namespaces, privilege removal, Landlock and a syscall filter before reading
// any request. Scripts use pipit/fs; the broker serves those requests within the granted
// roots only.
//
// Linux amd64 and arm64 only, requires Landlock ABI 8. Build statically with
// CGO_ENABLED=0.
package main
