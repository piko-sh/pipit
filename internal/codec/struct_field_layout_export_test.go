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

package codec

import (
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestStructLayoutTableExportRoundTrip(t *testing.T) {
	t.Parallel()

	source := &program.CompiledFunction{
		StructLayoutTable: []program.StructFieldLayout{
			{
				Offset:       0,
				TypeIndex:    7,
				Path:         [4]uint8{0, 0, 0, 0},
				PathLength:   1,
				Kind:         uint8(reflect.Int64),
				RegisterKind: uint8(isa.RegisterInt),
				Flags:        0,
			},
			{
				Offset:       16,
				TypeIndex:    7,
				Path:         [4]uint8{2, 1, 0, 0},
				PathLength:   2,
				Kind:         uint8(reflect.String),
				RegisterKind: uint8(isa.RegisterString),
				Flags:        compile.StructFieldLayoutFlagEmbedded,
			},
			{
				Offset:       24,
				TypeIndex:    11,
				Path:         [4]uint8{3, 0, 0, 0},
				PathLength:   1,
				Kind:         uint8(reflect.Float64),
				RegisterKind: uint8(isa.RegisterFloat),
				Flags:        0,
			},
		},
	}

	exported := StructLayoutTable(source)
	require.Len(t, exported, 3, "exported layoutTable preserves entry count")

	for i, entry := range exported {
		require.Equal(t, source.StructLayoutTable[i].Offset, entry.Offset, "entry %d Offset", i)
		require.Equal(t, source.StructLayoutTable[i].TypeIndex, entry.TypeIndex, "entry %d TypeIndex", i)
		require.Equal(t, source.StructLayoutTable[i].Path, entry.Path, "entry %d Path", i)
		require.Equal(t, source.StructLayoutTable[i].PathLength, entry.PathLength, "entry %d PathLength", i)
		require.Equal(t, source.StructLayoutTable[i].Kind, entry.Kind, "entry %d Kind", i)
		require.Equal(t, source.StructLayoutTable[i].RegisterKind, entry.RegisterKind, "entry %d RegisterKind", i)
		require.Equal(t, source.StructLayoutTable[i].Flags, entry.Flags, "entry %d Flags", i)
	}

	rebuilt := makeStructLayoutTableFromData(exported)
	require.Equal(t, source.StructLayoutTable, rebuilt,
		"makeStructLayoutTableFromData round trip restores byte-identical layoutTable")
}

func TestStructLayoutTableExportEmpty(t *testing.T) {
	t.Parallel()

	source := &program.CompiledFunction{}
	exported := StructLayoutTable(source)
	require.Empty(t, exported, "empty source produces empty exported table")

	rebuilt := makeStructLayoutTableFromData(exported)
	require.Empty(t, rebuilt, "empty exported table produces empty rebuilt table")
}
