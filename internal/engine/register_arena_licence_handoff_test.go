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
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"pipit.sh/pipit/internal/isa"
)

var handOffPrimitiveCauseArgument = map[string]int{
	"noteHandOff":              1,
	"noteBoxedHandOff":         1,
	"noteHandOffString":        1,
	"noteHandOffSliceRegister": 1,
	"revokeRemainder":          1,
	"recordHandOff":            0,
	"recordHandOffShared":      0,
	"recordContainerStore":     0,
	"revokeHandOff":            0,
}

var handOffHelperCauses = map[string][]isa.HandOffCause{
	"noteNativeCallArguments":   nativeArgumentCauses(),
	"noteNativeScalarArguments": nativeArgumentCauses(),
	"noteStringCopy":            {isa.HandOffUntracedContainer},
	"noteReflectCopy":           {isa.HandOffUntracedContainer},
	"noteGoroutineSpawn":        {isa.HandOffSpawn},
	"noteNativeMethodArguments": nativeArgumentCauses(),
	"noteAppendIntoBacking":     {isa.HandOffUntracedContainer},
	"noteKeptString":            {isa.HandOffIdentityKept},
}

var handOffPlumbing = func() map[string]bool {
	plumbing := map[string]bool{}
	for name := range handOffPrimitiveCauseArgument {
		plumbing[name] = true
	}
	for name := range handOffHelperCauses {
		plumbing[name] = true
	}
	return plumbing
}()

type handOffSite struct {
	ID     string
	Causes []isa.HandOffCause
}

type engineCallGraph struct {
	byName  map[string][]*ssa.Function
	calls   map[*ssa.Function][]*ssa.Function
	sites   map[*ssa.Function][]handOffSite
	fileSet *token.FileSet
}

var copyBarrierPrefixes = []string{
	"materialiseString", "MaterialiseString", "materialiseArena", "MaterialiseArena", "copyReflectValue",
	"escapeArenaValueForGlobal", "cloneForeign", "materialiseSliceForFieldStore", "valueCopyForBoundary",
	"ValueCopyForBoundary", "walkArenaEscape", "MaterialiseAnyForArena", "VM.snapshotToBoundary",
}

var (
	handOffGraphOnce sync.Once
	handOffGraph     *engineCallGraph
	handOffGraphErr  error
)

var stringCopyBarrierPrefixes = []string{"materialiseString", "MaterialiseString"}

func nativeArgumentCauses() []isa.HandOffCause {
	return []isa.HandOffCause{
		isa.HandOffNativeArgUnknown, isa.HandOffNativeArgRetains,
		isa.HandOffNativeArgBorrows, isa.HandOffNativeArgResultAliases,
	}
}

func buildEngineCallGraph() (*engineCallGraph, error) {
	config := &packages.Config{Mode: packages.LoadSyntax, Tests: false}
	loaded, err := packages.Load(config, ".")
	if err != nil {
		return nil, err
	}
	if len(loaded) != 1 || len(loaded[0].Errors) != 0 {
		return nil, fmt.Errorf("the engine package must load cleanly: %v", loaded)
	}
	program, ssaPackages := ssautil.Packages(loaded, ssa.InstantiateGenerics)
	program.Build()
	engine := ssaPackages[0]
	graph := &engineCallGraph{
		byName:  map[string][]*ssa.Function{},
		calls:   map[*ssa.Function][]*ssa.Function{},
		sites:   map[*ssa.Function][]handOffSite{},
		fileSet: program.Fset,
	}
	functions := packageFunctions(program, engine)
	implementers := packageMethodsByName(program, engine)
	for _, function := range functions {
		graph.byName[ssaFunctionName(function)] = append(graph.byName[ssaFunctionName(function)], function)
		graph.scan(function, engine, implementers)
	}
	return graph, nil
}

