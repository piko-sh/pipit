//go:build linux && (amd64 || arm64)

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

package sandboxbroker

import (
	"errors"
	"io"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// read opens a path-only handle before checking regular-file type and link count.
// Reopening through pinned procfs refers to that exact inode, not a mutable path.
//
// Takes root (*os.File) which is the pinned grant directory from a valid reservation.
// Takes name (string) which is the validated relative path from a valid reservation.
// Takes limit (int) which is the maximum byte count from a valid reservation.
//
// Returns at most limit bytes from the approved inode, or an error.
func (backend *LinuxFilesystem) read(root *os.File, name string, limit int) (data []byte, result error) {
	pinned, err := openLinuxBeneath(root, name, unix.O_PATH)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, pinned.Close()) }()
	var stat unix.Stat_t
	if err := unix.Fstat(int(pinned.Fd()), &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return nil, ErrDenied
	}
	descriptor, err := unix.Openat(int(backend.proc.Fd()), strconv.FormatUint(uint64(pinned.Fd()), 10),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	source := os.NewFile(uintptr(descriptor), "broker-read")
	defer func() { result = errors.Join(result, source.Close()) }()
	var opened unix.Stat_t
	if err := unix.Fstat(descriptor, &opened); err != nil {
		return nil, err
	}
	if opened.Dev != stat.Dev || opened.Ino != stat.Ino || opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Nlink != 1 {
		return nil, ErrDenied
	}
	return io.ReadAll(io.LimitReader(source, int64(limit)))
}

// listLinuxDirectory opens a separate directory description for each request. Only
// bounded portable names are returned, without stat calls on child entries.
//
// Takes root (*os.File) which is the pinned grant directory from a valid reservation.
// Takes name (string) which is the validated relative path from a valid reservation.
// Takes limit (int) which is the maximum entry count from a valid reservation.
//
// Returns at most limit names, each at most maximumNameBytes bytes.
func listLinuxDirectory(root *os.File, name string, limit int) (names []string, skipped int, result error) {
	directory, err := openLinuxBeneath(root, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK)
	if err != nil {
		return nil, 0, err
	}
	defer func() { result = errors.Join(result, directory.Close()) }()
	read, err := directory.Readdirnames(limit)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, err
	}

	names = read[:0]
	for _, entry := range read {
		if !validRelativePath(entry, false) || !validPathComponent(entry) {
			skipped++
			continue
		}
		names = append(names, entry)
	}
	return names, skipped, nil
}
