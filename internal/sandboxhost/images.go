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

// IsolatedImages holds worker and watchdog executables staged and hash-verified once by
// PrepareIsolatedImages.
type IsolatedImages struct {
	// cache is the platform-specific handle to pinned images.
	cache isolatedImageCache
}

// Close releases every pinned image descriptor and cache directory handle.
//
// Returns the joined error of every failed release, or nil for a nil or empty handle.
func (images *IsolatedImages) Close() error {
	if images == nil || images.cache == nil {
		return nil
	}
	return images.cache.Close()
}

// isolatedImageCache is the platform-owned handle to pre-staged launch images.
type isolatedImageCache interface {
	// Close releases pinned image descriptors and cache directories.
	//
	// Returns error when release fails.
	Close() error
}

// PrepareIsolatedImages stages and hash-verifies worker and watchdog executables once so
// later launches skip re-hashing.
//
// Takes config (IsolatedConfig) naming the executables and state directory.
//
// Returns *IsolatedImages to assign to IsolatedConfig.Images, or an error.
func PrepareIsolatedImages(ctx context.Context, config IsolatedConfig) (*IsolatedImages, error) {
	return prepareIsolatedImages(isolatedLoggerContext(ctx, config.Logger), config)
}
