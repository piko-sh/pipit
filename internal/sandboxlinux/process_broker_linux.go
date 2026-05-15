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
	"context"

	"pipit.sh/pipit/internal/sandboxbroker"
)

// filesystemBrokerAdmission bounds concurrent filesystem broker ownership per host
// process.
var filesystemBrokerAdmission admissionTable

// LaunchFilesystemBroker supervises an independently approved filesystem broker. A
// separate gate bounds one active and one queued broker per host process.
//
// Takes config (WorkerConfig) with the broker executable's approved digest.
// Takes bootstrap (*sandboxbroker.LinuxBootstrap) which is owned by the caller.
//
// Returns *WorkerProcess which owns resources, including on partial failure.
// Returns error when any prerequisite or launch step fails.
func LaunchFilesystemBroker(ctx context.Context, config WorkerConfig, bootstrap *sandboxbroker.LinuxBootstrap) (*WorkerProcess, error) {
	return launchFilesystemBroker(ctx, config, bootstrap, nil)
}

// launchFilesystemBroker preserves descriptor handoff under an optional aggregate group.
//
// Takes config (WorkerConfig) which is the approved policy.
// Takes bootstrap (*sandboxbroker.LinuxBootstrap) which holds the borrowed bootstrap
// handles.
// Takes parent (*Group) which is the pinned parent when supplied.
//
// Returns *WorkerProcess which must be closed before its aggregate parent.
// Returns error when the launch or bootstrap validation fails.
func launchFilesystemBroker(ctx context.Context, config WorkerConfig, bootstrap *sandboxbroker.LinuxBootstrap, parent *Group) (*WorkerProcess, error) {
	if bootstrap.RecoveryOnly() {
		return nil, ErrInvalidLimits
	}
	files, err := bootstrap.Files()
	if err != nil {
		return nil, err
	}
	process, err := launchConfinedProcess(ctx, config, &filesystemBrokerAdmission, files, parent)
	if process != nil {
		process.brokerNamespace = bootstrap.StagingNamespace()
		process.brokerParent = parent
	}
	return process, err
}
