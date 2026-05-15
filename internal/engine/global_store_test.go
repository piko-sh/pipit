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
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

type globalNamedInt int

func TestGlobalStoreAllocatesAndReadsBackEveryBank(t *testing.T) {
	t.Parallel()

	t.Run("the integer bank", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		index := store.AllocInt(42)

		require.Equal(t, int64(42), store.GetInt(index), "allocation seeds the slot")
		store.SetInt(index, -7)
		require.Equal(t, int64(-7), store.GetInt(index))
	})

	t.Run("the float bank", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		index := store.AllocFloat(1.5)

		require.InDelta(t, 1.5, store.GetFloat(index), 0)
		store.SetFloat(index, -2.5)
		require.InDelta(t, -2.5, store.GetFloat(index), 0)
	})

	t.Run("the string bank", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		index := store.AllocString("pipit")

		require.Equal(t, "pipit", store.GetString(index))
		store.SetString(index, "changed")
		require.Equal(t, "changed", store.GetString(index))
	})

	t.Run("the boolean bank", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		index := store.AllocBool(true)

		require.True(t, store.GetBool(index))
		store.SetBool(index, false)
		require.False(t, store.GetBool(index))
	})

	t.Run("the unsigned bank", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		index := store.AllocUint(42)

		require.Equal(t, uint64(42), store.GetUint(index))
		store.SetUint(index, math.MaxUint64)
		require.Equal(t, uint64(math.MaxUint64), store.GetUint(index))
	})

	t.Run("the general bank", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		index := store.AllocGeneral(reflect.ValueOf("boxed"))

		require.Equal(t, "boxed", store.GetGeneral(index).String())
		store.SetGeneral(index, reflect.ValueOf(7))
		require.Equal(t, int64(7), store.GetGeneral(index).Int())
	})

	t.Run("the complex bank", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		index := store.AllocComplex(1 + 2i)

		require.Equal(t, 0, index, "the first allocation of a bank is slot zero")
	})
}

func TestGlobalStoreAllocationsAreSequentialPerBank(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()

	require.Equal(t, 0, store.AllocInt(1))
	require.Equal(t, 1, store.AllocInt(2))
	require.Equal(t, 2, store.AllocInt(3))
	require.Equal(t, 0, store.AllocString("a"), "each bank numbers its slots independently")
	require.Equal(t, 0, store.AllocFloat(1))

	require.Equal(t, int64(1), store.GetInt(0))
	require.Equal(t, int64(3), store.GetInt(2))
}

func TestGlobalStoreLengthsTrackEveryAllocation(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	require.Equal(t, program.SlotAllocation{}, store.Lengths(), "a fresh store holds no slots")

	store.AllocInt(1)
	store.AllocInt(2)
	store.AllocFloat(1)
	store.AllocString("a")
	store.AllocBool(true)
	store.AllocUint(1)
	store.AllocGeneral(reflect.ValueOf(1))
	store.AllocComplex(1)

	lengths := store.Lengths()
	require.Equal(t, uint16(2), lengths[isa.RegisterInt])
	require.Equal(t, uint16(1), lengths[isa.RegisterFloat])
	require.Equal(t, uint16(1), lengths[isa.RegisterString])
	require.Equal(t, uint16(1), lengths[isa.RegisterGeneral])
	require.Equal(t, uint16(1), lengths[isa.RegisterBool])
	require.Equal(t, uint16(1), lengths[isa.RegisterUint])
	require.Equal(t, uint16(1), lengths[isa.RegisterComplex])
}

func TestReserveSlotsReturnsTheBasesBeforeItGrows(t *testing.T) {
	t.Parallel()

	t.Run("a reservation against an empty store starts at zero", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		request := program.SlotAllocation{}
		request[isa.RegisterInt] = 3
		request[isa.RegisterString] = 2

		bases := store.ReserveSlots(request)

		require.Equal(t, uint16(0), bases[isa.RegisterInt])
		require.Equal(t, uint16(0), bases[isa.RegisterString])
		require.Equal(t, uint16(3), store.Lengths()[isa.RegisterInt])
		require.Equal(t, uint16(2), store.Lengths()[isa.RegisterString])
	})

	t.Run("a second reservation starts where the first ended", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		request := program.SlotAllocation{}
		request[isa.RegisterInt] = 3

		store.ReserveSlots(request)
		bases := store.ReserveSlots(request)

		require.Equal(t, uint16(3), bases[isa.RegisterInt], "the base is the length before growth")
		require.Equal(t, uint16(6), store.Lengths()[isa.RegisterInt])
	})

	t.Run("reserved slots read back as their zero value", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		request := program.SlotAllocation{}
		request[isa.RegisterInt] = 2
		request[isa.RegisterString] = 1

		store.ReserveSlots(request)

		require.Equal(t, int64(0), store.GetInt(0))
		require.Empty(t, store.GetString(0))
	})

	t.Run("an empty reservation leaves the store untouched", func(t *testing.T) {
		t.Parallel()
		store := NewGlobalStore()
		store.AllocInt(1)

		bases := store.ReserveSlots(program.SlotAllocation{})

		require.Equal(t, uint16(1), bases[isa.RegisterInt])
		require.Equal(t, uint16(1), store.Lengths()[isa.RegisterInt])
	})
}

