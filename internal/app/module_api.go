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
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"

	"golang.org/x/tools/go/gcexportdata"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/module"
	"pipit.sh/pipit/internal/safeconv"
)

// PackageModule compiles a multi-package Go program and packages it as a module bundle.
//
// Takes moduleDescriptor (module.Descriptor) which declares the module's identity and
// capability set. Required fields: SchemaVersion, Ref.Path.
// Takes modulePath (string) which is the Go module path passed to CompileProgram.
// Takes packages (map[string]map[string]string) which maps package to file to source.
// Takes bytecodePacker (func(*CompiledFileSet) []byte) which serialises the compiled
// output.
//
// Returns *module.Bundle which is the packaged module.
// Returns error when compilation, packing, or fingerprinting fails.
func (s *Service) PackageModule(
	ctx context.Context,
	moduleDescriptor module.Descriptor,
	modulePath string,
	packages map[string]map[string]string,
	bytecodePacker func(*program.CompiledFileSet) []byte,
) (*module.Bundle, error) {
	if bytecodePacker == nil {
		return nil, errors.New("app: PackageModule requires a non-nil bytecodePacker")
	}
	if err := moduleDescriptor.Validate(); err != nil {
		return nil, err
	}
	ctx = logging.ContextWithLogger(ctx, s.config.logger)

	bridgesAtStart := len(s.pendingVarBridges)
	baseAtStart := s.globals.Lengths()

	compiled, err := s.CompileProgram(ctx, modulePath, packages)
	if err != nil {
		return nil, fmt.Errorf("app: PackageModule compile: %w", err)
	}

	baseAtEnd := s.globals.Lengths()
	if err := s.stampBundleMetadata(compiled, baseAtStart, baseAtEnd, s.pendingVarBridges[bridgesAtStart:]); err != nil {
		return nil, err
	}

	bytecode := bytecodePacker(compiled)

	typesExport, err := s.encodeTypesExportForPackages(modulePath, packages)
	if err != nil {
		return nil, fmt.Errorf("app: PackageModule type-export: %w", err)
	}

	bundle := &module.Bundle{
		Descriptor:  &moduleDescriptor,
		Bytecode:    bytecode,
		TypesExport: typesExport,
	}
	fingerprint, err := bundle.Fingerprint()
	if err != nil {
		return nil, fmt.Errorf("app: PackageModule fingerprint: %w", err)
	}
	bundle.Descriptor.Ref.Pin = fingerprint
	return bundle, nil
}

// LoadModule verifies, unpacks, and registers a module bundle. The bytecode verifier
// always runs here regardless of WithBytecodeVerification, because a bundle is
// third-party input.
//
// Takes bundle (*module.Bundle) which is the validated module to load.
// Takes reference (module.Ref) which carries the expected pin. An empty pin is rejected
// unless WithAllowUnpinnedModules is set.
// Takes _ (module.Provider) reserved for transitive dependencies.
// Takes bytecodeUnpacker (func([]byte, *symtab.SymbolRegistry) (*CompiledFileSet, error))
// which deserialises the bytecode.
//
// Returns *module.Loaded which is the loaded module handle.
// Returns error when verification, unpacking, or capability checks fail.
func (s *Service) LoadModule(
	ctx context.Context,
	bundle *module.Bundle,
	reference module.Ref,
	_ module.Provider,
	bytecodeUnpacker func([]byte, *symtab.SymbolRegistry) (*program.CompiledFileSet, error),
) (*module.Loaded, error) {
	if err := s.validateLoadModuleInputs(ctx, bundle, reference, bytecodeUnpacker); err != nil {
		return nil, err
	}
	ctx = logging.ContextWithLogger(ctx, s.config.logger)

	cfs, err := bytecodeUnpacker(bundle.Bytecode, s.symbols)
	if err != nil {
		return nil, fmt.Errorf("app: LoadModule unpack: %w", err)
	}
	if err := verifyLoadedFileSet(ctx, cfs); err != nil {
		return nil, fmt.Errorf("app: LoadModule %s: %w", bundle.Descriptor.Ref.Path, err)
	}

	if err := s.installLoadedBundle(ctx, cfs, bundle); err != nil {
		return nil, err
	}

	fingerprint, err := bundle.Fingerprint()
	if err != nil {
		return nil, err
	}
	return &module.Loaded{
		Descriptor:  bundle.Descriptor,
		Fingerprint: fingerprint,
	}, nil
}

