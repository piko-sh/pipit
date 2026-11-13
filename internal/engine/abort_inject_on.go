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

//go:build pipit_testhooks

package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/fault"
)

// abortInjectBuilt is true in the pipit_testhooks build.
const abortInjectBuilt = true

// abortInjectFrames is the number of caller frames an injection records.
const abortInjectFrames = 8

// abortInjectSkipFrames is the number of frames a stack reader skips: runtime.Callers,
// the reader and the hook that called it.
const abortInjectSkipFrames = 3

const (
	// abortSiteAllocBudget identifies an arena-budget panic at a bump allocator's entry.
	abortSiteAllocBudget = abortSiteLockWindow + 1 + iota

	// abortSiteAllocRecover identifies a recoverable panic at a bump allocator's entry.
	abortSiteAllocRecover

	// abortSiteHandOffBudget identifies an arena-budget panic when a hand-off begins its
	// licence work, after the handler's earlier effects and before revocation.
	abortSiteHandOffBudget

	// abortSiteHandOffRecover is abortSiteHandOffBudget with a panic the guest can recover.
	abortSiteHandOffRecover

	// numAbortSites is the number of sites.
	numAbortSites
)

// abortSweptSites are the sites armed by the guest-level sweep, in the sweep's order.
var abortSweptSites = [...]abortSite{
	abortSiteGrowCharge,
	abortSiteSafepointBudget,
	abortSiteAllocLimit,
	abortSiteAllocBudget,
	abortSiteAllocRecover,
	abortSiteHandOffBudget,
	abortSiteHandOffRecover,
	abortSiteCost,
}

// abortSiteNames are the sites' stable names, indexed by abortSite.
var abortSiteNames = [numAbortSites]string{
	abortSiteGrowCharge:      "grow-charge",
	abortSiteSafepointBudget: "safepoint-budget",
	abortSiteAllocLimit:      "alloc-limit",
	abortSiteAllocBudget:     "alloc-budget",
	abortSiteAllocRecover:    "alloc-recover",
	abortSiteHandOffBudget:   "handoff-budget",
	abortSiteHandOffRecover:  "handoff-recover",
	abortSiteCost:            "cost",
	abortSiteBlindWindow:     "blind-window",
	abortSiteForeignWindow:   "foreign-window",
	abortSiteLockWindow:      "lock-window",
}

// errAbortInjectionUnknownSite reports an abort-injection site name that names no site.
var errAbortInjectionUnknownSite = errors.New("abort injection: unknown site")

// abortInjection is the armed injection. Every site counts its reaches while armed; the
// armed site fires once, on its target reach.
var abortInjection struct {
	// firedAt is the caller stack of the firing, innermost first.
	firedAt string

	// mu serialises arming, disarming and the record of the firing.
	mu sync.Mutex

	// armed is true between ArmAbortInjection and DisarmAbortInjection.
	armed atomic.Bool

	// site is the armed site.
	site atomic.Uint32

	// target is the reach of site that fires; zero only counts.
	target atomic.Uint64

	// fired is true once the injection fired.
	fired atomic.Bool

	// hits counts each site's reaches since arming.
	hits [numAbortSites]atomic.Uint64
}

// AbortInjectionReport is what one armed interval saw.
type AbortInjectionReport struct {
	// Hits counts each site's reaches, by site name.
	Hits map[string]uint64

	// FiredAt is the stack the injection fired from, innermost frame first, or "".
	FiredAt string

	// Fired is true when the armed site reached its target.
	Fired bool
}

// errInjectedLockWindow is the cancellation cause the lock-window site applies.
var errInjectedLockWindow = fmt.Errorf("injected cancellation at %s: %w", abortSiteNames[abortSiteLockWindow], context.Canceled)

// AbortInjectionSites lists the sites the guest-level sweep arms, by name.
// ArmAbortInjection also accepts the release-window sites the engine tests arm.
//
// Returns []string which holds the names in a stable order.
func AbortInjectionSites() []string {
	names := make([]string, 0, len(abortSweptSites))
	for _, site := range abortSweptSites {
		names = append(names, abortSiteNames[site])
	}
	return names
}

