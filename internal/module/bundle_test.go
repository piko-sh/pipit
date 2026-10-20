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

package module

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureBundle() *Bundle {
	return &Bundle{
		Descriptor: &Descriptor{
			SchemaVersion: DescriptorVersion,
			Ref:           Ref{Path: "example.com/mod", Version: "v1.2.3"},
			Capabilities:  CapabilitySet{{Axis: "network"}},
		},
		Bytecode: []byte("PIPIT\x06\x02ignore"),
	}
}

func TestBundleValidate(t *testing.T) {
	t.Parallel()
	good := fixtureBundle()
	require.NoError(t, good.Validate(), "a well-formed bundle validates")

	var nilBundle *Bundle
	require.Error(t, nilBundle.Validate(), "a nil bundle is invalid")

	noDescriptor := *good
	noDescriptor.Descriptor = nil
	require.Error(t, noDescriptor.Validate(), "a bundle without a descriptor is invalid")

	noBytecode := *good
	noBytecode.Bytecode = nil
	require.Error(t, noBytecode.Validate(), "a bundle without bytecode is invalid")
}

func TestBundleFingerprintStability(t *testing.T) {
	t.Parallel()
	a := fixtureBundle()
	b := fixtureBundle()

	fpA, err := a.Fingerprint()
	require.NoError(t, err)
	fpB, err := b.Fingerprint()
	require.NoError(t, err)
	require.Equal(t, fpB, fpA, "equivalent bundles share a fingerprint")

	c := fixtureBundle()
	c.Bytecode = append([]byte(nil), c.Bytecode...)
	c.Bytecode[0] ^= 0xff
	fpC, err := c.Fingerprint()
	require.NoError(t, err)
	require.NotEqual(t, fpA, fpC, "changed bytecode changes the fingerprint")

	d := fixtureBundle()
	d.Descriptor = new(*d.Descriptor)
	d.Descriptor.Capabilities = CapabilitySet{{Axis: "exec"}}
	fpD, err := d.Fingerprint()
	require.NoError(t, err)
	require.NotEqual(t, fpA, fpD, "a changed descriptor changes the fingerprint")
}

func TestBundleVerifyAgainstRef(t *testing.T) {
	t.Parallel()
	bundle := fixtureBundle()
	fingerprint, err := bundle.Fingerprint()
	require.NoError(t, err)

	require.NoError(t, bundle.VerifyAgainstRef(Ref{Path: bundle.Descriptor.Ref.Path}), "a reference without a pin verifies")

	matchingRef := Ref{Path: bundle.Descriptor.Ref.Path, Pin: fingerprint}
	require.NoError(t, bundle.VerifyAgainstRef(matchingRef), "a matching pin verifies")

	mismatchRef := Ref{Path: bundle.Descriptor.Ref.Path, Pin: "sha256:0000000000000000000000000000000000000000000000000000000000000000"}
	err = bundle.VerifyAgainstRef(mismatchRef)
	require.ErrorIs(t, err, ErrIntegrityMismatch, "a mismatched pin fails verification")
}

func TestBundleBytecodeDigestRoundTrip(t *testing.T) {
	t.Parallel()
	a := fixtureBundle()
	b := fixtureBundle()
	require.Equal(t, a.BytecodeDigest(), b.BytecodeDigest(), "equal bytecode gives equal digests")
	c := fixtureBundle()
	c.Bytecode = append(c.Bytecode, 0x01)
	require.NotEqual(t, a.BytecodeDigest(), c.BytecodeDigest(), "changed bytecode changes the digest")
}
