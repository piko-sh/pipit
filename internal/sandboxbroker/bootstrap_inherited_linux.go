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
	"os"
)

const (
	// LinuxBootstrapFD follows the private IPC socket; no host procfs handle is inherited,
	// so the broker's own bootstrap descriptors begin immediately after descriptor 3.
	LinuxBootstrapFD = 4
)

// AdoptInheritedLinuxFilesystem consumes the fixed broker-only inherited descriptors.
// Each root is independently checked against its sealed inode and mount identity before
// authority is retained.
//
// Takes proc (*os.File) naming this process's verified procfs descriptor table.
//
// Returns an independent backend owner, or error without retained partial copies.
func AdoptInheritedLinuxFilesystem(proc *os.File) (*LinuxFilesystem, error) {
	configuration := os.NewFile(LinuxBootstrapFD, "inherited-broker-policy")
	policy, err := readLinuxBootstrap(configuration)
	if err != nil {
		return nil, errors.Join(err, configuration.Close())
	}
	files := make([]*os.File, len(policy.Roots)+1)
	files[0] = configuration
	for index := range policy.Roots {
		files[index+1] = os.NewFile(uintptr(LinuxBootstrapFD+index+1), "inherited-broker-root")
	}
	backend, result := adoptLinuxFilesystem(files, proc)
	for _, file := range files {
		result = errors.Join(result, file.Close())
	}
	if result != nil {
		if backend != nil {
			result = errors.Join(result, backend.Close())
		}
		return nil, result
	}
	return backend, nil
}
