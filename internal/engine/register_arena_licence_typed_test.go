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

//go:build !safe && !(js && wasm)

package engine

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"unsafe"
	"weak"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type licenceWalkOutcome struct {
	Revoked                                                  map[uintptr]HandOffCause
	Foreign                                                  map[licenceForeignKey]HandOffCause
	Pages                                                    map[uintptr][]uint64
	MemoEntries, MemoSpare                                   []licenceMemoEntry
	Fresh                                                    []uintptr
	Revision                                                 uint64
	CachedBase, CachedEnd                                    uintptr
	MemoCount                                                int
	Budget                                                   int
	MemoEpoch                                                uint32
	EpochCause                                               HandOffCause
	EpochReason                                              licenceEpochReason
	EpochRevoked, BanksRevoked, MemoUsed                     bool
	Exhausted, EpochStop, Opaque, SharedStop, StringsSkipped bool
	MapOrder                                                 bool
}

type licenceWalkStart struct {
	revoked             map[uintptr]HandOffCause
	foreign             map[licenceForeignKey]HandOffCause
	pages               map[uintptr][]uint64
	memo                licenceWalkMemo
	entries, spare      []licenceMemoEntry
	revision            uint64
	epochCause          HandOffCause
	epochReason         licenceEpochReason
	epochRevoked, banks bool
}

var (
	licenceTypedWalkDiffExact, licenceTypedWalkDiffNormalised, licenceTypedWalkDiffStopped atomic.Int64
)

type twItem struct {
	ID   int
	Name string
	Tags []string
}

type twNode struct {
	Name  string
	Left  *twNode
	Right *twNode
}

type twHostNode struct {
	Name     string
	Children []*twHostNode
}

type twEmbedded struct {
	twItem
	*twNode
	extra [2]string
	n     int
	f     float64
	inner struct {
		s string
		p *int64
	}
}

type twIfaces struct {
	A any
	E error
	S fmt.Stringer
	R reflect.Value
	T reflect.Type
	r reflect.Value
	a any
}

type twMisc struct {
	F  func()
	C  chan int
	U  unsafe.Pointer
	M  map[string]*twNode
	MK map[*twItem]int
	A  [3]*twItem
	Z  [0]*int
	P  *int64
	PS *string
	B  bool
	AA [2][2]string
	SS [][]string
	SA []any
}

type twDirect struct{ p *twNode }

type twArr1 [1]*twNode

type twChain struct {
	Next *twChain
	V    string
}

type twStringer struct{ s string }

type twError struct{ msg string }

type twPluginCtx struct {
	Path    string
	Headers map[string][]string
	Params  map[string]any
	Out     []map[string]any
	Hooks   []func(any) any
}

type twGen struct {
	random    *rand.Rand
	arena     *RegisterArena
	foreign   *RegisterArena
	registers Registers
	cells     []program.UpvalueCell
	pool      map[reflect.Type][]reflect.Value
	funcs     bool
}

var twBaseTypes = []reflect.Type{
	reflect.TypeFor[int](), reflect.TypeFor[string](), reflect.TypeFor[int64](), reflect.TypeFor[float64](),
	reflect.TypeFor[bool](), reflect.TypeFor[*int64](), reflect.TypeFor[*string](), reflect.TypeFor[[]string](),
	reflect.TypeFor[[]int64](), reflect.TypeFor[any](), reflect.TypeFor[error](), reflect.TypeFor[fmt.Stringer](),
	reflect.TypeFor[map[string]string](), reflect.TypeFor[chan int](), reflect.TypeFor[unsafe.Pointer](),
	reflect.TypeFor[reflect.Value](), reflect.TypeFor[twItem](), reflect.TypeFor[*twNode](), reflect.TypeFor[*twHostNode](),
	reflect.TypeFor[twEmbedded](), reflect.TypeFor[twIfaces](), reflect.TypeFor[twMisc](), reflect.TypeFor[twDirect](),
	reflect.TypeFor[twArr1](), reflect.TypeFor[*twChain](), reflect.TypeFor[twPluginCtx](), reflect.TypeFor[[]reflect.Value](),
	reflect.TypeFor[func()](), reflect.TypeFor[*program.UpvalueCell](), reflect.TypeFor[[4]byte](), reflect.TypeFor[struct{}](),
}

var (
	twShared        [2]*RegisterArena
	twSharedCreated bool
	twSharedUses    int
)

func licenceWalkOutcomesAgree(reflective, typed licenceWalkOutcome) (agree bool, how string) {
	if !reflective.MapOrder && !typed.MapOrder {
		if licenceCounting {
			licenceTypedWalkDiffExact.Add(1)
		}
		return reflect.DeepEqual(reflective, typed), "exact"
	}
	if reflective.Exhausted || typed.Exhausted || reflective.EpochRevoked || typed.EpochRevoked {
		if licenceCounting {
			licenceTypedWalkDiffStopped.Add(1)
		}
		return (reflective.Exhausted || reflective.EpochRevoked) && (typed.Exhausted || typed.EpochRevoked), "both stopped or epoch-revoked (map order)"
	}
	if licenceCounting {
		licenceTypedWalkDiffNormalised.Add(1)
	}
	return reflect.DeepEqual(licenceWalkOutcomeNormalised(reflective), licenceWalkOutcomeNormalised(typed)), "up to map order"
}

