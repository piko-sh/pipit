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
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/schema/schemagen"
)

func namedInterfaceEntriesPayload(entries []program.NamedInterface) []byte {
	builder := flatbuffers.NewBuilder(256)
	offset := packNamedInterfaces(builder, entries)
	schemagen.CompiledFileSetStart(builder)
	if offset != 0 {
		schemagen.CompiledFileSetAddNamedInterfaces(builder, offset)
	}
	root := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(root)
	return builder.FinishedBytes()
}

func TestUnpackNamedInterfacesValidatesEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []program.NamedInterface
		want    string
	}{
		{name: "empty name", entries: []program.NamedInterface{{PackagePath: "main", Name: "", Methods: nil}}, want: "has no name"},
		{name: "name not an identifier", entries: []program.NamedInterface{{PackagePath: "main", Name: "a.b", Methods: nil}}, want: "invalid name"},
		{name: "empty package path", entries: []program.NamedInterface{{PackagePath: "", Name: "Namer", Methods: nil}}, want: "invalid package path"},
		{name: "package path with a space", entries: []program.NamedInterface{{PackagePath: "a b", Name: "Namer", Methods: nil}}, want: "invalid package path"},
		{name: "package path ending in a slash", entries: []program.NamedInterface{{PackagePath: "a/", Name: "Namer", Methods: nil}}, want: "invalid package path"},
		{name: "package path with a control character", entries: []program.NamedInterface{{PackagePath: "a\x00b", Name: "Namer", Methods: nil}}, want: "invalid package path"},
		{name: "empty method name", entries: []program.NamedInterface{{PackagePath: "main", Name: "Namer", Methods: []string{""}}}, want: "invalid method name"},
		{name: "repeated method", entries: []program.NamedInterface{{PackagePath: "main", Name: "Namer", Methods: []string{"Name", "Name"}}}, want: "repeats method"},
		{name: "repeated interface", entries: []program.NamedInterface{
			{PackagePath: "main", Name: "Namer", Methods: []string{"Name"}},
			{PackagePath: "main", Name: "Namer", Methods: []string{"Other"}},
		}, want: "repeats main.Namer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := namedInterfaceEntriesPayload(tt.entries)
			interfaces, err := unpackNamedInterfaces(schemagen.GetRootAsCompiledFileSet(payload, 0), len(payload))
			require.ErrorIs(t, err, errCorruptBytecodePayload)
			require.ErrorContains(t, err, tt.want)
			require.Nil(t, interfaces)
		})
	}
}

func TestUnpackNamedInterfacesAcceptsValidEntries(t *testing.T) {
	t.Parallel()
	entries := []program.NamedInterface{
		{PackagePath: "main", Name: "Namer", Methods: []string{"Name", "Age"}},
		{PackagePath: "example.com/x/y", Name: "Empty", Methods: nil},
	}
	payload := namedInterfaceEntriesPayload(entries)
	interfaces, err := unpackNamedInterfaces(schemagen.GetRootAsCompiledFileSet(payload, 0), len(payload))
	require.NoError(t, err)
	require.Len(t, interfaces, 2)
	require.Equal(t, "Namer", interfaces[0].Name)
	require.Equal(t, []string{"Name", "Age"}, interfaces[0].Methods)
	require.Equal(t, "example.com/x/y", interfaces[1].PackagePath)
	require.Empty(t, interfaces[1].Methods)
}
