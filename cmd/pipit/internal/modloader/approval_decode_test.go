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
	"reflect"
	"strings"
	"testing"
)

func TestApprovalDecodeRejectsAmbiguousDocuments(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"", "null", "[]",
		`{"schema":2,"schema":2}`,
		`{"schema":2,"\u0073chema":2}`,
		`{"Schema":2}`,
		`{"schema":2,"unknown":true}`,
		`{"schema":2}{}`,
		`{"schema":2,"modules":[{"path":"first","path":"second"}]}`,
		`{"schema":2,"modules":[{"Path":"aliased"}]}`,
		`{"schema":2,"modules":[{"path":"module","extra":true}]}`,
		`{"schema":2,"modules":[{"path":"module"},{"path":"module"}]}`,
		`{"schema":2,"modules":[{"path":"module","approved_capabilities":{} }]}`,
		`{"schema":2,"modules":[null]}`,
		`{"schema":2,"script":null}`,
		`{"schema":2,"modules":[{"path":null}]}`,
		`{"schema":2,"modules":[{"path":"module","approved_capabilities":[null]}]}`,
		`{"schema":2,"script":"` + string([]byte{0xff}) + `"}`,
	} {
		decoded, err := decodeApprovalFile([]byte(input))
		if err == nil || !reflect.DeepEqual(decoded, Lockfile{}) {
			t.Fatalf("ambiguous document returned state: %q: %+v %v", input, decoded, err)
		}
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
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeApprovalFile(encoded)
		if (err != nil) != oversized {
			t.Fatalf("module boundary: %d: %v", count, err)
		}
		if !oversized && len(decoded.Modules) != count {
			t.Fatal("module entries lost")
		}
		if _, err := encodeApprovalSnapshot(Lockfile{Schema: LockfileSchema, Modules: modules}); (err != nil) != oversized {
			t.Fatalf("writer module boundary differs: %v", err)
		}
	}
	for _, count := range []int{maximumModuleApprovals, maximumModuleApprovals + 1} {
		input := `{"schema":2,"modules":[{"path":"module","approved_capabilities":[` +
			strings.TrimSuffix(strings.Repeat(`"cap",`, count), ",") + `]}]}`
		_, err := decodeApprovalFile([]byte(input))
		if (err != nil) != (count > maximumModuleApprovals) {
			t.Fatalf("capability boundary: %d: %v", count, err)
		}
	}
}

func TestApprovalDecodeFailureClearsLiveGrants(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	store := NewStore(path)
	store.Upsert(LockedModule{Path: "module", ApprovedCapabilities: []string{"network"}})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema":2,"schema":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err == nil {
		t.Fatal("ambiguous approval document loaded")
	}
	if _, exists := store.Lookup("module", ""); exists {
		t.Fatal("ambiguous reload retained a grant")
	}
}

func FuzzApprovalDecode(f *testing.F) {
	f.Add([]byte(`{"schema":2,"modules":[{"path":"module","approved_capabilities":["network"]}]}`))
	f.Add([]byte(`{"schema":1,"modules":[]}`))
	f.Add([]byte(`{"schema":2,"schema":1}`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := decodeApprovalFile(data)
		if err != nil && !reflect.DeepEqual(decoded, Lockfile{}) {
			t.Fatal("failed approval decode returned partial state")
		}
	})
}
