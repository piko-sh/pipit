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

// Package module is the host-facing contract for loading Go-as-bytecode modules into a
// pipit interpreter at runtime.
//
// It owns the types that cross the boundary between the interpreter and its hosts:
//
//   - [Ref]: how compiled code references a module dependency by path, version and
//     integrity pin.
//   - [Descriptor]: what a module is - declared capabilities, pinned stdlib version and
//     exported symbol allowlist.
//   - [Bundle]: the on-disk artefact pairing a descriptor with its compiled bytecode.
//   - [Capability]: a single capability claim the host's capability hook interprets.
//   - [Provider]: the port a host implements to resolve a [Ref] into a [Bundle].
//
// Every declaration here aliases pipit.sh/pipit/internal/module, which owns the
// implementation. The alias keeps the contract at a public import path while the shared
// code stays where internal packages may use it.
package module

import "pipit.sh/pipit/internal/module"

// DescriptorVersion is the current Descriptor schema version.
const DescriptorVersion = module.DescriptorVersion

var (
	// ErrNotFound reports a module reference no provider could resolve.
	ErrNotFound = module.ErrNotFound

	// ErrIntegrityMismatch reports a bundle whose digest does not match its ref's pin.
	ErrIntegrityMismatch = module.ErrIntegrityMismatch

	// ErrCapabilityDenied reports a capability the host's hook refused.
	ErrCapabilityDenied = module.ErrCapabilityDenied

	// ErrCapabilityExceedsPolicy reports claims wider than the host's grants.
	ErrCapabilityExceedsPolicy = module.ErrCapabilityExceedsPolicy

	// ErrStdlibIncompatible reports a module pinned to an incompatible stdlib version.
	ErrStdlibIncompatible = module.ErrStdlibIncompatible

	// ErrFrozen reports a frozen provider asked to resolve an unpinned reference.
	ErrFrozen = module.ErrFrozen

	// ErrUnpinnedRef reports a module reference that carries no integrity pin.
	ErrUnpinnedRef = module.ErrUnpinnedRef
)

// Ref identifies a module dependency by path, version and integrity pin.
type Ref = module.Ref

// Descriptor declares what a module is: its capabilities, pinned stdlib version and
// exported symbol allowlist.
type Descriptor = module.Descriptor

// Bundle pairs a descriptor with the compiled bytecode it describes.
type Bundle = module.Bundle

// Capability is one capability claim, an axis paired with a scope.
type Capability = module.Capability

// CapabilitySet is an ordered set of capability claims.
type CapabilitySet = module.CapabilitySet

// Provider is the port a host implements to resolve a reference into a bundle.
type Provider = module.Provider

// ProviderFunc adapts a plain function to the Provider interface.
type ProviderFunc = module.ProviderFunc

// Loaded is the handle returned once a bundle has been installed.
type Loaded = module.Loaded

// ParseRef parses the path@version#pin spelling of a module reference.
//
// Takes text (string) which is the reference to parse.
//
// Returns Ref which is the parsed reference.
// Returns error when the text is not a valid reference.
func ParseRef(text string) (Ref, error) { return module.ParseRef(text) }

// ParseCapability parses the axis(scope) spelling of a capability claim.
//
// Takes text (string) which is the claim to parse.
//
// Returns Capability which is the parsed claim, zero when the text is not valid.
func ParseCapability(text string) Capability { return module.ParseCapability(text) }

// UnmarshalDescriptor decodes a descriptor and validates it.
//
// Takes data ([]byte) which is the encoded descriptor.
//
// Returns *Descriptor which is the decoded, validated value.
// Returns error when decoding or validation fails.
func UnmarshalDescriptor(data []byte) (*Descriptor, error) {
	return module.UnmarshalDescriptor(data)
}
