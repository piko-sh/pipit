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

package sandboxlinux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceAdmissionRejectsOrdinaryDirectory(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	group, err := newServiceGroup(parent, Limits{}, testTenant)
	if group != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("accepted non-cgroup parent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, serviceGroupNameFor(testTenant))); !os.IsNotExist(err) {
		t.Fatalf("created reservation before validating cgroup parent: %v", err)
	}
}

func TestNativeServiceAdmission(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	group, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer group.closeGroup(ctx)
	runServiceAdmissionChild(t, ctx, "busy")
	if err := group.closeGroup(ctx); err != nil {
		t.Fatal(err)
	}
	runServiceAdmissionChild(t, ctx, "release")
	runServiceAdmissionChild(t, ctx, "abandon")
	path := filepath.Join(parent, serviceGroupNameFor(testTenant))
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	})
	runServiceAdmissionChild(t, ctx, "busy")
	if owner, err := newServiceGroup(parent, Limits{}, testTenant); owner != nil || !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("adopted an abandoned reservation: %v", err)
	}
}

func TestNativeServiceAdmissionChild(t *testing.T) {
	mode := os.Getenv("PIPIT_SERVICE_ADMISSION_CHILD")
	if mode == "" {
		return
	}
	group, err := newServiceGroup(os.Getenv("PIPIT_TEST_CGROUP_PARENT"), Limits{}, testTenant)
	if mode == "busy" {
		if group != nil || !errors.Is(err, ErrServiceBusy) {
			t.Fatalf("occupied slot admitted another host: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if mode == "abandon" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := group.closeGroup(ctx); err != nil {
		t.Fatal(err)
	}
}

func runServiceAdmissionChild(t *testing.T, ctx context.Context, mode string) {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeServiceAdmissionChild$")
	command.Env = append(os.Environ(), "PIPIT_SERVICE_ADMISSION_CHILD="+mode)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("service admission child %s: %v\n%s", mode, err, output)
	}
}

func TestNativeServiceGroupsPerTenant(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := newServiceGroup(parent, Limits{}, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.closeGroup(ctx) }()
	second, err := newServiceGroup(parent, Limits{}, "tenant-b")
	if err != nil {
		t.Fatalf("second tenant blocked by the first tenant's reservation: %v", err)
	}
	defer func() { _ = second.closeGroup(ctx) }()
	if duplicate, err := newServiceGroup(parent, Limits{}, "tenant-a"); duplicate != nil || !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("a tenant's reservation admitted a second owner: %v", err)
	}
	if err := second.closeGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, serviceGroupNameFor("tenant-b"))); !os.IsNotExist(err) {
		t.Fatalf("closed tenant reservation remained: %v", err)
	}
}
