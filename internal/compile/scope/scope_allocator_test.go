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

package scope

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestAllocatingFromABankHandsOutConsecutiveRegisters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
	}{
		{name: "the integer bank", kind: isa.RegisterInt64},
		{name: "the float bank", kind: isa.RegisterFloat64},
		{name: "the string bank", kind: isa.RegisterString},
		{name: "the general bank", kind: isa.RegisterGeneral},
		{name: "the boolean bank", kind: isa.RegisterBool},
		{name: "the unsigned bank", kind: isa.RegisterUint64},
		{name: "the complex bank", kind: isa.RegisterComplex128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			allocator := &RegisterAllocator{FunctionName: "run"}

			require.Equal(t, uint8(0), allocator.Alloc(tt.kind))
			require.Equal(t, uint8(1), allocator.Alloc(tt.kind))
			require.Equal(t, uint8(2), allocator.Alloc(tt.kind))
			require.NoError(t, allocator.OverflowErr)
		})
	}
}

func TestAllocatingOneBankLeavesTheOthersUntouched(t *testing.T) {
	t.Parallel()

	allocator := &RegisterAllocator{FunctionName: "run"}

	require.Equal(t, uint8(0), allocator.Alloc(isa.RegisterInt64))
	require.Equal(t, uint8(1), allocator.Alloc(isa.RegisterInt64))

	require.Equal(t, uint8(0), allocator.Alloc(isa.RegisterFloat64),
		"each bank is its own register file, so an integer allocation cannot consume a float slot")
}

func TestATemporaryReleasedInOrderIsHandedOutAgain(t *testing.T) {
	t.Parallel()

	allocator := &RegisterAllocator{FunctionName: "run"}
	allocator.Alloc(isa.RegisterInt64)

	temporary := allocator.AllocTemp(isa.RegisterInt64)
	require.Equal(t, uint8(1), temporary)

	allocator.FreeTemp(isa.RegisterInt64, temporary)

	require.Equal(t, uint8(1), allocator.AllocTemp(isa.RegisterInt64),
		"a freed temporary is the next register handed out, which is what keeps expression scratch space bounded")
	require.Equal(t, uint32(2), allocator.Snapshot()[isa.RegisterInt64],
		"the live count follows the frees, or every expression would grow the frame")
}

func TestTheHighWaterMarkSurvivesEveryRelease(t *testing.T) {
	t.Parallel()

	allocator := &RegisterAllocator{FunctionName: "run"}
	for range 4 {
		allocator.AllocTemp(isa.RegisterInt64)
	}
	for range 4 {
		allocator.FreeTemp(isa.RegisterInt64, 0)
	}

	allocator.EnsureMin(isa.RegisterInt64, 1)

	require.Equal(t, uint32(0), allocator.Snapshot()[isa.RegisterInt64],
		"every temporary was released, so nothing is live")
	require.Equal(t, uint8(0), allocator.Alloc(isa.RegisterInt64),
		"the next allocation reuses the space the temporaries gave back")
}

func TestEnsuringAMinimumOnlyEverRaisesTheFrameSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		allocate  int
		ensure    uint32
		wantAlloc uint8
	}{
		{name: "a minimum above what was allocated", allocate: 1, ensure: 4, wantAlloc: 1},
		{name: "a minimum below what was allocated", allocate: 3, ensure: 1, wantAlloc: 3},
		{name: "a minimum of zero", allocate: 2, ensure: 0, wantAlloc: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			allocator := &RegisterAllocator{FunctionName: "run"}
			for range tt.allocate {
				allocator.Alloc(isa.RegisterInt64)
			}

			allocator.EnsureMin(isa.RegisterInt64, tt.ensure)

			require.Equal(t, tt.wantAlloc, allocator.Alloc(isa.RegisterInt64),
				"the minimum sizes the frame without handing any register out, so the next allocation is unaffected")
		})
	}
}

func TestReservingLowRegistersPushesTheNextAllocationPastThem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		allocate  int
		reserve   uint32
		wantAlloc uint8
	}{
		{name: "a reservation on an untouched bank", allocate: 0, reserve: 3, wantAlloc: 3},
		{name: "a reservation larger than what is allocated", allocate: 1, reserve: 3, wantAlloc: 3},
		{name: "a reservation smaller than what is allocated", allocate: 4, reserve: 2, wantAlloc: 4},
		{name: "a reservation of none", allocate: 2, reserve: 0, wantAlloc: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			allocator := &RegisterAllocator{FunctionName: "run"}
			for range tt.allocate {
				allocator.Alloc(isa.RegisterInt64)
			}

			allocator.ReserveLow(isa.RegisterInt64, tt.reserve)

			require.Equal(t, tt.wantAlloc, allocator.Alloc(isa.RegisterInt64),
				"a reserved low register belongs to a call's argument or return slot and must not be handed out as scratch")
		})
	}
}

func TestARecycledRegisterIsHandedOutBeforeAFreshOne(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("run")
	stack.PushScope()
	first := stack.DeclareVar("a", isa.RegisterInt64)
	second := stack.DeclareVar("b", isa.RegisterInt64)

	stack.Alloc.RecycleRegister(isa.RegisterInt64, first.Register)
	reused := stack.DeclareVar("c", isa.RegisterInt64)

	require.Equal(t, first.Register, reused.Register,
		"a variable that is provably dead gives its register back, which is what keeps frames small")
	require.NotEqual(t, second.Register, reused.Register,
		"only the recycled register may be reused, never one still holding a live variable")
}

func TestAnInnerScopeCannotTakeARegisterRecycledByAnOuterOne(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("run")
	stack.PushScope()
	outer := stack.DeclareVar("a", isa.RegisterInt64)
	stack.Alloc.RecycleRegister(isa.RegisterInt64, outer.Register)

	stack.PushScope()
	fresh := stack.DeclareVar("c", isa.RegisterInt64)

	require.NotEqual(t, outer.Register, fresh.Register,
		"a register the enclosing scope gave back is not an inner scope's to take, or leaving the inner scope would free it twice")
}

func TestASnapshotRestoresEveryBankAtOnce(t *testing.T) {
	t.Parallel()

	allocator := &RegisterAllocator{FunctionName: "run"}
	allocator.Alloc(isa.RegisterInt64)
	allocator.Alloc(isa.RegisterFloat64)
	saved := allocator.Snapshot()

	allocator.Alloc(isa.RegisterInt64)
	allocator.Alloc(isa.RegisterInt64)
	allocator.Alloc(isa.RegisterString)

	allocator.Restore(saved)

	require.Equal(t, saved, allocator.Snapshot(),
		"leaving a scope gives back every register it took, in every bank at once")
	require.Equal(t, uint8(1), allocator.Alloc(isa.RegisterInt64),
		"the restored counter is what the next allocation continues from")
}

func TestRestoringPastTheCurrentAllocationIsRefusedLoudly(t *testing.T) {
	t.Parallel()

	allocator := &RegisterAllocator{FunctionName: "run"}
	allocator.Alloc(isa.RegisterInt64)
	allocator.Alloc(isa.RegisterInt64)
	saved := allocator.Snapshot()
	allocator.Restore([isa.NumRegisterKinds]uint32{})

	require.Panics(t, func() { allocator.Restore(saved) },
		"restoring to a larger frame than the one in hand would hand out registers nothing ever wrote")
}
