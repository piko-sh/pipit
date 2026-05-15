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
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestRefillCostChunkExhaustsWhenTrackerDrained(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	vm := &engine.VM{Limits: engine.VMLimits{CostBudget: 10, CostTable: isa.SharedDefaultCostTable(), Tracker: tracker}}

	tracker.CostRemaining.Store(0)
	exhausted, err := vm.RefillCostChunk()
	require.True(t, exhausted)
	require.ErrorIs(t, err, fault.ErrCostBudgetExceeded)

	tracker.CostRemaining.Store(5)
	exhausted, err = vm.RefillCostChunk()
	require.False(t, exhausted)
	require.NoError(t, err)
	require.EqualValues(t, 5, vm.CostRemaining)
	require.EqualValues(t, 0, tracker.CostRemaining.Load())

	tracker.CostRemaining.Store(engine.CostChunk * 4)
	vm.CostRemaining = -3
	exhausted, err = vm.RefillCostChunk()
	require.False(t, exhausted)
	require.NoError(t, err)
	require.EqualValues(t, engine.CostChunk-3, vm.CostRemaining)
	require.EqualValues(t, engine.CostChunk*3, tracker.CostRemaining.Load())
}

func TestRefillCostChunkIsUnmeteredWithoutBudget(t *testing.T) {
	t.Parallel()
	vm := &engine.VM{Limits: engine.VMLimits{Tracker: &engine.ResourceTracker{}}}
	exhausted, err := vm.RefillCostChunk()
	require.False(t, exhausted)
	require.NoError(t, err)
}

func TestReturnUnspentCostChunkRefundsTracker(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	vm := &engine.VM{Limits: engine.VMLimits{CostBudget: 10, CostTable: isa.SharedDefaultCostTable(), Tracker: tracker}}
	vm.CostRemaining = 7
	vm.ReturnUnspentCostChunk()
	require.EqualValues(t, 7, tracker.CostRemaining.Load())
	require.EqualValues(t, 0, vm.CostRemaining)
}

func TestPutRegisterArenaReleasesSharedCharge(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	arena := engine.GetRegisterArena()
	arena.AttachBudgetTracker(tracker)
	before := tracker.ArenaBytes.Load()
	arena.ChargeArenaAllocation(1<<16, 0)
	require.EqualValues(t, before+(1<<16), tracker.ArenaBytes.Load())
	arena.ChargeArenaAllocation(1<<15, 1<<16)
	require.EqualValues(t, before+(1<<15), tracker.ArenaBytes.Load())
	engine.PutRegisterArena(arena)
	require.EqualValues(t, 0, tracker.ArenaBytes.Load())
}

func TestChargeArenaAllocationEnforcesSharedWorkingSet(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	first := engine.GetRegisterArena()
	first.MaxArenaBytes = 1 << 20
	first.AttachBudgetTracker(tracker)
	defer engine.PutRegisterArena(first)
	second := engine.GetRegisterArena()
	second.MaxArenaBytes = 1 << 20
	second.AttachBudgetTracker(tracker)
	defer engine.PutRegisterArena(second)

	first.ChargeArenaAllocation(3<<18, 0)
	require.PanicsWithError(t, "arena: 1572864 bytes exceeds budget 1048576: register arena byte budget exceeded", func() {
		second.ChargeArenaAllocation(3<<18, 0)
	})
}

func TestMainReturnJoinsGoroutines(t *testing.T) {
	t.Parallel()
	service := newTestService(t)
	ctx := context.Background()
	_, err := service.Eval(ctx, `go func() { x := 0; for i := 0; ; i++ { x += i } }(); 1`)
	require.NoError(t, err)
	require.EqualValues(t, 0, service.LeakedGoroutines())
	require.Eventually(t, func() bool {
		return service.limits.Tracker.GoroutineCount.Load() == 0
	}, 5*time.Second, 5*time.Millisecond)
	result, err := service.Eval(ctx, `2`)
	require.NoError(t, err)
	require.Equal(t, 2, result)
}

