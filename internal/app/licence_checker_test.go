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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/verify"
)

const licenceCheckerProgram = `
type T struct{ P *int }
func set(t *T, v *int) { t.P = v }
g := &T{}
x := 7
set(g, &x)
*g.P
`

const licenceCheckerChurnSource = `package main

import "host"

func churn(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		xs := make([]int, 256)
		xs[0] = i
		total += xs[0] & 1
	}
	return total
}

type Labeller struct{ N int }

func (l Labeller) String() string {
	churn(l.N)
	return "labeller"
}

func Run() int {
	done := make(chan int)
	go func() { done <- churn(4000) }()
	total := <-done
	total += host.Call(func() int { return churn(4000) })
	total += len(host.Label(Labeller{N: 4000}))
	return total
}
`

const licenceCheckerHeaderReuseSource = `package main

func AppendInto(xs [][]string, n int) int {
	ys := xs[:0]
	ys = append(ys, []string{"a", "b"})
	return len(ys)
}

func Run() int {
	return AppendInto(make([][]string, 0, 4), 1)
}
`

const licenceCheckerCallSource = `package main

type P struct{ X, Y int }

type H struct {
	A P
	B *int
}

func mk(k int) P { return P{k, k + 1} }

func keep(h *H) {}

func Field(k int) int {
	h := &H{A: mk(k)}
	return h.A.X + h.A.Y
}

func Passed(k int) int {
	x := k
	h := &H{}
	keep(h)
	h.B = &x
	return *h.B
}

func Kept(k int) int {
	x := k
	h := &H{}
	h.B = &x
	return *h.B
}

func Run() int { return Field(1) + Passed(2) + Kept(3) }
`

func plantWrongGrant(t *testing.T, root *program.CompiledFunction) (*program.CompiledFunction, int) {
	t.Helper()
	var found *program.CompiledFunction
	programCounter := -1
	var walk func(*program.CompiledFunction)
	walk = func(function *program.CompiledFunction) {
		if function == nil || found != nil {
			return
		}
		converted := verify.ConvertForLicenceChecker(function)
		for index, instr := range converted.Code {
			if instr.Kind == verify.CheckerStore && !instr.Granted {
				found, programCounter = function, index
				return
			}
		}
		for _, child := range function.Functions {
			walk(child)
		}
	}
	walk(root)
	require.NotNil(t, found, "the program has an ungranted container store")
	if found.FieldStoreArenaSafePCs == nil {
		found.FieldStoreArenaSafePCs = map[int]bool{}
	}
	found.FieldStoreArenaSafePCs[programCounter] = true
	return found, programCounter
}

func checkerVerdict(t *testing.T, converted *verify.CheckerProgram) verify.CheckerVerdict {
	t.Helper()
	verdict, err := converted.CheckLicenceGrantsVerdict(t.Context())
	require.NoError(t, err)
	return verdict
}

func TestRunLicenceCheckerReturnsTheCancellationCause(t *testing.T) {
	service := NewService()
	root, err := service.Compile(t.Context(), licenceCheckerProgram)
	require.NoError(t, err)
	stop := errors.New("compile abandoned")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop)

	require.ErrorIs(t, runLicenceChecker(ctx, root, true), stop)
}

func TestLicenceCheckerRejectsAPlantedWrongGrant(t *testing.T) {
	service := NewService()
	root, err := service.Compile(t.Context(), licenceCheckerProgram)
	require.NoError(t, err)
	function, programCounter := plantWrongGrant(t, root)
	require.Equal(t, verify.CheckerRejectedGrant, checkerVerdict(t, verify.ConvertForLicenceChecker(function)),
		"the planted grant at %s:%d is a wrong grant", function.Name, programCounter)
	require.ErrorIs(t, runLicenceChecker(t.Context(), root, true), errLicenceGrantsRejected)
}

func withLicenceVerdict(t *testing.T, verdict verify.CheckerVerdict) {
	t.Helper()
	forced := false
	licenceCheckerVerdictHook = func(ctx context.Context, _ *program.CompiledFunction, computed verify.CheckerVerdict) verify.CheckerVerdict {
		if forced || arenaPromotionOff(ctx) {
			return computed
		}
		forced = true
		return verdict
	}
	t.Cleanup(func() { licenceCheckerVerdictHook = nil })
}

