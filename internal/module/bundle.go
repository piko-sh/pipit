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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// Bundle is the self-contained artefact pairing a Descriptor with the pipit bytecode that
// implements it.
type Bundle struct {
	// Descriptor declares what the module is. Always non-nil for a well-formed bundle.
	Descriptor *Descriptor

	// Bytecode is the schema-versioned pipit bytecode payload. The bytes are exactly what
	// pipit.PackCompiledFileSetToBytes returned at packaging time and are passed unchanged
	// to the loader.
	Bytecode []byte

	// TypesExport carries the bundle's go/types.Package metadata in gcexportdata format, one
	// entry per exported sub-package of the module. May be empty for older bundles.
	TypesExport []byte
}

// Validate runs cheap consistency checks: descriptor non-nil, bytecode non-empty,
// descriptor itself schema-valid. Does NOT verify integrity hashes against the bundle's
// Ref.Pin - that's VerifyAgainstRef's job.
//
// Returns error when the bundle is structurally invalid, or nil when it is well-formed.
func (b *Bundle) Validate() error {
	if b == nil {
		return errors.New("module: nil bundle")
	}
	if b.Descriptor == nil {
		return errors.New("module: bundle missing descriptor")
	}
	if err := b.Descriptor.Validate(); err != nil {
		return err
	}
	if len(b.Bytecode) == 0 {
		return errors.New("module: bundle has empty bytecode")
	}
	return nil
}

// Fingerprint returns a stable identifier for the bundle as a whole, computed by hashing
// the canonical descriptor JSON followed by the bytecode payload. Two bundles that are
// structurally equivalent share a Fingerprint; tampering with either component changes
// it.
//
// Returns the fingerprint string ("sha256:<64-hex>") or a marshalling error from the
// descriptor.
func (b *Bundle) Fingerprint() (string, error) {
	descriptorBytes, err := b.Descriptor.MarshalCanonicalJSON()
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	_, _ = hasher.Write(descriptorBytes)
	_, _ = hasher.Write(b.Bytecode)
	_, _ = hasher.Write(b.TypesExport)
	sum := hasher.Sum(nil)
	return "sha256:" + hex.EncodeToString(sum), nil
}

// VerifyAgainstRef checks that the bundle's Fingerprint matches the Pin in the supplied
// reference. An empty reference.Pin disables verification and returns nil.
//
// Takes reference (Ref) which carries the expected pin.
//
// Returns nil on match, ErrIntegrityMismatch on mismatch, or a fingerprint computation
// error.
func (b *Bundle) VerifyAgainstRef(reference Ref) error {
	if reference.Pin == "" {
		return nil
	}
	fingerprint, err := b.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != reference.Pin {
		return fmt.Errorf("%w: expected %s, got %s", ErrIntegrityMismatch, reference.Pin, fingerprint)
	}
	return nil
}

// BytecodeDigest returns the SHA-256 hash of the bytecode payload in "sha256:<64-hex>"
// form. Used by lockfiles and verification flows that need to detect changes in the
// compiled output independent of descriptor metadata.
//
// Returns the digest string.
func (b *Bundle) BytecodeDigest() string {
	sum := sha256.Sum256(b.Bytecode)
	return "sha256:" + hex.EncodeToString(sum[:])
}
