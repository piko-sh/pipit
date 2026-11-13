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
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/symtab"
)

const recoverRoundTripSource = `package main

func guarded(divisor int) (result int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = -1
		}
	}()
	return 100 / divisor
}

func EntrypointRun() int { return guarded(0) }
`

func TestRecoverSurvivesBytecodeRoundTrip(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store, err := rootfs.Open(directory)
	require.NoError(t, err)
	bytecodeStore := adapters.NewBytecodeStore(store)

	service := newTestService(t, WithBytecodeStore(bytecodeStore))
	compiled, err := service.CompileFileSet(t.Context(),
		map[string]string{"main.go": recoverRoundTripSource})
	require.NoError(t, err)

	direct, err := service.ExecuteEntrypoint(t.Context(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, -1, direct, "the compiled program recovers from the division panic")

	require.NoError(t, service.SaveCompiled(t.Context(), "recover-round-trip", compiled))
	loaded, err := service.LoadCompiled(t.Context(), "recover-round-trip")
	require.NoError(t, err)

	reloaded, err := service.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, -1, reloaded, "recover must work identically in a loaded bundle")

	recoveringFunctions := 0
	for _, function := range program.ExportFunctions(loaded.Root()) {
		if !function.HasRecover {
			continue
		}
		recoveringFunctions++
	}
	require.Equalf(t, 2, recoveringFunctions,
		"guarded and its deferred closure both call recover, so both must carry HasRecover\n"+
			"after unpacking. A lower count means the codec dropped the flag.")
}

const receiverRoundTripSource = `package main

type counter struct{ n int }

func (c *counter) Add(delta int) int {
	c.n += delta
	return c.n
}

func EntrypointRun() int {
	c := &counter{}
	c.Add(2)
	double := func(v int) int { return v * 2 }
	var boxed any = double
	if _, isFunc := boxed.(func(int) int); !isFunc {
		return -1
	}
	if _, wrongShape := boxed.(func(string) int); wrongShape {
		return -2
	}
	return c.Add(3)
}
`

func TestReceiverAndSignatureSurviveBytecodeRoundTrip(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store, err := rootfs.Open(directory)
	require.NoError(t, err)
	bytecodeStore := adapters.NewBytecodeStore(store)

	service := newTestService(t, WithBytecodeStore(bytecodeStore))
	compiled, err := service.CompileFileSet(t.Context(),
		map[string]string{"main.go": receiverRoundTripSource})
	require.NoError(t, err)

	direct, err := service.ExecuteEntrypoint(t.Context(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, 5, direct,
		"the closure must assert to its own func type and not to a differently shaped one")

	require.NoError(t, service.SaveCompiled(t.Context(), "receiver-round-trip", compiled))
	loaded, err := service.LoadCompiled(t.Context(), "receiver-round-trip")
	require.NoError(t, err)

	reloaded, err := service.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, direct, reloaded,
		"a loaded bundle must keep both the receiver marking and closure signature identity")

	methods := 0
	signatures := 0
	for _, function := range program.ExportFunctions(loaded.Root()) {
		if function.HasReceiver {
			methods++
		}
		if function.SignatureReflectType != nil {
			signatures++
		}
	}
	require.Positivef(t, signatures,
		"a loaded function must carry its recorded signature type; without it a closure has no\n"+
			"func identity, so a type assertion, a type switch, reflect.TypeOf and %%T all miss it.")
	require.Equalf(t, 1, methods,
		"Add is the only method, and its receiver must still be marked one after unpacking.\n"+
			"A zero count means the codec dropped HasReceiver, which makes every method of a\n"+
			"loaded module look like it takes one argument too many and lose the Format, Error\n"+
			"and String adapters fmt dispatches through.")
}

func TestLoadedBytecodeHasItsDerivedStateBuiltBeforeItRuns(t *testing.T) {
	t.Parallel()

	store, err := rootfs.Open(t.TempDir())
	require.NoError(t, err)
	service := newTestService(t, WithBytecodeStore(adapters.NewBytecodeStore(store)))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": receiverRoundTripSource})
	require.NoError(t, err)
	require.NoError(t, service.SaveCompiled(t.Context(), "derived-state", compiled))

	loaded, err := service.LoadCompiled(t.Context(), "derived-state")
	require.NoError(t, err)
	require.True(t, loaded.Root().PrecomputedAllocCountsValid)
	for _, function := range program.ExportFunctions(loaded.Root()) {
		require.True(t, function.PrecomputedAllocCountsValid,
			"%s: goroutines a loaded program starts must not build this state at once", function.Name)
	}
}

