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

// Package hostmem reports how much memory the host is willing to give the process.
//
// The register arena sizes its slabs against the smaller of the machine's physical memory
// and the cgroup limit the process runs under, so a container with a low limit does not
// get an arena sized for the host it happens to sit on.
//
// Detection is best-effort: an unreadable cgroup file or an unsupported platform yields
// zero, and the caller falls back to a fixed default.
package hostmem