func licenceWalkOutcomeNormalised(outcome licenceWalkOutcome) licenceWalkOutcome {
	entries := make([]licenceMemoEntry, 0, outcome.MemoCount)
	for _, entry := range outcome.MemoEntries {
		if entry.pointer != nil {
			entries = append(entries, entry)
		}
	}
	slices.SortFunc(entries, func(x, y licenceMemoEntry) int {
		if c := cmp.Compare(uintptr(x.pointer), uintptr(y.pointer)); c != 0 {
			return c
		}
		return cmp.Compare(uintptr(x.typ), uintptr(y.typ))
	})
	outcome.MemoEntries, outcome.MemoSpare, outcome.CachedBase, outcome.CachedEnd = entries, nil, 0, 0
	if len(outcome.Pages) != 0 {

		outcome.Revision = 0
	}
	return outcome
}

func (a *RegisterArena) licenceTypedWalkBoth(walk *licenceWalk, value reflect.Value, boxed bool) (reflectOutcome, typedOutcome licenceWalkOutcome) {
	state := &a.licenceState
	if !state.foreignLocked {
		licenceForeignRevokedMu.Lock()
		state.foreignLocked = true
		defer func() {
			state.foreignLocked = false
			licenceForeignRevokedMu.Unlock()
		}()
	}

	arenaSlabRangesMu.Lock()
	lockLicenceRegistry()
	defer func() {
		unlockLicenceRegistry()
		arenaSlabRangesMu.Unlock()
	}()
	start := a.licenceWalkStartState()
	initial := *walk
	reference := initial
	reference.typedOff = true
	reference.visitTop(value, boxed, licenceWalkDepth)
	reflectOutcome = a.licenceWalkOutcomeOf(&reference, start)
	a.restoreLicenceWalkStart(start)
	*walk = initial
	walk.typedOff = false
	walk.visitTop(value, boxed, licenceWalkDepth)
	typedOutcome = a.licenceWalkOutcomeOf(walk, start)
	return reflectOutcome, typedOutcome
}

func (a *RegisterArena) licenceWalkStartState() licenceWalkStart {
	state := &a.licenceState
	return licenceWalkStart{
		revoked: maps.Clone(state.revoked), foreign: maps.Collect(licenceForeignRevoked.all()), pages: licenceWalkPages(a),
		memo: state.memo, entries: slices.Clone(state.memo.entries), spare: slices.Clone(state.memo.spare),
		revision: state.revision, epochCause: state.epochCause, epochReason: state.epochReason,
		epochRevoked: state.epochRevoked, banks: state.banksRevoked,
	}
}

func (a *RegisterArena) restoreLicenceWalkStart(start licenceWalkStart) {
	state := &a.licenceState
	state.coverCoveredKnown = false
	if state.revoked != nil {
		clear(state.revoked)
		maps.Copy(state.revoked, start.revoked)
	}
	for key := range licenceForeignRevoked.all() {
		if _, before := start.foreign[key]; !before && licenceForeignRevoked.forget(key) {
			licenceForeignRevokedCount.Add(-1)
		}
	}
	for base := range state.pages {
		a.forgetRevokedPages(base)
	}
	for base, words := range start.pages {
		set := a.revokedPageSetFor(base)
		set.words = append(set.words, words...)
	}
	state.memo = start.memo
	state.memo.entries, state.memo.spare = slices.Clone(start.entries), slices.Clone(start.spare)
	state.revision, state.epochCause, state.epochReason = start.revision, start.epochCause, start.epochReason
	state.epochRevoked, state.banksRevoked = start.epochRevoked, start.banks
}

func (a *RegisterArena) licenceWalkOutcomeOf(walk *licenceWalk, start licenceWalkStart) licenceWalkOutcome {
	state := &a.licenceState
	foreign := map[licenceForeignKey]HandOffCause{}
	for key, cause := range licenceForeignRevoked.all() {
		if _, before := start.foreign[key]; !before {
			foreign[key] = cause
		}
	}
	revoked := maps.Clone(state.revoked)
	if revoked == nil {
		revoked = map[uintptr]HandOffCause{}
	}
	return licenceWalkOutcome{
		Revoked: revoked, Foreign: foreign, Pages: licenceWalkPages(a),
		MemoEntries: slices.Clone(state.memo.entries), MemoSpare: slices.Clone(state.memo.spare),
		Fresh: slices.Clone(walk.fresh), Revision: state.revision,
		CachedBase: walk.cachedBase, CachedEnd: walk.cachedEnd,
		MemoCount: state.memo.count, Budget: walk.budget, MemoEpoch: state.memo.epoch,
		EpochCause: state.epochCause, EpochReason: state.epochReason,
		EpochRevoked: state.epochRevoked, BanksRevoked: state.banksRevoked, MemoUsed: state.memo.used,
		Exhausted: walk.exhausted, EpochStop: walk.epochStop, Opaque: walk.opaque,
		SharedStop: walk.sharedStop, StringsSkipped: walk.stringsSkipped, MapOrder: walk.mapOrder,
	}
}