func requireNoGrants(t *testing.T, root *program.CompiledFunction) {
	t.Helper()
	var walk func(*program.CompiledFunction)
	walk = func(function *program.CompiledFunction) {
		if function == nil {
			return
		}
		require.Empty(t, function.ArenaSafeAllocPCs, "%s has no alloc grant", function.Name)
		require.Empty(t, function.FieldStoreArenaSafePCs, "%s has no field-store grant", function.Name)
		require.Empty(t, function.InPlaceHeaderReusePCs, "%s has no header-reuse grant", function.Name)
		require.Empty(t, function.LocalContainerAllocPCs, "%s has no local-container allocation", function.Name)
		require.Empty(t, function.LocalContainerStorePCs, "%s has no local-container store grant", function.Name)
		walk(function.VariableInitFunction)
		for _, child := range function.Functions {
			walk(child)
		}
	}
	walk(root)
}

func TestLicenceCheckerRecompilesAProgramWhoseGrantsAreRejected(t *testing.T) {
	service := NewService()
	withLicenceVerdict(t, verify.CheckerRejectedGrant)
	before := engine.LicenceDiagnostics().LicenceCheck
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerHeaderReuseSource})
	require.NoError(t, err)
	after := engine.LicenceDiagnostics().LicenceCheck
	require.Equal(t, before.Recompiled+1, after.Recompiled, "the program was compiled again")
	require.Equal(t, before.Rejected, after.Rejected, "no defect: the program is not abandoned")
	requireNoGrants(t, fileSet.Root())

	result, err := service.ExecuteEntrypoint(t.Context(), fileSet, "Run")
	require.NoError(t, err)
	require.EqualValues(t, 1, result)
}

const licenceCheckerLocalMapSource = `package main

var names = []string{"ash", "birch", "cedar"}

func Fill(n int) map[string][]string {
	m := map[string][]string{}
	for i := 0; i < n; i++ {
		key := "k-" + names[i%3]
		m[key] = append(m[key], "v-"+names[i%2])
	}
	return m
}

func Other(n int) map[string][]string {
	m := map[string][]string{}
	for i := 0; i < n; i++ {
		key := "o-" + names[i%2]
		m[key] = append(m[key], "w-"+names[i%3])
	}
	return m
}

func Run() int { return len(Fill(12)) + len(Other(12)) }
`

func TestLicenceCheckerInfersLocalContainerGrants(t *testing.T) {
	service := NewService()
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerLocalMapSource})
	require.NoError(t, err)

	for _, name := range []string{"Fill", "Other"} {
		function := licenceCheckerFunction(t, fileSet.Root(), name)
		require.Equal(t, 1, function.LocalContainerAllocPCs.Count(), "%s makes one map", name)
		require.Equal(t, 1, function.LocalContainerStorePCs.Count(), "%s stores into it once", name)
		require.Equal(t, verify.CheckerAccepted, checkerVerdict(t, verify.ConvertForLicenceChecker(function)))
	}
	result, err := service.ExecuteEntrypoint(t.Context(), fileSet, "Run")
	require.NoError(t, err)
	require.EqualValues(t, 5, result)
}

func TestLicenceCheckerKeepsNoLocalContainerGrantOfAFunctionItRejects(t *testing.T) {
	service := NewService(withLicenceCheckerStrict(false))
	licenceCheckerVerdictHook = func(_ context.Context, function *program.CompiledFunction, computed verify.CheckerVerdict) verify.CheckerVerdict {
		if function.Name == "Fill" {
			return verify.CheckerRejectedInstruction
		}
		return computed
	}
	t.Cleanup(func() { licenceCheckerVerdictHook = nil })
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerLocalMapSource})
	require.NoError(t, err)

	rejected := licenceCheckerFunction(t, fileSet.Root(), "Fill")
	require.Empty(t, rejected.LocalContainerAllocPCs)
	require.Empty(t, rejected.LocalContainerStorePCs, "a function the checker rejects stores through the barrier")
	require.NotEmpty(t, licenceCheckerFunction(t, fileSet.Root(), "Other").LocalContainerStorePCs, "the others keep theirs")
	result, err := service.ExecuteEntrypoint(t.Context(), fileSet, "Run")
	require.NoError(t, err)
	require.EqualValues(t, 5, result)
}

