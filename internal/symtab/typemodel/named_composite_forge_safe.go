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

//go:build safe || (js && wasm)

package typemodel

import (
	"fmt"
	"reflect"
)

const (
	// namedCompositeForging is false in builds without the descriptor forging code, where a
	// named composite type shares its underlying type's runtime type.
	namedCompositeForging = false
)

// forgeLayoutError reports that this build cannot forge type descriptors.
//
// Returns error which wraps ErrNamedCompositeUnsupported.
func forgeLayoutError() error {
	return fmt.Errorf("%w: this build has no descriptor forging", ErrNamedCompositeUnsupported)
}

// forgeNamedType refuses to forge in a build without the forging code. The registry never
// calls it there.
//
// Takes underlying (reflect.Type) which is the type that would be named.
// Takes qualifiedName (string) which is the name it would get.
// Takes pkgPath (string) which is the package path it would get.
//
// Returns reflect.Type which is always nil.
// Returns error which wraps ErrNamedCompositeUnsupported.
func forgeNamedType(underlying reflect.Type, qualifiedName, pkgPath string) (reflect.Type, error) {
	return nil, fmt.Errorf("%w: cannot forge %s (%s in %s)", forgeLayoutError(), qualifiedName, underlying, pkgPath)
}

// patchCycleMember refuses to patch in a build without the forging code. The registry
// never calls it there.
//
// Takes named (reflect.Type) which would be the forged member.
// Takes underlying (reflect.Type) which would be its underlying type.
//
// Returns error which wraps ErrNamedCompositeUnsupported.
func patchCycleMember(named, underlying reflect.Type) error {
	return fmt.Errorf("%w: cannot patch %s from %s", forgeLayoutError(), named, underlying)
}
