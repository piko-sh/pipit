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
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func singleTypeDescriptorFileSet(fill func(builder *flatbuffers.Builder)) []byte {
	builder := flatbuffers.NewBuilder(128)
	schemagen.TypeDescriptorStart(builder)
	fill(builder)
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
	return builder.FinishedBytes()
}

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
		payload := singleTypeDescriptorFileSet(func(builder *flatbuffers.Builder) {
			schemagen.TypeDescriptorAddBasicKind(builder, byte(encoding))
		})
		loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
		if allowed[reflect.Kind(encoding)] {
			require.NoError(t, err, "basic encoding %d is accepted", encoding)
			require.NotNil(t, loaded)
		} else {
			require.ErrorIs(t, err, errCorruptBytecodePayload, "basic encoding %d is rejected", encoding)
			require.Nil(t, loaded)
		}
	}
}

func TestBytecodeDescriptorKindsMatchSchema(t *testing.T) {
	t.Parallel()
	tests := []struct {
		internal descriptor.TypeDescriptorKind
		wire     schemagen.TypeDescKind
	}{
		{internal: descriptor.KindBasic, wire: schemagen.TypeDescKindBasic},
		{internal: descriptor.KindNamed, wire: schemagen.TypeDescKindNamed},
		{internal: descriptor.KindPtr, wire: schemagen.TypeDescKindPointer},
		{internal: descriptor.KindSlice, wire: schemagen.TypeDescKindSlice},
		{internal: descriptor.KindArray, wire: schemagen.TypeDescKindArray},
		{internal: descriptor.KindMap, wire: schemagen.TypeDescKindMap},
		{internal: descriptor.KindChan, wire: schemagen.TypeDescKindChannel},
		{internal: descriptor.KindFunc, wire: schemagen.TypeDescKindFunction},
		{internal: descriptor.KindStruct, wire: schemagen.TypeDescKindStruct},
		{internal: descriptor.KindInterface, wire: schemagen.TypeDescKindInterface},
		{internal: descriptor.KindNil, wire: schemagen.TypeDescKindNil},
	}
	for _, tt := range tests {
		t.Run(tt.wire.String(), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, int(tt.wire), int(tt.internal), "the internal encoding matches the schema")
			builder := flatbuffers.NewBuilder(128)
			root := packTypeDescriptor(builder, descriptor.TypeDescriptorData{Kind: uint8(tt.internal)})
			builder.Finish(root)
			payload := builder.FinishedBytes()
			encoded := schemagen.GetRootAsTypeDescriptor(payload, 0)
			require.Equal(t, tt.wire, encoded.Kind())
			decoded, err := unpackTypeDescriptor(encoded, 0, len(payload), newDescriptorDecodeCache())
			require.NoError(t, err)
			require.Equal(t, uint8(tt.internal), decoded.Kind)
		})
	}
}

func TestBytecodeRejectsUnknownTypeKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []schemagen.TypeDescKind{-128, -1, 11, 12, 127} {
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			payload := singleTypeDescriptorFileSet(func(builder *flatbuffers.Builder) {
				schemagen.TypeDescriptorAddKind(builder, kind)
			})
			loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
			require.Nil(t, loaded, "an unknown type kind is rejected")
			require.ErrorIs(t, err, errCorruptBytecodePayload)
		})
	}
}
