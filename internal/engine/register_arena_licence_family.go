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

package engine

import (
	"runtime"
	"sync"
	"sync/atomic"
	"weak"
)

// familyExports counts the VMs of one family (the VMs sharing a GlobalStore) that
// exported guest code, by the execution each belongs to (its ResourceTracker).
//
// An exported wrapper may run on any goroutine at any time and write the family's
// globals. It holds its own execution's gates while it runs, which exclude only that
// execution's owner sections. A VM of another execution has no gate to take, so while an
// exporter of another execution may be live its walks and collections, which read the
// globals, run shared (familySharesGlobals). That is conservative. A VM counts until the
// collector reclaims it, and its wrappers keep it reachable, so the count covers every
// live wrapper and errs only by counting a VM after its last wrapper has gone.
type familyExports struct {
	// byTracker counts the exporting VMs of each execution.
	byTracker map[*ResourceTracker]int

	// mu guards byTracker.
	mu sync.RWMutex
}

// familyExporter is what the cleanup of an exporting VM needs to count it out.
type familyExporter struct {
	// family is the VM's family.
	family *familyExports

	// tracker is the VM's execution.
	tracker *ResourceTracker
}

var (
	// familyExportersLive counts the exporting VMs every family counts, so that a process
	// whose code has exported nothing pays one atomic load per sharing test.
	familyExportersLive atomic.Int64

	// familyExportStates maps a family's GlobalStore, held weakly, to its familyExports.
	familyExportStates sync.Map
)

// add counts an exporting VM of the execution tracker.
//
// Takes tracker (*ResourceTracker) which is the VM's execution.
//
// Safe for concurrent use. The family mutex guards the exporting VM counts.
func (family *familyExports) add(tracker *ResourceTracker) {
	family.mu.Lock()
	family.byTracker[tracker]++
	family.mu.Unlock()
}

// remove counts an exporting VM of the execution tracker out.
//
// Takes tracker (*ResourceTracker) which is the VM's execution.
//
// Safe for concurrent use. The family mutex guards the exporting VM counts.
func (family *familyExports) remove(tracker *ResourceTracker) {
	family.mu.Lock()
	if family.byTracker[tracker]--; family.byTracker[tracker] <= 0 {
		delete(family.byTracker, tracker)
	}
	family.mu.Unlock()
}

// exportsOutside reports whether a VM of an execution other than tracker exported guest
// code that may still run.
//
// Takes tracker (*ResourceTracker) which is the reading VM's execution.
//
// Returns bool which is true when one did.
//
// Safe for concurrent use. The family mutex guards the exporting VM counts.
func (family *familyExports) exportsOutside(tracker *ResourceTracker) bool {
	family.mu.RLock()
	defer family.mu.RUnlock()
	for exporter := range family.byTracker {
		if exporter != tracker {
			return true
		}
	}
	return false
}

// noteFamilyExporter counts the VM in its family's exporters once, until the collector
// reclaims it. It runs on the VM's goroutine, or one the VM is parked below.
func (vm *VM) noteFamilyExporter() {
	if vm.familyExporterCounted || vm.Globals == nil {
		return
	}
	vm.familyExporterCounted = true
	family := familyExportsOf(vm.Globals)
	tracker := vm.Limits.Tracker
	family.add(tracker)
	familyExportersLive.Add(1)
	runtime.AddCleanup(vm, releaseFamilyExporter, familyExporter{family: family, tracker: tracker})
}

// refreshFamilyShared sets licenceFamilyShared for a licensed arena from its VM's family
// (familySharesGlobals).
func (a *RegisterArena) refreshFamilyShared() {
	a.licenceFamilyShared = false
	if !a.licensed || familyExportersLive.Load() == 0 {
		return
	}
	if vm := a.licenceSubjectVM(); vm != nil {
		a.licenceFamilyShared = vm.familySharesGlobals()
	}
}

// familySharesGlobals reports whether a VM of another execution of the VM's family
// exported guest code that may write the family's globals while the VM reads them.
//
// Returns bool which is true when the VM must read shared.
func (vm *VM) familySharesGlobals() bool {
	if vm.Globals == nil {
		return false
	}
	raw, found := familyExportStates.Load(weak.Make(vm.Globals))
	if !found {
		return false
	}
	family, typed := raw.(*familyExports)
	return !typed || family.exportsOutside(vm.Limits.Tracker)
}

// familyExportsOf returns the familyExports of globals, making it on first use.
//
// Takes globals (*GlobalStore) which identifies the family.
//
// Returns *familyExports which is never nil.
func familyExportsOf(globals *GlobalStore) *familyExports {
	key := weak.Make(globals)
	if raw, found := familyExportStates.Load(key); found {
		if family, typed := raw.(*familyExports); typed {
			return family
		}
	}
	fresh := &familyExports{byTracker: map[*ResourceTracker]int{}, mu: sync.RWMutex{}}
	raw, loaded := familyExportStates.LoadOrStore(key, fresh)
	if !loaded {
		runtime.AddCleanup(globals, forgetFamilyExports, key)
		return fresh
	}
	if family, typed := raw.(*familyExports); typed {
		return family
	}
	familyExportStates.Store(key, fresh)
	return fresh
}

// releaseFamilyExporter counts a reclaimed exporting VM out, as its cleanup.
//
// Takes exporter (familyExporter) which names its family and execution.
func releaseFamilyExporter(exporter familyExporter) {
	exporter.family.remove(exporter.tracker)
	familyExportersLive.Add(-1)
}

// forgetFamilyExports drops the entry of a reclaimed GlobalStore, as its cleanup.
//
// Takes key (weak.Pointer[GlobalStore]) which is the entry's key.
func forgetFamilyExports(key weak.Pointer[GlobalStore]) {
	familyExportStates.Delete(key)
}
