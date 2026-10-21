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

package app

import (
	"runtime"
	"sync"
	"sync/atomic"
	"weak"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
)

// varInitReuseRegistry is a service's global reuse state.
type varInitReuseRegistry struct {
	// states holds each analysed file set's state, keyed weakly so the file set can be
	// collected; a cleanup on the file set removes its entry. Guarded by mu.
	states map[weak.Pointer[program.CompiledFileSet]]*varInitReuseState

	// lastFallback says why the last eligible file set's initialisers were not skipped.
	lastFallback string

	// mu guards states and lastFallback.
	mu sync.Mutex

	// inFlight counts the executions of the service running now.
	inFlight atomic.Int32

	// skips counts skipped initialiser runs.
	skips atomic.Int64

	// reruns counts initialiser runs of an eligible file set that were not skipped.
	reruns atomic.Int64

	// ineligible counts initialiser runs of a file set the analysis did not admit.
	ineligible atomic.Int64
}

// varInitReuseState is one file set's global reuse state.
type varInitReuseState struct {
	// image is the global slots at the end of the last finished initialiser run, or nil.
	image *engine.GlobalSlotImage

	// watch watches the storage the globals reached then, or nil.
	watch *engine.GlobalReuseWatch

	// plan is the compile-time analysis.
	plan compile.VarInitReusePlan

	// misses counts the consecutive runs whose initialisers had to re-run although an image
	// existed.
	misses int

	// cooldown counts the initialiser runs left before the next image is taken, so a program
	// that writes its globals on every run stops paying for images and watches.
	cooldown int
}

// fileSetRecordOwner names what a service records about one file set, for the cleanup
// that drops the record once the file set is collected. Both pointers are weak, so the
// cleanup keeps neither alive.
type fileSetRecordOwner struct {
	// service is the service holding the record.
	service weak.Pointer[Service]

	// fileSet is the record's key.
	fileSet weak.Pointer[program.CompiledFileSet]
}

// varInitReuseMaxCooldown bounds the backoff after repeated misses, in initialiser runs.
const varInitReuseMaxCooldown = 64

// varInitReuseMissesBeforeCooldown is the number of consecutive misses after which the
// skip decision backs off.
const varInitReuseMissesBeforeCooldown = 2

// varInitReuseMaxCooldownShift caps the exponent of the doubling backoff.
const varInitReuseMaxCooldownShift = 6

// VarInitReuseStats counts what global reuse did on a service.
type VarInitReuseStats struct {
	// LastFallback says why the last eligible file set's initialisers were re-run.
	LastFallback string

	// Skips counts initialiser runs skipped.
	Skips int64

	// Reruns counts initialiser runs of eligible file sets that were not skipped.
	Reruns int64

	// Ineligible counts initialiser runs of file sets the analysis did not admit.
	Ineligible int64
}

// VarInitReuseStatistics returns the service's global reuse counters.
//
// Returns VarInitReuseStats which is a snapshot.
//
// Concurrency: Protects the fallback reason with the reuse registry's mutex and reads
// counters atomically; the counters need not represent one instant.
func (s *Service) VarInitReuseStatistics() VarInitReuseStats {
	r := &s.varInitReuse
	r.mu.Lock()
	defer r.mu.Unlock()
	return VarInitReuseStats{LastFallback: r.lastFallback, Skips: r.skips.Load(), Reruns: r.reruns.Load(), Ineligible: r.ineligible.Load()}
}

// VarInitReusePlanFor returns the compile-time global reuse analysis of cfs on this
// service.
//
// Takes cfs (*program.CompiledFileSet) which was compiled by the service.
//
// Returns compile.VarInitReusePlan which is the zero plan when none was recorded.
//
// Concurrency: Reads the plan while holding the service's reuse-registry mutex.
func (s *Service) VarInitReusePlanFor(cfs *program.CompiledFileSet) compile.VarInitReusePlan {
	registry := &s.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if state := registry.states[weak.Make(cfs)]; state != nil {
		return state.plan
	}
	return compile.VarInitReusePlan{}
}

// DeclareNativeBorrowClass declares the borrow class of a native function the host
// registers.
//
// Takes function (any) which is the native function.
// Takes class (engine.BorrowClass) which is the declared class.
//
// Returns error which wraps engine.ErrBorrowDeclarationsFrozen or
// engine.ErrBorrowDeclarationRefused when the declaration is not recorded.
func (s *Service) DeclareNativeBorrowClass(function any, class engine.BorrowClass) error {
	return s.borrowDeclarations.Declare(function, class)
}

// BorrowDeclarationRefusals lists the declarations the service refused.
//
// Returns []string which is "name: class: reason" per refusal.
func (s *Service) BorrowDeclarationRefusals() []string {
	return s.borrowDeclarations.Refusals()
}

// recordVarInitReusePlan records a freshly compiled file set's global reuse analysis.
//
// Takes cfs (*program.CompiledFileSet) which was compiled.
// Takes plan (compile.VarInitReusePlan) which is the analysis.
//
// Concurrency: Serialises updates to the service's reuse registry with its mutex.
func (s *Service) recordVarInitReusePlan(cfs *program.CompiledFileSet, plan compile.VarInitReusePlan) {
	key := weak.Make(cfs)
	registry := &s.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if s.closed.Load() {
		return
	}
	if registry.states == nil {
		registry.states = make(map[weak.Pointer[program.CompiledFileSet]]*varInitReuseState)
	}
	if previous, known := registry.states[key]; known {
		previous.watch.Stop()
	} else {
		runtime.AddCleanup(cfs, dropVarInitReuseState, fileSetRecordOwner{service: weak.Make(s), fileSet: key})
	}
	registry.states[key] = &varInitReuseState{plan: plan, image: nil, watch: nil, misses: 0, cooldown: 0}
}