func licenceWalkPages(arena *RegisterArena) map[uintptr][]uint64 {
	pages := map[uintptr][]uint64{}
	for base, set := range arena.licenceState.pages {
		words := set.words
		for len(words) > 0 && words[len(words)-1] == 0 {
			words = words[:len(words)-1]
		}
		pages[base] = slices.Clone(words)
	}
	return pages
}

func (s twStringer) String() string { return s.s }

func (e *twError) Error() string { return e.msg }

func newTwGen(seed uint64, arena, foreign *RegisterArena) *twGen {
	generator := &twGen{random: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), arena: arena, foreign: foreign, pool: map[reflect.Type][]reflect.Value{}}
	var counts [isa.NumRegisterKinds]uint32
	for index := range counts {
		counts[index] = 4
	}
	generator.registers = arena.AllocRegisters(counts)
	generator.cells = arena.allocUpvalueCells(2)
	generator.funcs = generator.random.IntN(4) == 0
	return generator
}

func (g *twGen) randomText() string {
	switch g.random.IntN(6) {
	case 0:
		return ""
	case 1:
		return "heap-" + strconv.Itoa(g.random.IntN(1000))
	case 2:
		return arenaString(g.foreign, "foreign-"+strconv.Itoa(g.random.IntN(1000)))
	default:
		return arenaString(g.arena, "arena-"+strconv.Itoa(g.random.IntN(1000)))
	}
}

func (g *twGen) arenaBoxedAny() any {
	var element any
	if g.random.IntN(2) == 0 {
		element = int64(0)
		(*emptyInterface)(unsafe.Pointer(&element)).data = unsafe.Pointer(g.arena.allocIntBox(int64(g.random.IntN(100))))
	} else {
		element = ""
		(*emptyInterface)(unsafe.Pointer(&element)).data = unsafe.Pointer(g.arena.allocStringBox(g.randomText()))
	}
	return element
}

func (g *twGen) typ(depth int) reflect.Type {
	if depth <= 0 || g.random.IntN(3) == 0 {
		return twBaseTypes[g.random.IntN(len(twBaseTypes))]
	}
	switch g.random.IntN(6) {
	case 0:
		return reflect.SliceOf(g.typ(depth - 1))
	case 1:
		return reflect.ArrayOf(g.random.IntN(4), g.typ(depth-1))
	case 2:
		return reflect.PointerTo(g.typ(depth - 1))
	case 3:
		key := reflect.TypeFor[string]()
		if g.random.IntN(3) == 0 {
			key = reflect.TypeFor[*twItem]()
		}
		return reflect.MapOf(key, g.typ(depth-1))
	default:
		fields := make([]reflect.StructField, g.random.IntN(5))
		for index := range fields {
			fields[index] = reflect.StructField{Name: "F" + strconv.Itoa(index), Type: g.typ(depth - 1)}
		}
		return reflect.StructOf(fields)
	}
}

func (g *twGen) value(t reflect.Type, depth int) reflect.Value {
	v := reflect.New(t).Elem()
	g.fill(v, depth)
	return v
}

func settable(v reflect.Value) reflect.Value {
	if v.CanSet() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}

func (g *twGen) fill(value reflect.Value, depth int) {
	value = settable(value)
	valueType := value.Type()
	if depth <= 0 && g.random.IntN(4) != 0 {
		return
	}
	switch valueType {
	case reflect.TypeFor[reflect.Value]():
		if g.random.IntN(4) != 0 {
			value.Set(reflect.ValueOf(g.value(g.typ(1), depth-1)))
		}
		return
	case reflect.TypeFor[reflect.Type]():
		if g.random.IntN(2) == 0 {
			value.Set(reflect.ValueOf(reflect.TypeFor[twItem]()))
		}
		return
	case reflect.TypeFor[*program.UpvalueCell]():
		if g.random.IntN(6) == 0 {
			value.Set(reflect.ValueOf(&g.cells[g.random.IntN(len(g.cells))]))
		}
		return
	}
	switch valueType.Kind() {
	case reflect.Int, reflect.Int64:
		value.SetInt(int64(g.random.IntN(100)))
	case reflect.Bool:
		value.SetBool(g.random.IntN(2) == 0)
	case reflect.Float64:
		value.SetFloat(g.random.Float64())
	case reflect.String:
		value.SetString(g.randomText())
	case reflect.Slice:
		g.fillSlice(value, depth)
	case reflect.Array:
		for index := range value.Len() {
			g.fill(value.Index(index), depth-1)
		}
	case reflect.Struct:
		for _, field := range value.Fields() {
			g.fill(field, depth-1)
		}
	case reflect.Pointer:
		g.fillPointer(value, depth)
	case reflect.Interface:
		g.fillInterface(value, depth)
	case reflect.Map:
		if g.random.IntN(4) == 0 {
			return
		}
		mapValue := reflect.MakeMap(valueType)
		for range g.random.IntN(5) {
			mapValue.SetMapIndex(g.value(valueType.Key(), depth-1), g.value(valueType.Elem(), depth-1))
		}
		value.Set(mapValue)
	case reflect.Func:
		if g.funcs && g.random.IntN(8) == 0 {
			value.Set(reflect.MakeFunc(valueType, func([]reflect.Value) []reflect.Value { return nil }))
		}
	case reflect.Chan:
		if g.random.IntN(2) == 0 {
			value.Set(reflect.MakeChan(valueType, 0))
		}
	case reflect.UnsafePointer:
		switch g.random.IntN(4) {
		case 0:
			value.SetPointer(g.arena.AllocBytes(8, 8))
		case 1:
			value.SetPointer(unsafe.Pointer(new(int)))
		case 2:
			value.SetPointer(unsafe.Pointer(&g.registers.Ints[0]))
		}
	default:
	}
}