func TestSnapshotVarReadsTheSlotInItsOwnType(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	intIndex := store.AllocInt(42)
	floatIndex := store.AllocFloat(1.5)
	stringIndex := store.AllocString("pipit")
	boolIndex := store.AllocBool(true)
	uintIndex := store.AllocUint(7)
	generalIndex := store.AllocGeneral(reflect.ValueOf([]int{1}))

	tests := []struct {
		check func(t *testing.T, got reflect.Value)
		name  string
		slot  program.GlobalVariableInfo
	}{
		{name: "an integer slot", slot: program.GlobalVariableInfo{Index: intIndex, Kind: isa.RegisterInt},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, int64(42), got.Int()) }},
		{name: "a float slot", slot: program.GlobalVariableInfo{Index: floatIndex, Kind: isa.RegisterFloat},
			check: func(t *testing.T, got reflect.Value) { require.InDelta(t, 1.5, got.Float(), 0) }},
		{name: "a string slot", slot: program.GlobalVariableInfo{Index: stringIndex, Kind: isa.RegisterString},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, "pipit", got.String()) }},
		{name: "a boolean slot", slot: program.GlobalVariableInfo{Index: boolIndex, Kind: isa.RegisterBool},
			check: func(t *testing.T, got reflect.Value) { require.True(t, got.Bool()) }},
		{name: "an unsigned slot", slot: program.GlobalVariableInfo{Index: uintIndex, Kind: isa.RegisterUint},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, uint64(7), got.Uint()) }},
		{name: "a general slot", slot: program.GlobalVariableInfo{Index: generalIndex, Kind: isa.RegisterGeneral},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 1, got.Len()) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := store.SnapshotVar(tt.slot)

			require.True(t, ok)
			tt.check(t, got)
		})
	}
}

func TestSnapshotVarRefusesSlotsOutsideTheBank(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	store.AllocInt(42)

	tests := []struct {
		name string
		slot program.GlobalVariableInfo
	}{
		{name: "an index past the integer bank", slot: program.GlobalVariableInfo{Index: 5, Kind: isa.RegisterInt}},
		{name: "a negative index", slot: program.GlobalVariableInfo{Index: -1, Kind: isa.RegisterInt}},
		{name: "an index into an empty bank", slot: program.GlobalVariableInfo{Index: 0, Kind: isa.RegisterString}},
		{name: "an indirect slot with no cell", slot: program.GlobalVariableInfo{Index: 0, Kind: isa.RegisterGeneral, IsIndirect: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := store.SnapshotVar(tt.slot)

			require.False(t, ok, "a slot outside the bank must be refused rather than read")
		})
	}
}

func TestSnapshotVarAsConvertsToTheRequestedType(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	intIndex := store.AllocInt(42)

	t.Run("an integer slot converts to a named integer type", func(t *testing.T) {
		t.Parallel()
		got, ok := store.SnapshotVarAs(program.GlobalVariableInfo{Index: intIndex, Kind: isa.RegisterInt}, reflect.TypeFor[globalNamedInt]())

		require.True(t, ok)
		require.Equal(t, reflect.TypeFor[globalNamedInt](), got.Type())
		require.Equal(t, int64(42), got.Int())
	})

	t.Run("a nil target type is refused", func(t *testing.T) {
		t.Parallel()
		_, ok := store.SnapshotVarAs(program.GlobalVariableInfo{Index: intIndex, Kind: isa.RegisterInt}, nil)

		require.False(t, ok)
	})

	t.Run("a slot outside the bank is refused", func(t *testing.T) {
		t.Parallel()
		_, ok := store.SnapshotVarAs(program.GlobalVariableInfo{Index: 99, Kind: isa.RegisterInt}, reflect.TypeFor[int64]())

		require.False(t, ok)
	})
}

