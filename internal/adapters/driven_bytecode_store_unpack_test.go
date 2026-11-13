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

//go:build !js || !wasm

package adapters

import (
	"bytes"
	"cmp"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestBoundedCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		declared  int
		wantCount int
		wantErr   error
	}{
		{name: "longer than the payload", declared: math.MaxInt32, wantCount: 0, wantErr: errCorruptBytecodePayload},
		{name: "negative", declared: -1, wantCount: 0, wantErr: errCorruptBytecodePayload},
		{name: "empty", declared: 0, wantCount: 0, wantErr: nil},
		{name: "one element", declared: 1, wantCount: 1, wantErr: nil},
		{name: "half the payload", declared: 50, wantCount: 50, wantErr: nil},
		{name: "the whole payload", declared: 100, wantCount: 100, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			count, err := boundedCount(tt.declared, 100, "function body")
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCount, count)
		})
	}
}

func TestValidatedRegisterKind_RejectsOutOfRange(t *testing.T) {
	t.Parallel()

	for _, raw := range []int8{-1, int8(isa.NumRegisterKinds), 14, 100, 127} {
		t.Run(fmt.Sprintf("raw %d", raw), func(t *testing.T) {
			t.Parallel()
			kind, err := validatedRegisterKind(raw, "parameter")
			require.ErrorIs(t, err, errCorruptBytecodePayload)
			assert.Equal(t, uint8(0), kind)
		})
	}
}

func TestValidatedRegisterKind_AcceptsEveryRealKind(t *testing.T) {
	t.Parallel()

	for raw := range isa.NumRegisterKinds {
		t.Run(isa.RegisterKind(raw).String(), func(t *testing.T) {
			t.Parallel()
			kind, err := validatedRegisterKind(int8(raw), "result")
			require.NoError(t, err)
			assert.Equal(t, uint8(raw), kind)
		})
	}
}

type receiverEntryFixture struct {
	method    uint16
	target    uint16
	named     bool
	noType    bool
	basicKind reflect.Kind
}

func receiverTablePayload(children int, entries []receiverEntryFixture) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(entries))
	for i, entry := range entries {
		var descriptorOffset flatbuffers.UOffsetT
		if !entry.noType {
			var path, name flatbuffers.UOffsetT
			if entry.named {
				path = builder.CreateString("example.com/missing")
				name = builder.CreateString("Absent")
			}
			schemagen.TypeDescriptorStart(builder)
			if entry.named {
				schemagen.TypeDescriptorAddKind(builder, schemagen.TypeDescKind(descriptor.KindNamed))
				schemagen.TypeDescriptorAddPackagePath(builder, path)
				schemagen.TypeDescriptorAddName(builder, name)
			} else {
				schemagen.TypeDescriptorAddKind(builder, schemagen.TypeDescKind(descriptor.KindBasic))
				schemagen.TypeDescriptorAddBasicKind(builder, byte(cmp.Or(entry.basicKind, reflect.Int)))
			}
			descriptorOffset = schemagen.TypeDescriptorEnd(builder)
		}
		schemagen.ReceiverMethodEntryStart(builder)
		if descriptorOffset != 0 {
			schemagen.ReceiverMethodEntryAddReceiverTypeDescriptor(builder, descriptorOffset)
		}
		schemagen.ReceiverMethodEntryAddMethodIndex(builder, entry.method)
		schemagen.ReceiverMethodEntryAddFunctionIndex(builder, entry.target)
		offsets[i] = schemagen.ReceiverMethodEntryEnd(builder)
	}
	table := createVector(builder, offsets)
	schemagen.CompiledFunctionStart(builder)
	child := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFunctionStartFunctionsVector(builder, children)
	for range children {
		builder.PrependUOffsetT(child)
	}
	functions := builder.EndVector(children)
	schemagen.CompiledFunctionStart(builder)
	schemagen.CompiledFunctionAddFunctions(builder, functions)
	schemagen.CompiledFunctionAddReceiverMethods(builder, table)
	root := schemagen.CompiledFunctionEnd(builder)
	builder.Finish(root)
	return builder.FinishedBytes()
}

func TestUnpackReceiverMethodsRefusesCorruptEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []receiverEntryFixture
		want    string
	}{
		{name: "method index outside the table", entries: []receiverEntryFixture{{method: 2, target: 0}}, want: "outside a table"},
		{name: "target index outside the table", entries: []receiverEntryFixture{{method: 0, target: 9}}, want: "outside a table"},
		{name: "two bodies for one receiver", entries: []receiverEntryFixture{{method: 0, target: 1}, {method: 0, target: 0}}, want: "different body"},
		{name: "no receiver type", entries: []receiverEntryFixture{{method: 0, target: 1, noType: true}}, want: "no receiver type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := receiverTablePayload(2, tt.entries)
			cache := newDescriptorDecodeCache()
			table, err := unpackReceiverMethods(t.Context(), schemagen.GetRootAsCompiledFunction(payload, 0), symtab.NewSymbolRegistry(nil), len(payload), cache)
			require.ErrorIs(t, err, errCorruptBytecodePayload)
			require.ErrorContains(t, err, tt.want)
			require.Nil(t, table)
		})
	}
}

func TestUnpackReceiverMethodsKeepsRepeatsAndCountsDrops(t *testing.T) {
	t.Parallel()
	payload := receiverTablePayload(2, []receiverEntryFixture{
		{method: 0, target: 1},
		{method: 0, target: 1},
		{method: 1, target: 0, named: true},
	})
	cache := newDescriptorDecodeCache()
	table, err := unpackReceiverMethods(t.Context(), schemagen.GetRootAsCompiledFunction(payload, 0), symtab.NewSymbolRegistry(nil), len(payload), cache)
	require.NoError(t, err, "an identical repeat and an unbuildable receiver are not corruption")
	require.Len(t, table, 1)
	require.Equal(t, uint16(1), table[program.ReceiverMethodKey{Receiver: reflect.TypeFor[int](), Method: 0}])
	require.Equal(t, 1, cache.droppedReceiverEntries, "the unbuildable receiver is counted")
}

func TestDecodeLogsDroppedReceiverEntriesOnce(t *testing.T) {
	t.Parallel()
	payload := receiverTablePayload(2, []receiverEntryFixture{
		{method: 0, target: 1, named: true},
		{method: 1, target: 0, named: true},
	})
	var records bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&records, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := logging.WithLogger(t.Context(), logger)
	fileSet := schemagen.GetRootAsCompiledFunction(payload, 0)
	cache := newDescriptorDecodeCache()
	_, err := unpackReceiverMethods(t.Context(), fileSet, symtab.NewSymbolRegistry(nil), len(payload), cache)
	require.NoError(t, err)
	require.Equal(t, 2, cache.droppedReceiverEntries)
	reportDroppedReceiverEntries(ctx, cache)
	require.Equal(t, 1, strings.Count(records.String(), "receiver types this registry cannot build"))
	require.Contains(t, records.String(), "entries=2")
}

func TestUnpackStringsReadsEveryIndex(t *testing.T) {
	t.Parallel()
	values := [][]byte{[]byte("a"), []byte("bc")}
	decoded, err := unpackStrings(len(values), func(index int) []byte { return values[index] }, 16)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "bc"}, decoded)
	_, err = unpackStrings(17, func(int) []byte { return nil }, 16)
	require.ErrorIs(t, err, errCorruptBytecodePayload)
}