func packageFunctions(program *ssa.Program, engine *ssa.Package) []*ssa.Function {
	var functions []*ssa.Function
	for function := range ssautil.AllFunctions(program) {
		if function.Package() == engine || (function.Origin() != nil && function.Origin().Package() == engine) {
			functions = append(functions, function)
		}
	}
	slices.SortFunc(functions, func(left, right *ssa.Function) int { return cmp.Compare(left.String(), right.String()) })
	return functions
}

func packageMethodsByName(program *ssa.Program, engine *ssa.Package) map[string][]*ssa.Function {
	methods := map[string][]*ssa.Function{}
	for _, member := range engine.Members {
		named, ok := member.(*ssa.Type)
		if !ok {
			continue
		}
		for _, receiver := range []types.Type{named.Type(), types.NewPointer(named.Type())} {
			set := program.MethodSets.MethodSet(receiver)
			for method := range set.Methods() {
				if method := program.MethodValue(method); method != nil && method.Package() == engine {
					methods[method.Name()] = append(methods[method.Name()], method)
				}
			}
		}
	}
	return methods
}

func ssaFunctionName(function *ssa.Function) string {
	if function.Origin() != nil {
		function = function.Origin()
	}
	if function.Parent() != nil {
		return ssaFunctionName(function.Parent())
	}
	if receiver := function.Signature.Recv(); receiver != nil {
		receiverType := receiver.Type()
		if pointer, ok := receiverType.(*types.Pointer); ok {
			receiverType = pointer.Elem()
		}
		if named, ok := receiverType.(*types.Named); ok {
			return named.Obj().Name() + "." + function.Name()
		}
	}
	return function.Name()
}

func baseName(function *ssa.Function) string {
	name := ssaFunctionName(function)
	if _, after, ok := strings.CutLast(name, "."); ok {
		return after
	}
	return name
}

func (g *engineCallGraph) scan(function *ssa.Function, engine *ssa.Package, implementers map[string][]*ssa.Function) {
	inPlumbing := handOffPlumbing[baseName(function)]
	g.calls[function] = append(g.calls[function], function.AnonFuncs...)
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			call, ok := instruction.(ssa.CallInstruction)
			if !ok {
				continue
			}
			common := call.Common()
			if common.IsInvoke() {
				g.calls[function] = append(g.calls[function], implementing(common, implementers)...)
				continue
			}
			callee := common.StaticCallee()
			if callee == nil || !inPackage(callee, engine) {
				continue
			}
			g.calls[function] = append(g.calls[function], callee)
			name := baseName(callee)
			if inPlumbing || !handOffPlumbing[name] {
				continue
			}
			position := g.fileSet.Position(call.Pos())
			g.sites[function] = append(g.sites[function], handOffSite{
				ID:     filepath.Base(position.Filename) + ":" + itoaPositive(position.Line) + "(" + ssaFunctionName(function) + ")",
				Causes: siteCauses(name, common),
			})
		}
	}
}

func implementing(common *ssa.CallCommon, implementers map[string][]*ssa.Function) []*ssa.Function {
	iface, ok := common.Value.Type().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	var candidates []*ssa.Function
	for _, method := range implementers[common.Method.Name()] {
		if types.Implements(method.Signature.Recv().Type(), iface) {
			candidates = append(candidates, method)
		}
	}
	return candidates
}

func inPackage(function *ssa.Function, engine *ssa.Package) bool {
	if function.Origin() != nil {
		function = function.Origin()
	}
	return function.Package() == engine
}

func itoaPositive(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for ; value > 0; value /= 10 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
	}
	return string(digits)
}

func siteCauses(callee string, call *ssa.CallCommon) []isa.HandOffCause {
	if causes, ok := handOffHelperCauses[callee]; ok {
		return causes
	}
	arguments := call.Args
	if call.Signature().Recv() != nil || strings.HasPrefix(call.Value.Name(), "(") {
		arguments = arguments[1:]
	} else if function, ok := call.Value.(*ssa.Function); ok && function.Signature.Recv() != nil {
		arguments = arguments[1:]
	}
	index := handOffPrimitiveCauseArgument[callee]
	if index >= len(arguments) {
		return nil
	}
	constant, ok := arguments[index].(*ssa.Const)
	if !ok || constant.Value == nil {
		return nil
	}
	return []isa.HandOffCause{isa.HandOffCause(constant.Uint64())}
}

