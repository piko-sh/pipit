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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

const gateWaitLimit = 10 * time.Second

const gatePollInterval = 50 * time.Microsecond

type gateTestClock struct {
	now   time.Time
	slept time.Duration
	mu    sync.Mutex
}

func (c *gateTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *gateTestClock) Since(start time.Time) time.Duration {
	return c.Now().Sub(start)
}

func (c *gateTestClock) Until(deadline time.Time) time.Duration {
	return deadline.Sub(c.Now())
}

func (c *gateTestClock) Sleep(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
	c.slept += duration
}

func (*gateTestClock) NewTimer(time.Duration) *time.Timer {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	return timer
}

func (*gateTestClock) NewTicker(time.Duration) *time.Ticker {
	ticker := time.NewTicker(time.Hour)
	ticker.Stop()
	return ticker
}

func (c *gateTestClock) sleptFor() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.slept
}

func newTimedExportGate(t *testing.T) (*exportGate, *gateTestClock) {
	t.Helper()
	gate := newExportGate()
	testClock := &gateTestClock{now: time.Unix(0, 0)}
	gate.clock = testClock
	return gate, testClock
}

func newPatientExportGate(t *testing.T) *exportGate {
	t.Helper()
	gate := newExportGate()
	gate.guestPatience = time.Hour
	gate.nativePatience = time.Hour
	return gate
}

func TestAnOwnerSectionWaitsForTheReaderToLetTheGateGo(t *testing.T) {
	gate := newPatientExportGate(t)
	gate.enterReader()
	shared := []int{0}
	entered := make(chan bool, 1)
	go func() {
		entered <- gate.enterOwner()
	}()
	require.Eventually(t, gate.ownerWaiting, gateWaitLimit, gatePollInterval, "the owner starts to wait for the reader")
	shared[0] = 7
	gate.leaveReader()
	var held bool
	require.Eventually(t, func() bool {
		select {
		case held = <-entered:
			return true
		default:
			return false
		}
	}, gateWaitLimit, gatePollInterval, "the owner enters its section once the reader lets the gate go")
	require.True(t, held, "the reader let the gate go within the owner's patience")
	require.Equal(t, 7, shared[0], "the reader's writes are ordered before the owner's section")
	require.True(t, gate.ownerHolds())
	gate.leaveOwner()
	require.False(t, gate.ownerHolds())
	require.False(t, gate.ownerWaiting())
}

func TestAReaderWaitsForTheOwnersSectionToEnd(t *testing.T) {
	gate := newExportGate()
	require.True(t, gate.enterOwner(), "no reader holds the gate")
	written := 0
	entered := make(chan int, 1)
	go func() {
		gate.enterReader()
		entered <- written
		gate.leaveReader()
	}()
	require.Eventually(t, func() bool { return gate.sleepers.Load() == 1 }, gateWaitLimit, gatePollInterval,
		"the reader sleeps until the owner's section ends")
	require.Empty(t, entered, "a reader never runs guest code inside the owner's section")
	written = 3
	gate.leaveOwner()
	var seen int
	require.Eventually(t, func() bool {
		select {
		case seen = <-entered:
			return true
		default:
			return false
		}
	}, gateWaitLimit, gatePollInterval, "the reader is woken")
	require.Equal(t, 3, seen, "the owner's section is ordered before the reader's run")
}

func TestAnOwnerSectionRunsSharedWhenEveryReaderWaitsInANativeCall(t *testing.T) {
	gate, testClock := newTimedExportGate(t)
	gate.enterReader()
	gate.inNative.Add(1)
	require.False(t, gate.enterOwner(), "a reader inside a native call may be waiting for the owner, so the owner must not wait for it")
	require.Greater(t, testClock.sleptFor(), gate.nativePatience-exportGateNap, "the owner waits out the native patience")
	require.Less(t, testClock.sleptFor(), gate.guestPatience, "and gives up long before the guest patience")
	require.False(t, gate.ownerWaiting(), "an owner that gave up lets readers in")
	require.False(t, gate.ownerHolds())
	gate.leaveOwner()
	other := make(chan struct{})
	go func() {
		gate.enterReader()
		gate.leaveReader()
		close(other)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-other:
			return true
		default:
			return false
		}
	}, gateWaitLimit, gatePollInterval, "a reader is let in after the owner gave up")
	gate.inNative.Add(-1)
	gate.leaveReader()
}

func TestAnOwnerSectionGivesUpOnAReaderThatNeverYields(t *testing.T) {
	gate, testClock := newTimedExportGate(t)
	gate.enterReader()
	require.False(t, gate.enterOwner())
	require.GreaterOrEqual(t, testClock.sleptFor(), gate.guestPatience, "a reader in guest code is waited for up to the guest patience")
	gate.leaveOwner()
	gate.leaveReader()
	require.True(t, gate.enterOwner())
	gate.leaveOwner()
}

func TestOwnerSectionsNest(t *testing.T) {
	gate, _ := newTimedExportGate(t)
	require.True(t, gate.enterOwner())
	require.True(t, gate.enterOwner(), "a nested section shares the outer one's hold")
	gate.leaveOwner()
	require.True(t, gate.ownerHolds(), "the outer section still holds the gate")
	gate.leaveOwner()
	require.False(t, gate.ownerHolds())

	gate.enterReader()
	gate.inNative.Add(1)
	require.False(t, gate.enterOwner())
	gate.inNative.Add(-1)
	gate.leaveReader()
	require.False(t, gate.enterOwner(), "a section nested in one that gave up runs shared too")
	gate.leaveOwner()
	gate.leaveOwner()
	require.True(t, gate.enterOwner())
	gate.leaveOwner()
}