func TestLicenceCheckerMakesNoLocalContainerGrantInARecompiledProgram(t *testing.T) {
	service := NewService()
	withLicenceVerdict(t, verify.CheckerRejectedGrant)
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerLocalMapSource})
	require.NoError(t, err)

	requireNoGrants(t, fileSet.Root())
	result, err := service.ExecuteEntrypoint(t.Context(), fileSet, "Run")
	require.NoError(t, err)
	require.EqualValues(t, 5, result)
}

func TestLicenceCheckerDefectFailsAStrictCompile(t *testing.T) {
	service := NewService(withLicenceCheckerStrict(true))
	withLicenceVerdict(t, verify.CheckerRejectedInstruction)
	_, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerHeaderReuseSource})
	require.ErrorIs(t, err, errLicenceCheckerDefect)
}

func TestLicenceCheckerDefectRunsAbandonedOutsideStrictBuilds(t *testing.T) {
	service := NewService(withLicenceCheckerStrict(false))
	withLicenceVerdict(t, verify.CheckerRejectedInstruction)
	before := engine.LicenceDiagnostics().LicenceCheck
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerHeaderReuseSource})
	require.NoError(t, err)
	require.Equal(t, before.Rejected+1, engine.LicenceDiagnostics().LicenceCheck.Rejected)

	_, err = service.ExecuteEntrypoint(t.Context(), fileSet, "Run")
	require.NoError(t, err)
}

func TestLicenceCheckerDefectIsLoggedOutsideStrictBuilds(t *testing.T) {
	var logged bytes.Buffer
	service := NewService(withLicenceCheckerStrict(false), WithLogger(slog.New(slog.NewTextHandler(&logged, nil))))
	withLicenceVerdict(t, verify.CheckerRejectedInstruction)
	_, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerHeaderReuseSource})
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(logged.String(), "level=WARN"), logged.String())
	require.Contains(t, logged.String(), "function=")
	require.Contains(t, logged.String(), "verdict=")
}

func churnCollections(t *testing.T, reject bool) map[string]int {
	t.Helper()
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
		"Call":  reflect.ValueOf(func(fn func() int) int { return fn() }),
		"Label": reflect.ValueOf(func(s fmt.Stringer) string { return s.String() }),
	}})
	service := NewService(withLicenceCheckerStrict(false))
	service.UseSymbols(symbols)
	if reject {
		withLicenceVerdict(t, verify.CheckerRejectedInstruction)
	}
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerChurnSource})
	require.NoError(t, err)
	var mu sync.Mutex
	counts := map[string]int{}
	restore := engine.SetMinorGCSiteHook(func() {
		stack := string(debug.Stack())
		kind := "main"
		switch {
		case strings.Contains(stack, "engine.runCompiledGoroutine"):
			kind = "goroutine"
		case strings.Contains(stack, "engine.(*boundMethodVM)"):
			kind = "bound-method"
		case strings.Contains(stack, "engine.(*closureWrapper)"):
			kind = "callback"
		}
		mu.Lock()
		counts[kind]++
		mu.Unlock()
	})
	defer restore()
	_, err = service.ExecuteEntrypoint(t.Context(), fileSet, "Run")
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	return maps.Clone(counts)
}

func TestLicenceCheckerDefectRunsEveryVMWithoutTheMinorCollection(t *testing.T) {
	accepted := churnCollections(t, false)
	for _, kind := range []string{"goroutine", "callback", "bound-method"} {
		require.Positive(t, accepted[kind], "test harness: an accepted program's %s collects (%v)", kind, accepted)
	}

	require.Empty(t, churnCollections(t, true), "no VM of a rejected program runs the minor collection")
}

func TestLicenceCheckerAcceptsAnInPlaceHeaderReuseGrant(t *testing.T) {
	service := NewService()
	before := engine.LicenceDiagnostics().LicenceCheck
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerHeaderReuseSource})
	require.NoError(t, err)
	after := engine.LicenceDiagnostics().LicenceCheck
	require.Equal(t, before.Programs+1, after.Programs, "the compile ran the checker")
	require.Equal(t, before.Rejected, after.Rejected, "the program is accepted")

	granted := 0
	var walk func(*program.CompiledFunction)
	walk = func(function *program.CompiledFunction) {
		if function == nil {
			return
		}
		if len(function.Body) > 0 {

			converted := verify.ConvertForLicenceChecker(function)
			for pc := range function.InPlaceHeaderReusePCs {
				granted++
				require.Less(t, pc, len(converted.Code))
				require.False(t, converted.Code[pc].Granted, "%s:%d converts as the ungranted append", function.Name, pc)
			}
			require.Equal(t, verify.CheckerAccepted, checkerVerdict(t, converted), "%s is accepted", function.Name)
		}
		walk(function.VariableInitFunction)
		for _, child := range function.Functions {
			walk(child)
		}
	}
	walk(fileSet.Root())
	require.Positive(t, granted, "the escape pass granted the append in-place header reuse")
}

