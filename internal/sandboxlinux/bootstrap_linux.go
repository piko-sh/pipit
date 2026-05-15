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

import "errors"

// BootstrapWorker seals the disposable worker before any protocol input is read. The host
// launcher must independently establish cgroup placement, supervision and crash recovery.
//
// Returns *WorkerIO only after descriptor, filesystem, capability and syscall sealing.
// Returns error after any failed stage, closing IPC; the entrypoint must then exit.
func BootstrapWorker() (*WorkerIO, error) {
	workerIO, err := prepareWorkerIO()
	if err != nil {
		return nil, err
	}
	for _, seal := range []func() error{
		sealWorkerDescriptors, sealWorkerFilesystem, sealWorkerPrivileges,
		workerIO.installSyscallFilter,
	} {
		if err := seal(); err != nil {
			return nil, errors.Join(err, workerIO.Stream().Close())
		}
	}
	return workerIO, nil
}