// ArmAbortInjection arms site to fail on its target reach, counting from now. A zero
// target only counts the reaches of every site.
//
// Takes site (string) which names the site.
// Takes target (uint64) which is the reach that fails, from 1.
//
// Returns error when site is unknown.
//
// Concurrency: Serialises configuration changes with abortInjection.mu and publishes the
// armed state atomically. Tests must coordinate arming and disarming with executions.
func ArmAbortInjection(site string, target uint64) error {
	index := -1
	for candidate, name := range abortSiteNames {
		if name == site {
			index = candidate
		}
	}
	if index < 0 {
		return fmt.Errorf("%w %q", errAbortInjectionUnknownSite, site)
	}
	abortInjection.mu.Lock()
	defer abortInjection.mu.Unlock()
	for counter := range abortInjection.hits {
		abortInjection.hits[counter].Store(0)
	}
	abortInjection.site.Store(uint32(index))
	abortInjection.target.Store(target)
	abortInjection.fired.Store(false)
	abortInjection.firedAt = ""
	abortInjection.armed.Store(true)
	return nil
}

// DisarmAbortInjection disarms the injection and reports what the armed interval saw.
//
// Returns AbortInjectionReport which holds the counts and the firing.
//
// Concurrency: Disarms atomically and copies the report under abortInjection.mu. Tests
// must stop the observed execution before expecting a final report.
func DisarmAbortInjection() AbortInjectionReport {
	abortInjection.armed.Store(false)
	abortInjection.mu.Lock()
	defer abortInjection.mu.Unlock()
	report := AbortInjectionReport{Hits: make(map[string]uint64, numAbortSites), Fired: abortInjection.fired.Load(), FiredAt: abortInjection.firedAt}
	for site, name := range abortSiteNames {
		report.Hits[name] = abortInjection.hits[site].Load()
	}
	return report
}

// abortInjectArmed reports whether an injection is armed between ArmAbortInjection and
// DisarmAbortInjection.
//
// Returns bool which is true while armed.
func abortInjectArmed() bool { return abortInjection.armed.Load() }

// abortInjectFires counts a reach of site and reports whether it is the armed target.
//
// Takes site (abortSite) which is the site reached.
//
// Returns bool which is true exactly once per arming, on the target reach.
//
// Concurrency: Uses atomic counters to select one firing and abortInjection.mu to record
// its stack; tests must coordinate configuration changes with executions.
func abortInjectFires(site abortSite) bool {
	if !abortInjection.armed.Load() {
		return false
	}
	reach := abortInjection.hits[site].Add(1)
	if abortInjection.site.Load() != uint32(site) || reach != abortInjection.target.Load() {
		return false
	}
	if abortSiteRecoverable(site) && !abortInjectInGuardedRun() {
		abortInjection.target.Add(1)
		return false
	}
	if !abortInjection.fired.CompareAndSwap(false, true) {
		return false
	}
	stack := abortInjectStack()
	abortInjection.mu.Lock()
	abortInjection.firedAt = stack
	abortInjection.mu.Unlock()
	return true
}

// abortSiteRecoverable reports whether site raises a panic the guest can recover, which
// only a handler running under a guarded dispatch loop can raise.
//
// Takes site (abortSite) which is the site.
//
// Returns bool which is true for the recoverable sites.
func abortSiteRecoverable(site abortSite) bool {
	return site == abortSiteAllocLimit || site == abortSiteAllocRecover || site == abortSiteHandOffRecover
}

// abortInjectInGuardedRun reports whether the caller runs under RunDispatchedGuarded or
// RunGuarded, whose recovery turns a handler's Go panic into a guest panic.
//
// Returns bool which is true when a guarded loop is on the stack.
func abortInjectInGuardedRun() bool {
	var pcs [128]uintptr
	count := runtime.Callers(abortInjectSkipFrames, pcs[:])
	frames := runtime.CallersFrames(pcs[:count])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, ".RunDispatchedGuarded") || strings.HasSuffix(frame.Function, ".RunGuarded") {
			return true
		}
		if !more {
			return false
		}
	}
}