func TestAnExportGateSetGrowsByReplacement(t *testing.T) {
	first, second := newExportGate(), newExportGate()
	set := exportGateSet{}.with(first)
	grown := set.with(first, second, nil)
	require.Len(t, set, 1, "a published set never changes")
	require.Equal(t, exportGateSet{first, second}, grown)
	require.Equal(t, grown, grown.with(second), "nothing new shares the set")
}

func gatedRunOf(t *testing.T, gate *exportGate) *VM {
	t.Helper()
	vm := newTestVM(t)
	vm.reentrantInterpreterVM = true
	vm.runGates = exportGateSet{gate}
	require.True(t, vm.beginGatedRun())
	require.True(t, vm.holdsGates)
	return vm
}

func TestANativeCallThatKeepsTheHostGoroutineCountsTheGatedRunInside(t *testing.T) {
	gate, testClock := newTimedExportGate(t)
	vm := gatedRunOf(t, gate)
	site := &program.CallSite{}

	hold := vm.enterNativeCall(site)

	require.Equal(t, nativeCallCounted, hold, "the run keeps its gate, since the native may write through its arguments")
	require.EqualValues(t, 1, gate.readers.Load())
	require.EqualValues(t, 1, gate.inNative.Load())
	require.False(t, gate.enterOwner(), "the run may wait for the owner inside the call, so the owner gives up")
	require.Less(t, testClock.sleptFor(), gate.guestPatience, "after the native patience")
	gate.leaveOwner()
	vm.leaveNativeCall(hold)
	require.Zero(t, gate.inNative.Load(), "leaving the call counts the run out")
	vm.endGatedRun()
	require.Zero(t, gate.readers.Load())
}

func TestANativeCallThatBlocksTheHostGoroutineLetsTheGatesGo(t *testing.T) {
	gate, _ := newTimedExportGate(t)
	vm := gatedRunOf(t, gate)
	site := &program.CallSite{BlocksHostGoroutine: true}

	hold := vm.enterNativeCall(site)

	require.Equal(t, nativeCallReleased, hold, "a VM that holds no interpreter lock releases only its gates")
	require.Zero(t, gate.readers.Load(), "a blocked run holds no gate")
	require.Zero(t, gate.inNative.Load())
	require.True(t, gate.enterOwner(), "the owner's section runs unshared while the call blocks")
	gate.leaveOwner()
	vm.leaveNativeCall(hold)
	require.EqualValues(t, 1, gate.readers.Load(), "the run takes its gate back when the call returns")
	vm.endGatedRun()
	require.Zero(t, gate.readers.Load())
}

func TestANativeCallOfARunWithoutGatesDoesNothing(t *testing.T) {
	vm := newTestVM(t)
	require.Equal(t, nativeCallHoldsNothing, vm.enterNativeCall(&program.CallSite{}))
	vm.leaveNativeCall(nativeCallHoldsNothing)
	require.False(t, vm.holdsGates)
}

func TestAParkedGatedRunIsNotCountedInsideANativeCall(t *testing.T) {
	gate, _ := newTimedExportGate(t)
	vm := gatedRunOf(t, gate)
	vm.parkGates()
	require.Zero(t, gate.readers.Load())

	hold := vm.enterNativeCallSlow(&program.CallSite{})

	require.Equal(t, nativeCallHoldsNothing, hold, "a run that parked its gates holds none to count")
	require.Zero(t, gate.inNative.Load())
	vm.leaveNativeCall(hold)
	require.Zero(t, gate.inNative.Load(), "and counts nothing out")
	vm.unparkGates()
	vm.endGatedRun()
	require.Zero(t, gate.readers.Load())
}

func TestAGatedRunYieldsItsGateOnlyToAWaitingOwner(t *testing.T) {
	gate := newPatientExportGate(t)
	vm := gatedRunOf(t, gate)
	vm.yieldExportGates()
	require.EqualValues(t, 1, gate.readers.Load(), "with no owner waiting the run keeps its gate")

	entered := make(chan bool, 1)
	go func() {
		entered <- gate.enterOwner()
	}()
	require.Eventually(t, gate.ownerWaiting, gateWaitLimit, gatePollInterval)
	yielded := make(chan struct{})
	go func() {
		vm.yieldExportGates()
		close(yielded)
	}()
	var held bool
	require.Eventually(t, func() bool {
		select {
		case held = <-entered:
			return true
		default:
			return false
		}
	}, gateWaitLimit, gatePollInterval, "the run lets a waiting owner in at its checkpoint")
	require.True(t, held)
	require.Eventually(t, func() bool { return gate.sleepers.Load() == 1 }, gateWaitLimit, gatePollInterval,
		"the run waits for the section to end before taking its gate back")
	gate.leaveOwner()
	require.Eventually(t, func() bool {
		select {
		case <-yielded:
			return true
		default:
			return false
		}
	}, gateWaitLimit, gatePollInterval)
	require.EqualValues(t, 1, gate.readers.Load())
	vm.parkGates()
	vm.yieldExportGates()
	require.Zero(t, gate.readers.Load(), "a parked run has nothing to yield")
	vm.unparkGates()
	vm.endGatedRun()
}

func TestExportGateStatisticsCountSectionsWaitsAndGiveUps(t *testing.T) {
	previous := SetLicenceCountingForTest(true)
	t.Cleanup(func() { SetLicenceCountingForTest(previous) })
	before := exportGateStatistics()
	gate, _ := newTimedExportGate(t)
	require.True(t, gate.enterOwner())
	gate.leaveOwner()
	gate.enterReader()
	require.False(t, gate.enterOwner())
	gate.leaveOwner()
	gate.leaveReader()
	after := exportGateStatistics()
	require.Equal(t, before.Sections+1, after.Sections)
	require.Equal(t, before.Waits+1, after.Waits)
	require.Equal(t, before.GiveUps+1, after.GiveUps)
}