func (g *twGen) fillSlice(value reflect.Value, depth int) {
	valueType := value.Type()
	if g.random.IntN(5) == 0 {
		return
	}
	length := g.random.IntN(6)
	capacity := length + g.random.IntN(3)
	switch {
	case valueType == reflect.TypeFor[[]string]() && g.random.IntN(3) == 0:
		backing := g.arena.AllocStringBacking(capacity)
		for index := range backing[:length] {
			backing[index] = g.randomText()
		}
		value.Set(reflect.ValueOf(backing[:length]))
		return
	case valueType == reflect.TypeFor[[]string]() && g.random.IntN(4) == 0:
		value.Set(reflect.ValueOf(g.registers.Strings[:g.random.IntN(len(g.registers.Strings)+1)]))
		return
	case valueType == reflect.TypeFor[[]int64]() && g.random.IntN(2) == 0:
		value.Set(reflect.ValueOf(g.arena.AllocIntBacking(capacity)[:length]))
		return
	case valueType == reflect.TypeFor[[]reflect.Value]() && g.random.IntN(2) == 0:
		for index := range g.registers.General {
			if g.random.IntN(2) == 0 {
				g.registers.General[index] = reflect.ValueOf(g.randomText())
			}
		}
		value.Set(reflect.ValueOf(g.registers.General))
		return
	}
	if shared := g.pool[valueType]; len(shared) > 0 && g.random.IntN(3) == 0 {
		value.Set(shared[g.random.IntN(len(shared))])
		return
	}
	slice := reflect.MakeSlice(valueType, length, capacity)
	full := slice.Slice3(0, capacity, capacity)
	for index := range capacity {
		g.fill(full.Index(index), depth-1)
	}
	g.pool[valueType] = append(g.pool[valueType], slice)
	value.Set(slice)
}

func (g *twGen) fillPointer(value reflect.Value, depth int) {
	valueType := value.Type()
	switch {
	case g.random.IntN(5) == 0:
		return
	case valueType == reflect.TypeFor[*int64]() && g.random.IntN(2) == 0:
		if g.random.IntN(2) == 0 {
			value.Set(reflect.ValueOf(g.arena.allocIntBox(int64(g.random.IntN(9)))))
		} else {
			value.Set(reflect.ValueOf(&g.registers.Ints[g.random.IntN(len(g.registers.Ints))]))
		}
		return
	case valueType == reflect.TypeFor[*string]() && g.random.IntN(2) == 0:
		value.Set(reflect.ValueOf(g.arena.allocStringBox(g.randomText())))
		return
	}
	if shared := g.pool[valueType]; len(shared) > 0 && g.random.IntN(3) == 0 {
		value.Set(shared[g.random.IntN(len(shared))])
		return
	}
	if valueType == reflect.TypeFor[*program.UpvalueCell]() && cellWalksApply(g.arena) && g.random.IntN(2) == 0 {

		cell := g.arena.newChunkCell()
		cell.Kind, cell.StringValue = isa.RegisterString, g.randomText()
		value.Set(reflect.ValueOf(cell))
		return
	}
	if g.arena.krefPlaces() && valueType.Elem().Size() != 0 && g.random.IntN(3) == 0 {

		if slot, ok := g.arena.krefSlot(valueType.Elem()); ok {
			g.pool[valueType] = append(g.pool[valueType], slot.Addr())
			value.Set(slot.Addr())
			g.fill(slot, depth-1)
			return
		}
	}
	pointer := reflect.New(valueType.Elem())
	g.pool[valueType] = append(g.pool[valueType], pointer)
	value.Set(pointer)
	g.fill(pointer.Elem(), depth-1)
}

func (g *twGen) fillInterface(value reflect.Value, depth int) {
	valueType := value.Type()
	if g.random.IntN(5) == 0 {
		return
	}
	switch valueType {
	case reflect.TypeFor[error]():
		if g.random.IntN(2) == 0 {
			value.Set(reflect.ValueOf(errors.New(g.randomText())))
		} else {
			value.Set(reflect.ValueOf(&twError{msg: g.randomText()}))
		}
		return
	case reflect.TypeFor[fmt.Stringer]():
		value.Set(reflect.ValueOf(twStringer{s: g.randomText()}))
		return
	}
	if valueType.NumMethod() != 0 {
		return
	}
	var generated any
	switch g.random.IntN(12) {
	case 0:
		generated = g.arenaBoxedAny()
	case 1:
		generated = g.randomText()
	case 2:
		generated = reflect.TypeFor[twNode]()
	case 3:
		generated = reflect.ValueOf(g.randomText())
	case 4:
		generated = int64(g.random.IntN(1000))
	case 5:
		generated = twDirect{p: g.value(reflect.TypeFor[*twNode](), depth-1).Interface().(*twNode)}
	case 6:
		generated = twArr1{g.value(reflect.TypeFor[*twNode](), depth-1).Interface().(*twNode)}
	default:
		if depth <= 0 {
			return
		}
		generated = g.value(g.typ(2), depth-1).Interface()
	}
	if generated != nil {
		value.Set(reflect.ValueOf(generated))
	}
}

