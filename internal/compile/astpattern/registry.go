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

package astpattern

import (
	"sync"

	"pipit.sh/pipit/internal/compile/patterns"
)

var (
	// defaultRegistry holds the process-wide registry DefaultRegistry() builds on first use.
	defaultRegistry *patterns.Registry

	// defaultRegistryOnce guards the one-time initialisation of defaultRegistry.
	defaultRegistryOnce sync.Once
)

// register adds every recogniser to registry, including the SIMD kernels and the loop
// unroller. Each recogniser reads its own switch from RecogniseContext.Options, so
// registering both is always safe.
//
// Takes registry (*patterns.Registry) which receives the recognisers.
func register(registry *patterns.Registry) {
	registry.Register(&simdKernelRecogniser{})
	registry.Register(&loopUnrollRecogniser{})
}

// DefaultRegistry returns the shared registry holding every recogniser the package
// provides.
//
// Built once and never mutated afterwards, so it is safe to share between concurrent
// compilations.
//
// Returns *patterns.Registry which the compiler uses when no registry is configured.
func DefaultRegistry() *patterns.Registry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = patterns.NewRegistry()
		register(defaultRegistry)
	})
	return defaultRegistry
}