// stampBundleMetadata relativises operands and records bundle metadata.
//
// Relativises the compiled bundle's global operands against baseAtStart and records slot
// allocation and exported-var metadata on the CompiledFileSet so the unpack path can
// rebuild storage and registry entries without source access.
//
// Takes compiled (*CompiledFileSet) which receives the metadata stamps.
// Takes baseAtStart (SlotAllocation) which is the slot count before compilation began.
// Takes baseAtEnd (SlotAllocation) which is the slot count after compilation completed.
// Takes newBridges ([]pendingVarBridge) which lists the var bridges produced by this
// compilation.
//
// Returns error when relativisation or metadata collection fails.
func (s *Service) stampBundleMetadata(compiled *program.CompiledFileSet, baseAtStart, baseAtEnd program.SlotAllocation, newBridges []pendingVarBridge) error {
	var slotAlloc program.SlotAllocation
	for k := range program.NumGlobalRegisterKinds {
		slotAlloc[k] = baseAtEnd[k] - baseAtStart[k]
	}
	if err := program.RelativiseGlobalOperands(compiled.Root(), baseAtStart); err != nil {
		return fmt.Errorf("app: PackageModule relativise: %w", err)
	}
	if err := program.RelativiseGlobalOperands(compiled.VariableInitFunction(), baseAtStart); err != nil {
		return fmt.Errorf("app: PackageModule relativise varinit: %w", err)
	}
	compiled.SetSlotAllocation(slotAlloc)
	if len(newBridges) > 0 {
		packageVars, err := s.collectPackageVariables(newBridges, baseAtStart)
		if err != nil {
			return fmt.Errorf("app: PackageModule var metadata: %w", err)
		}
		compiled.SetPackageVariables(packageVars)
	}
	return nil
}

// collectPackageVariables converts pendingVarBridges to PackageVariableMetadata with slot
// indices made relative to baseAtStart.
//
// Takes pending ([]pendingVarBridge) which is the new var bridge list.
// Takes baseAtStart (SlotAllocation) which is the slot count before this compilation
// began.
//
// Returns []PackageVariableMetadata which is the per-var metadata.
// Returns error when a kind is out of range or a slot precedes baseAtStart.
func (*Service) collectPackageVariables(pending []pendingVarBridge, baseAtStart program.SlotAllocation) ([]program.PackageVariableMetadata, error) {
	count := 0
	for _, p := range pending {
		count += len(p.vars)
	}
	out := make([]program.PackageVariableMetadata, 0, count)
	for _, p := range pending {
		for _, v := range p.vars {
			kind := uint8(v.slot.Kind)
			if int(kind) >= program.NumGlobalRegisterKinds {
				return nil, fmt.Errorf("var %s.%s has out-of-range kind %d", p.importPath, v.name, kind)
			}
			bank := uint8(v.slot.SlotKind())
			absoluteSlot := safeconv.MustIntToUint16(v.slot.Index)
			if absoluteSlot < baseAtStart[bank] {
				return nil, fmt.Errorf("var %s.%s slot %d precedes baseAtStart %d (bank %d)", p.importPath, v.name, absoluteSlot, baseAtStart[bank], bank)
			}
			out = append(out, program.PackageVariableMetadata{
				Name:         v.name,
				PackagePath:  p.importPath,
				Type:         new(codec.ExportTypeDescriptor(descriptor.ReflectTypeToDescriptor(v.reflectType))),
				RegisterKind: kind,
				RelativeSlot: absoluteSlot - baseAtStart[bank],
				IsIndirect:   v.slot.IsIndirect,
			})
		}
	}
	return out, nil
}