func newRoundTripService(t *testing.T, store program.BytecodeStorePort) *Service {
	t.Helper()
	service := newTestService(t, WithBytecodeStore(store))
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{
		"fmt":     {"Sprint": reflect.ValueOf(fmt.Sprint)},
		"reflect": {"TypeOf": reflect.ValueOf(reflect.TypeOf), "Type": reflect.ValueOf((*reflect.Type)(nil))},
		"strings": {"Join": reflect.ValueOf(strings.Join), "Builder": reflect.ValueOf((*strings.Builder)(nil))},
	}))
	return service
}

const globalsRoundTripSource = `package main

import "strings"

var counter = 40

var names = []string{"a", "b"}

var log strings.Builder

func bump() int {
	counter++
	log.WriteString("+")
	return counter
}

func EntrypointRun() string {
	bump()
	return strings.Join(names, ",") + ":" + log.String() + ":" + string(rune('0'+bump()-40))
}
`

func TestBytecodeWithPackageVariablesRunsInAServiceThatDidNotCompileIt(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := newRoundTripService(t, store)
	padding, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": `package main

var first, second = 1, "x"

func EntrypointRun() int { return first + len(second) }
`})
	require.NoError(t, err)
	require.NotNil(t, padding)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": globalsRoundTripSource})
	require.NoError(t, err)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "globals", compiled))

	fresh := newRoundTripService(t, store)
	loaded, err := fresh.LoadCompiled(t.Context(), "globals")
	require.NoError(t, err)
	for range 2 {
		result, err := fresh.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
		require.NoError(t, err)
		require.Equal(t, "a,b:+:2", result, "each run initialises the package variables again")
	}

	again, err := compiler.LoadCompiled(t.Context(), "globals")
	require.NoError(t, err)
	result, err := compiler.ExecuteEntrypoint(t.Context(), again, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "a,b:+:2", result, "the compiling service runs its own bytecode too")
	direct, err := compiler.ExecuteEntrypoint(t.Context(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "a,b:+:2", direct, "the loaded copy has slots of its own")
}

func TestALoadedProgramsGlobalsMoveIntoSlotsReservedForIt(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := newRoundTripService(t, store)
	_, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": `package main

var a, b, c = 1, 2, 3

func EntrypointRun() int { return a + b + c }
`})
	require.NoError(t, err)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": globalsRoundTripSource})
	require.NoError(t, err)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "moved", compiled))

	fresh := newRoundTripService(t, store)
	before := fresh.globals.Lengths()
	loaded, err := fresh.LoadCompiled(t.Context(), "moved")
	require.NoError(t, err)
	low, count, err := program.GlobalOperandExtent(loaded.Root(), loaded.VariableInitFunction())
	require.NoError(t, err)
	require.NotZero(t, count, "the program uses package variables")
	after := fresh.globals.Lengths()
	bases := loaded.Root().GlobalBases
	require.NotNil(t, bases)
	for kind := range program.NumGlobalRegisterKinds {
		if count[kind] == 0 {
			continue
		}
		first := int(low[kind]) + int(bases[kind])
		require.GreaterOrEqual(t, first, int(before[kind]), "kind %d starts in the reserved region", kind)
		require.LessOrEqual(t, first+int(count[kind]), int(after[kind]), "kind %d ends in the reserved region", kind)
	}
	require.Same(t, bases, loaded.VariableInitFunction().GlobalBases, "the initialiser shares the bases")
}

const namedInterfaceRoundTripSource = `package main

import (
	"fmt"
	"reflect"
)

type Namer interface{ Name() string }

type Plain struct{}

type Base struct{}

func (Base) Name() string { return "base" }

func EntrypointRun() string {
	namer := reflect.TypeOf((*Namer)(nil)).Elem()
	return fmt.Sprint(namer.NumMethod(), reflect.TypeOf(Base{}).Implements(namer), reflect.TypeOf(Plain{}).Implements(namer))
}
`

func TestANamedInterfaceKeepsItsMethodsInAServiceThatDidNotCompileIt(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := newRoundTripService(t, store)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": namedInterfaceRoundTripSource})
	require.NoError(t, err)
	direct, err := compiler.ExecuteEntrypoint(t.Context(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "1 true false", direct)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "namer", compiled))
	require.Equal(t, []program.NamedInterface{{PackagePath: "main", Name: "Namer", Methods: []string{"Name"}}},
		compiled.NamedInterfaces())

	fresh := newRoundTripService(t, store)
	loaded, err := fresh.LoadCompiled(t.Context(), "namer")
	require.NoError(t, err)
	result, err := fresh.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "1 true false", result)
}

const oneGlobalSource = `package main

var counter = 1

func EntrypointRun() int {
	counter++
	return counter
}
`

func saveWithAllocation(t *testing.T, store program.BytecodeStorePort, key, source string, allocation program.SlotAllocation) {
	t.Helper()
	compiler := newRoundTripService(t, store)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	compiled.SetSlotAllocation(allocation)
	require.NoError(t, compiler.SaveCompiled(t.Context(), key, compiled))
}

func intSlots(count uint16) program.SlotAllocation {
	var allocation program.SlotAllocation
	allocation[isa.RegisterInt64] = count
	return allocation
}

func TestRepeatedLoadsOfOneProgramShareTheirSlots(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "large", oneGlobalSource, intSlots(30000))
	service := newRoundTripService(t, store)
	var first *program.CompiledFileSet
	for range 4 {
		loaded, err := service.LoadCompiled(t.Context(), "large")
		require.NoError(t, err, "loading the same bytecode again reuses its reservation")
		if first == nil {
			first = loaded
		}
		require.Equal(t, *first.Root().GlobalBases, *loaded.Root().GlobalBases)
		result, err := service.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
		require.NoError(t, err)
		require.EqualValues(t, 2, result)
	}
	require.Equal(t, uint16(30000), service.globals.Lengths()[isa.RegisterInt64])
}

func TestALoadThatWouldPassTheSlotLimitIsRefused(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "first", oneGlobalSource, intSlots(40000))
	saveWithAllocation(t, store, "second", oneGlobalSource+"\nvar other = 2\n", intSlots(40000))
	service := newRoundTripService(t, store)
	_, err := service.LoadCompiled(t.Context(), "first")
	require.NoError(t, err)
	_, err = service.LoadCompiled(t.Context(), "second")
	require.ErrorIs(t, err, fault.ErrGlobalSlotsExhausted)
	require.ErrorContains(t, err, "int64", "the error names the kind, not its number")
	require.Equal(t, uint16(40000), service.globals.Lengths()[isa.RegisterInt64], "a refused load reserves nothing")
}

func TestADeclaredAllocationSmallerThanTheOperandsIsRefused(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "short", `package main

var a, b, c = 1, 2, 3

func EntrypointRun() int { return a + b + c }
`, intSlots(1))
	service := newRoundTripService(t, store)
	before := service.globals.Lengths()
	loaded, err := service.LoadCompiled(t.Context(), "short")
	require.ErrorIs(t, err, fault.ErrBytecodeVerification)
	require.Nil(t, loaded)
	require.Equal(t, before, service.globals.Lengths(), "nothing is reserved for a refused file")
}

func TestConcurrentLoadsNeverPassTheSlotLimit(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	keys := []string{"a", "b", "c", "d"}
	for i, key := range keys {
		saveWithAllocation(t, store, key, oneGlobalSource+fmt.Sprintf("\nvar pad%d = %d\n", i, i), intSlots(20000))
	}
	service := newRoundTripService(t, store)
	var group sync.WaitGroup
	errs := make([]error, len(keys))
	for i, key := range keys {
		group.Go(func() {
			_, errs[i] = service.LoadCompiled(t.Context(), key)
		})
	}
	group.Wait()
	refused := 0
	for _, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, fault.ErrGlobalSlotsExhausted)
			refused++
		}
	}
	require.Equal(t, 1, refused, "three loads fit and the fourth is refused")
	require.Equal(t, uint16(60000), service.globals.Lengths()[isa.RegisterInt64])
}

func TestUnloadReturnsSlotsOnceNoRunHoldsThem(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "first", oneGlobalSource, intSlots(40000))
	saveWithAllocation(t, store, "second", oneGlobalSource+"\nvar other = 2\n", intSlots(40000))
	service := newRoundTripService(t, store)
	first, err := service.LoadCompiled(t.Context(), "first")
	require.NoError(t, err)

	endRun, err := service.beginLoadedRun(first)
	require.NoError(t, err)
	require.NoError(t, service.UnloadCompiled(first))
	_, err = service.LoadCompiled(t.Context(), "second")
	require.ErrorIs(t, err, fault.ErrGlobalSlotsExhausted, "a running program keeps its slots")
	endRun()

	second, err := service.LoadCompiled(t.Context(), "second")
	require.NoError(t, err, "the slots return once the run ends")
	require.Equal(t, uint16(40000), service.globals.Lengths()[isa.RegisterInt64], "the freed region is reused")
	result, err := service.ExecuteEntrypoint(t.Context(), second, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, 2, result)

	_, err = service.ExecuteEntrypoint(t.Context(), first, "EntrypointRun")
	require.ErrorIs(t, err, ErrProgramUnloaded)
	require.NoError(t, service.UnloadCompiled(first), "a second unload is a no-op")
}

func TestUnloadRefusesAProgramThisServiceDidNotLoad(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": oneGlobalSource})
	require.NoError(t, err)
	require.ErrorIs(t, service.UnloadCompiled(compiled), ErrProgramNotLoaded)
	require.ErrorIs(t, service.UnloadCompiled(nil), ErrProgramNotLoaded)
}

func TestFreedSlotsAreZeroedBeforeReuse(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "writer", `package main

var counter int

func EntrypointRun() int {
	counter += 7
	return counter
}
`, intSlots(1))
	saveWithAllocation(t, store, "reader", `package main

var counter int

func EntrypointRun() int { return counter }
`, intSlots(1))
	service := newRoundTripService(t, store)
	writer, err := service.LoadCompiled(t.Context(), "writer")
	require.NoError(t, err)
	_, err = service.ExecuteEntrypoint(t.Context(), writer, "EntrypointRun")
	require.NoError(t, err)
	slot := int(writer.Root().GlobalBases[isa.RegisterInt64])
	require.EqualValues(t, 7, service.globals.GetInt(slot))
	require.NoError(t, service.UnloadCompiled(writer))
	require.Zero(t, service.globals.GetInt(slot), "the freed slot is cleared")
	reader, err := service.LoadCompiled(t.Context(), "reader")
	require.NoError(t, err)
	require.Equal(t, *writer.Root().GlobalBases, *reader.Root().GlobalBases, "the reader takes the writer's freed slot")
	result, err := service.ExecuteEntrypoint(t.Context(), reader, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, 0, result, "the reused slot starts at zero")
}

func TestResetInvalidatesLoadedPrograms(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "program", oneGlobalSource, intSlots(1))
	service := newRoundTripService(t, store)
	loaded, err := service.LoadCompiled(t.Context(), "program")
	require.NoError(t, err)
	service.Reset()
	_, err = service.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.ErrorIs(t, err, ErrProgramUnloaded, "the reset store no longer holds its slots")
	again, err := service.LoadCompiled(t.Context(), "program")
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), again, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, 2, result)
}

const otherNamerSource = `package main

import (
	"fmt"
	"reflect"
)

type Namer interface {
	Name() string
	Age() int
}

func EntrypointRun() string {
	return fmt.Sprint(reflect.TypeOf((*Namer)(nil)).Elem().NumMethod())
}
`

func TestALoadedInterfaceCannotRedefineAnotherProgramsInterface(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := newRoundTripService(t, store)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": namedInterfaceRoundTripSource})
	require.NoError(t, err)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "namer", compiled))

	host := newRoundTripService(t, store)
	own, err := host.CompileFileSet(t.Context(), map[string]string{"main.go": otherNamerSource})
	require.NoError(t, err)
	loaded, err := host.LoadCompiled(t.Context(), "namer")
	require.ErrorIs(t, err, fault.ErrBytecodeVerification, "a conflicting main.Namer is refused")
	require.ErrorContains(t, err, "main.Namer")
	require.Nil(t, loaded)
	result, err := host.ExecuteEntrypoint(t.Context(), own, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "2", result, "the host's own interface keeps its methods")
}

func TestSaveRecordsTheInterfacesOfTheProgramItSaves(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := newRoundTripService(t, store)
	first, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": namedInterfaceRoundTripSource})
	require.NoError(t, err)
	_, err = compiler.CompileFileSet(t.Context(), map[string]string{"main.go": otherNamerSource})
	require.NoError(t, err)
	recorded := slices.Clone(first.NamedInterfaces())
	require.Equal(t, []program.NamedInterface{{PackagePath: "main", Name: "Namer", Methods: []string{"Name"}}}, recorded,
		"the compiler records the program's own interfaces when it compiles it")
	require.NoError(t, compiler.SaveCompiled(t.Context(), "first", first))
	require.Equal(t, recorded, first.NamedInterfaces(), "saving does not change the file set")

	fresh := newRoundTripService(t, store)
	loaded, err := fresh.LoadCompiled(t.Context(), "first")
	require.NoError(t, err)
	result, err := fresh.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "1 true false", result)
}

func TestLoadCompiledStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "program", oneGlobalSource, intSlots(1))
	service := newRoundTripService(t, store)
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("stop"))
	loaded, err := service.LoadCompiled(ctx, "program")
	require.Error(t, err)
	require.Nil(t, loaded)
	require.Equal(t, program.SlotAllocation{}, service.globals.Lengths(), "a cancelled load reserves nothing")
}

func TestPointerStaticTypeNamesKeepOnlyQualifiedNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typeString string
		want       string
	}{
		{typeString: "*main.Namer", want: "main.Namer"},
		{typeString: "**main.Namer", want: "main.Namer"},
		{typeString: "*example.com/x/y.Namer", want: "example.com/x/y.Namer"},
		{typeString: "*main.Box[int]", want: "main.Box[int]"},
		{typeString: "*map[a.B]C", want: ""},
		{typeString: "*[]main.T", want: ""},
		{typeString: "*func(a.B)", want: ""},
		{typeString: "*int", want: ""},
		{typeString: "main.Namer", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.typeString, func(t *testing.T) {
			t.Parallel()
			name, ok := pointeeQualifiedName(tt.typeString)
			require.Equal(t, tt.want != "", ok)
			require.Equal(t, tt.want, name)
		})
	}
}

func TestEveryDeclaredInterfaceIsRecorded(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": `package main

type Namer interface{ Name() string }

type Aged interface {
	Namer
	Age() int
}

type Box[T any] interface{ Get() T }

type notAnInterface struct{}

func EntrypointRun() int { return 1 }
`})
	require.NoError(t, err)
	require.Equal(t, []program.NamedInterface{
		{PackagePath: "main", Name: "Aged", Methods: []string{"Age", "Name"}},
		{PackagePath: "main", Name: "Namer", Methods: []string{"Name"}},
	}, compiled.NamedInterfaces(), "declared interfaces are recorded whether or not a call names them")
}

func TestADeclaredButUnusedConflictingInterfaceKeepsTheHostsEntry(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := newRoundTripService(t, store)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": `package main

type Namer interface{ Name() string }

func EntrypointRun() int { return 7 }
`})
	require.NoError(t, err)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "quiet", compiled))

	host := newRoundTripService(t, store)
	own, err := host.CompileFileSet(t.Context(), map[string]string{"main.go": otherNamerSource})
	require.NoError(t, err)
	loaded, err := host.LoadCompiled(t.Context(), "quiet")
	require.NoError(t, err, "a program that never reflects on its interface still loads")
	result, err := host.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, 7, result)
	ownResult, err := host.ExecuteEntrypoint(t.Context(), own, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "2", ownResult, "the host's own interface is unchanged")
}

func TestTheLoadedInterfaceLimitIsEnforced(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := newRoundTripService(t, store)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": `package main

type A interface{ A() }

type B interface{ B() }

func EntrypointRun() int { return 1 }
`})
	require.NoError(t, err)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "two", compiled))

	limited := newTestService(t, WithBytecodeStore(store), WithMaxLoadedNamedInterfaces(1))
	_, err = limited.LoadCompiled(t.Context(), "two")
	require.ErrorIs(t, err, ErrNamedInterfaceLimit)

	invalid := newTestService(t, WithMaxLoadedNamedInterfaces(0))
	_, err = invalid.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n"})
	require.ErrorIs(t, err, ErrInvalidOption)
}

func manyGlobalsSource(count int) string {
	var source strings.Builder
	source.WriteString("package main\n\n")
	for i := range count {
		fmt.Fprintf(&source, "var g%d = %d\n", i, i)
	}
	source.WriteString("\nfunc EntrypointRun() int { g1++; return g1 }\n")
	return source.String()
}

func TestAServiceStaysUsableAfterCompilesExhaustItsSlots(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	saveWithAllocation(t, store, "small", oneGlobalSource, program.SlotAllocation{})
	service := newRoundTripService(t, store)
	source := manyGlobalsSource(8000)
	var compileErr error
	for range 10 {
		if _, compileErr = service.CompileFileSet(t.Context(), map[string]string{"main.go": source}); compileErr != nil {
			break
		}
	}
	require.Error(t, compileErr, "a compile past the slot limit fails")

	require.NotPanics(t, func() {
		_, _ = service.LoadCompiled(t.Context(), "small")
	}, "a store a failed compile overfilled reports an error rather than panicking")

	loaded, err := service.LoadCompiled(t.Context(), "small")
	require.NoError(t, err, "the failed compile's slots were given back")
	result, err := service.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.EqualValues(t, 2, result)
}
