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

//go:build linux

package modloader

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInvocationExecutableHash(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "executable")
	data := []byte("approved executable bytes")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	actual, err := hashInvocationExecutable(file)
	expected := fmt.Sprintf("linux-executable-sha256:%x", sha256.Sum256(data))
	if err != nil || actual != expected {
		t.Fatalf("wrong executable identity: %q %v", actual, err)
	}
}

func TestInvocationExecutableRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	for _, size := range []int64{0, maximumApprovalExecutableBytes + 1} {
		file, err := os.CreateTemp(t.TempDir(), "executable")
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(size); err != nil {
			t.Fatal(err)
		}
		identity, err := hashInvocationExecutable(file)
		if err == nil || identity != "" {
			t.Fatalf("accepted invalid executable size %d", size)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		identity, err = hashInvocationExecutable(file)
		if err == nil || identity != "" {
			t.Fatal("accepted closed executable")
		}
	}
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if identity, err := hashInvocationExecutable(directory); err == nil || identity != "" {
		t.Fatal("accepted directory as executable")
	}
}

func TestInvocationExecutableUsesRunningImage(t *testing.T) {
	t.Parallel()
	trailers := []string{"", "changed executable bytes"}
	identities := make([]string, 0, len(trailers))
	for _, trailer := range trailers {
		path := filepath.Join(t.TempDir(), "approval-test")
		identity := copyApprovalTestExecutable(t, path, trailer)
		identities = append(identities, identity)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		command := exec.CommandContext(ctx, path, "-test.run=^TestInvocationExecutableChild$")
		command.Env = append(os.Environ(), "PIPIT_APPROVAL_EXECUTABLE_CHILD="+identity)
		output, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("running-image measurement failed: %v\n%s", err, output)
		}
	}
	if identities[0] == identities[1] ||
		invocationHash("source", nil, "main", nil, identities[:1]) ==
			invocationHash("source", nil, "main", nil, identities[1:]) {
		t.Fatal("changed executable retained approval identity")
	}
}

func TestInvocationExecutableChild(t *testing.T) {
	expected := os.Getenv("PIPIT_APPROVAL_EXECUTABLE_CHILD")
	if expected == "" {
		t.Skip("subprocess helper")
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".running"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	actual, err := invocationExecutable()
	if err != nil || actual != expected {
		t.Fatalf("measurement followed replaced pathname: got=%q expected=%q error=%v", actual, expected, err)
	}
	again, err := invocationExecutable()
	if err != nil || again != actual {
		t.Fatalf("cached measurement changed: %v", err)
	}
}

func copyApprovalTestExecutable(t *testing.T, path, trailer string) string {
	t.Helper()
	source, err := os.Open("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if _, err := io.Copy(target, source); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(target, trailer); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	identity, err := hashInvocationExecutable(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	return identity
}