func (g *engineCallGraph) reachable(roots ...string) map[*ssa.Function]bool {
	return g.reachableCut(nil, roots...)
}

func (g *engineCallGraph) reachableCut(cut map[string]bool, roots ...string) map[*ssa.Function]bool {
	seen := map[*ssa.Function]bool{}
	var stack []*ssa.Function
	for _, root := range roots {
		stack = append(stack, g.byName[root]...)
	}
	for len(stack) > 0 {
		function := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[function] {
			continue
		}
		seen[function] = true
		for _, callee := range g.calls[function] {
			if !cut[ssaFunctionName(callee)] || callee.Parent() != nil {
				stack = append(stack, callee)
			}
		}
	}
	return seen
}

func (g *engineCallGraph) reachableSites(cut map[string]bool, roots ...string) []handOffSite {
	var sites []handOffSite
	for function := range g.reachableCut(cut, roots...) {
		sites = append(sites, g.sites[function]...)
	}
	return sortSites(sites)
}

func (g *engineCallGraph) allSites() []handOffSite {
	var sites []handOffSite
	for _, list := range g.sites {
		sites = append(sites, list...)
	}
	return sortSites(sites)
}

func sortSites(sites []handOffSite) []handOffSite {
	slices.SortFunc(sites, func(left, right handOffSite) int { return cmp.Compare(left.ID, right.ID) })
	return slices.CompactFunc(sites, func(left, right handOffSite) bool { return left.ID == right.ID })
}

func sharedHandOffGraph(t *testing.T) *engineCallGraph {
	t.Helper()
	if runtime.GOOS == "js" {
		t.Skip("the call graph is loaded with go/packages, which runs the go command; js/wasm cannot start processes")
	}
	handOffGraphOnce.Do(func() {
		handOffGraph, handOffGraphErr = buildEngineCallGraph()
	})
	require.NoError(t, handOffGraphErr)
	return handOffGraph
}

func handOffCut() map[string]bool {
	cut := map[string]bool{}
	for _, name := range isa.HandOffReentryFunctions() {
		cut[name] = true
	}
	for _, boundary := range isa.HandOffBoundaries() {
		for _, root := range boundary.Roots {
			cut[root] = true
		}
	}
	return cut
}

func reachedCauses(sites []handOffSite) map[isa.HandOffCause][]string {
	causes := map[isa.HandOffCause][]string{}
	for _, site := range sites {
		for _, cause := range site.Causes {
			causes[cause] = append(causes[cause], site.ID)
		}
	}
	return causes
}

func declaredCausesOf(entries []isa.HandOffEntry) map[isa.HandOffCause]bool {
	causes := map[isa.HandOffCause]bool{}
	for _, entry := range entries {
		for _, cause := range entry.Causes {
			causes[cause] = true
		}
	}
	return causes
}

func columnHasKind(entries []isa.HandOffEntry, kind isa.HandOffKind) bool {
	return slices.ContainsFunc(entries, func(entry isa.HandOffEntry) bool { return entry.Kind == kind })
}

