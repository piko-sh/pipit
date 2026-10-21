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
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

type sharingLeaf struct {
	Name  string
	Count int
}

type sharingNode struct {
	Next     *sharingNode
	Leaves   []sharingLeaf
	Index    map[string]*sharingLeaf
	Callback func(sharingLeaf, ...int) (*sharingNode, error)
	Pair     [2]sharingLeaf
}

func sharingFunctions() []*program.CompiledFunction {
	node := reflect.TypeFor[sharingNode]()
	leaf := reflect.TypeFor[sharingLeaf]()
	return []*program.CompiledFunction{
		{TypeTable: []reflect.Type{node, leaf, reflect.PointerTo(node), reflect.TypeFor[int]()}},
		{TypeTable: []reflect.Type{leaf, node, reflect.TypeFor[[]sharingNode]()}},
		{TypeTable: []reflect.Type{node, leaf, reflect.PointerTo(node), reflect.TypeFor[int]()}},
		{
			TypeTable:            []reflect.Type{leaf, node},
			TypeTableDescriptors: []descriptor.TypeDescriptor{descriptor.ReflectTypeToDescriptor(reflect.TypeFor[string]())},
		},
		{TypeTable: []reflect.Type{leaf}},
		{TypeTable: nil},
	}
}

func packTypeTablesOf(t *testing.T) func(*flatbuffers.Builder, *program.CompiledFunction) flatbuffers.UOffsetT {
	t.Helper()
	return func(builder *flatbuffers.Builder, function *program.CompiledFunction) flatbuffers.UOffsetT {
		var failure error
		offset := packTypeTableDescriptors(t.Context(), builder, function, &failure)
		require.NoError(t, failure)
		return offset
	}
}

func packTypeTablesWith(t *testing.T, share bool, pack func(*flatbuffers.Builder, *program.CompiledFunction) flatbuffers.UOffsetT) ([]flatbuffers.UOffsetT, []byte) {
	t.Helper()

	builder := flatbuffers.NewBuilder(1024)
	if share {
		endSharing := beginDescriptorSharing(builder)
		defer endSharing()
	}
	functions := sharingFunctions()
	offsets := make([]flatbuffers.UOffsetT, len(functions))
	for index, function := range functions {
		offsets[index] = pack(builder, function)
	}
	return offsets, append([]byte(nil), builder.Bytes[builder.Head():]...)
}

func TestTypeTablePackingMatchesPackingTheExportedDescriptors(t *testing.T) {
	t.Parallel()

	viaExport := func(builder *flatbuffers.Builder, function *program.CompiledFunction) flatbuffers.UOffsetT {
		return packTypeDescriptors(builder, codec.TypeTableDescriptors(function))
	}
	for _, share := range []bool{true, false} {
		wantOffsets, wantBytes := packTypeTablesWith(t, share, viaExport)
		gotOffsets, gotBytes := packTypeTablesWith(t, share, packTypeTablesOf(t))
		require.Equal(t, wantOffsets, gotOffsets, "sharing=%v", share)
		require.Equal(t, wantBytes, gotBytes, "sharing=%v", share)
	}
}

func TestTypeTablePackingSharesARepeatedTypeTable(t *testing.T) {
	t.Parallel()

	offsets, shared := packTypeTablesWith(t, true, packTypeTablesOf(t))
	require.Equal(t, offsets[0], offsets[2], "an identical type table is written once")
	require.NotEqual(t, offsets[0], offsets[1])
	require.Zero(t, offsets[5], "an empty type table packs to no vector")

	_, unshared := packTypeTablesWith(t, false, packTypeTablesOf(t))
	require.Less(t, len(shared), len(unshared), "sharing writes each descriptor once")
}

func TestTypeTablePackingKeepsAStoredDescriptorApartFromADerivedOne(t *testing.T) {
	t.Parallel()

	builder := flatbuffers.NewBuilder(1024)
	endSharing := beginDescriptorSharing(builder)
	defer endSharing()
	memo := descriptorMemoFor(builder)
	functions := sharingFunctions()

	derived := packTypeTableDescriptor(builder, memo, functions[4], 0)
	stored := packTypeTableDescriptor(builder, memo, functions[3], 0)
	require.NotEqual(t, derived.offset, stored.offset,
		"a descriptor the function stores is packed as stored, not as the entry's type would derive")
	require.Equal(t, derived, packTypeTableDescriptor(builder, memo, functions[1], 0),
		"a derived descriptor is reused for the same type")
}

func TestTypeTablePackingStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var failure error
	function := sharingFunctions()[0]
	packTypeTableDescriptors(ctx, flatbuffers.NewBuilder(1024), function, &failure)
	require.ErrorIs(t, failure, context.Canceled)
}