func (g *twGen) handOff() (reflect.Value, bool) {
	value := g.value(g.typ(3), 5)
	boxed := g.random.IntN(3) == 0
	switch g.random.IntN(3) {
	case 0:
		return value, boxed
	case 1:
		return reflect.ValueOf(value.Interface()), boxed
	default:
		if value.CanAddr() {
			return value.Addr(), boxed
		}
		return value, boxed
	}
}

func twCorpus(arena, foreign *RegisterArena) []reflect.Value {
	arenaText := func(text string) string { return arenaString(arena, text) }
	var tree func(lo, hi int) *twNode
	tree = func(low, high int) *twNode {
		if low >= high {
			return nil
		}
		mid := (low + high) / 2
		return &twNode{Name: arenaText("node-" + strconv.Itoa(mid)), Left: tree(low, mid), Right: tree(mid+1, high)}
	}
	structs := make([]twItem, 300)
	for index := range structs {
		structs[index] = twItem{ID: index, Name: arenaText("item"), Tags: []string{arenaText("tag")}}
	}
	items := map[string]twItem{}
	for index := range 50 {
		items[arenaText("key"+strconv.Itoa(index))] = twItem{ID: index, Name: arenaText("v")}
	}
	strs := arena.AllocStringBacking(64)
	for index := range strs {
		strs[index] = arenaText("s")
	}
	root := &twHostNode{Name: "root"}
	for index := range 200 {
		root.Children = append(root.Children, &twHostNode{Name: arenaText("child" + strconv.Itoa(index))})
	}
	cycle := &twNode{Name: arenaText("cycle")}
	cycle.Left, cycle.Right = cycle, &twNode{Name: arenaText("r"), Left: cycle}
	chain := &twChain{V: arenaText("head")}
	for link, index := chain, 0; index < 2*licenceWalkDepth; index++ {
		link.Next = &twChain{V: arenaText("link")}
		link = link.Next
	}
	embedded := twEmbedded{twItem: twItem{Name: arenaText("emb"), Tags: strs[:3]}, twNode: tree(0, 5), extra: [2]string{arenaText("x"), "heap"}}
	embedded.inner.s, embedded.inner.p = arenaText("inner"), arena.allocIntBox(3)
	ifaces := twIfaces{A: arenaText("a"), E: &twError{msg: arenaText("err")}, S: twStringer{s: arenaText("str")}, R: reflect.ValueOf(arenaText("rv")),
		T: reflect.TypeFor[twItem](), r: reflect.ValueOf(&twItem{Name: arenaText("hidden")}), a: map[string]any{arenaText("k"): []string{arenaText("v")}}}
	misc := twMisc{C: make(chan int), U: arena.AllocBytes(16, 8), M: map[string]*twNode{arenaText("m"): tree(0, 3)},
		MK: map[*twItem]int{{Name: arenaText("key")}: 1}, A: [3]*twItem{{Name: arenaText("a0")}, nil, &structs[0]}, P: arena.allocIntBox(1),
		PS: arena.allocStringBox(arenaText("ps")), AA: [2][2]string{{arenaText("a"), ""}, {"", arenaText("b")}}, SS: [][]string{strs[:2], nil, {arenaText("q")}},
		SA: []any{arenaText("x"), 1, nil, &structs[1], reflect.ValueOf(arenaText("y")), twDirect{p: cycle}, twArr1{cycle}}}
	plugin := twPluginCtx{Path: arenaText("/p"), Headers: map[string][]string{arenaText("h"): {arenaText("v")}}, Params: map[string]any{"n": arenaText("1"), "m": map[string]any{"x": arenaText("y")}},
		Out: []map[string]any{{"k": arenaText("v")}, nil}}
	boxedAny := []any{nil, nil}
	boxedAny[0] = int64(0)
	(*emptyInterface)(unsafe.Pointer(&boxedAny[0])).data = unsafe.Pointer(arena.allocIntBox(9))
	boxedAny[1] = ""
	(*emptyInterface)(unsafe.Pointer(&boxedAny[1])).data = unsafe.Pointer(arena.allocStringBox(arenaText("boxed")))
	values := []any{
		structs, items, tree(0, 255), strs, arena.AllocIntBacking(64), root, root.Children,
		cycle, chain, embedded, &embedded, ifaces, &ifaces, misc, &misc, plugin, boxedAny,
		[]string{arenaText("a"), arenaString(foreign, "foreign")}, twDirect{p: cycle}, twArr1{cycle},
		reflect.ValueOf(&structs[2]), map[string][]string{arenaText("x"): strs[4:8]}, [3]twItem{structs[3], structs[4]},
		[]*twNode{cycle, cycle, tree(0, 7)}, make([]twItem, 2, 9),
	}
	out := make([]reflect.Value, 0, 3*len(values))
	for _, value := range values {
		reflected := reflect.ValueOf(value)
		out = append(out, reflected)
		addressable := reflect.New(reflected.Type())
		addressable.Elem().Set(reflected)
		out = append(out, addressable.Elem(), addressable)
	}
	return out
}

