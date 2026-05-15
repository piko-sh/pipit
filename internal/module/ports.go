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
	"context"
	"errors"
)

var (
	// ErrNotFound indicates the provider could not resolve the requested reference.
	ErrNotFound = errors.New("module: module not found")

	// ErrIntegrityMismatch indicates the resolved bundle's content hash does not match the
	// expected pin.
	ErrIntegrityMismatch = errors.New("module: integrity mismatch")

	// ErrCapabilityDenied indicates the host's CapabilityHook refused to load the module.
	// Distinct from ErrIntegrityMismatch so audit logs can route policy denials separately
	// from tampering events.
	ErrCapabilityDenied = errors.New("module: capability denied")

	// ErrCapabilityExceedsPolicy indicates the module's declared capability set is not a
	// subset of the host policy's grants: the validator-time equivalent of CapabilityDenied.
	// Surfaced at LoadModule before the bundle is registered.
	ErrCapabilityExceedsPolicy = errors.New("module: module capabilities exceed policy grants")

	// ErrStdlibIncompatible indicates the descriptor's pinned stdlib version is not
	// satisfied by the running pipit.
	ErrStdlibIncompatible = errors.New("module: stdlib version incompatible")

	// ErrFrozen indicates a provider operating in frozen mode (no fetch, lockfile-only) was
	// asked to resolve a reference absent from its lockfile.
	ErrFrozen = errors.New("module: frozen provider rejects unpinned reference")

	// ErrUnpinnedRef indicates LoadModule was given a reference without a pin while the
	// service requires one. Distinct from ErrIntegrityMismatch so hosts can tell "nothing to
	// verify against" from "verified and wrong"; interactive hosts opt in to unpinned loads
	// with WithAllowUnpinnedModules.
	ErrUnpinnedRef = errors.New("module: module reference carries no pin")
)

// Provider is the port a host implements to translate a Ref into a loadable Bundle.
// Implementations must be safe for concurrent use.
type Provider interface {
	// Resolve fetches the bundle for reference via the provider's backend.
	//
	// Takes reference (Ref) which identifies the requested module.
	//
	// Returns *Bundle which is the resolved artefact.
	// Returns error when lookup, integrity, or policy checks fail.
	Resolve(ctx context.Context, reference Ref) (*Bundle, error)
}

// ProviderFunc adapts an ordinary function to the Provider interface. Useful for tests
// and for ad-hoc providers that don't need any state.
type ProviderFunc func(ctx context.Context, reference Ref) (*Bundle, error)

// Resolve forwards to the wrapped function.
//
// Takes reference (Ref) which identifies the requested module.
//
// Returns *Bundle which is the resolved artefact.
// Returns error when the wrapped function reports a failure.
func (f ProviderFunc) Resolve(ctx context.Context, reference Ref) (*Bundle, error) {
	return f(ctx, reference)
}

// Loaded is the host's handle to a successfully-loaded module, returned after the bundle
// has been verified and its symbols registered.
type Loaded struct {
	// Descriptor is the canonical descriptor used at load time. Held by reference; callers
	// must not mutate.
	Descriptor *Descriptor

	// Fingerprint is the bundle fingerprint captured at load time, stable across invocations
	// and used for cache lookups and cross-machine deduplication.
	Fingerprint string
}
