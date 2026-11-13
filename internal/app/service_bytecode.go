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

package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/verify"
)

// errFmtLoadingBytecode wraps an error from LoadCompiled with the key being loaded.
const errFmtLoadingBytecode = "loading compiled bytecode %q: %w"

// SaveCompiled persists a compiled file set under the given key using the configured
// BytecodeStorePort. The file set is only read.
//
// Takes key (string) which identifies the compiled file set.
// Takes cfs (*CompiledFileSet) which is the compiled file set to persist.
//
// Returns error when no store is configured, or serialisation fails.
func (s *Service) SaveCompiled(ctx context.Context, key string, cfs *program.CompiledFileSet) error {
	if s.config.bytecodeStore == nil {
		return fault.ErrNoBytecodeStore
	}
	var bindErr error
	if ctx, bindErr = s.bindLogger(ctx); bindErr != nil {
		return bindErr
	}
	return s.config.bytecodeStore.SaveCompiledFileSet(ctx, key, cfs)
}

// LoadCompiled loads a previously saved compiled file set by key, reconstructing runtime
// types and values via the service's SymbolRegistry.
//
// The program's package variables get slots of their own in the service's store. Loading
// the same bytecode again, from a store that reports a digest of what it read, shares the
// slots of the earlier load rather than reserving more. UnloadCompiled returns them.
// Slots of a file set that is dropped without UnloadCompiled stay reserved for the life
// of the service.
//
// Takes key (string) which identifies the compiled file set to load.
//
// Returns *CompiledFileSet which is the reconstructed compiled file set.
// Returns error when no store is configured, the key is not found, the schema version has
// changed, reconstruction or verification fails, the file's slot claim does not cover its
// code, one of its interfaces conflicts with one already registered, or the service's
// slots (fault.ErrGlobalSlotsExhausted) or interface names run out.
func (s *Service) LoadCompiled(ctx context.Context, key string) (*program.CompiledFileSet, error) {
	if s.config.bytecodeStore == nil {
		return nil, fault.ErrNoBytecodeStore
	}
	var bindErr error
	if ctx, bindErr = s.bindLogger(ctx); bindErr != nil {
		return nil, bindErr
	}
	cfs, digest, err := s.loadFromStore(ctx, key)
	if err != nil {
		return nil, fmt.Errorf(errFmtLoadingBytecode, key, err)
	}
	if verifyErr := verify.VerifyLoadedFileSet(ctx, cfs); verifyErr != nil {
		return nil, fmt.Errorf(errFmtLoadingBytecode, key, verifyErr)
	}
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf(errFmtLoadingBytecode, key, err)
	}
	pretouchCompileTimeDerivedState(cfs.Root())
	if err := s.placeLoadedProgram(ctx, cfs, digest); err != nil {
		return nil, fmt.Errorf(errFmtLoadingBytecode, key, err)
	}
	return cfs, nil
}

// loadFromStore reads a file set from the configured store, with the digest of the bytes
// it came from when the store reports one.
//
// Takes key (string) which identifies the file set.
//
// Returns *CompiledFileSet which is the decoded file set.
// Returns *[sha256.Size]byte which is the digest, or nil when the store gives none.
// Returns error when the store fails.
func (s *Service) loadFromStore(ctx context.Context, key string) (*program.CompiledFileSet, *[sha256.Size]byte, error) {
	if store, ok := s.config.bytecodeStore.(digestingBytecodeStore); ok {
		cfs, digest, err := store.LoadCompiledFileSetWithDigest(ctx, key, s.symbols)
		if err != nil {
			return nil, nil, err
		}
		return cfs, &digest, nil
	}
	cfs, err := s.config.bytecodeStore.LoadCompiledFileSet(ctx, key, s.symbols)
	return cfs, nil, err
}