func twArenas(t testing.TB) (*RegisterArena, *RegisterArena) {
	t.Helper()
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	foreign := NewRegisterArena()
	foreign.attachLicencePolicy(&VMLimits{})
	require.True(t, arena.licensed && foreign.licensed)
	t.Cleanup(func() { twDropForeign(foreign) })
	return arena, foreign
}

func twWalkBoth(t testing.TB, arena *RegisterArena, value reflect.Value, boxed bool, label string) {
	t.Helper()
	state := &arena.licenceState
	walk := licenceWalk{arena: arena, cause: handOffUntracedContainer, budget: state.walkBudget, budgetAtStart: state.walkBudget,
		banksPrivate: !state.banksRevoked, fresh: state.walkFresh[:0], memo: &state.memo}
	reflectOutcome, typedOutcome := arena.licenceTypedWalkBoth(&walk, value, boxed)
	if agree, how := licenceWalkOutcomesAgree(reflectOutcome, typedOutcome); !agree {
		t.Logf("comparison: %s", how)
		t.Logf("reflect: %s", twOutcomeSummary(reflectOutcome))
		t.Logf("typed:   %s", twOutcomeSummary(typedOutcome))
		if how != "exact" {
			reflectOutcome, typedOutcome = licenceWalkOutcomeNormalised(reflectOutcome), licenceWalkOutcomeNormalised(typedOutcome)
		}
		require.Equal(t, reflectOutcome, typedOutcome, "%s: typed walk disagrees with the reflect walk on %s (boxed %v)", label, value.Type(), boxed)
	}
	state.walkBudget = max(walk.budget, 0)
}

func twResetLicence(a *RegisterArena, budget int) {
	a.clearLicence()
	a.licenceState.memo = licenceWalkMemo{}
	a.licenceState.walkBudget = budget
}

func twSharedArenas(t testing.TB) (*RegisterArena, *RegisterArena) {
	t.Helper()
	if !twSharedCreated {
		for index := range twShared {
			twShared[index] = NewRegisterArena()
			twShared[index].attachLicencePolicy(&VMLimits{})
		}
		twSharedCreated = true
	}
	if twSharedUses++; twSharedUses%64 == 0 {
		for _, arena := range twShared {
			twDropForeign(arena)
			arena.Reset()
			arena.attachLicencePolicy(&VMLimits{})
		}
	}
	own, foreign := twShared[0], twShared[1]
	t.Cleanup(func() { twDropForeign(foreign) })
	return own, foreign
}

func twDropForeign(foreign *RegisterArena) {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	for key := range licenceForeignRevoked.all() {
		if _, class := foreign.licenceSlabOf(key.start); class != licenceSlabNone && licenceForeignRevoked.forget(key) {
			licenceForeignRevokedCount.Add(-1)
		}
	}
}

func twRunRandom(t testing.TB, seed uint64) {
	t.Helper()
	arena, foreign := twSharedArenas(t)
	defer twDropForeign(foreign)
	generator := newTwGen(seed, arena, foreign)
	budgets := []int{licenceWalkLeafBudget, 1 + generator.random.IntN(5), 1 + generator.random.IntN(60), 1 + generator.random.IntN(600)}
	twResetLicence(arena, budgets[generator.random.IntN(len(budgets))])
	for step := range 1 + generator.random.IntN(4) {
		v, boxed := generator.handOff()
		twWalkBoth(t, arena, v, boxed, fmt.Sprintf("seed %d step %d", seed, step))
	}
}

func TestTypedLicenceWalkMatchesTheReflectWalkOnRandomValues(t *testing.T) {
	seeds := 3000
	if testing.Short() {
		seeds = 300
	}
	exact, normalised, stopped := licenceTypedWalkDiffExact.Load(), licenceTypedWalkDiffNormalised.Load(), licenceTypedWalkDiffStopped.Load()
	for seed := range seeds {
		twRunRandom(t, uint64(seed))
	}
	t.Logf("TYPED-WALK-DIFF seeds=%d exact=%d normalised=%d stopped=%d", seeds, licenceTypedWalkDiffExact.Load()-exact,
		licenceTypedWalkDiffNormalised.Load()-normalised, licenceTypedWalkDiffStopped.Load()-stopped)
}

func TestTypedLicenceWalkTakesTheTypedPath(t *testing.T) {
	arena, foreign := twArenas(t)
	for index, value := range twCorpus(arena, foreign) {
		walk := licenceWalk{arena: arena, cause: handOffUntracedContainer, budget: licenceWalkLeafBudget, banksPrivate: true}
		taken := walk.visitTyped(value, false, licenceWalkDepth)
		raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
		indirect := raw.flag&flagIndir != 0
		expected := (indirect && value.Kind() != reflect.Map) || (!indirect && value.Kind() == reflect.Pointer)
		require.Equal(t, expected, taken, "corpus[%d] %s", index, value.Type())
	}
	generator := newTwGen(7, arena, foreign)
	for range 500 {
		typ := generator.typ(3)
		plan := licenceTypedPlanFor(typ)
		boxedValue := reflect.ValueOf(reflect.New(typ).Elem().Interface())
		raw := (*unsafeReflectValue)(unsafe.Pointer(&boxedValue))
		if typ.Kind() == reflect.Interface {
			continue
		}
		require.Equal(t, raw.flag&flagIndir == 0, plan.direct, "%s", typ)
	}
}

