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

package policy

import (
	"reflect"
	"sync"
)

// nativeSymbolIdentities records the identity under which each native function was
// registered, keyed by the function's code pointer.
//
// The typed capability gates key on this identity rather than on the name the Go runtime
// gives the code, so a function registered through a wrapper, a closure or an overlay is
// still gated as the package symbol a script called. The table holds one entry per
// distinct code pointer, so its size is bounded by the host binary's code rather than by
// how often packages are registered.
var nativeSymbolIdentities = nativeSymbolIdentityTable{
	identities: make(map[uintptr]string),
	mu:         sync.RWMutex{},
}

// nativeSymbolIdentityTable maps a native function's code pointer to the dotted identity
// it was registered under.
type nativeSymbolIdentityTable struct {
	// identities maps a code pointer to its "package/path.Symbol" identity, or to the empty
	// string when one code pointer was registered under two different identities.
	identities map[uintptr]string

	// mu guards identities.
	mu sync.RWMutex
}

// RecordNativeSymbols notes the identity of every function a package registers.
//
// The symbol registry calls it whenever a package or an overlay is registered. A code
// pointer registered under two different identities, as happens when one closure factory
// or reflect.MakeFunc serves several symbols, becomes ambiguous and resolves to nothing,
// so the gate falls back to the runtime name rather than guessing.
//
// Takes packagePath (string) which is the import path the symbols are registered under.
// Takes symbols (map[string]reflect.Value) which maps symbol names to their values. Only
// non-nil functions are recorded.
//
// Safe for concurrent use. The identity registry mutex guards updates.
func RecordNativeSymbols(packagePath string, symbols map[string]reflect.Value) {
	nativeSymbolIdentities.mu.Lock()
	defer nativeSymbolIdentities.mu.Unlock()
	for name, value := range symbols {
		if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
			continue
		}
		identity := packagePath + "." + name
		pointer := value.Pointer()
		existing, seen := nativeSymbolIdentities.identities[pointer]
		switch {
		case !seen:
			nativeSymbolIdentities.identities[pointer] = identity
		case existing != identity:
			nativeSymbolIdentities.identities[pointer] = ""
		default:
		}
	}
}

// NativeSymbolIdentity returns the identity a native function was registered under.
//
// Takes function (reflect.Value) which is the function about to be called.
//
// Returns string which is the "package/path.Symbol" identity.
// Returns bool which is false when the function was never recorded, or when its code
// pointer was recorded under more than one identity.
//
// Safe for concurrent use. The identity registry mutex guards lookups.
func NativeSymbolIdentity(function reflect.Value) (string, bool) {
	if !function.IsValid() || function.Kind() != reflect.Func || function.IsNil() {
		return "", false
	}
	nativeSymbolIdentities.mu.RLock()
	identity, ok := nativeSymbolIdentities.identities[function.Pointer()]
	nativeSymbolIdentities.mu.RUnlock()
	if !ok || identity == "" {
		return "", false
	}
	return identity, true
}