func TestGlobalStoreResetEmptiesEveryBank(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	store.AllocInt(1)
	store.AllocFloat(1)
	store.AllocString("a")
	store.AllocBool(true)
	store.AllocUint(1)
	store.AllocGeneral(reflect.ValueOf(1))
	store.AllocComplex(1)

	store.Reset()

	require.Equal(t, program.SlotAllocation{}, store.Lengths(), "a reset store holds no slots")

	require.Equal(t, 0, store.AllocInt(9), "allocation restarts at slot zero after a reset")
	require.Equal(t, int64(9), store.GetInt(0))
}

func TestGlobalStoreSharedModeTakesTheLockedPath(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	intIndex := store.AllocInt(1)
	floatIndex := store.AllocFloat(1.5)
	stringIndex := store.AllocString("s")
	boolIndex := store.AllocBool(true)
	uintIndex := store.AllocUint(2)
	complexIndex := store.AllocComplex(3i)
	generalIndex := store.AllocGeneral(reflect.ValueOf("boxed"))

	store.markShared()

	require.Equal(t, int64(1), store.GetInt(intIndex), "a shared store reads the same values through the lock")
	require.InDelta(t, 1.5, store.GetFloat(floatIndex), 0)
	require.Equal(t, "s", store.GetString(stringIndex))
	require.True(t, store.GetBool(boolIndex))
	require.Equal(t, uint64(2), store.GetUint(uintIndex))
	require.Equal(t, 3i, store.getComplex(complexIndex))
	require.Equal(t, "boxed", store.GetGeneral(generalIndex).Interface())

	store.SetInt(intIndex, 4)
	store.SetFloat(floatIndex, 2.5)
	store.SetString(stringIndex, "t")
	store.SetBool(boolIndex, false)
	store.SetUint(uintIndex, 5)
	store.setComplex(complexIndex, 4i)
	store.SetGeneral(generalIndex, reflect.ValueOf("replaced"))

	require.Equal(t, int64(4), store.GetInt(intIndex))
	require.InDelta(t, 2.5, store.GetFloat(floatIndex), 0)
	require.Equal(t, "t", store.GetString(stringIndex))
	require.False(t, store.GetBool(boolIndex))
	require.Equal(t, uint64(5), store.GetUint(uintIndex))
	require.Equal(t, 4i, store.getComplex(complexIndex))
	require.Equal(t, "replaced", store.GetGeneral(generalIndex).Interface())
}

func TestGlobalStoreComplexBankRoundTrips(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	index := store.AllocComplex(1 + 2i)

	require.Equal(t, 1+2i, store.getComplex(index))

	store.setComplex(index, 3-4i)
	require.Equal(t, 3-4i, store.getComplex(index))
}

func TestRegisterExternalMethodBridgesAcrossPackages(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	root := program.NewNamedFunction("root")

	t.Run("a registered key resolves to its owner", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		store.RegisterExternalMethod("Score.String", root, 3)

		entry, ok := store.lookupExternalMethod("Score.String")
		require.True(t, ok)
		require.Same(t, root, entry.rootFunction)
		require.Equal(t, uint16(3), entry.methodIndex)
	})

	t.Run("an unregistered key resolves to nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := store.lookupExternalMethod("Score.Absent")
		require.False(t, ok)
	})

	t.Run("a malformed registration is ignored", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		store.RegisterExternalMethod("", root, 0)
		store.RegisterExternalMethod("Score.String", nil, 0)

		_, ok := store.lookupExternalMethod("Score.String")
		require.False(t, ok)
	})

	t.Run("a nil store resolves to nothing", func(t *testing.T) {
		t.Parallel()

		var absent *GlobalStore
		absent.RegisterExternalMethod("Score.String", root, 0)

		_, ok := absent.lookupExternalMethod("Score.String")
		require.False(t, ok)
	})

	t.Run("a later registration wins", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		later := program.NewNamedFunction("later")
		store.RegisterExternalMethod("Score.String", root, 1)
		store.RegisterExternalMethod("Score.String", later, 2)

		entry, ok := store.lookupExternalMethod("Score.String")
		require.True(t, ok)
		require.Same(t, later, entry.rootFunction)
	})
}

