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

package sandboxhost

import (
	"context"
	"path/filepath"

	"pipit.sh/pipit/internal/sandboxlinux"
)

// prepareIsolatedImages stages the config's approved worker and watchdog under its state
// directory's image cache.
//
// Takes config (IsolatedConfig) naming the executables and digests.
//
// Returns *IsolatedImages backed by pinned, verified images, or an error.
func prepareIsolatedImages(ctx context.Context, config IsolatedConfig) (*IsolatedImages, error) {
	if err := validateIsolatedConfig(ctx, &config); err != nil {
		return nil, err
	}
	cache, err := sandboxlinux.StageWorkerImageCache(ctx,
		filepath.Join(config.StateDirectory, "images", "cache"),
		[]sandboxlinux.WorkerImageRequest{
			{Executable: config.WorkerPath, Digest: config.WorkerSHA256},
			{Executable: config.WatchdogPath, Digest: config.WatchdogSHA256},
		})
	if err != nil {
		return nil, isolatedLaunchError(err)
	}
	return &IsolatedImages{cache: cache}, nil
}

// workerImageCache recovers the native cache a launch threads into its WorkerConfig.
//
// Takes images (*IsolatedImages) which may be nil.
//
// Returns *sandboxlinux.WorkerImageCache or nil when no images were prepared.
func workerImageCache(images *IsolatedImages) *sandboxlinux.WorkerImageCache {
	if images == nil {
		return nil
	}
	if cache, ok := images.cache.(*sandboxlinux.WorkerImageCache); ok {
		return cache
	}
	return nil
}