func TestTypedLicenceWalkRevokesStructStrings(t *testing.T) {
	arena, _ := twArenas(t)
	text := arenaString(arena, "a struct's string handed to the host")
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf([]twItem{{Name: text}}))
	base, class := arena.licenceSlabOf(uintptr(unsafe.Pointer(unsafe.StringData(text))))
	require.Equal(t, licenceSlabData, class)
	require.Contains(t, arena.licenceState.revoked, base, "the typed walk must revoke the byte slab of a struct's string field")
}

func BenchmarkLicenceWalkTree(b *testing.B) {
	arena, foreign := twArenas(b)
	corpus := twCorpus(arena, foreign)
	state := &arena.licenceState
	for _, typed := range []bool{false, true} {
		b.Run(fmt.Sprintf("typed=%v", typed), func(b *testing.B) {
			for b.Loop() {
				for _, v := range corpus[:9] {
					twResetLicence(arena, licenceWalkLeafBudget)
					walk := licenceWalk{arena: arena, cause: handOffUntracedContainer, budget: state.walkBudget, budgetAtStart: state.walkBudget,
						banksPrivate: !state.banksRevoked, fresh: state.walkFresh[:0], memo: &state.memo, typedOff: !typed}
					walk.visitTop(v, false, licenceWalkDepth)
				}
			}
		})
	}
}

func twOutcomeSummary(o licenceWalkOutcome) string {
	return fmt.Sprintf("revoked=%v foreign=%v memoCount=%d rev=%d budget=%d epoch=%v/%d/%d banks=%v exh=%v stop=%v opaque=%v cached=%#x",
		o.Revoked, o.Foreign, o.MemoCount, o.Revision, o.Budget, o.EpochRevoked, o.EpochReason, o.EpochCause, o.BanksRevoked,
		o.Exhausted, o.EpochStop, o.Opaque, o.CachedBase)
}

func twChunkCorpus(t *testing.T, arena *RegisterArena) ([]reflect.Value, []any) {
	t.Helper()
	arenaText := func(text string) string { return arenaString(arena, text) }
	krefNode := func(name string, left, right *twNode) *twNode {
		slot, ok := arena.krefSlot(reflect.TypeFor[twNode]())
		require.True(t, ok, "the licensed arena gives a KRef slot")
		node := requireAs[*twNode](t, slot.Addr().Interface())
		node.Name, node.Left, node.Right = arenaText(name), left, right
		return node
	}
	var tree func(lo, hi int) *twNode
	tree = func(low, high int) *twNode {
		if low >= high {
			return nil
		}
		mid := (low + high) / 2
		return krefNode("kref-"+strconv.Itoa(mid), tree(low, mid), tree(mid+1, high))
	}
	itemSlot, ok := arena.krefSlot(reflect.TypeFor[twItem]())
	require.True(t, ok)
	item := requireAs[*twItem](t, itemSlot.Addr().Interface())
	item.Name, item.Tags = arenaText("kref-item"), []string{arenaText("t0"), arenaText("t1")}
	run, ok := arena.krefRun(reflect.TypeFor[*twNode](), 6)
	require.True(t, ok, "the licensed arena gives a KRef run")
	nodes := requireAs[[]*twNode](t, reflect.NewAt(reflect.ArrayOf(6, reflect.TypeFor[*twNode]()), run).Elem().Slice(0, 6).Interface())
	for index := range nodes {
		nodes[index] = krefNode("run-"+strconv.Itoa(index), nil, nil)
	}
	nodes[5] = nil
	cycle := krefNode("kref-cycle", nil, nil)
	cycle.Left = cycle

	cell := func(kind isa.RegisterKind, fill func(*program.UpvalueCell)) *program.UpvalueCell {
		c := arena.newChunkCell()
		c.Kind = kind
		fill(c)
		return c
	}
	strings := arena.AllocStringBacking(8)
	for index := range strings {
		strings[index] = arenaText("cell-" + strconv.Itoa(index))
	}
	general := cell(isa.RegisterGeneral, func(c *program.UpvalueCell) { c.GeneralValue = reflect.ValueOf(tree(0, 3)) })
	text := cell(isa.RegisterString, func(c *program.UpvalueCell) { c.StringValue = arenaText("captured") })
	slice := cell(isa.RegisterSliceString, func(c *program.UpvalueCell) { c.SliceStringValue = strings[:3] })
	ints := cell(isa.RegisterSliceInt64, func(c *program.UpvalueCell) { c.SliceIntValue = arena.AllocIntBacking(16)[:4] })
	heapCell := &program.UpvalueCell{Kind: isa.RegisterString, StringValue: arenaText("heap-cell")}
	closure := func(cells ...*program.UpvalueCell) *RuntimeClosure {
		c := arena.newChunkClosure()
		c.upvalues = cells
		return c
	}
	inner := closure(text, heapCell)
	innerCell := cell(isa.RegisterGeneral, func(c *program.UpvalueCell) { c.GeneralValue = reflect.ValueOf(inner) })
	outer := closure(general, slice, ints, innerCell)
	self := closure()
	selfCell := cell(isa.RegisterGeneral, func(c *program.UpvalueCell) { c.GeneralValue = reflect.ValueOf(self) })
	self.upvalues = []*program.UpvalueCell{selfCell, text}

	wrapper := reflect.MakeFunc(reflect.TypeFor[func() int](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(1)}
	})
	closureWrapperRegister(wrapper, closureWrapperEntry{closure: weak.Make(outer)})
	unregistered := reflect.MakeFunc(reflect.TypeFor[func() int](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(2)}
	})
	registered := requireAs[func() int](t, wrapper.Interface())

	boxes := []*int64{arena.allocIntBox(1), arena.allocIntBox(2), nil, arena.allocIntBox(3)}
	type twChunkHook struct {
		Name string
		Run  func() int
		Item *twItem
		Tree *twNode
		Box  *int64
	}
	type twChunkHolder struct {
		Closure *RuntimeClosure
		Cell    *program.UpvalueCell
		Any     any
		Nodes   []*twNode
	}
	values := []any{
		tree(0, 31), item, *item, nodes, nodes[:2], cycle, [2]*twNode{cycle, nodes[0]},
		outer, inner, self, general, slice, ints, heapCell,
		registered, twChunkHook{Name: arenaText("hook"), Run: registered, Item: item, Tree: tree(0, 5), Box: boxes[0]},
		&twChunkHook{Name: arenaText("hook-ptr"), Run: registered, Tree: cycle},
		twChunkHolder{Closure: outer, Cell: text, Any: inner, Nodes: nodes},
		[]any{outer, registered, item, cycle, boxes[1]},
		map[string]*twNode{arenaText("k"): nodes[1]},
		strings, arena.AllocIntBacking(64)[:10], boxes,
	}
	out := make([]reflect.Value, 0, 3*len(values)+2)
	for _, value := range values {
		reflected := reflect.ValueOf(value)
		out = append(out, reflected)
		addressable := reflect.New(reflected.Type())
		addressable.Elem().Set(reflected)
		out = append(out, addressable.Elem(), addressable)
	}

	out = append(out, unregistered, reflect.ValueOf(twChunkHook{Run: unregistered.Interface().(func() int)}))
	return out, []any{outer, inner, self, wrapper, unregistered}
}

