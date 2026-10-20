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

package modloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApprovalDecodeRejectsAmbiguousDocuments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "null", input: "null"},
		{name: "array", input: "[]"},
		{name: "duplicate schema", input: `{"schema":2,"schema":2}`},
		{name: "duplicate schema through an escape", input: `{"schema":2,"schema":2}`},
		{name: "field name in another case", input: `{"Schema":2}`},
		{name: "unknown field", input: `{"schema":2,"unknown":true}`},
		{name: "trailing value", input: `{"schema":2}{}`},
		{name: "duplicate module field", input: `{"schema":2,"modules":[{"path":"first","path":"second"}]}`},
		{name: "module field name in another case", input: `{"schema":2,"modules":[{"Path":"aliased"}]}`},
		{name: "unknown module field", input: `{"schema":2,"modules":[{"path":"module","extra":true}]}`},
		{name: "duplicate module", input: `{"schema":2,"modules":[{"path":"module"},{"path":"module"}]}`},
		{name: "capabilities as an object", input: `{"schema":2,"modules":[{"path":"module","approved_capabilities":{} }]}`},
		{name: "null module", input: `{"schema":2,"modules":[null]}`},
		{name: "null script", input: `{"schema":2,"script":null}`},
		{name: "null module path", input: `{"schema":2,"modules":[{"path":null}]}`},
		{name: "null capability", input: `{"schema":2,"modules":[{"path":"module","approved_capabilities":[null]}]}`},
		{name: "invalid UTF-8 script", input: `{"schema":2,"script":"` + string([]byte{0xff}) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decoded, err := decodeApprovalFile([]byte(tt.input))
			require.Error(t, err, "an ambiguous document is rejected")
			require.Equal(t, Lockfile{}, decoded, "a rejected document returns no state")
		})
	}
}

func TestApprovalDecodeCollectionBoundaries(t *testing.T) {
	t.Parallel()
	for _, oversized := range []bool{false, true} {
		count := maximumApprovalModules
		if oversized {
			count++
		}
		modules := make([]LockedModule, count)
		for index := range modules {
			modules[index].Path = fmt.Sprintf("module-%d", index)
		}
		encoded, err := json.Marshal(Lockfile{Schema: LockfileSchema, Modules: modules})
		require.NoError(t, err)
		decoded, err := decodeApprovalFile(encoded)
		require.Equal(t, oversized, err != nil, "module boundary %d: %v", count, err)
		if !oversized {
			require.Len(t, decoded.Modules, count, "no module entry is lost")
		}
		_, err = encodeApprovalSnapshot(Lockfile{Schema: LockfileSchema, Modules: modules})
		require.Equal(t, oversized, err != nil, "the writer applies the same module boundary: %v", err)
	}
	for _, count := range []int{maximumModuleApprovals, maximumModuleApprovals + 1} {
		input := `{"schema":2,"modules":[{"path":"module","approved_capabilities":[` +
			strings.TrimSuffix(strings.Repeat(`"cap",`, count), ",") + `]}]}`
		_, err := decodeApprovalFile([]byte(input))
		require.Equal(t, count > maximumModuleApprovals, err != nil, "capability boundary %d: %v", count, err)
	}
}

func TestApprovalDecodeFailureClearsLiveGrants(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	store := NewStore(path)
	store.Upsert(LockedModule{Path: "module", ApprovedCapabilities: []string{"network"}})
	require.NoError(t, store.Save())
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":2,"schema":2}`), 0o600))
	require.Error(t, store.Load(), "an ambiguous approval document is refused")
	_, exists := store.Lookup("module", "")
	require.False(t, exists, "an ambiguous reload drops grants")
}

func FuzzApprovalDecode(f *testing.F) {
	f.Add([]byte(`{"schema":2,"modules":[{"path":"module","approved_capabilities":["network"]}]}`))
	f.Add([]byte(`{"schema":1,"modules":[]}`))
	f.Add([]byte(`{"schema":2,"schema":1}`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := decodeApprovalFile(data)
		if err != nil {
			require.Equal(t, Lockfile{}, decoded, "a failed decode returns no partial state")
		}
	})
}
