//go:build !safe && !(js && wasm)

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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type tier0General struct {
	Values  []int64
	Boxed   any
	Pointer *tier0Fields
	Text    string
}

func TestUnwrapInterfaceLeafPeelsOnlyANonNilInterface(t *testing.T) {
	t.Parallel()

	var boxed any = 42
	interfaceValue := reflect.ValueOf(&boxed).Elem()

	var empty any
	nilInterface := reflect.ValueOf(&empty).Elem()

	tests := []struct {
		field reflect.Value
		check func(t *testing.T, got reflect.Value)
		name  string
	}{
		{
			name:  "a non-nil interface is peeled to its dynamic value",
			field: interfaceValue,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.Int, got.Kind()) },
		},
		{
			name:  "a nil interface becomes the invalid value",
			field: nilInterface,
			check: func(t *testing.T, got reflect.Value) { require.False(t, got.IsValid()) },
		},
		{
			name:  "a concrete value passes through",
			field: reflect.ValueOf("pipit"),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, "pipit", got.String()) },
		},
		{
			name:  "an invalid value stays invalid",
			field: reflect.Value{},
			check: func(t *testing.T, got reflect.Value) { require.False(t, got.IsValid()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, unwrapInterfaceLeaf(tt.field))
		})
	}
}

func TestSnapshotPointerLeafOnlyRewritesAnAddressablePointer(t *testing.T) {
	t.Parallel()

	t.Run("a non-pointer field passes through", func(t *testing.T) {
		t.Parallel()
		field := reflect.ValueOf("pipit")

		require.Equal(t, field, snapshotPointerLeaf(field))
	})

	t.Run("an invalid value passes through", func(t *testing.T) {
		t.Parallel()
		require.False(t, snapshotPointerLeaf(reflect.Value{}).IsValid())
	})

	t.Run("a non-addressable pointer passes through", func(t *testing.T) {
		t.Parallel()
		field := reflect.ValueOf(&tier0Fields{})

		require.Equal(t, field.Pointer(), snapshotPointerLeaf(field).Pointer())
	})

	t.Run("an addressable pointer field keeps pointing at the same object", func(t *testing.T) {
		t.Parallel()
		target := &tier0Fields{Signed: 42}
		holder := &tier0General{Pointer: target}
		field := reflect.ValueOf(holder).Elem().Field(2)

		snapshot := snapshotPointerLeaf(field)

		require.Equal(t, reflect.Pointer, snapshot.Kind())
		require.Equal(t, field.Pointer(), snapshot.Pointer(),
			"the snapshot must address the same object the field did")
	})
}

func TestLayoutStructMatchesReceiverRefusesAnUnknownTypeIndex(t *testing.T) {
	t.Parallel()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	compiledFunction := builder.build()

	_, frame, registers := newFramedVM(t, compiledFunction)
	registers.General[1] = reflect.ValueOf(&tier0Fields{})

	layout := tier0Layout(0)
	layout.TypeIndex = 99

	require.False(t, layoutStructMatchesReceiver(frame, registers, 1, layout),
		"a type index past the word table cannot be matched against the receiver")
}
