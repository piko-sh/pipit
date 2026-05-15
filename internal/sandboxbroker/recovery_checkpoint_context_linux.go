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
	"crypto/sha256"
	"encoding/json"
	"slices"
)

// RecoveryCheckpointContext returns host metadata only after independent approval.
//
// Takes encoded ([]byte) which contains stored checkpoint bytes.
// Takes approved ([sha256.Size]byte) which is the trusted original approval digest.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
//
// Returns owned metadata bytes, never authority to terminate or adopt a process.
func RecoveryCheckpointContext(encoded []byte, approved, binding [sha256.Size]byte) ([]byte, error) {
	if err := ValidateRecoveryCheckpoint(encoded, approved, binding); err != nil {
		return nil, err
	}
	var policy recoveryCheckpoint
	if err := json.Unmarshal(encoded, &policy); err != nil {
		return nil, err
	}
	return policy.Context, nil
}

// attachRecoveryContext binds original native-owner metadata into a fresh checkpoint.
//
// Takes encoded ([]byte) which contains internally captured checkpoint bytes.
// Takes metadata ([]byte) which contains bounded independently captured host context.
// Takes binding ([sha256.Size]byte) which is the current host security binding.
//
// Returns canonical bytes covering both authorities with the same total size limit.
func attachRecoveryContext(encoded, metadata []byte, binding [sha256.Size]byte) ([]byte, error) {
	if len(metadata) > maximumRecoveryContextBytes {
		return nil, errLimit
	}
	var policy recoveryCheckpoint
	if err := json.Unmarshal(encoded, &policy); err != nil {
		return nil, err
	}
	policy.Context = slices.Clone(metadata)
	captured, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	if err := ValidateRecoveryCheckpoint(captured, sha256.Sum256(captured), binding); err != nil {
		return nil, err
	}
	return captured, nil
}
