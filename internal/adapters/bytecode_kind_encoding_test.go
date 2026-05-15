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
	"context"
	"errors"
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestBytecodeBasicKindEncodings(t *testing.T) {
	t.Parallel()
	allowed := map[reflect.Kind]bool{
		reflect.Invalid: true, reflect.Bool: true, reflect.Int: true,
		reflect.Int8: true, reflect.Int16: true, reflect.Int32: true,
		reflect.Int64: true, reflect.Uint: true, reflect.Uint8: true,
		reflect.Uint16: true, reflect.Uint32: true, reflect.Uint64: true,
		reflect.Uintptr: true, reflect.Float32: true, reflect.Float64: true,
		reflect.Complex64: true, reflect.Complex128: true, reflect.String: true,
		reflect.UnsafePointer: true,
	}
	for encoding := range 256 {
		builder := flatbuffers.NewBuilder(128)
		schemagen.TypeDescriptorStart(builder)
		schemagen.TypeDescriptorAddBasicKind(builder, byte(encoding))
		typeDescriptor := schemagen.TypeDescriptorEnd(builder)
		schemagen.CompiledFunctionStartTypeTableDescriptorsVector(builder, 1)
		builder.PrependUOffsetT(typeDescriptor)
		types := builder.EndVector(1)
		schemagen.CompiledFunctionStart(builder)
		schemagen.CompiledFunctionAddTypeTableDescriptors(builder, types)
		function := schemagen.CompiledFunctionEnd(builder)
		schemagen.CompiledFileSetStart(builder)
		schemagen.CompiledFileSetAddRoot(builder, function)
		root := schemagen.CompiledFileSetEnd(builder)
		builder.Finish(root)
		loaded, err := decodeCompiledFileSet(context.Background(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
		if allowed[reflect.Kind(encoding)] {
			if err != nil || loaded == nil {
				t.Fatalf("valid basic encoding %d rejected: %v", encoding, err)
			}
		} else if loaded != nil || !errors.Is(err, errCorruptBytecodePayload) {
			t.Fatalf("invalid basic encoding %d accepted: %v", encoding, err)
		}
	}
}

func TestBytecodeDescriptorKindsMatchSchema(t *testing.T) {
	t.Parallel()
	kinds := []struct {
		internal descriptor.TypeDescriptorKind
		wire     schemagen.TypeDescKind
	}{
		{descriptor.KindBasic, schemagen.TypeDescKindBasic},
		{descriptor.KindNamed, schemagen.TypeDescKindNamed},
		{descriptor.KindPtr, schemagen.TypeDescKindPointer},
		{descriptor.KindSlice, schemagen.TypeDescKindSlice},
		{descriptor.KindArray, schemagen.TypeDescKindArray},
		{descriptor.KindMap, schemagen.TypeDescKindMap},
		{descriptor.KindChan, schemagen.TypeDescKindChannel},
		{descriptor.KindFunc, schemagen.TypeDescKindFunction},
		{descriptor.KindStruct, schemagen.TypeDescKindStruct},
		{descriptor.KindInterface, schemagen.TypeDescKindInterface},
		{descriptor.KindNil, schemagen.TypeDescKindNil},
	}
	for _, kind := range kinds {
		if int(kind.internal) != int(kind.wire) {
			t.Fatalf("descriptor kind %v has internal encoding %d", kind.wire, kind.internal)
		}
		builder := flatbuffers.NewBuilder(128)
		root := packTypeDescriptor(builder, descriptor.TypeDescriptorData{Kind: uint8(kind.internal)})
		builder.Finish(root)
		payload := builder.FinishedBytes()
		encoded := schemagen.GetRootAsTypeDescriptor(payload, 0)
		if encoded.Kind() != kind.wire {
			t.Fatalf("writer emitted %v for %v", encoded.Kind(), kind.wire)
		}
		decoded, err := unpackTypeDescriptor(encoded, 0, len(payload), newDescriptorDecodeCache())
		if err != nil || decoded.Kind != uint8(kind.internal) {
			t.Fatal("descriptor numbering did not round trip:", kind.wire, err)
		}
	}
}

func TestBytecodeRejectsUnknownTypeKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []schemagen.TypeDescKind{-128, -1, 11, 12, 127} {
		builder := flatbuffers.NewBuilder(128)
		schemagen.TypeDescriptorStart(builder)
		schemagen.TypeDescriptorAddKind(builder, kind)
		typeDescriptor := schemagen.TypeDescriptorEnd(builder)
		schemagen.CompiledFunctionStartTypeTableDescriptorsVector(builder, 1)
		builder.PrependUOffsetT(typeDescriptor)
		types := builder.EndVector(1)
		schemagen.CompiledFunctionStart(builder)
		schemagen.CompiledFunctionAddTypeTableDescriptors(builder, types)
		function := schemagen.CompiledFunctionEnd(builder)
		schemagen.CompiledFileSetStart(builder)
		schemagen.CompiledFileSetAddRoot(builder, function)
		root := schemagen.CompiledFileSetEnd(builder)
		builder.Finish(root)
		loaded, err := decodeCompiledFileSet(context.Background(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
		if loaded != nil || !errors.Is(err, errCorruptBytecodePayload) {
			t.Fatal("unknown type kind became runtime authority:", kind, err)
		}
	}
}
