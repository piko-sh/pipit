//go:build !linux || (!amd64 && !arm64)

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

package sandboxhost

import "context"

// newIsolatedFilesystemProcess rejects platforms without the required native boundary.
//
// Takes the host context and configuration without performing filesystem I/O.
//
// Returns no owner and ErrIsolatedUnavailable, never a weaker fallback.
func newIsolatedFilesystemProcess(_ context.Context, _ *IsolatedFilesystemConfig) (isolatedFilesystemProcess, error) {
	return nil, ErrIsolatedUnavailable
}

// newIsolatedFilesystemRecovery refuses recovery without the required native boundary.
//
// Returns no owner or filesystem access on unsupported platforms.
func newIsolatedFilesystemRecovery(_ context.Context, _ *IsolatedFilesystemConfig) (isolatedFilesystemRecovery, error) {
	return nil, ErrIsolatedUnavailable
}
