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

//go:build !safe && !(js && wasm)

package engine

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

const hazardPoisonPair = "\xdd\xdd"

type hazardObservations struct {
	fired   []string
	details []string
}

func (o *hazardObservations) fire(observable string, format string, arguments ...any) {
	o.fired = append(o.fired, observable)
	o.details = append(o.details, observable+": "+fmt.Sprintf(format, arguments...))
}

func (o *hazardObservations) note(format string, arguments ...any) {
	o.details = append(o.details, fmt.Sprintf(format, arguments...))
}

func (o *hazardObservations) compareString(label, want, got string) {
	switch {
	case strings.Contains(got, hazardPoisonPair):
		o.fire("poison-bytes", "%s read %q, want %q", label, got, want)
	case got != want:
		o.fire("value-changed", "%s read %q, want %q", label, got, want)
	default:
		o.note("%s unchanged", label)
	}
}

func assertNoHazard(t *testing.T, observations *hazardObservations, inconclusiveReason string) {
	t.Helper()
	detail := strings.Join(observations.details, "; ")
	switch {
	case len(observations.fired) > 0:
		seen := map[string]bool{}
		var unique []string
		for _, name := range observations.fired {
			if !seen[name] {
				seen[name] = true
				unique = append(unique, name)
			}
		}
		require.Failf(t, "failure observed", "seen by %s: %s", strings.Join(unique, ", "), detail)
	case inconclusiveReason != "":
		t.Skipf("precondition not reached, so the probe tested nothing: %s; %s", inconclusiveReason, detail)
	default:
		t.Logf("no failure observed: %s", detail)
	}
}

func slabBase[T any](slab []T) uintptr {
	if cap(slab) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
}

func foreignPending(base uintptr) bool {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	for key := range licenceForeignRevoked.all() {
		if key.start == base {
			return true
		}
	}
	return false
}

func foreignPendingFor(owner uint64, base uintptr) bool {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	_, found := licenceForeignRevoked.lookup(licenceForeignKey{start: base, owner: owner})
	return found
}

func injectForeignPending(owner uint64, base uintptr) {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	key := licenceForeignKey{start: base, owner: owner}
	if _, found := licenceForeignRevoked.lookup(key); !found {
		licenceForeignRevoked.record(key, handOffNativeArgUnknown)
		licenceForeignRevokedCount.Add(1)
	}
}

func dropForeignPending(bases ...uintptr) {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	for key := range licenceForeignRevoked.all() {
		if slices.Contains(bases, key.start) && licenceForeignRevoked.forget(key) {
			licenceForeignRevokedCount.Add(-1)
		}
	}
}

func relicence(a *RegisterArena) {
	a.attachLicencePolicy(&VMLimits{})
}

func overwriteStrings(a *RegisterArena, total int) {
	for written := 0; written < total; written += 16 {
		arenaString(a, "XXXXXXXXXXXXXXXX")
	}
}

func callerBankPointer(t *testing.T, caller *RegisterArena) *int64 {
	t.Helper()
	require.NotZero(t, cap(caller.intSlab), "test harness: the caller needs an int register bank")
	slot := &caller.intSlab[:1][0]
	*slot = 424242
	require.True(t, caller.registerBanksHold(uintptr(unsafe.Pointer(slot))), "test harness: the pointer must address a register bank")
	return slot
}

func reuseCallerBank(caller *RegisterArena, slot *int64, observations *hazardObservations) {
	bank := slabBase(caller.intSlab)
	caller.Reset()
	relicence(caller)
	reused := slabBase(caller.intSlab) == bank
	if reused {
		caller.intSlab[:1][0] = -1
	}
	observations.note("bank reused by the next epoch=%v", reused)
	observations.compareString("host pointer into the caller's register bank", "424242", fmt.Sprint(*slot))
	runtime.KeepAlive(slot)
}
