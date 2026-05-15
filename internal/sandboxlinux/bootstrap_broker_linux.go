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

package sandboxlinux

import (
	"errors"

	"pipit.sh/pipit/internal/sandboxbroker"
)

// BootstrapFilesystemBroker adopts approved roots and seals the broker process inside a
// disposable executable launched with WorkerAttributes.
//
// IPC and procfs occupy descriptors 3 and 4; sealed policy starts at descriptor 5. No
// protocol input is read until inherited authority, mounts, privileges, Landlock and the
// broker syscall profile have all been sealed. The host must provide independent resource
// placement, deadlines, output bounds, descendant reaping and crash recovery.
//
// Returns *WorkerIO which is the sealed worker I/O handles.
// Returns *LinuxFilesystem which is the filesystem backend, ready after every seal stage
// succeeds.
// Returns error which is non-nil after closing owned resources; the entrypoint must exit.
func BootstrapFilesystemBroker() (*WorkerIO, *sandboxbroker.LinuxFilesystem, error) {
	workerIO, err := prepareWorkerIO()
	if err != nil {
		return nil, nil, err
	}
	backend, err := adoptBrokerFilesystem()
	if err != nil {
		return nil, nil, errors.Join(err, workerIO.Stream().Close())
	}
	for _, seal := range []func() error{
		sealWorkerDescriptors, sealWorkerFilesystem, sealWorkerPrivileges,
		backend.SealProcess, workerIO.installFilesystemBrokerFilter,
	} {
		if err := seal(); err != nil {
			return nil, nil, errors.Join(err, backend.Close(), workerIO.Stream().Close())
		}
	}
	return workerIO, backend, nil
}

// adoptBrokerFilesystem opens the current descriptor table through verified procfs.
// Approved root copies are close-on-exec before the inherited-descriptor audit; policy
// and original root handles are consumed by the adoption routine.
//
// Returns an independently owned backend, or error after closing temporary handles.
func adoptBrokerFilesystem() (*sandboxbroker.LinuxFilesystem, error) {
	directory, err := openOwnProcSelfFd()
	if err != nil {
		return nil, err
	}
	backend, err := sandboxbroker.AdoptInheritedLinuxFilesystem(directory)
	err = errors.Join(err, directory.Close())
	if err != nil {
		if backend != nil {
			err = errors.Join(err, backend.Close())
		}
		return nil, err
	}
	return backend, nil
}
