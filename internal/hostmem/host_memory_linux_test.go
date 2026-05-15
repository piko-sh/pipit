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
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetectHostMemoryBytesIsPositiveOnLinux(t *testing.T) {
	t.Parallel()
	got := DetectHostMemoryBytes()
	require.Positive(t, got, "/proc/meminfo must yield a total")
	require.Zero(t, got%bytesPerKiB, "the total is a whole number of kibibytes")
}

func TestDetectCgroupMemoryLimitBytesIsStable(t *testing.T) {
	t.Parallel()
	first := DetectCgroupMemoryLimitBytes()
	second := DetectCgroupMemoryLimitBytes()
	require.Equal(t, first, second, "the live cgroup limit must not flap between calls")
}

func TestReadCgroupLimitFrom(t *testing.T) {
	t.Parallel()
	const (
		v2Path = "memory.max"
		v1Path = "memory/memory.limit_in_bytes"
	)
	var nowZero time.Time
	file := func(content string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(content), Mode: 0o444, ModTime: nowZero, Sys: nil}
	}
	cases := []struct {
		name string
		fsys fstest.MapFS
		want uint64
	}{
		{name: "no cgroup files", fsys: fstest.MapFS{}, want: 0},
		{name: "v2 limit", fsys: fstest.MapFS{v2Path: file("1073741824\n")}, want: 1 << 30},
		{name: "v2 unlimited falls through to nothing", fsys: fstest.MapFS{v2Path: file("max\n")}, want: 0},
		{name: "v2 unlimited falls back to v1", fsys: fstest.MapFS{v2Path: file("max"), v1Path: file("536870912")}, want: 512 << 20},
		{name: "v2 wins over v1 when both are finite", fsys: fstest.MapFS{v2Path: file("4096"), v1Path: file("8192")}, want: 4096},
		{name: "v1 only", fsys: fstest.MapFS{v1Path: file("  2097152  \n")}, want: 2 << 20},
		{name: "v1 kernel unlimited sentinel is reported verbatim", fsys: fstest.MapFS{v1Path: file("9223372036854771712")}, want: 9223372036854771712},
		{name: "empty v2 content falls back to v1", fsys: fstest.MapFS{v2Path: file(""), v1Path: file("100")}, want: 100},
		{name: "whitespace-only v2 content falls back to v1", fsys: fstest.MapFS{v2Path: file("  \n"), v1Path: file("100")}, want: 100},
		{name: "garbage v2 content falls back to v1", fsys: fstest.MapFS{v2Path: file("lots"), v1Path: file("100")}, want: 100},
		{name: "negative v2 content falls back to v1", fsys: fstest.MapFS{v2Path: file("-1"), v1Path: file("100")}, want: 100},
		{name: "zero is not a limit", fsys: fstest.MapFS{v2Path: file("0"), v1Path: file("0")}, want: 0},
		{name: "garbage everywhere", fsys: fstest.MapFS{v2Path: file("x"), v1Path: file("y")}, want: 0},
		{name: "oversized content is truncated and rejected", fsys: fstest.MapFS{v2Path: file(strings.Repeat("9", cgroupMemoryFileReadLimit+8))}, want: 0},
		{name: "content at the read limit still parses", fsys: fstest.MapFS{v2Path: file("18446744073709551615" + strings.Repeat(" ", cgroupMemoryFileReadLimit-20))}, want: ^uint64(0)},
		{name: "v2 path is a directory", fsys: fstest.MapFS{v2Path + "/child": file("1")}, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, readCgroupLimitFrom(tc.fsys))
		})
	}
}
