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
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceSnapshotFreezesApprovedInputs(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target := filepath.Join(directory, "main.go")
	original := "package main; func main() {}"
	metadata := "module example.test/main\nrequire example.test/helper v1.2.3\n"
	for name, data := range map[string]string{"main.go": original, "go.mod": metadata} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	axes := []string{"filesystem.read"}
	path, identity, err := InvocationIdentity(target, axes, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureInvocation(target, axes, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Path() != path || snapshot.Identity() != identity || snapshot.IsDirectory() {
		t.Fatal("captured source diverged from the approval identity")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	captured, err := snapshot.ReadFile(target)
	if err != nil || string(captured) != original {
		t.Fatalf("source was reopened: %q %v", captured, err)
	}
	captured[0] = 'X'
	again, err := snapshot.ReadFile(target)
	if err != nil || string(again) != original {
		t.Fatal("returned byte slice mutated snapshot")
	}
	module, err := snapshot.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil || string(module) != metadata {
		t.Fatal("module metadata was reopened")
	}
	if versions := invocationVersions(directory, snapshot); versions["example.test/helper"] != "v1.2.3" {
		t.Fatal("module resolution did not use approved version metadata")
	}
	resolution, err := ResolveScript(context.Background(), target, ScriptResolverOptions{Snapshot: snapshot})
	if err != nil || resolution.MainSources[target] != original {
		t.Fatalf("resolver did not use captured source: resolution=%+v error=%v", resolution, err)
	}
	if _, err := snapshot.ReadFile(filepath.Join(directory, "..", "outside.go")); err == nil {
		t.Fatal("snapshot allowed source outside its captured root")
	}
}

func TestSourceSnapshotDirectoryReturnsIndependentMap(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, name := range []string{"main.go", "helper.go", "helper_test.go", "child/nested.go"} {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package main"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := CaptureInvocation(directory, nil, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.IsDirectory() {
		t.Fatal("directory kind was not retained")
	}
	sources := snapshot.DirectorySources()
	if len(sources) != 2 || sources["helper.go"] != "package main" {
		t.Fatalf("wrong package selection: %v", sources)
	}
	sources["main.go"] = "changed"
	delete(sources, "helper.go")
	if again := snapshot.DirectorySources(); len(again) != 2 || again["main.go"] != "package main" {
		t.Fatal("returned map mutated the captured package")
	}
}
