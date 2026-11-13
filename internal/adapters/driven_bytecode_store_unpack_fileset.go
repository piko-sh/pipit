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

package adapters

import (
	"fmt"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/mem"
	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

// unpackSlotAllocation reconstructs the per-bank global-store slot reservation counts
// from their FlatBuffer struct. A nil struct (no reservations were serialised) yields the
// zero allocation.
//
// Takes fbFileSet (*schemagen.CompiledFileSet) which contains the serialised slot
// allocation.
//
// Returns program.SlotAllocation with one count per bank.
func unpackSlotAllocation(fbFileSet *schemagen.CompiledFileSet) program.SlotAllocation {
	var slotAllocation program.SlotAllocation
	if alloc := fbFileSet.SlotAllocation(nil); alloc != nil {
		slotAllocation[slotBankInt] = alloc.IntCount()
		slotAllocation[slotBankFloat] = alloc.FloatCount()
		slotAllocation[slotBankString] = alloc.StringCount()
		slotAllocation[slotBankBool] = alloc.BoolCount()
		slotAllocation[slotBankUint] = alloc.UintCount()
		slotAllocation[slotBankComplex] = alloc.ComplexCount()
		slotAllocation[slotBankGeneral] = alloc.GeneralCount()
	}
	return slotAllocation
}

// unpackPackageVariables reconstructs the exported package-variable metadata vector,
// recursively unpacking each entry's optional type descriptor.
//
// Takes fbFileSet (*schemagen.CompiledFileSet) which contains the serialised
// package-variable entries.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared entry count.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns []program.PackageVariableMetadata which holds the reconstructed metadata.
// Returns error when a serialised type descriptor cannot be unpacked, the declared entry
// count exceeds the payload size, or a register kind is out of range.
func unpackPackageVariables(fbFileSet *schemagen.CompiledFileSet, payloadLen int, cache *descriptorDecodeCache) ([]program.PackageVariableMetadata, error) {
	count, err := boundedCount(fbFileSet.PackageVariablesLength(), payloadLen, "package variables")
	if err != nil {
		return nil, err
	}
	packageVariables := make([]program.PackageVariableMetadata, 0, count)
	var fbVar schemagen.PackageVariableEntry
	for i := range count {
		if !fbFileSet.PackageVariables(&fbVar, i) {
			continue
		}
		var typeData *descriptor.TypeDescriptorData
		if td := fbVar.TypeDescriptor(nil); td != nil {
			value, err := unpackTypeDescriptor(td, 0, payloadLen, cache)
			if err != nil {
				return nil, fmt.Errorf("unpacking package-variable type descriptor for %s.%s: %w", mem.String(fbVar.PackagePath()), mem.String(fbVar.Name()), err)
			}
			typeData = &value
		}
		registerKind, err := validatedRegisterKind(int8(fbVar.RegisterKind()), "package variable")
		if err != nil {
			return nil, fmt.Errorf("unpacking package variable %s.%s: %w", mem.String(fbVar.PackagePath()), mem.String(fbVar.Name()), err)
		}
		packageVariables = append(packageVariables, program.PackageVariableMetadata{
			Name:         mem.String(fbVar.Name()),
			PackagePath:  mem.String(fbVar.PackagePath()),
			Type:         typeData,
			RegisterKind: registerKind,
			RelativeSlot: fbVar.RelativeSlot(),
			IsIndirect:   fbVar.IsIndirect(),
		})
	}
	return packageVariables, nil
}

// unpackNamedInterfaces reconstructs the interface types a program declares and names in
// a reflect.TypeOf call.
//
// Takes fbFileSet (*schemagen.CompiledFileSet) which contains the serialised entries.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared counts.
//
// Returns []program.NamedInterface which describes each interface, or nil when there are
// none.
// Returns error when a declared count exceeds the payload size, or an entry is malformed
// or repeats another.
func unpackNamedInterfaces(fbFileSet *schemagen.CompiledFileSet, payloadLen int) ([]program.NamedInterface, error) {
	count, err := boundedCount(fbFileSet.NamedInterfacesLength(), payloadLen, "named interfaces")
	if err != nil || count == 0 {
		return nil, err
	}
	interfaces := make([]program.NamedInterface, 0, count)
	seen := make(map[string]struct{}, count)
	var fbEntry schemagen.NamedInterfaceEntry
	for i := range count {
		if !fbFileSet.NamedInterfaces(&fbEntry, i) {
			continue
		}
		entry, err := unpackNamedInterface(&fbEntry, i, payloadLen)
		if err != nil {
			return nil, err
		}
		qualifiedName := entry.PackagePath + "." + entry.Name
		if _, repeated := seen[qualifiedName]; repeated {
			return nil, fmt.Errorf("%w: named interface %d repeats %s", errCorruptBytecodePayload, i, qualifiedName)
		}
		seen[qualifiedName] = struct{}{}
		interfaces = append(interfaces, entry)
	}
	return interfaces, nil
}

// unpackNamedInterface reconstructs and validates one named interface entry. The name and
// every method name must be Go identifiers, the package path must be a plausible import
// path, and no method may repeat.
//
// Takes fbEntry (*schemagen.NamedInterfaceEntry) which is the serialised entry.
// Takes index (int) which is the entry's position, for messages.
// Takes payloadLen (int) which bounds the declared method count.
//
// Returns program.NamedInterface which describes the interface.
// Returns error when the entry is malformed.
func unpackNamedInterface(fbEntry *schemagen.NamedInterfaceEntry, index, payloadLen int) (program.NamedInterface, error) {
	name := mem.String(fbEntry.Name())
	if name == "" {
		return program.NamedInterface{}, fmt.Errorf("%w: named interface %d has no name", errCorruptBytecodePayload, index)
	}
	if !token.IsIdentifier(name) {
		return program.NamedInterface{}, fmt.Errorf("%w: named interface %d has an invalid name", errCorruptBytecodePayload, index)
	}
	packagePath := mem.String(fbEntry.PackagePath())
	if !validInterfacePackagePath(packagePath) {
		return program.NamedInterface{}, fmt.Errorf("%w: named interface %d has an invalid package path", errCorruptBytecodePayload, index)
	}
	methodCount, err := boundedCount(fbEntry.MethodsLength(), payloadLen, "named interface methods")
	if err != nil {
		return program.NamedInterface{}, err
	}
	methods := make([]string, methodCount)
	seen := make(map[string]struct{}, methodCount)
	for j := range methodCount {
		method := mem.String(fbEntry.Methods(j))
		if !token.IsIdentifier(method) {
			return program.NamedInterface{}, fmt.Errorf("%w: named interface %d has an invalid method name", errCorruptBytecodePayload, index)
		}
		if _, repeated := seen[method]; repeated {
			return program.NamedInterface{}, fmt.Errorf("%w: named interface %d repeats method %s", errCorruptBytecodePayload, index, method)
		}
		seen[method] = struct{}{}
		methods[j] = method
	}
	return program.NamedInterface{PackagePath: packagePath, Name: name, Methods: methods}, nil
}

// validInterfacePackagePath reports whether path can be the package path of a declared
// interface. It must be non-empty valid UTF-8 of printable characters with no spaces, and
// must not start or end with a slash.
//
// Takes path (string) which is the decoded package path.
//
// Returns bool which is true when the path is acceptable.
func validInterfacePackagePath(path string) bool {
	if path == "" || !utf8.ValidString(path) || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	for _, character := range path {
		if !unicode.IsGraphic(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}