func licenceCheckerFunction(t *testing.T, root *program.CompiledFunction, name string) *program.CompiledFunction {
	t.Helper()
	var found *program.CompiledFunction
	var walk func(*program.CompiledFunction)
	walk = func(function *program.CompiledFunction) {
		if function == nil || found != nil {
			return
		}
		if function.Name == name {
			found = function
			return
		}
		for _, child := range function.Functions {
			walk(child)
		}
	}
	walk(root)
	require.NotNil(t, found, "the program has a function %s", name)
	return found
}

func TestLicenceCheckerModelsACallByItsCallSite(t *testing.T) {
	service := NewService()
	before := engine.LicenceDiagnostics().LicenceCheck
	fileSet, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": licenceCheckerCallSource})
	require.NoError(t, err)
	after := engine.LicenceDiagnostics().LicenceCheck
	require.Equal(t, before.Rejected, after.Rejected, "the program is accepted")
	field := licenceCheckerFunction(t, fileSet.Root(), "Field")
	require.NotEmpty(t, field.FieldStoreArenaSafePCs, "the escape pass granted Field's stores")
	converted := verify.ConvertForLicenceChecker(field)
	calls := 0
	for pc, instr := range converted.Code {
		if row, ok := isa.SpecForInstruction(field.Body[pc]); ok && row.Calls() {
			calls++
			require.Equal(t, verify.CheckerOp, instr.Kind, "Field:%d (%s) converts over its call site", pc, row.Name)
		}
	}
	require.Positive(t, calls, "Field calls mk")
	require.Equal(t, verify.CheckerAccepted, checkerVerdict(t, converted), "Field's grants are accepted")
}

func TestLicenceCheckerRejectsAGrantOnAnObjectACallReceives(t *testing.T) {
	service := NewService()
	fileSet, err := service.CompileFileSet(compileWithoutArenaPromotion(t.Context()), map[string]string{"main.go": licenceCheckerCallSource})
	require.NoError(t, err)
	plant := func(name string) verify.CheckerVerdict {
		function := licenceCheckerFunction(t, fileSet.Root(), name)
		converted := verify.ConvertForLicenceChecker(function)
		store := -1
		for pc, instr := range converted.Code {
			if instr.Kind == verify.CheckerStore && !instr.Granted {
				store = pc
			}
		}
		require.GreaterOrEqual(t, store, 0, "%s stores into h", name)
		receiver := converted.Code[store].Receiver
		alloc := -1
		for pc := store - 1; pc >= 0 && alloc < 0; pc-- {
			instr := converted.Code[pc]
			if instr.Kind == verify.CheckerMove && instr.Destination == receiver || instr.Kind == verify.CheckerOp && slices.Contains(instr.Destinations, receiver) {
				alloc = pc
			}
		}
		require.GreaterOrEqual(t, alloc, 0, "%s sets h", name)
		converted.Code[alloc] = verify.CheckerInstr{Kind: verify.CheckerAlloc, Destination: receiver}
		converted.Code[store].Granted = true
		converted.Granted++
		return checkerVerdict(t, converted)
	}
	require.Equal(t, verify.CheckerAccepted, plant("Kept"), "with no call between them the planted grants hold")
	require.Equal(t, verify.CheckerRejectedGrant, plant("Passed"), "keep(h) hands h off, so the planted store grant is wrong")
}

func TestLicenceCheckerStrictnessFollowsTheTestDefault(t *testing.T) {
	require.True(t, NewService().licenceCheckerStrict(), "the package's TestMain makes services strict")
	SetLicenceCheckerStrictForTest(false)
	t.Cleanup(func() { SetLicenceCheckerStrictForTest(true) })
	require.False(t, NewService().licenceCheckerStrict(), "the default is off unless a test turns it on")
	require.True(t, NewService(withLicenceCheckerStrict(true)).licenceCheckerStrict())
}
