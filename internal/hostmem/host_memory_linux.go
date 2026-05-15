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

//go:build linux

package hostmem

import (
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

const (
	// hostMemInfoPath is the kernel-exported file that exposes total physical RAM on Linux.
	// The MemTotal line is reported in kibibytes.
	hostMemInfoPath = "/proc/meminfo"

	// hostMemInfoKeyPrefix is the line prefix in /proc/meminfo carrying the total
	// physical-memory value.
	hostMemInfoKeyPrefix = "MemTotal:"

	// hostMemInfoUnitKiB names the suffix /proc/meminfo writes after the total memory value.
	hostMemInfoUnitKiB = "kB"

	// bytesPerKiB is the multiplier to convert /proc/meminfo's kibibyte figures into bytes.
	bytesPerKiB uint64 = 1024

	// hostMemInfoExpectedFieldCount is the minimum number of whitespace-separated tokens a
	// well-formed MemTotal line must have ("MemTotal:", "<number>", "kB").
	hostMemInfoExpectedFieldCount = 3

	// cgroupMemoryBasePath is the root of the cgroup filesystem, opened read-only through a
	// directory-rooted handle so a hostile symlink cannot escape the cgroup tree.
	cgroupMemoryBasePath = "/sys/fs/cgroup"

	// cgroupMemoryFileReadLimit bounds the bytes read from a cgroup pseudo-file so a hostile
	// bind mount cannot force an unbounded allocation. Real limit files hold a short
	// decimal.
	cgroupMemoryFileReadLimit = 64
)

// DetectCgroupMemoryLimitBytes reads the container memory ceiling from the cgroup
// filesystem, trying cgroup v2 before the v1 fallback. Returns 0 when unconstrained or
// undetectable.
//
// Returns uint64 which is the cgroup memory limit in bytes, or 0.
func DetectCgroupMemoryLimitBytes() uint64 {
	root, err := os.OpenRoot(cgroupMemoryBasePath)
	if err != nil {
		return 0
	}
	defer func() { _ = root.Close() }()

	return readCgroupLimitFrom(root.FS())
}

// DetectHostMemoryBytes parses /proc/meminfo and returns the host's total physical memory
// in bytes, or zero when the value cannot be determined.
//
// Returns uint64 which is the total host RAM in bytes, or 0 on any detection failure.
func DetectHostMemoryBytes() uint64 {
	data, err := os.ReadFile(hostMemInfoPath)
	if err != nil {
		return 0
	}
	for line := range strings.Lines(string(data)) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, hostMemInfoKeyPrefix) {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < hostMemInfoExpectedFieldCount || fields[2] != hostMemInfoUnitKiB {
			return 0
		}
		kib, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return 0
		}
		if kib > ^uint64(0)/bytesPerKiB {
			return 0
		}
		return kib * bytesPerKiB
	}
	return 0
}

// readCgroupLimitFrom reads the cgroup memory ceiling from fsys, which is rooted at the
// cgroup filesystem, trying cgroup v2 (memory.max) before the cgroup v1 fallback
// (memory/memory.limit_in_bytes). It is the seam DetectCgroupMemoryLimitBytes() reads
// through so a test can supply a synthetic cgroup tree.
//
// Takes fsys (fs.FS) which is rooted at the cgroup filesystem.
//
// Returns uint64 which is the cgroup memory limit in bytes, or 0 when unconstrained or
// undetectable.
func readCgroupLimitFrom(fsys fs.FS) uint64 {
	for _, name := range []string{"memory.max", "memory/memory.limit_in_bytes"} {
		file, openErr := fsys.Open(name)
		if openErr != nil {
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(file, cgroupMemoryFileReadLimit))
		_ = file.Close()
		if readErr != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" || content == "max" {
			continue
		}
		if limit, parseErr := strconv.ParseUint(content, 10, 64); parseErr == nil && limit > 0 {
			return limit
		}
	}
	return 0
}
