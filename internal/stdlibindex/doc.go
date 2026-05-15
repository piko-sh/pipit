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

// Package stdlibindex names the standard-library packages the symbol tables register,
// without referencing any of them.
//
// The tables themselves cost around 17 MiB of binary and live in
// pipit.sh/pipit/sdk/stdlib, which a host opts into. This list is a few kilobytes with no
// init work, so the parts of the interpreter that only need to know whether a path is a
// standard-library package can link it unconditionally.
package stdlibindex