func TestTypedLicenceWalkMatchesTheReflectWalkOnChunkStorage(t *testing.T) {
	arena, _ := twArenas(t)
	if !arena.krefPlaces() || !cellWalksApply(arena) || !closureWrapperWalksApply(arena) || !arena.pageRevocationApplies() {
		t.Skip("the chunk placements do not apply to this build's arenas")
	}
	corpus, keep := twChunkCorpus(t, arena)
	opaque := len(corpus) - 2
	for _, budget := range []int{licenceWalkLeafBudget, 1, 7, 64, 500} {
		for _, boxed := range []bool{false, true} {
			twResetLicence(arena, budget)
			for index, v := range corpus[:opaque] {
				twWalkBoth(t, arena, v, boxed, fmt.Sprintf("chunk corpus[%d] sequence budget %d", index, budget))
			}
			for index, v := range corpus {
				twResetLicence(arena, budget)
				twWalkBoth(t, arena, v, boxed, fmt.Sprintf("chunk corpus[%d] alone budget %d", index, budget))
				twWalkBoth(t, arena, v, boxed, fmt.Sprintf("chunk corpus[%d] again budget %d", index, budget))
			}
		}
	}
	twResetLicence(arena, licenceWalkLeafBudget)
	for index, v := range corpus {
		twWalkBoth(t, arena, v, false, fmt.Sprintf("chunk corpus[%d] through the opaque wrapper", index))
	}
	twResetLicence(arena, licenceWalkLeafBudget)
	runtime.KeepAlive(keep)
}

func TestTypedLicenceWalkReachesChunkStorageOnTheTypedPath(t *testing.T) {
	arena, _ := twArenas(t)
	if !arena.krefPlaces() || !cellWalksApply(arena) || !closureWrapperWalksApply(arena) {
		t.Skip("the chunk placements do not apply to this build's arenas")
	}
	corpus, keep := twChunkCorpus(t, arena)
	for index, value := range corpus {
		twResetLicence(arena, licenceWalkLeafBudget)
		walk := licenceWalk{arena: arena, cause: handOffUntracedContainer, budget: licenceWalkLeafBudget, banksPrivate: true}
		raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
		if value.Kind() == reflect.Map || raw.flag&flagIndir == 0 && value.Kind() != reflect.Pointer {
			continue
		}
		require.Truef(t, walk.visitTyped(value, false, licenceWalkDepth), "chunk corpus[%d] (%s) takes the typed path", index, value.Type())
	}
	twResetLicence(arena, licenceWalkLeafBudget)
	runtime.KeepAlive(keep)
}
