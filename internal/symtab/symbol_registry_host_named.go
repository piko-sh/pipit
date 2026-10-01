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

package symtab

import "reflect"

// hostNamedTypeKey identifies a host type by its own reflect identity.
type hostNamedTypeKey struct {
	// pkgPath is the reflect.Type.PkgPath of the type's declaring package.
	pkgPath string

	// name is the reflect.Type.Name of the type.
	name string
}

// HostNamedType returns the registered host type whose own reflect identity is
// pkgPath.name.
//
// Takes pkgPath (string) which is the declaring package path, as reflect reports it.
// Takes name (string) which is the type name, as reflect reports it.
//
// Returns reflect.Type which is the registered type on a hit.
// Returns bool which is true when a type with exactly that identity is registered.
func (r *SymbolRegistry) HostNamedType(pkgPath, name string) (reflect.Type, bool) {
	if reflectType, ok := r.ReflectTypeForNamed(pkgPath, name); ok &&
		reflectType.PkgPath() == pkgPath && reflectType.Name() == name {
		return reflectType, true
	}
	reflectType, ok := r.hostNamedTypeIndex()[hostNamedTypeKey{pkgPath: pkgPath, name: name}]
	return reflectType, ok
}

// hostNamedTypeIndex returns the identity index of registered type carriers, building it
// on first use after a change to the registered symbols.
//
// Returns map[hostNamedTypeKey]reflect.Type which must not be mutated by callers.
//
// Concurrency: acquires the registry read lock, then the write lock when the index has to
// be built.
func (r *SymbolRegistry) hostNamedTypeIndex() map[hostNamedTypeKey]reflect.Type {
	r.mu.RLock()
	index := r.hostNamedTypes
	r.mu.RUnlock()
	if index != nil {
		return index
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hostNamedTypes == nil {
		r.hostNamedTypes = buildHostNamedTypeIndex(r.symbols)
	}
	return r.hostNamedTypes
}

// buildHostNamedTypeIndex indexes every typed-nil type carrier in symbols by the reflect
// identity of the type it carries.
//
// Takes symbols (map[string]map[string]reflect.Value) which is the registered symbol
// table.
//
// Returns map[hostNamedTypeKey]reflect.Type which is non-nil even when empty, so an empty
// registry is not rebuilt on every lookup.
func buildHostNamedTypeIndex(symbols map[string]map[string]reflect.Value) map[hostNamedTypeKey]reflect.Type {
	index := make(map[hostNamedTypeKey]reflect.Type)
	for _, packageSymbols := range symbols {
		for _, value := range packageSymbols {
			if !value.IsValid() || value.Kind() != reflect.Pointer || !value.IsNil() {
				continue
			}
			carried := value.Type().Elem()
			if carried.Name() == "" || carried.PkgPath() == "" {
				continue
			}
			index[hostNamedTypeKey{pkgPath: carried.PkgPath(), name: carried.Name()}] = carried
		}
	}
	return index
}
