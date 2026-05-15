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
	"testing"

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
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	bundle := unpinnedTestBundle()
	if err := EnsureBundlePin(bundle); err != nil {
		t.Fatalf("EnsureBundlePin: %v", err)
	}
	if bundle.Descriptor.Ref.Pin != want {
		t.Fatalf("pin = %q, want %q", bundle.Descriptor.Ref.Pin, want)
	}
}

func TestEnsureBundlePinKeepsExistingPin(t *testing.T) {
	t.Parallel()
	bundle := unpinnedTestBundle()
	bundle.Descriptor.Ref.Pin = "sha256:already-pinned"

	if err := EnsureBundlePin(bundle); err != nil {
		t.Fatalf("EnsureBundlePin: %v", err)
	}
	if bundle.Descriptor.Ref.Pin != "sha256:already-pinned" {
		t.Fatalf("pin = %q, want the existing pin kept", bundle.Descriptor.Ref.Pin)
	}
}

func TestEnsureBundlePinRejectsMissingDescriptor(t *testing.T) {
	t.Parallel()
	for name, bundle := range map[string]*module.Bundle{
		"nil bundle":     nil,
		"nil descriptor": {Bytecode: []byte("bytecode")},
	} {
		err := EnsureBundlePin(bundle)
		if !errors.Is(err, errBundleWithoutDescriptor) {
			t.Fatalf("%s: err = %v, want errBundleWithoutDescriptor", name, err)
		}
	}
}