func TestGoroutineStuckInNativeCallReportsJoinTimeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	entered := make(chan struct{})
	var enterOnce sync.Once
	service := newTestServiceWithFunctions(t, "hostblock", map[string]reflect.Value{
		"Wait": reflect.ValueOf(func() {
			enterOnce.Do(func() { close(entered) })
			<-release
		}),
		"AwaitEntry": reflect.ValueOf(func() { <-entered }),
	})
	t.Cleanup(func() {
		close(release)
		require.Eventually(t, func() bool {
			return service.limits.Tracker.GoroutineCount.Load() == 0
		}, 5*time.Second, 5*time.Millisecond)
	})
	source := `package main
import (
	"hostblock"
)

func run() int {
	go func() { hostblock.Wait() }()
	hostblock.AwaitEntry()
	return 1
}
func main() {}
`
	_, err := service.EvalFile(context.Background(), source, "run")
	require.ErrorIs(t, err, fault.ErrGoroutineJoinTimeout)
	require.EqualValues(t, 1, service.LeakedGoroutines())
}

func TestWaitForGoroutinesExtendsGraceWhileUnscheduled(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	tracker.GoroutineCount.Add(1)
	tracker.RegisterGoroutine()
	grace := 200 * time.Millisecond
	go func() {
		time.Sleep(3 * grace)
		tracker.MarkGoroutineStarted()
		tracker.ReleaseGoroutine()
	}()

	require.True(t, engine.WaitForGoroutines(tracker, grace))
	require.Zero(t, tracker.GoroutinesUnscheduled.Load())
}

func TestWaitForGoroutinesExtendsGraceOnProgress(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	tracker.GoroutineCount.Add(3)
	for range 3 {
		tracker.RegisterGoroutine()
		tracker.MarkGoroutineStarted()
	}
	go func() {
		for range 3 {
			time.Sleep(engine.GoroutineJoinGrace / 8)
			tracker.ReleaseGoroutine()
		}
	}()

	require.True(t, engine.WaitForGoroutines(tracker, engine.GoroutineJoinGrace))
}

func TestWaitForGoroutinesTimesOutOnStartedStraggler(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	tracker.GoroutineCount.Add(1)
	tracker.RegisterGoroutine()
	tracker.MarkGoroutineStarted()
	defer tracker.ReleaseGoroutine()

	grace := 100 * time.Millisecond
	began := time.Now()
	require.False(t, engine.WaitForGoroutines(tracker, grace))
	require.Less(t, time.Since(began), engine.GoroutineJoinScheduleCap/2)
}

func TestAbandonGoroutineClearsUnscheduledCount(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	tracker.GoroutineCount.Add(1)
	tracker.RegisterGoroutine()
	require.EqualValues(t, 1, tracker.GoroutinesUnscheduled.Load())

	tracker.AbandonGoroutine()
	require.Zero(t, tracker.GoroutinesUnscheduled.Load())
	require.Zero(t, tracker.GoroutineCount.Load())
	require.True(t, engine.WaitForGoroutines(tracker, engine.GoroutineJoinGrace))
}

func TestAccountForInstructionCostUnmeteredParksAtSentinel(t *testing.T) {
	t.Parallel()
	vm := &engine.VM{Limits: engine.VMLimits{CostTable: isa.SharedDefaultCostTable(), Tracker: &engine.ResourceTracker{}}}
	vm.SeedCostBudget()
	require.EqualValues(t, engine.UnmeteredCostSentinel, vm.CostRemaining)

	for range 3 * engine.CostChunk {
		exhausted, err := vm.AccountForInstructionCost(isa.NewInstruction(isa.OpDrillTier1, 0, 0, 0))
		require.False(t, exhausted)
		require.NoError(t, err)
	}
	require.EqualValues(t, engine.UnmeteredCostSentinel, vm.CostRemaining)
	require.Zero(t, vm.CostUsed())
}

func TestAccountForInstructionCostMeteredNeverLeavesCounterNegative(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	vm := &engine.VM{Limits: engine.VMLimits{CostBudget: 3 * engine.CostChunk, CostTable: isa.SharedDefaultCostTable(), Tracker: tracker}}
	vm.SeedCostBudget()
	require.Zero(t, vm.CostRemaining)

	for {
		exhausted, err := vm.AccountForInstructionCost(isa.NewInstruction(isa.OpDrillTier1, 0, 0, 0))
		if exhausted {
			require.ErrorIs(t, err, fault.ErrCostBudgetExceeded)
			break
		}
		require.Positive(t, vm.CostRemaining)
	}
	require.LessOrEqual(t, tracker.CostRemaining.Load(), int64(0))
}
