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

//go:build linux && (amd64 || arm64)

package sandboxlinux

import (
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// maximumRecoveryStatusBytes is the read limit for procfs status files.
	maximumRecoveryStatusBytes = 1 << 20

	// recoveryCredentialIDCount is the expected field count for Uid and Gid lines.
	recoveryCredentialIDCount = 4

	// recoveryDecimalBase is the numeric base for decimal identity fields.
	recoveryDecimalBase = 10

	// recoveryHexBase is the numeric base for hexadecimal capability fields.
	recoveryHexBase = 16

	// recoveryIDBits is the bit width for user and group identifiers.
	recoveryIDBits = 32

	// recoveryCapabilityBits is the bit width for kernel capability masks.
	recoveryCapabilityBits = 64
)

// recoveryHostSecurity captures saved/filesystem IDs and kernel privilege metadata.
//
// Takes proc (*os.File) which is the verified procfs while the caller remains on a locked
// operating-system thread.
//
// Returns canonical selected status fields, not a claim to fingerprint LSM policy.
func recoveryHostSecurity(proc *os.File) (fields map[string]string, result error) {
	descriptor, err := unix.Openat(int(proc.Fd()), "thread-self/status", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), "recovery-host-security")
	defer func() { result = errors.Join(result, file.Close()) }()
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(descriptor, &filesystem); err != nil || filesystem.Type != unix.PROC_SUPER_MAGIC {
		return nil, errors.Join(ErrUnavailable, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumRecoveryStatusBytes+1))
	if err != nil {
		return nil, err
	}
	return decodeRecoveryHostSecurity(data)
}

// decodeRecoveryHostSecurity selects a complete bounded set of kernel status fields.
//
// Takes data ([]byte) which is the kernel metadata, never script assertions about the
// host principal.
//
// Returns no partial identity for missing, duplicated, malformed or oversized fields.
func decodeRecoveryHostSecurity(data []byte) (map[string]string, error) {
	if len(data) == 0 || len(data) > maximumRecoveryStatusBytes {
		return nil, ErrUnavailable
	}
	names := []string{"Uid", "Gid", "Groups", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs", "Seccomp", "Seccomp_filters"}
	selected := make(map[string]string, len(names))
	for line := range strings.SplitSeq(string(data), "\n") {
		name, value, found := strings.Cut(line, ":")
		if !found || !slices.Contains(names, name) {
			continue
		}
		if _, exists := selected[name]; exists {
			return nil, ErrUnavailable
		}
		canonical, err := canonicalRecoverySecurityField(name, strings.Fields(value))
		if err != nil {
			return nil, err
		}
		selected[name] = canonical
	}
	if len(selected) != len(names) {
		return nil, ErrUnavailable
	}
	return selected, nil
}

// canonicalRecoverySecurityField validates one known numeric kernel identity field.
//
// Takes name (string) which is the internally recognised field.
// Takes values ([]string) which holds the bounded whitespace-separated values.
//
// Returns a canonical encoding, sorting supplementary groups without changing authority.
func canonicalRecoverySecurityField(name string, values []string) (string, error) {
	count := 1
	if name == "Uid" || name == "Gid" {
		count = recoveryCredentialIDCount
	}
	if name == "Groups" {
		count = len(values)
	}
	if len(values) != count || count > maximumRecoveryGroups {
		return "", ErrUnavailable
	}
	base, bits := recoveryDecimalBase, recoveryIDBits
	if strings.HasPrefix(name, "Cap") {
		base, bits = recoveryHexBase, recoveryCapabilityBits
	}
	canonical := make([]string, len(values))
	for index, value := range values {
		number, err := strconv.ParseUint(value, base, bits)
		if err != nil {
			return "", err
		}
		if name == "NoNewPrivs" && number > 1 || name == "Seccomp" && number > 2 {
			return "", ErrUnavailable
		}
		canonical[index] = strconv.FormatUint(number, base)
	}
	if name == "Groups" {
		slices.Sort(canonical)
		canonical = slices.Compact(canonical)
	}
	return strings.Join(canonical, " "), nil
}