// encodeTypesExportForPackages serialises each non-main sub-package's types.Package as
// gcexportdata for inclusion in a module bundle.
//
// Takes modulePath (string) which is the Go module path.
// Takes packages (map[string]map[string]string) which maps package to file to source.
//
// Returns []byte which is the TLV-encoded TypesExport payload, or nil bytes when no
// non-main packages are present.
// Returns error when a gcexportdata serialisation step fails.
func (s *Service) encodeTypesExportForPackages(modulePath string, packages map[string]map[string]string) ([]byte, error) {
	relPaths := make([]string, 0, len(packages))
	for relPath := range packages {
		relPaths = append(relPaths, relPath)
	}
	slices.Sort(relPaths)

	entries := make([]module.TypesExportEntry, 0, len(relPaths))
	for _, relPath := range relPaths {
		importPath := modulePath
		if relPath != "" {
			importPath = modulePath + "/" + relPath
		}
		pkg, err := s.symbols.Import(importPath)
		if err != nil {
			continue
		}
		if pkg == nil || pkg.Name() == "main" {
			continue
		}
		var buffer bytes.Buffer
		if err := gcexportdata.Write(&buffer, s.fileSet, pkg); err != nil {
			return nil, fmt.Errorf("gcexportdata.Write %q: %w", importPath, err)
		}
		functionTable := s.deriveFunctionTableForPackage(importPath)
		entries = append(entries, module.TypesExportEntry{
			ImportPath:    importPath,
			Data:          buffer.Bytes(),
			FunctionTable: functionTable,
		})
	}
	return module.EncodeTypesExport(entries)
}

// deriveFunctionTableForPackage recovers function indices for a package by walking the
// symbol registry's exports and matching each function closure to its position in the
// shared root function.
//
// Takes importPath (string) which is the package import path.
//
// Returns map[string]uint16 which maps exported function name to its index in the shared
// root function, or nil when no exports are registered for importPath.
func (s *Service) deriveFunctionTableForPackage(importPath string) map[string]uint16 {
	exports, ok := s.symbols.LookupPackage(importPath)
	if !ok {
		return nil
	}
	out := make(map[string]uint16, len(exports))
	for name, value := range exports {
		if !value.IsValid() || value.Kind() != reflect.Pointer {
			continue
		}
		closure, ok := reflect.TypeAssert[*engine.RuntimeClosure](value)
		if !ok || closure == nil || closure.Function == nil || closure.RootFunction == nil {
			continue
		}
		for i, fn := range program.ExportFunctions(closure.RootFunction) {
			if fn == closure.Function {
				out[name] = uint16(i)
				break
			}
		}
	}
	return out
}

// appendLoadedVarBridges rebuilds pendingVarBridge entries at load.
//
// Rebuilds entries from the bundle's per-package var metadata and the load-time slot
// bases. Each var gets a freshly-allocated settable reflect.Value (the storage advertised
// in the symbol registry by bridgeBytecodeModule); after Service.ExecuteInits runs the
// bundle's variableInitFunction, finalisePendingVarBridges snapshots the populated
// GlobalStore values into these storages.
//
// Takes vars ([]PackageVariableMetadata) which is the bundle's per-package var metadata.
// Takes bases (SlotAllocation) which is the load-time slot base table.
//
// Returns error when a type descriptor decode fails or a kind is out of range.
func (s *Service) appendLoadedVarBridges(vars []program.PackageVariableMetadata, bases program.SlotAllocation) error {
	if len(vars) == 0 {
		return nil
	}
	byPackage := make(map[string][]packageVarExport)
	for _, v := range vars {
		if v.Type == nil {
			return fmt.Errorf("var %s.%s missing type descriptor", v.PackagePath, v.Name)
		}
		if int(v.RegisterKind) >= program.NumGlobalRegisterKinds {
			return fmt.Errorf("var %s.%s register-kind %d out of range", v.PackagePath, v.Name, v.RegisterKind)
		}
		reflectType, err := s.importTypeDescriptorAsReflectType(*v.Type)
		if err != nil {
			return fmt.Errorf("var %s.%s reflect type: %w", v.PackagePath, v.Name, err)
		}
		storage := reflect.New(reflectType).Elem()
		bank := v.RegisterKind
		if v.IsIndirect {
			bank = uint8(isa.RegisterGeneral)
		}
		absoluteSlot := int(bases[bank]) + int(v.RelativeSlot)
		byPackage[v.PackagePath] = append(byPackage[v.PackagePath], packageVarExport{
			name:        v.Name,
			reflectType: reflectType,
			storage:     storage,
			slot:        program.GlobalVariableInfo{Index: absoluteSlot, Kind: isa.RegisterKind(v.RegisterKind), IsIndirect: v.IsIndirect},
		})
	}
	for importPath, vars := range byPackage {
		s.pendingVarBridges = append(s.pendingVarBridges, pendingVarBridge{
			importPath: importPath,
			vars:       vars,
		})
	}
	return nil
}

