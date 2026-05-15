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

package modloader

import (
	"errors"
	"fmt"

	"pipit.sh/pipit/sdk/module"
)

// errBundleWithoutDescriptor is returned by EnsureBundlePin for a bundle that carries no
// descriptor and therefore nothing to fingerprint.
var errBundleWithoutDescriptor = errors.New("modloader: bundle carries no descriptor")

// EnsureBundlePin fills bundle.Descriptor.Ref.Pin with the bundle fingerprint when the
// producer left it empty, so callers can hand the bundle's own reference to LoadModule(),
// which refuses unpinned refs unless the host opted in with WithAllowUnpinnedModules(). A
// pin that is already present is kept as is.
//
// Takes bundle (*module.Bundle) which is the compiled or cached bundle to pin.
//
// Returns error when the bundle has no descriptor or the fingerprint cannot be computed.
func EnsureBundlePin(bundle *module.Bundle) error {
	if bundle == nil || bundle.Descriptor == nil {
		return errBundleWithoutDescriptor
	}
	if bundle.Descriptor.Ref.Pin != "" {
		return nil
	}
	fingerprint, err := bundle.Fingerprint()
	if err != nil {
		return fmt.Errorf("modloader: fingerprint bundle %s: %w", bundle.Descriptor.Ref, err)
	}
	bundle.Descriptor.Ref.Pin = fingerprint
	return nil
}
