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
	"unsafe"

	"github.com/stretchr/testify/require"
)

func unsafeFieldPointer(base unsafe.Pointer, offset uintptr) unsafe.Pointer {
	return unsafe.Add(base, offset)
}

func TestWriteScalarFieldAtCoversEveryScalarWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check    func(t *testing.T, target *leafCounter)
		name     string
		kind     reflect.Kind
		argument tinyLeafScalarArgument
		offset   uintptr
		want     bool
	}{
		{
			name: "a signed field", kind: reflect.Int64, offset: 0,
			argument: tinyLeafScalarArgument{intValue: 42}, want: true,
			check: func(t *testing.T, target *leafCounter) { require.Equal(t, int64(42), target.Signed) },
		},
		{
			name: "an unsigned field", kind: reflect.Uint64, offset: 8,
			argument: tinyLeafScalarArgument{uintValue: 42}, want: true,
			check: func(t *testing.T, target *leafCounter) { require.Equal(t, uint64(42), target.Unsigned) },
		},
		{
			name: "a float field", kind: reflect.Float64, offset: 16,
			argument: tinyLeafScalarArgument{floatValue: 1.5}, want: true,
			check: func(t *testing.T, target *leafCounter) { require.InDelta(t, 1.5, target.Ratio, 0) },
		},
		{
			name: "a boolean field", kind: reflect.Bool, offset: 24,
			argument: tinyLeafScalarArgument{boolValue: true}, want: true,
			check: func(t *testing.T, target *leafCounter) { require.True(t, target.Flag) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := &leafCounter{}
			base := reflect.ValueOf(target).UnsafePointer()

			got := writeScalarFieldAt(unsafeFieldPointer(base, tt.offset), tt.kind, tt.argument)

			require.Equal(t, tt.want, got)
			tt.check(t, target)
		})
	}

	t.Run("a kind the writer does not handle is declined", func(t *testing.T) {
		t.Parallel()
		target := &leafCounter{}
		base := reflect.ValueOf(target).UnsafePointer()

		require.False(t, writeScalarFieldAt(base, reflect.String, tinyLeafScalarArgument{}),
			"a non-scalar kind must fall back rather than scribble over the field")
	})
}