// recordNamedInterfaces records the interfaces a freshly compiled file set declares and
// the pointer-named interfaces its call sites use.
//
// Takes cfs (*CompiledFileSet) which is the file set just compiled.
// Takes packages ([]*types.Package) which are the program's type-checked packages.
func (s *Service) recordNamedInterfaces(cfs *program.CompiledFileSet, packages []*types.Package) {
	if cfs == nil || len(cfs.NamedInterfaces()) > 0 {
		return
	}
	recorded := make(map[string]program.NamedInterface)
	for _, typesPackage := range packages {
		collectDeclaredInterfaces(typesPackage, recorded)
	}
	names := make(map[string]struct{})
	visited := make(map[*program.CompiledFunction]struct{})
	collectPointerStaticTypeNames(cfs.Root(), names, visited)
	collectPointerStaticTypeNames(cfs.VariableInitFunction(), names, visited)
	for name := range names {
		if _, known := recorded[name]; known {
			continue
		}
		identity, ok := s.globals.UserNamedInterface(name)
		if !ok {
			continue
		}
		if packagePath, interfaceName, methods, ok := identity.NamedInterfaceParts(); ok {
			recorded[name] = program.NamedInterface{PackagePath: packagePath, Name: interfaceName, Methods: methods}
		}
	}
	interfaces := make([]program.NamedInterface, 0, len(recorded))
	for _, name := range slices.Sorted(maps.Keys(recorded)) {
		interfaces = append(interfaces, recorded[name])
	}
	if len(interfaces) > 0 {
		cfs.SetNamedInterfaces(interfaces)
	}
}

// collectDeclaredInterfaces adds to recorded every non-generic interface type a package
// declares at package level, keyed by its qualified name, with its sorted method names
// (embedded interfaces' methods included), as the compiler registers them.
//
// Takes typesPackage (*types.Package) which is the type-checked package. It may be nil.
// Takes recorded (map[string]program.NamedInterface) which receives the interfaces.
func collectDeclaredInterfaces(typesPackage *types.Package, recorded map[string]program.NamedInterface) {
	if typesPackage == nil {
		return
	}
	scope := typesPackage.Scope()
	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || typeName.IsAlias() {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok || named.TypeParams().Len() > 0 {
			continue
		}
		underlying, ok := named.Underlying().(*types.Interface)
		if !ok {
			continue
		}
		completed := underlying.Complete()
		methods := make([]string, completed.NumMethods())
		for i := range methods {
			methods[i] = completed.Method(i).Name()
		}
		slices.Sort(methods)
		recorded[typesPackage.Path()+"."+name] = program.NamedInterface{PackagePath: typesPackage.Path(), Name: name, Methods: methods}
	}
}

// WithBytecodeStore configures a BytecodeStorePort for persisting compiled bytecode. When
// set, SaveCompiled and LoadCompiled become available on the service.
//
// Takes store (BytecodeStorePort) which provides bytecode serialisation and persistence.
//
// Returns Option which configures the service.
func WithBytecodeStore(store program.BytecodeStorePort) Option {
	return func(c *serviceConfig) {
		c.bytecodeStore = store
	}
}

// collectPointerStaticTypeNames adds to names the qualified type each call argument's
// static type points to, for arguments whose static type is a pointer to a named type, in
// function and the functions it contains.
//
// Takes function (*CompiledFunction) which is the function to walk. It may be nil.
// Takes names (map[string]struct{}) which receives "pkg.T".
// Takes visited (map[*CompiledFunction]struct{}) which tracks the functions walked.
func collectPointerStaticTypeNames(function *program.CompiledFunction, names map[string]struct{}, visited map[*program.CompiledFunction]struct{}) {
	if function == nil {
		return
	}
	if _, seen := visited[function]; seen {
		return
	}
	visited[function] = struct{}{}
	for i := range function.CallSites {
		for _, typeString := range function.CallSites[i].ArgumentStaticTypeStrings {
			if name, ok := pointeeQualifiedName(typeString); ok {
				names[name] = struct{}{}
			}
		}
	}
	collectPointerStaticTypeNames(function.VariableInitFunction, names, visited)
	for _, nested := range function.Functions {
		collectPointerStaticTypeNames(nested, names, visited)
	}
}

// pointeeQualifiedName returns the qualified name a pointer type string points to, for a
// pointer (or pointer to pointer) to a named type such as "*pkg.T", "**pkg.T" or
// "*example.com/x.T[int]".
//
// Takes typeString (string) which is a static type as types.TypeString renders it.
//
// Returns string which is the pointee's qualified name.
// Returns bool which is false when the type is not a pointer to a named type.
func pointeeQualifiedName(typeString string) (string, bool) {
	pointee := strings.TrimLeft(typeString, "*")
	if pointee == typeString || pointee == "" {
		return "", false
	}
	head, _, _ := strings.Cut(pointee, "[")
	dot := strings.LastIndexByte(head, '.')
	if dot <= 0 || dot == len(head)-1 || strings.ContainsAny(head, " ()[]{},;*") {
		return "", false
	}
	return pointee, true
}
