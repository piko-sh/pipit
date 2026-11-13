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
	"sync"
	"sync/atomic"
)

// vmShellMaxRootSnapshots bounds the root-snapshot table a parked shell keeps.
const vmShellMaxRootSnapshots = 1024

// vmShellPool holds zeroed VM structs ready for NewVM.
var vmShellPool sync.Pool

// vmShellProbe counts shell traffic while a test has installed it.
type vmShellProbe struct {
	// reused counts NewVM calls served from the pool.
	reused atomic.Int64

	// parked counts VMs zeroed and returned to the pool.
	parked atomic.Int64

	// refusedPinned counts VMs kept out of the pool because something outside the run holds
	// them.
	refusedPinned atomic.Int64

	// refusedState counts VMs excluded from the pool by an attached arena, debugger, safe
	// mode, leaked goroutines or unwinding, or a fired or active context watcher.
	refusedState atomic.Int64
}

// VMShellCounts reports the VM shell pool traffic a probe observed.
type VMShellCounts struct {
	// Reused counts VMs NewVM took from the pool.
	Reused int64

	// Parked counts VMs returned to the pool.
	Parked int64

	// RefusedPinned counts VMs not parked because their identity was pinned.
	RefusedPinned int64

	// RefusedState counts VMs not parked for any other reason.
	RefusedState int64
}

// installedVMShellProbe is the installed probe, or nil. Production code pays one atomic
// load per shell event.
var installedVMShellProbe atomic.Pointer[vmShellProbe]

// InstallVMShellProbe starts counting VM shell traffic, for tests.
//
// Returns func() VMShellCounts which reads the counts and removes the probe.
func InstallVMShellProbe() func() VMShellCounts {
	probe := new(vmShellProbe)
	installedVMShellProbe.Store(probe)
	return func() VMShellCounts {
		installedVMShellProbe.CompareAndSwap(probe, nil)
		return VMShellCounts{
			Reused:        probe.reused.Load(),
			Parked:        probe.parked.Load(),
			RefusedPinned: probe.refusedPinned.Load(),
			RefusedState:  probe.refusedState.Load(),
		}
	}
}

// newVMShell returns a zeroed VM, recycled from the pool when one is parked there.
//
// Returns *VM which is the zero VM.
func newVMShell() *VM {
	if shell, ok := vmShellPool.Get().(*VM); ok && shell != nil {
		if probe := installedVMShellProbe.Load(); probe != nil {
			probe.reused.Add(1)
		}
		return shell
	}
	return new(VM)
}

// pinIdentity records that an object which can outlive the current run now holds vm, so
// vm must never be recycled. It returns immediately when vm is nil.
func (vm *VM) pinIdentity() {
	if vm != nil && !vm.identityPinned.Load() {
		vm.identityPinned.Store(true)
	}
}

// recyclable reports whether vm can be zeroed and parked.
//
// Nothing outside the run may hold it, its arena and call stack must be gone, and no
// goroutine, watcher, debugger or unwinding may still access it.
//
// Returns bool which is true when vm may be parked.
func (vm *VM) recyclable() bool {
	return vm.Arena == nil && vm.CallStack == nil && vm.FramePointer == -1 &&
		vm.Limits.Debug == nil && !vm.Limits.SafeMode && vm.debugThread == nil &&
		vm.pendingGilHandoff == nil && !vm.holdsInterpreterLock &&
		vm.stopWatcher == nil && !vm.watcherFired &&
		!vm.goroutinesLeaked && !vm.unwindLeaked.Load() && !vm.abortUnwinding &&
		vm.unwindNative.Load() == 0 &&
		(vm.callbackVMs == nil || vm.callbackVMs.slot.Load() == nil) && vm.boundArena.Load() == nil
}

// recycle zeroes vm and parks it for NewVM, unless it is pinned or not recyclable. The
// caller must own vm exclusively and must not touch it afterwards.
//
// The VM's own callback cache is kept and zeroed in place. Only newCallbackVMSpec hands
// the cache out, and it pins the VM.
func (vm *VM) recycle() {
	if vm.identityPinned.Load() {
		if probe := installedVMShellProbe.Load(); probe != nil {
			probe.refusedPinned.Add(1)
		}
		return
	}
	if !vm.recyclable() {
		if probe := installedVMShellProbe.Load(); probe != nil {
			probe.refusedState.Add(1)
		}
		return
	}
	vm.resetShell()
	if probe := installedVMShellProbe.Load(); probe != nil {
		probe.parked.Add(1)
	}
	if vmShellAuditEnabled {
		auditRecordVMShell(vm)
		return
	}
	vmShellPool.Put(vm)
}

// resetShell clears every VM field while retaining its own callback cache and up to
// vmShellMaxRootSnapshots entries of root-snapshot backing storage.
//
// Both retained allocations are emptied. Only pinning sites hand out the callback cache.
func (vm *VM) resetShell() {
	cache, restores := vm.callbackVMs, vm.argumentRestores
	var snapshots []*frameRootSnapshot
	if cap(vm.rootSnapshots) <= vmShellMaxRootSnapshots {
		snapshots = vm.rootSnapshots[:cap(vm.rootSnapshots)]
		clear(snapshots)
	}
	*vm = VM{}
	if cache != nil {
		*cache = callbackVMCache{}
		vm.callbackVMs = cache
	}
	vm.rootSnapshots = snapshots[:0]
	if restores != nil {
		restores.nativeCopy.reset()
		restores.detach.reset()
		vm.argumentRestores = restores
	}
}

// Recycle zeroes the VM and parks it for a later NewVM when nothing outside its execution
// holds it and it owns nothing any more (see recyclable); otherwise it leaves the VM to
// the collector.
func (vm *VM) Recycle() {
	vm.recycle()
}
