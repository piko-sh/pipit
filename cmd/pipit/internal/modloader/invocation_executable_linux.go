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

package modloader

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
)

// maximumApprovalExecutableBytes is the ceiling on the running executable size read for
// approval identity.
const maximumApprovalExecutableBytes = 128 << 20

// invocationExecutable caches the running executable's approval identity so it is
// measured only once.
var invocationExecutable = sync.OnceValues(readInvocationExecutable)

// readInvocationExecutable measures the kernel-selected running Linux executable.
//
// Returns string which is a cached approval identity without following its replaceable
// original path.
// Returns error which reports read or validation failure.
func readInvocationExecutable() (identity string, result error) {
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return "", err
	}
	defer func() {
		result = errors.Join(result, file.Close())
		if result != nil {
			identity = ""
		}
	}()
	return hashInvocationExecutable(file)
}

// hashInvocationExecutable streams a bounded regular executable into its approval digest.
//
// Takes file (*os.File) which is an owned-by-caller descriptor. Never closed or reopened.
//
// Returns string which is the domain-labelled digest.
// Returns error which is set without a partial identity.
func hashInvocationExecutable(file *os.File) (string, error) {
	before, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximumApprovalExecutableBytes {
		return "", fs.ErrInvalid
	}
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(file, maximumApprovalExecutableBytes+1))
	if err != nil {
		return "", err
	}
	after, err := file.Stat()
	if err != nil {
		return "", err
	}
	if read != before.Size() || after.Size() != before.Size() ||
		!after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
		return "", errors.New("modloader: executable changed during approval capture")
	}
	return fmt.Sprintf("linux-executable-sha256:%x", digest.Sum(nil)), nil
}
