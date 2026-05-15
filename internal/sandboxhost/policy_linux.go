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

package sandboxhost

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"runtime"

	"pipit.sh/pipit/internal/sandboxworker"
)

// isolatedLaunchPolicy digests the whole worker configuration with the platform and the
// worker protocol profile, so a recovery of the same configuration derives the same host
// binding as the launch and a different one rejects the record.
//
// Takes config (*IsolatedConfig) which must validate.
//
// Returns the policy digest, or an error when the configuration is invalid.
func isolatedLaunchPolicy(ctx context.Context, config *IsolatedConfig) ([sha256.Size]byte, error) {
	if config == nil {
		return [sha256.Size]byte{}, ErrInvalidIsolatedConfig
	}
	copied := *config
	if err := validateIsolatedConfig(ctx, &copied); err != nil {
		return [sha256.Size]byte{}, err
	}

	copied.Lifetime = 0
	copied.OutputBytes = 0
	encoded, err := json.Marshal(struct {
		Profile      string
		Platform     string
		Architecture string
		Source       string
		Config       IsolatedConfig
	}{
		Profile: "isolated-worker-approval-v1", Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		Source: sandboxworker.Profile, Config: copied,
	})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
