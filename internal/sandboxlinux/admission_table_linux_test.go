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
	"strconv"
	"strings"
	"testing"
)

func TestAdmissionTableTenantsAreIndependent(t *testing.T) {
	t.Parallel()

	var table admissionTable
	releaseA, err := table.acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	releaseB, err := table.acquire(context.Background(), "b")
	if err != nil {
		t.Fatalf("second tenant blocked by the first: %v", err)
	}
	if table.tracked() != 2 {
		t.Fatalf("tracked %d gates, want 2", table.tracked())
	}
	releaseA()
	releaseB()
	if table.tracked() != 0 {
		t.Fatalf("idle gates retained: %d", table.tracked())
	}
}

func TestAdmissionTableBound(t *testing.T) {
	t.Parallel()

	var table admissionTable
	releases := make([]func(), 0, maximumAdmissionKeys)
	for i := range maximumAdmissionKeys {
		release, err := table.acquire(context.Background(), "tenant-"+strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if _, err := table.acquire(context.Background(), "one-too-many"); !errors.Is(err, errAdmissionFull) {
		t.Fatalf("full table admitted a new tenant: %v", err)
	}
	releases[0]()
	release, err := table.acquire(context.Background(), "one-too-many")
	if err != nil {
		t.Fatalf("freed slot not reusable: %v", err)
	}
	release()
	for _, release := range releases[1:] {
		release()
	}
	if table.tracked() != 0 {
		t.Fatalf("idle gates retained: %d", table.tracked())
	}
}

func TestAdmissionTableSecondSlotPerTenant(t *testing.T) {
	t.Parallel()

	var table admissionTable
	release, err := table.acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() {
		lease, err := table.acquire(ctx, "a")
		if lease != nil {
			lease()
		}
		queued <- err
	}()
	waitAdmissionPending(t, table.gate("a"))
	if _, err := table.acquire(context.Background(), "a"); !errors.Is(err, errAdmissionFull) {
		t.Fatalf("third request for one tenant admitted: %v", err)
	}
	release()
	if err := <-queued; err != nil {
		t.Fatalf("queued request failed: %v", err)
	}
	cancel()
	if table.tracked() != 0 {
		t.Fatalf("idle gates retained: %d", table.tracked())
	}
}

func TestServiceGroupNameFor(t *testing.T) {
	t.Parallel()

	name := serviceGroupNameFor("tenant")
	if !isServiceGroupName(name) || !strings.HasPrefix(name, serviceGroupPrefix) {
		t.Fatalf("derived name %q has the wrong shape", name)
	}
	if name == serviceGroupNameFor("tenant-2") {
		t.Fatal("two tenants share a service group name")
	}
	if name != serviceGroupNameFor("tenant") {
		t.Fatal("a tenant's service group name is not stable")
	}
	for _, bad := range []string{"pipit-service", "pipit-service-", name + "0", "pipit-service-" + strings.Repeat("g", serviceGroupDigestLength)} {
		if isServiceGroupName(bad) {
			t.Fatalf("%q accepted as a service group name", bad)
		}
	}
	for _, tenant := range []string{"", strings.Repeat("a", maximumTenantBytes+1), "bad\x00tenant", string([]byte{0xff})} {
		if err := validateTenant(tenant); !errors.Is(err, errInvalidTenant) {
			t.Fatalf("tenant %q accepted: %v", tenant, err)
		}
	}
}
