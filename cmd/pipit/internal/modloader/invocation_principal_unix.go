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

//go:build unix

package modloader

import (
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"syscall"
)

// unixPrincipalFields is the number of fixed credential fields before supplementary
// groups.
const unixPrincipalFields = 7

// invocationPrincipal binds Unix credentials and the current directory inode.
//
// Takes directory (fs.FileInfo) which carries the metadata for the path used to resolve
// relative host paths.
//
// Returns []string which holds framed identity fields, not an authenticated application
// tenant identity.
// Returns error which reports unsupported metadata or too many groups.
func invocationPrincipal(directory fs.FileInfo) ([]string, error) {
	metadata, ok := directory.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fs.ErrInvalid
	}
	groups, err := os.Getgroups()
	if err != nil {
		return nil, err
	}
	if len(groups) > maxInvocationArguments-unixPrincipalFields-invocationContextFields {
		return nil, fs.ErrInvalid
	}
	return unixPrincipalIdentity(os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid(),
		groups, fmt.Sprint(metadata.Dev), fmt.Sprint(metadata.Ino)), nil
}

// unixPrincipalIdentity canonicalises host credentials without changing caller slices.
//
// Takes uid (int) which is the real user ID.
// Takes effectiveUID (int) which is the effective user ID.
// Takes gid (int) which is the real group ID.
// Takes effectiveGID (int) which is the effective group ID.
// Takes groups ([]int) which holds the supplementary group IDs.
// Takes device (string) which identifies the directory device.
// Takes inode (string) which identifies the directory inode.
//
// Returns []string with ordered fields for the length-framed invocation hash.
func unixPrincipalIdentity(uid, effectiveUID, gid, effectiveGID int, groups []int, device, inode string) []string {
	canonicalGroups := slices.Clone(groups)
	slices.Sort(canonicalGroups)
	canonicalGroups = slices.Compact(canonicalGroups)
	fields := make([]string, 0, unixPrincipalFields+len(canonicalGroups))
	fields = append(fields, "unix-credentials-v1", strconv.Itoa(uid), strconv.Itoa(effectiveUID),
		strconv.Itoa(gid), strconv.Itoa(effectiveGID), device, inode)
	for _, group := range canonicalGroups {
		fields = append(fields, strconv.Itoa(group))
	}
	return fields
}
