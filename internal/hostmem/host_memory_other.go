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

//go:build !linux

package hostmem

// DetectHostMemoryBytes returns zero on non-Linux platforms because there is no portable
// cross-OS way to query total physical memory without pulling in additional dependencies.
// Callers fall back to a static default budget when the return value is zero.
//
// Returns uint64 which is always 0 on non-Linux platforms.
func DetectHostMemoryBytes() uint64 {
	return 0
}

// DetectCgroupMemoryLimitBytes returns zero on non-Linux platforms because cgroup memory
// limits are a Linux-only mechanism. Callers fall back to the host figure (itself zero
// here, so ultimately the static default budget).
//
// Returns uint64 which is always 0 on non-Linux platforms.
func DetectCgroupMemoryLimitBytes() uint64 {
	return 0
}
