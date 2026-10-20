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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/module"
)

func unpinnedTestBundle() *module.Bundle {
	return &module.Bundle{
		Descriptor: &module.Descriptor{
			SchemaVersion: module.DescriptorVersion,
			Ref:           module.Ref{Path: "example.com/foo", Version: "v1.0.0"},
		},
		Bytecode: []byte("bytecode"),
	}
}

func TestEnsureBundlePinFillsEmptyPin(t *testing.T) {
	t.Parallel()
	want, err := unpinnedTestBundle().Fingerprint()
	require.NoError(t, err)

	bundle := unpinnedTestBundle()
	require.NoError(t, EnsureBundlePin(bundle))
	require.Equal(t, want, bundle.Descriptor.Ref.Pin)
}

func TestEnsureBundlePinKeepsExistingPin(t *testing.T) {
	t.Parallel()
	bundle := unpinnedTestBundle()
	bundle.Descriptor.Ref.Pin = "sha256:already-pinned"

	require.NoError(t, EnsureBundlePin(bundle))
	require.Equal(t, "sha256:already-pinned", bundle.Descriptor.Ref.Pin)
}

func TestEnsureBundlePinRejectsMissingDescriptor(t *testing.T) {
	t.Parallel()
	for name, bundle := range map[string]*module.Bundle{
		"nil bundle":     nil,
		"nil descriptor": {Bytecode: []byte("bytecode")},
	} {
		err := EnsureBundlePin(bundle)
		require.ErrorIs(t, err, errBundleWithoutDescriptor, "%s", name)
	}
}