// abortInjectStack renders the caller stack above the injection hooks.
//
// Returns string which lists function file:line per frame, innermost first.
func abortInjectStack() string {
	var pcs [abortInjectFrames + 4]uintptr
	count := runtime.Callers(abortInjectSkipFrames, pcs[:])
	frames := runtime.CallersFrames(pcs[:count])
	var parts []string
	for len(parts) < abortInjectFrames {
		frame, more := frames.Next()
		name := frame.Function[strings.LastIndexByte(frame.Function, '.')+1:]
		if !strings.HasPrefix(name, "injectAllocationAbort") && !strings.HasPrefix(name, "injectHandOffAbort") {
			parts = append(parts, fmt.Sprintf("%s %s:%d", name, filepath.Base(frame.File), frame.Line))
		}
		if !more {
			break
		}
	}
	return strings.Join(parts, " < ")
}

// abortInjectedError is the value an injection at site raises, matching the error the
// real limit at that site carries.
//
// Takes site (abortSite) which is the site that fired.
//
// Returns error which wraps fault.ErrArenaBudgetExceeded, fault.ErrAllocationLimit or
// fault.ErrCostBudgetExceeded.
func abortInjectedError(site abortSite) error {
	switch site {
	case abortSiteAllocLimit, abortSiteAllocRecover, abortSiteHandOffRecover:
		return fmt.Errorf("arena: injected abort at %s: %w", abortSiteNames[site], fault.ErrAllocationLimit)
	case abortSiteCost:
		return fault.ErrCostBudgetExceeded
	default:
		return fmt.Errorf("arena: injected abort at %s: %w", abortSiteNames[site], fault.ErrArenaBudgetExceeded)
	}
}

// injectAllocationAbort is the bump allocators' hook.
//
// It raises the armed allocation abort, of either kind, on its target reach. A budget
// abort goes through raiseAbort as the real one does; a recoverable one stands for a Go
// runtime panic in the allocation.
//
// Takes arena (*RegisterArena) which is the allocating arena.
//
// Panics with the injected allocation-limit error when the recoverable allocation site
// fires in a pipit_testhooks build.
func injectAllocationAbort(arena *RegisterArena) {
	if abortInjectFires(abortSiteAllocBudget) {
		arena.raiseAbort(abortInjectedError(abortSiteAllocBudget))
	}
	if abortInjectFires(abortSiteAllocRecover) {
		panic(abortInjectedError(abortSiteAllocRecover))
	}
}

// injectHandOffAbort is the hand-off hook.
//
// It raises the armed hand-off abort, of either kind, on its target reach.
//
// Takes arena (*RegisterArena) which is the handing-off arena.
//
// Panics with the injected allocation-limit error when the recoverable hand-off site
// fires in a pipit_testhooks build.
func injectHandOffAbort(arena *RegisterArena) {
	if abortInjectFires(abortSiteHandOffBudget) {
		arena.raiseAbort(abortInjectedError(abortSiteHandOffBudget))
	}
	if abortInjectFires(abortSiteHandOffRecover) {
		panic(abortInjectedError(abortSiteHandOffRecover))
	}
}

// injectLockWindowStop is the lock-window site's stop.
//
// It cancels the execution, as the host or the deadline would, and stops the VM at once
// with that cancellation, holding the lock its Lock call has just taken.
//
// Returns OpResult which is the stop.
func (vm *VM) injectLockWindowStop() OpResult {
	if tracker := vm.Limits.Tracker; tracker != nil {
		if cancel := tracker.executionCancel.Load(); cancel != nil {
			(*cancel)(errInjectedLockWindow)
		}
	}
	return vm.surfaceContextCancellation()
}