// runLoadedBundleInits executes the bundle's variable initialisers.
//
// Executes both the top-level variableInitFunction (when present) and each function
// indexed by initFunctionIndices.
//
// Takes cfs (*CompiledFileSet) which is the loaded bundle.
//
// Returns error when an init function execution fails.
func (s *Service) runLoadedBundleInits(ctx context.Context, cfs *program.CompiledFileSet) error {
	if cfs == nil || cfs.Root() == nil {
		return nil
	}
	if cfs.VariableInitFunction() != nil {
		if err := s.runVariableInits(ctx, cfs); err != nil {
			return err
		}
	}
	for _, initIndex := range cfs.InitFunctions() {
		if int(initIndex) >= len(program.ExportFunctions(cfs.Root())) {
			continue
		}
		function := program.ExportFunctions(cfs.Root())[initIndex]
		if err := s.executeInitFunction(ctx, cfs.Root(), function); err != nil {
			return fmt.Errorf("executing loaded-bundle init function: %w", err)
		}
	}
	return nil
}

// snapshotLoadedVarBridges copies post-init GlobalStore values into the bridge storages
// for entries in the half-open range [startIndex, len). Entries remain in the queue for
// later bridgeBytecodeModule iteration.
//
// Takes startIndex (int) which is the first pendingVarBridge index to snapshot.
func (s *Service) snapshotLoadedVarBridges(startIndex int) {
	if startIndex >= len(s.pendingVarBridges) {
		return
	}
	for _, pending := range s.pendingVarBridges[startIndex:] {
		for _, v := range pending.vars {
			value, ok := s.globals.SnapshotVarAs(v.slot, v.reflectType)
			if !ok {
				continue
			}
			v.storage.Set(value)
		}
	}
}

// importTypeDescriptorAsReflectType decodes a symtab.TypeDescriptorData.
//
// Decodes a serialised symtab.TypeDescriptorData into a reflect.Type via the same import
// path the bytecode unpacker uses to reconstruct type-table entries.
//
// Takes data (symtab.TypeDescriptorData) which is the serialised descriptor.
//
// Returns reflect.Type which is the resolved native type.
// Returns error when the descriptor cannot be resolved.
func (s *Service) importTypeDescriptorAsReflectType(data descriptor.TypeDescriptorData) (reflect.Type, error) {
	typeDescriptor := codec.ImportTypeDescriptor(data)
	return symtab.ReflectTypeFor(typeDescriptor, s.symbols)
}