func TestRegisterUserNamedInterfaceKeepsTheIdentity(t *testing.T) {
	t.Parallel()

	identity := &typemodel.Type{}

	t.Run("a registered name resolves to its identity", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		store.RegisterUserNamedInterface("main.Shape", identity)

		found, ok := store.lookupUserNamedInterface("main.Shape")
		require.True(t, ok)
		require.Same(t, identity, found)
	})

	t.Run("an unregistered name resolves to nothing", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()

		_, ok := store.lookupUserNamedInterface("main.Absent")
		require.False(t, ok)
	})

	t.Run("a malformed registration is ignored", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		store.RegisterUserNamedInterface("", identity)
		store.RegisterUserNamedInterface("main.Shape", nil)

		_, ok := store.lookupUserNamedInterface("main.Shape")
		require.False(t, ok)
	})

	t.Run("an empty name resolves to nothing", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		store.RegisterUserNamedInterface("main.Shape", identity)

		_, ok := store.lookupUserNamedInterface("")
		require.False(t, ok)
	})
}

func TestRecordGoroutinePanicKeepsTheFirstOne(t *testing.T) {
	t.Parallel()

	store := NewGlobalStore()
	require.Nil(t, store.goroutinePanic.Load())
	require.Nil(t, store.goroutinePanicWakeChan(), "an unshared store can have no sibling goroutine")

	store.recordGoroutinePanic("first", "stack one")
	store.recordGoroutinePanic("second", "stack two")

	info := store.goroutinePanic.Load()
	require.NotNil(t, info)
	require.Equal(t, "first", info.Value(), "the first panic wins so the wake signal closes once")

	store.markShared()
	require.NotNil(t, store.goroutinePanicWakeChan())

	select {
	case <-store.goroutinePanicWakeChan():
	default:
		require.Fail(t, "the wake channel must be closed once a panic is recorded")
	}
}

func TestSnapshotVarAsFollowsAnIndirectSlot(t *testing.T) {
	t.Parallel()

	t.Run("an indirect slot reads through the cell", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		promoted := int64(7)
		index := store.AllocGeneral(reflect.ValueOf(&promoted))
		slot := program.GlobalVariableInfo{Index: index, Kind: isa.RegisterGeneral, IsIndirect: true}

		value, ok := store.SnapshotVarAs(slot, reflect.TypeFor[int64]())
		require.True(t, ok)
		require.Equal(t, int64(7), value.Interface())
	})

	t.Run("an indirect slot converts the pointee", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		promoted := int64(7)
		index := store.AllocGeneral(reflect.ValueOf(&promoted))
		slot := program.GlobalVariableInfo{Index: index, Kind: isa.RegisterGeneral, IsIndirect: true}

		value, ok := store.SnapshotVarAs(slot, reflect.TypeFor[int32]())
		require.True(t, ok)
		require.Equal(t, int32(7), value.Interface())
	})

	t.Run("an indirect slot past the bank reads nothing", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		slot := program.GlobalVariableInfo{Index: 9, Kind: isa.RegisterGeneral, IsIndirect: true}

		_, ok := store.SnapshotVarAs(slot, reflect.TypeFor[int64]())
		require.False(t, ok)
	})

	t.Run("an indirect slot holding no pointer reads nothing", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		index := store.AllocGeneral(reflect.ValueOf(7))
		slot := program.GlobalVariableInfo{Index: index, Kind: isa.RegisterGeneral, IsIndirect: true}

		_, ok := store.SnapshotVarAs(slot, reflect.TypeFor[int64]())
		require.False(t, ok)
	})

	t.Run("an indirect slot holding a nil pointer reads nothing", func(t *testing.T) {
		t.Parallel()

		store := NewGlobalStore()
		index := store.AllocGeneral(reflect.ValueOf((*int64)(nil)))
		slot := program.GlobalVariableInfo{Index: index, Kind: isa.RegisterGeneral, IsIndirect: true}

		_, ok := store.SnapshotVarAs(slot, reflect.TypeFor[int64]())
		require.False(t, ok)
	})
}

func TestSnapshotGeneralSlotAsTypesTheZeroSlot(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", snapshotGeneralSlotAs(reflect.Value{}, reflect.TypeFor[string]()).Interface(),
		"a slot that was allocated but never written still has to answer with a usable value")
	require.Equal(t, int64(4), snapshotGeneralSlotAs(reflect.ValueOf(int32(4)), reflect.TypeFor[int64]()).Interface())
	require.Equal(t, "s", snapshotGeneralSlotAs(reflect.ValueOf("s"), reflect.TypeFor[string]()).Interface())
	require.Equal(t, []int{1}, snapshotGeneralSlotAs(reflect.ValueOf([]int{1}), reflect.TypeFor[string]()).Interface(),
		"an impossible conversion must not be forced")
}
