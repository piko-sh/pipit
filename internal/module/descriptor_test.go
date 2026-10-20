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
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptorValidate(t *testing.T) {
	t.Parallel()

	good := &Descriptor{
		SchemaVersion: DescriptorVersion,
		Ref:           Ref{Path: "example.com/mod", Version: "v1"},
		Capabilities:  CapabilitySet{{Axis: "network"}},
		Entrypoints:   map[string]string{"init": "PkgInit"},
	}
	require.NoError(t, good.Validate(), "a well-formed descriptor validates")
	var nilDescriptor *Descriptor
	require.Error(t, nilDescriptor.Validate(), "a nil descriptor is invalid")

	tests := []struct {
		name    string
		mutate  func(*Descriptor)
		wantMsg string
	}{
		{name: "missing schema version", mutate: func(d *Descriptor) { d.SchemaVersion = 0 }, wantMsg: "schema_version"},
		{name: "future schema version", mutate: func(d *Descriptor) { d.SchemaVersion = DescriptorVersion + 1 }, wantMsg: "newer than supported"},
		{name: "missing path", mutate: func(d *Descriptor) { d.Ref.Path = "" }, wantMsg: "reference.path"},
		{name: "empty capability axis", mutate: func(d *Descriptor) {
			d.Capabilities = CapabilitySet{{Axis: ""}}
		}, wantMsg: "empty axis"},
		{name: "empty entrypoint key", mutate: func(d *Descriptor) {
			d.Entrypoints = map[string]string{"": "x"}
		}, wantMsg: "empty key"},
		{name: "empty entrypoint value", mutate: func(d *Descriptor) {
			d.Entrypoints = map[string]string{"k": ""}
		}, wantMsg: "empty function name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := *good
			d.Capabilities = append(CapabilitySet(nil), good.Capabilities...)
			d.Entrypoints = maps.Clone(good.Entrypoints)
			tt.mutate(&d)
			err := d.Validate()
			require.ErrorContains(t, err, tt.wantMsg)
		})
	}
}

func TestDescriptorContentDigestIsStable(t *testing.T) {
	t.Parallel()
	a := &Descriptor{
		SchemaVersion: DescriptorVersion,
		Ref:           Ref{Path: "example.com/mod", Version: "v1"},
		Capabilities: CapabilitySet{
			{Axis: "network"},
			{Axis: "filesystem.read", Scope: "/etc/*"},
		},
		SymbolAllowlist: []string{"Zeta", "Alpha"},
	}
	b := &Descriptor{
		SchemaVersion: DescriptorVersion,
		Ref:           Ref{Path: "example.com/mod", Version: "v1"},
		Capabilities: CapabilitySet{
			{Axis: "filesystem.read", Scope: "/etc/*"},
			{Axis: "network"},
		},
		SymbolAllowlist: []string{"Alpha", "Zeta"},
	}
	hashA, err := a.ContentDigest()
	require.NoError(t, err)
	hashB, err := b.ContentDigest()
	require.NoError(t, err)
	require.Equal(t, hashB, hashA, "equivalent descriptors share a digest")
}

func TestUnmarshalDescriptorRoundTrip(t *testing.T) {
	t.Parallel()
	original := &Descriptor{
		SchemaVersion: DescriptorVersion,
		Ref:           Ref{Path: "example.com/mod", Version: "v1.2.3"},
		Capabilities:  CapabilitySet{{Axis: "network"}},
		StdlibVersion: "1.0.0",
		Entrypoints:   map[string]string{"init": "PkgInit"},
	}
	data, err := original.MarshalCanonicalJSON()
	require.NoError(t, err)
	decoded, err := UnmarshalDescriptor(data)
	require.NoError(t, err)
	assert.Equal(t, original.Ref, decoded.Ref)
	assert.Equal(t, original.StdlibVersion, decoded.StdlibVersion)
	if assert.Len(t, decoded.Capabilities, 1) {
		assert.Equal(t, "network", decoded.Capabilities[0].Axis)
	}
}

func TestUnmarshalDescriptorRejectsBadSchema(t *testing.T) {
	t.Parallel()
	bad := `{"schema_version": 0, "reference": {"path": "x"}}`
	_, err := UnmarshalDescriptor([]byte(bad))
	require.Error(t, err, "a malformed descriptor is rejected")
	_, err = UnmarshalDescriptor([]byte("not json"))
	require.Error(t, err, "input that is not JSON is rejected")
}