// varInitReuseActive reports whether global reuse applies to the service's runs.
//
// Returns bool which is true when no debugger is attached.
func (s *Service) varInitReuseActive() bool {
	return s.config == nil || s.config.debugger == nil
}

// skipVariableInits decides whether ExecuteEntrypoint may skip cfs's initialisers.
//
// Takes cfs (*program.CompiledFileSet) which is about to run.
//
// Returns bool which is true when they may be skipped.
//
// Concurrency: Holds the reuse-registry mutex while checking the image and watch; the
// caller must control access to the file set's global slots.
func (s *Service) skipVariableInits(cfs *program.CompiledFileSet) bool {
	if cfs.VariableInitFunction() == nil || !s.varInitReuseActive() {
		return false
	}
	key := weak.Make(cfs)
	registry := &s.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	state := registry.states[key]
	if state == nil || !state.plan.Eligible {
		registry.ineligible.Add(1)
		return false
	}
	if reason := s.varInitRerunReason(state); reason != "" {
		state.noteMiss()
		registry.lastFallback = reason
		registry.reruns.Add(1)
		return false
	}
	state.misses = 0
	registry.skips.Add(1)
	return true
}

// noteVariableInitsRan records the outcome of cfs's initialiser run.
//
// Takes cfs (*program.CompiledFileSet) whose initialisers ran.
// Takes ok (bool) which is true when they finished without error.
//
// Concurrency: Holds the reuse-registry mutex while replacing the image and watch; the
// caller must control access to the file set's global slots.
func (s *Service) noteVariableInitsRan(cfs *program.CompiledFileSet, ok bool) {
	key := weak.Make(cfs)
	registry := &s.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	state := registry.states[key]
	if state == nil || !state.plan.Eligible {
		return
	}
	state.watch.Stop()
	state.image, state.watch = nil, nil
	if !ok || !s.varInitReuseActive() || s.closed.Load() {
		return
	}
	if state.cooldown > 0 {
		state.cooldown--
		return
	}
	state.image = engine.CaptureGlobalSlotImage(s.globals, state.plan.Slots)
	state.watch = engine.WatchGlobalsForReuse(s.globals, state.plan.Slots)
}

// forgetVariableInitImages drops every image and watch, so each file set re-runs its
// initialisers; Reset calls it, since it clears the globals.
//
// Concurrency: Serialises registry updates with its mutex; the caller must coordinate the
// service reset with executions.
func (s *Service) forgetVariableInitImages() {
	registry := &s.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, state := range registry.states {
		state.watch.Stop()
		state.image, state.watch = nil, nil
	}
}

// dropAllVariableInitStates stops every watch and drops every file set's state; Close
// calls it.
//
// Concurrency: Serialises registry updates with its mutex.
func (s *Service) dropAllVariableInitStates() {
	registry := &s.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, state := range registry.states {
		state.watch.Stop()
	}
	registry.states = nil
}

// beginExecution counts an execution currently executing; the returned function ends it.
//
// Returns func() which the caller defers.
func (s *Service) beginExecution() func() {
	s.varInitReuse.inFlight.Add(1)
	return func() { s.varInitReuse.inFlight.Add(-1) }
}

// dropVarInitReuseState is the cleanup that drops a collected file set's reuse state and
// stops its watch.
//
// Takes owner (fileSetRecordOwner) which names the service and the file set.
//
// Concurrency: Runs on the runtime's cleanup goroutine and takes the registry's mutex.
func dropVarInitReuseState(owner fileSetRecordOwner) {
	service := owner.service.Value()
	if service == nil {
		return
	}
	registry := &service.varInitReuse
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if state, ok := registry.states[owner.fileSet]; ok {
		state.watch.Stop()
		delete(registry.states, owner.fileSet)
	}
}

// varInitRerunReason says why a file set's initialisers must run again, or "" when the
// recorded image still holds.
//
// Takes state (*varInitReuseState) which is the file set's eligible state.
//
// Returns string which is the reason, or "".
func (s *Service) varInitRerunReason(state *varInitReuseState) string {
	switch {
	case state.cooldown > 0:
		return "backing off after repeated re-runs"
	case state.image == nil || state.watch == nil:
		return "no finished initialiser run"
	case s.varInitReuse.inFlight.Load() > 1:
		return "another execution is in flight"
	}
	if dirty, why := state.watch.Dirty(); dirty {
		return "dirty: " + why
	}
	if matches, why := state.image.Matches(s.globals); !matches {
		return why
	}
	return ""
}

// noteMiss counts a re-run of initialisers that had a finished image, and starts a
// doubling backoff once the misses repeat.
//
// The caller must hold the reuse-registry mutex.
func (state *varInitReuseState) noteMiss() {
	if state.image == nil || state.cooldown != 0 {
		return
	}
	state.misses++
	if state.misses >= varInitReuseMissesBeforeCooldown {
		state.cooldown = min(1<<min(state.misses, varInitReuseMaxCooldownShift), varInitReuseMaxCooldown)
	}
}