// validateLoadModuleInputs performs LoadModule() pre-flight checks.
//
// Checks context liveness, a non-nil unpacker, bundle self-validation, pin presence,
// reference verification, and capability consultation.
//
// Takes bundle (*module.Bundle) which must self-validate.
// Takes reference (module.Ref) which must carry a pin (unless the service allows unpinned
// modules) and match the bundle fingerprint.
// Takes bytecodeUnpacker (func([]byte, *symtab.SymbolRegistry) (*CompiledFileSet, error))
// which must be non-nil.
//
// Returns error when any pre-flight check fails.
func (s *Service) validateLoadModuleInputs(
	ctx context.Context,
	bundle *module.Bundle,
	reference module.Ref,
	bytecodeUnpacker func([]byte, *symtab.SymbolRegistry) (*program.CompiledFileSet, error),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if bytecodeUnpacker == nil {
		return errors.New("app: LoadModule requires a non-nil bytecodeUnpacker")
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if reference.Pin == "" && !s.allowsUnpinnedModules() {
		return fmt.Errorf("%w: %s", module.ErrUnpinnedRef, reference.String())
	}
	if err := bundle.VerifyAgainstRef(reference); err != nil {
		return err
	}
	return s.consultModuleCapabilities(ctx, bundle.Descriptor)
}

// allowsUnpinnedModules reports whether this service accepts LoadModule refs without a
// pin, as configured by WithAllowUnpinnedModules.
//
// Returns true when unpinned refs are accepted.
func (s *Service) allowsUnpinnedModules() bool {
	return s.config != nil && s.config.allowUnpinnedModules
}

// installLoadedBundle reserves slots and installs an unpacked bundle.
//
// Reserves global-store slots for the unpacked bundle and stamps per-kind bases onto its
// functions, rebuilds the pendingVarBridge entries, bridges the bundle's TypesExport
// packages into the registry, runs its variable initialisers, and snapshots the
// initialised values into the registered storages. Each loaded bundle owns its own
// variableInitFunction and is installed independently.
//
// Takes cfs (*CompiledFileSet) which is the unpacked bundle bytecode.
// Takes bundle (*module.Bundle) which carries the TypesExport payload to bridge.
//
// Returns error when any installation step fails.
func (s *Service) installLoadedBundle(ctx context.Context, cfs *program.CompiledFileSet, bundle *module.Bundle) error {
	loadBases := s.globals.ReserveSlots(cfs.SlotAllocation())
	program.SetGlobalBases(cfs.Root(), &loadBases)
	program.SetGlobalBases(cfs.VariableInitFunction(), &loadBases)

	bridgesAtLoad := len(s.pendingVarBridges)
	if err := s.appendLoadedVarBridges(cfs.PackageVariables(), loadBases); err != nil {
		return fmt.Errorf("app: LoadModule var bridges: %w", err)
	}
	if err := s.bridgeLoadedTypesExport(bundle, cfs); err != nil {
		return err
	}
	if err := s.runLoadedBundleInits(ctx, cfs); err != nil {
		return fmt.Errorf("app: LoadModule inits: %w", err)
	}
	if len(s.pendingVarBridges) > bridgesAtLoad {
		s.snapshotLoadedVarBridges(bridgesAtLoad)
	}
	return nil
}

// bridgeLoadedTypesExport bridges the bundle's TypesExport into registry.
//
// Decodes the bundle's TypesExport and bridges each go/types.Package into the symbol
// registry so a downstream importer can resolve this module's packages. No-op when the
// bundle carries no TypesExport (function-only module).
//
// Takes bundle (*module.Bundle) which carries the TypesExport payload.
// Takes cfs (*CompiledFileSet) which is the unpacked bundle bytecode.
//
// Returns error when decode or bridging fails.
func (s *Service) bridgeLoadedTypesExport(bundle *module.Bundle, cfs *program.CompiledFileSet) error {
	if len(bundle.TypesExport) == 0 {
		return nil
	}
	entries, err := module.DecodeTypesExport(bundle.TypesExport)
	if err != nil {
		return fmt.Errorf("app: LoadModule TypesExport decode: %w", err)
	}
	for _, entry := range entries {
		if err := s.bridgeBytecodeModule(entry, cfs); err != nil {
			return fmt.Errorf("app: LoadModule bridge %q: %w", entry.ImportPath, err)
		}
	}
	return nil
}

// consultModuleCapabilities walks the descriptor's capability set and asks the installed
// CapabilityHook to approve each claim. A nil hook is permissive because every gated
// operation is still checked at dispatch time.
//
// Takes moduleDescriptor (*module.Descriptor) which supplies the claims.
//
// Returns the first denial error or nil.
func (s *Service) consultModuleCapabilities(ctx context.Context, moduleDescriptor *module.Descriptor) error {
	hook := s.limits.CapabilityHook
	if hook == nil {
		return nil
	}
	for _, capability := range moduleDescriptor.Capabilities {
		path := "module/load:" + capability.Axis
		if err := hook.CheckFunctionCall(ctx, moduleDescriptor.Ref.Path, path, nil); err != nil {
			return fmt.Errorf("%w: %s: %w", module.ErrCapabilityDenied, capability.String(), err)
		}
	}
	return nil
}