func conformance(graph *engineCallGraph, owner string, entries []isa.HandOffEntry, cut map[string]bool, roots ...string) []string {
	var problems []string
	reached := reachedCauses(graph.reachableSites(cut, roots...))
	declared := declaredCausesOf(entries)
	for cause, sites := range reached {
		if !declared[cause] {
			problems = append(problems, fmt.Sprintf("%s reaches %s hand-off sites %v but declares no entry naming %s", owner, cause, sites, cause))
		}
	}
	for cause := range declared {
		if _, ok := reached[cause]; !ok {
			problems = append(problems, fmt.Sprintf("%s declares %s but reaches no hand-off site of that cause", owner, cause))
		}
	}
	functions := graph.reachableCut(cut, roots...)
	grants, copiesOut := false, false
	for function := range functions {
		name := ssaFunctionName(function)
		grants = grants || name == "pcGranted" || name == "localStoreGranted"
		for _, prefix := range copyBarrierPrefixes {
			copiesOut = copiesOut || strings.HasPrefix(name, prefix)
		}
	}
	if columnHasKind(entries, isa.HandOffGrant) != grants {
		problems = append(problems, fmt.Sprintf("%s: grant entry %v but reaches pcGranted %v", owner, columnHasKind(entries, isa.HandOffGrant), grants))
	}
	if columnHasKind(entries, isa.HandOffCopy) && !copiesOut {
		problems = append(problems, fmt.Sprintf("%s declares a copy but reaches no copy barrier", owner))
	}
	slices.Sort(problems)
	return problems
}

func rowConformance(graph *engineCallGraph, row isa.OpSpec, entries []isa.HandOffEntry, cut map[string]bool) []string {
	problems := conformance(graph, row.String(), entries, cut, row.Handler)
	if !row.RequiresHandOff() || columnHasKind(entries, isa.HandOffCopy) {
		return problems
	}
	var reached []string
	for function := range graph.reachableCut(cut, row.Handler) {
		name := ssaFunctionName(function)
		for _, prefix := range stringCopyBarrierPrefixes {
			if strings.HasPrefix(name, prefix) {
				reached = append(reached, name)
			}
		}
	}
	if len(reached) != 0 {
		slices.Sort(reached)
		problems = append(problems, fmt.Sprintf("%s reaches string copy barriers %v but declares no copy", row, reached))
	}
	return problems
}

func TestHandOffColumnMatchesReachableSites(t *testing.T) {
	graph := sharedHandOffGraph(t)
	cut := handOffCut()
	for name := range cut {
		require.NotEmptyf(t, graph.byName[name], "the ISA hand-off table names %s, which the engine does not define", name)
	}
	var problems []string
	for _, row := range isa.AllSpecs() {
		if row.Handler == "" {
			continue
		}
		require.NotEmptyf(t, graph.byName[row.Handler], "%s names handler %s, which the engine does not define", row, row.Handler)
		entries, _ := row.HandOffs()
		problems = append(problems, rowConformance(graph, row, entries, cut)...)
	}
	for _, boundary := range isa.HandOffBoundaries() {
		boundaryCut := handOffCut()
		for _, root := range boundary.Roots {
			delete(boundaryCut, root)
		}
		problems = append(problems, conformance(graph, "boundary "+boundary.Name, boundary.HandOffs, boundaryCut, boundary.Roots...)...)
	}
	assert.Empty(t, problems, "the hand-off column disagrees with the call graph:\n%s", strings.Join(problems, "\n"))
}

func TestEveryHandOffSiteBelongsToARowOrBoundary(t *testing.T) {
	graph := sharedHandOffGraph(t)
	cut := handOffCut()
	covered := map[string]bool{}
	for _, row := range isa.AllSpecs() {
		if row.Handler == "" {
			continue
		}
		for _, site := range graph.reachableSites(cut, row.Handler) {
			covered[site.ID] = true
		}
	}
	for _, boundary := range isa.HandOffBoundaries() {
		for _, site := range graph.reachableSites(nil, boundary.Roots...) {
			covered[site.ID] = true
		}
	}
	var orphans []string
	for _, site := range graph.allSites() {
		if !covered[site.ID] {
			orphans = append(orphans, site.ID)
		}
	}
	assert.Empty(t, orphans, "hand-off sites no row or boundary reaches; attribute each to a row's handler or a boundary in internal/isa/spec_handoff.go")
}
