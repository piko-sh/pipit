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

package module

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeTypesExport_EmptyReturnsNil(t *testing.T) {
	t.Parallel()
	out, err := EncodeTypesExport(nil)
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestDecodeTypesExport_EmptyReturnsNil(t *testing.T) {
	t.Parallel()
	entries, err := DecodeTypesExport(nil)
	require.NoError(t, err)
	require.Nil(t, entries)
}

func TestTypesExport_RoundTrip(t *testing.T) {
	t.Parallel()
	in := []TypesExportEntry{
		{ImportPath: "github.com/foo/bar", Data: []byte{0x01, 0x02, 0x03}},
		{ImportPath: "github.com/foo/bar/sub", Data: []byte("hello world")},
	}
	encoded, err := EncodeTypesExport(in)
	require.NoError(t, err)
	decoded, err := DecodeTypesExport(encoded)
	require.NoError(t, err)
	require.Len(t, decoded, len(in))
	for i, entry := range decoded {
		require.Equal(t, in[i].ImportPath, entry.ImportPath, "entry %d import path", i)
		require.Equal(t, in[i].Data, entry.Data, "entry %d data", i)
	}
}

func TestEncodeTypesExport_DeterministicOrder(t *testing.T) {
	t.Parallel()
	a := []TypesExportEntry{
		{ImportPath: "z/last", Data: []byte("a")},
		{ImportPath: "a/first", Data: []byte("b")},
		{ImportPath: "m/middle", Data: []byte("c")},
	}
	b := []TypesExportEntry{
		{ImportPath: "m/middle", Data: []byte("c")},
		{ImportPath: "z/last", Data: []byte("a")},
		{ImportPath: "a/first", Data: []byte("b")},
	}
	encA, err := EncodeTypesExport(a)
	require.NoError(t, err)
	encB, err := EncodeTypesExport(b)
	require.NoError(t, err)
	require.Equal(t, encA, encB, "the encoding does not depend on the input order")
}

func TestDecodeTypesExport_TruncatedErrors(t *testing.T) {
	t.Parallel()
	in := []TypesExportEntry{{ImportPath: "p", Data: []byte{0x42}}}
	encoded, err := EncodeTypesExport(in)
	require.NoError(t, err)

	for n := 1; n < len(encoded); n++ {
		_, err := DecodeTypesExport(encoded[:n])
		require.Error(t, err, "a %d-byte prefix is rejected", n)
	}
}

func TestBundleFingerprint_IncludesTypesExport(t *testing.T) {
	t.Parallel()
	descriptor := &Descriptor{
		SchemaVersion: 1,
		Ref:           Ref{Path: "example.com/m"},
	}
	a := &Bundle{Descriptor: descriptor, Bytecode: []byte("bytecode"), TypesExport: []byte("export-A")}
	b := &Bundle{Descriptor: descriptor, Bytecode: []byte("bytecode"), TypesExport: []byte("export-B")}
	fa, err := a.Fingerprint()
	require.NoError(t, err)
	fb, err := b.Fingerprint()
	require.NoError(t, err)
	require.NotEqual(t, fb, fa, "a different types export changes the fingerprint")
}
