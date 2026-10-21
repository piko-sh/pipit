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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type krefHost struct {
	mu   sync.Mutex
	kept []any
}

type krefAdapters struct {
	mu        sync.Mutex
	stringers []fmt.Stringer
	errs      []error
}

const krefSource = `package main

import (
	"krefhost"
	"runtime"
	"sort"
	"strconv"
)

type Node struct {
	Name  string
	Left  *Node
	Right *Node
}

type Item struct {
	ID   int
	Name string
	Tags []string
}

type Ring struct {
	S    string
	Next *Ring
}

type Box struct {
	V    any
	M    map[string]*Item
	P    *string
	Kids []*Node
}

var gItem Item

var gBox Box

var gRing *Ring

var gCell *string

var gAny any

func mk(n int) string { return "kref-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) }

func tree(lo, hi int) *Node {
	if lo >= hi {
		return nil
	}
	mid := (lo + hi) / 2
	return &Node{Name: mk(mid), Left: tree(lo, mid), Right: tree(mid+1, hi)}
}

func items(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: i, Name: mk(i), Tags: []string{mk(i + 1)}}
	}
	return out
}

func cycle(n int) *Ring {
	head := &Ring{S: mk(n)}
	cur := head
	for i := 1; i < 5; i++ {
		next := &Ring{S: mk(n + i)}
		cur.Next = next
		cur = next
	}
	cur.Next = head
	return head
}

func boxed(n int) *Box {
	s := mk(n)
	m := map[string]*Item{mk(n + 1): {ID: n, Name: mk(n + 2)}}
	return &Box{V: &Item{ID: n, Name: mk(n + 3), Tags: []string{mk(n + 4)}}, M: m, P: &s, Kids: []*Node{tree(n, n+3), nil}}
}

func Tree(n int) *Node { return tree(0, n) }

func Items(n int) []Item { return items(n) }

func Cycle(n int) *Ring { return cycle(n) }

func Boxed(n int) any { return boxed(n) }

func Ptrs(n int) []*Item {
	out := make([]*Item, 3)
	for i := range out {
		out[i] = &Item{ID: n + i, Name: mk(n + i)}
	}
	out[2] = out[0]
	return out
}

type E struct{ N *Node }

func Boom(n int) int { panic(&Node{Name: mk(n), Left: &Node{Name: mk(n + 1)}}) }

func BoomValue(n int) int { panic(Node{Name: mk(n), Left: &Node{Name: mk(n + 1)}}) }

func Err(n int) any { return E{N: &Node{Name: mk(n)}} }

type Lab struct {
	Name string
	P    *Node
}

func (l Lab) String() string { return l.Name + "/" + l.P.Name }

type Fail struct{ N *Node }

func (f Fail) Error() string { return "fail:" + f.N.Name }

func Adapt(n int) int {
	l := Lab{Name: mk(n), P: &Node{Name: mk(n + 1)}}
	krefhost.KeepStringer(l)
	krefhost.KeepError(Fail{N: &Node{Name: mk(n + 2)}})
	return n
}

func Keep(n int) int {
	krefhost.Keep(tree(0, 4+n%3))
	krefhost.Keep(Box{V: Item{Name: mk(n)}, Kids: []*Node{{Name: mk(n + 1)}}})
	krefhost.Keep(cycle(n + 2))
	krefhost.Keep(items(3 + n%2))
	return n
}

func SetGlobals(n int) int {
	gItem = Item{ID: n, Name: mk(n), Tags: []string{mk(n + 1), mk(n + 2)}}
	gBox = Box{V: &Ring{S: mk(n + 3)}, Kids: []*Node{{Name: mk(n + 4)}}}
	s := mk(n + 6)
	gCell = &s
	gAny = Item{ID: n, Name: mk(n + 7)}
	return n
}

func SetRing(n int) int {
	gRing = cycle(n + 5)
	return n
}

func Globals() []any { return []any{gItem, gBox, gRing, *gCell, gAny} }

func Covered(n int) int {
	p := &Node{}
	krefhost.Keep(p)
	p.Name = mk(n)
	p.Left = &Node{Name: mk(n + 1)}
	q := &Item{}
	krefhost.Keep(q)
	q.Name = mk(n + 2)
	q.Tags = []string{mk(n + 3)}
	b := &Box{}
	krefhost.Keep(b)
	b.V = mk(n + 4)
	b.Kids = []*Node{{Name: mk(n + 5)}}
	return n
}

func Cell(n int) *string {
	s := mk(n)
	return &s
}

func NewAny(n int) *any {
	p := new(any)
	*p = Item{Name: mk(n)}
	return p
}

func NewPtr(n int) **Node {
	p := new(*Node)
	*p = &Node{Name: mk(n)}
	return p
}

var apply = func(p *string, suffix string) { *p += suffix }

func CellLocal(n int) int {
	s := mk(n)
	apply(&s, "!")
	q := new(*Node)
	*q = &Node{Name: s}
	return len((*q).Name)
}

func confined(n int) int {
	t := tree(0, 7+n%5)
	r := cycle(n)
	b := boxed(n)
	is := items(5)
	total := len(t.Name) + len(r.Next.Next.S) + len(*b.P) + len(is[4].Tags[0])
	for c := r.Next; c != r; c = c.Next {
		total += len(c.S)
	}
	return total
}

func Confined(n int) int { return confined(n) }

func Goroutines(n int) []int {
	results := make(chan int, 4)
	for g := 0; g < 4; g++ {
		go func(g int) { results <- confined(n + g) }(g)
	}
	out := make([]int, 4)
	for i := range out {
		out[i] = <-results
	}
	sum := 0
	for _, v := range out {
		sum += v
	}
	return []int{sum, len(out)}
}

func GoroutineTrees(n int) []*Node {
	results := make(chan *Node, 3)
	for g := 0; g < 3; g++ {
		go func(g int) { results <- tree(n+g, n+g+5) }(g)
	}
	out := make([]*Node, 3)
	for i := range out {
		out[i] = <-results
	}
	return out
}

func Callback(n int) int {
	krefhost.Each(3, func(i int) {
		krefhost.Keep(&Node{Name: mk(n + i), Left: &Node{Name: mk(n + i + 10)}})
	})
	return n
}

func Finalised(n int) int {
	p := &Node{Name: mk(n)}
	runtime.SetFinalizer(p, func(*Node) {})
	return len(p.Name)
}

var gHeld *Node

type byName []*Node

func (b byName) Len() int { return len(b) }

func (b byName) Less(i, j int) bool {
	gHeld = b[0]
	return b[i].Name < b[j].Name
}

func (b byName) Swap(i, j int) { b[i], b[j] = b[j], b[i] }

func Publish(n int) int {
	nodes := make([]*Node, 3)
	for i := range nodes {
		nodes[i] = &Node{Name: mk(n + 2 - i)}
	}
	sort.Sort(byName(nodes))
	gHeld.Left = &Node{Name: mk(n + 10)}
	gHeld.Name = mk(n + 20)
	return n
}

func Held() *Node { return gHeld }

func Churn(n int) int {
	total := 0
	for i := 0; i < 32; i++ {
		t := tree(n, n+9)
		r := cycle(n + i)
		b := boxed(n + i)
		is := items(6)
		ps := make([]*Item, 4)
		ps[0] = &Item{Name: "ZZZZZZZZZZZZ" + strconv.Itoa(i)}
		c := new(any)
		*c = &Ring{S: "YYYYYYYYYYYY"}
		s := "XXXXXXXXXXXXXXXX" + strconv.Itoa(i)
		cell := &s
		total += len(t.Name) + len(r.S) + len(*b.P) + len(is[5].Name) + len(ps[0].Name) + len(*cell)
	}
	return total
}
`

type (
	krefTestNode struct {
		Name  string
		Left  *krefTestNode
		Right *krefTestNode
	}
	krefTestItem struct {
		ID   int
		Name string
		Tags []string
	}
	krefTestRing struct {
		S    string
		Next *krefTestRing
	}
	krefTestBox struct {
		V    any
		M    map[string]*krefTestItem
		P    *string
		Kids []*krefTestNode
	}
)

const krefRounds = 12

type guardHostNode struct {
	Name     string
	Children []*guardHostNode
}

const guardSource = `package main

import (
	"guardhost"
	"strconv"
)

func AppendHostNodes(n int) *guardhost.Node {
	root := &guardhost.Node{Name: "root"}
	for i := 0; i < n; i++ {
		root.Children = append(root.Children, &guardhost.Node{Name: "n" + strconv.Itoa(i)})
	}
	return root
}

func many(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "s" + strconv.Itoa(i)
	}
	return out
}

var apply = func(p *string, suffix string) { *p += suffix }

func PlaceThenRevoke(n int) int {
	s := "q" + strconv.Itoa(n)
	apply(&s, "!")
	guardhost.Keep(many(n))
	return len(s)
}
`

func (h *krefHost) keep(v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kept = append(h.kept, v)
}

func krefMk(n int) string { return "kref-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) }

func krefTree(low, high int) *krefTestNode {
	if low >= high {
		return nil
	}
	mid := (low + high) / 2
	return &krefTestNode{Name: krefMk(mid), Left: krefTree(low, mid), Right: krefTree(mid+1, high)}
}

func krefItems(count int) []krefTestItem {
	out := make([]krefTestItem, count)
	for i := range out {
		out[i] = krefTestItem{ID: i, Name: krefMk(i), Tags: []string{krefMk(i + 1)}}
	}
	return out
}

func krefCycle(count int) *krefTestRing {
	head := &krefTestRing{S: krefMk(count)}
	current := head
	for i := 1; i < 5; i++ {
		next := &krefTestRing{S: krefMk(count + i)}
		current.Next = next
		current = next
	}
	current.Next = head
	return head
}

func krefBoxed(n int) *krefTestBox {
	m := map[string]*krefTestItem{krefMk(n + 1): {ID: n, Name: krefMk(n + 2)}}
	return &krefTestBox{V: &krefTestItem{ID: n, Name: krefMk(n + 3), Tags: []string{krefMk(n + 4)}}, M: m, P: new(krefMk(n)), Kids: []*krefTestNode{krefTree(n, n+3), nil}}
}

func krefPtrs(count int) []*krefTestItem {
	out := make([]*krefTestItem, 3)
	for i := range out {
		out[i] = &krefTestItem{ID: count + i, Name: krefMk(count + i)}
	}
	out[2] = out[0]
	return out
}

func krefRingOrNil(n int) *krefTestRing {
	if n < 0 {
		return nil
	}
	return krefCycle(n + 5)
}

func krefConfined(seed int) int {
	t := krefTree(0, 7+seed%5)
	ring := krefCycle(seed)
	b := krefBoxed(seed)
	is := krefItems(5)
	total := len(t.Name) + len(ring.Next.Next.S) + len(*b.P) + len(is[4].Tags[0])
	for c := ring.Next; c != ring; c = c.Next {
		total += len(c.S)
	}
	return total
}

func renderByStructure(v any) string {
	var b strings.Builder
	renderValueByStructure(&b, reflect.ValueOf(v), map[uintptr]int{})
	return b.String()
}

func renderValueByStructure(b *strings.Builder, value reflect.Value, seen map[uintptr]int) {
	if !value.IsValid() {
		b.WriteString("nil")
		return
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			b.WriteString("nil")
			return
		}
		if id, ok := seen[value.Pointer()]; ok {
			fmt.Fprintf(b, "@%d", id)
			return
		}
		seen[value.Pointer()] = len(seen)
		b.WriteString("&")
		renderValueByStructure(b, value.Elem(), seen)
	case reflect.Interface:
		if value.IsNil() {
			b.WriteString("nil")
			return
		}
		renderValueByStructure(b, value.Elem(), seen)
	case reflect.Struct:
		b.WriteString("{")
		written := 0
		for index := range value.NumField() {
			name := value.Type().Field(index).Name
			if strings.HasPrefix(name, "_pipitID_") {
				continue
			}
			if written > 0 {
				b.WriteString(" ")
			}
			written++
			b.WriteString(name + ":")
			renderValueByStructure(b, value.Field(index), seen)
		}
		b.WriteString("}")
	case reflect.Slice, reflect.Array:
		b.WriteString("[")
		for index := range value.Len() {
			if index > 0 {
				b.WriteString(" ")
			}
			renderValueByStructure(b, value.Index(index), seen)
		}
		b.WriteString("]")
	case reflect.Map:
		keys := value.MapKeys()
		rendered := make([]string, 0, len(keys))
		for _, key := range keys {
			var entry strings.Builder
			renderValueByStructure(&entry, key, map[uintptr]int{})
			entry.WriteString("=")
			renderValueByStructure(&entry, value.MapIndex(key), seen)
			rendered = append(rendered, entry.String())
		}
		slices.Sort(rendered)
		b.WriteString("map[" + strings.Join(rendered, " ") + "]")
	case reflect.String:
		b.WriteString(strconv.Quote(value.String()))
	case reflect.Int, reflect.Int64:
		b.WriteString(strconv.FormatInt(value.Int(), 10))
	default:
		fmt.Fprintf(b, "%v", value)
	}
}

func krefService(t *testing.T, h *krefHost) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	return krefServiceWith(t, h, &krefAdapters{})
}

func krefServiceWith(t *testing.T, host *krefHost, adapters *krefAdapters) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	exports := maps.Clone(stdlib.Exports())
	exports["krefhost"] = map[string]reflect.Value{
		"KeepStringer": reflect.ValueOf(func(s fmt.Stringer) {
			adapters.mu.Lock()
			defer adapters.mu.Unlock()
			adapters.stringers = append(adapters.stringers, s)
		}),
		"KeepError": reflect.ValueOf(func(err error) {
			adapters.mu.Lock()
			defer adapters.mu.Unlock()
			adapters.errs = append(adapters.errs, err)
		}),
		"Keep": reflect.ValueOf(host.keep),
		"Each": reflect.ValueOf(func(n int, fn func(int)) {
			for i := range n {
				fn(i)
			}
		}),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled := hazardCompile(t, service, krefSource)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	return service, compiled
}

func reuseKRefArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 16)
	for round := range 8 {
		hazardCall(t, service, compiled, "Churn", 1000*round)
	}
}

func TestKRefChunkShapesSurviveReuse(t *testing.T) {
	host := &krefHost{}
	service, compiled := krefService(t, host)
	window := openHazardWindows(t)
	before := engine.LicenceDiagnostics().KRefChunks
	observations := &hazardObservations{}
	type returned struct {
		label string
		value any
		want  string
	}
	results := make([]returned, 0, 6*krefRounds)
	for round := range krefRounds {
		results = append(results,
			returned{fmt.Sprintf("Tree(%d)", round), hazardCall(t, service, compiled, "Tree", 3+round), renderByStructure(krefTree(0, 3+round))},
			returned{fmt.Sprintf("Items(%d)", round), hazardCall(t, service, compiled, "Items", 2+round), renderByStructure(krefItems(2 + round))},
			returned{fmt.Sprintf("Cycle(%d)", round), hazardCall(t, service, compiled, "Cycle", round), renderByStructure(krefCycle(round))},
			returned{fmt.Sprintf("Boxed(%d)", round), hazardCall(t, service, compiled, "Boxed", round), renderByStructure(krefBoxed(round))},
			returned{fmt.Sprintf("Ptrs(%d)", round), hazardCall(t, service, compiled, "Ptrs", round), renderByStructure(krefPtrs(round))},
		)
		hazardCall(t, service, compiled, "Keep", round)
	}
	type panicked struct {
		label string
		err   error
		want  string
		first string
	}
	panics := make([]panicked, 0, 2*krefRounds)
	for round := range krefRounds {
		for _, entry := range []string{"Boom", "BoomValue"} {
			_, err := service.CallFunction(t.Context(), compiled, entry, round)
			require.Error(t, err, "%s panics", entry)
			node := &krefTestNode{Name: krefMk(round), Left: &krefTestNode{Name: krefMk(round + 1)}}
			want := renderByStructure(node)
			if entry == "BoomValue" {
				want = renderByStructure(*node)
			}
			panics = append(panics, panicked{fmt.Sprintf("%s(%d)", entry, round), err, want, renderByStructure(err)})
		}
		results = append(results, returned{fmt.Sprintf("Err(%d)", round), hazardCall(t, service, compiled, "Err", round), renderByStructure(struct{ N *krefTestNode }{&krefTestNode{Name: krefMk(round)}})})
	}
	for _, p := range panics {
		if !strings.Contains(p.first, p.want) {
			observations.fire("host-value-changed", "%s panic value %s does not hold %s", p.label, p.first, p.want)
		}
	}
	for _, r := range results {
		observations.compareString(r.label+" as returned", r.want, renderByStructure(r.value))
	}
	reuseKRefArenas(t, service, compiled)
	for _, r := range results {
		observations.compareString(r.label+" after reuse", r.want, renderByStructure(r.value))
	}
	for _, p := range panics {
		observations.compareString(p.label+" panic value after reuse", p.first, renderByStructure(p.err))
	}
	require.Len(t, host.kept, 4*krefRounds)
	for round := range krefRounds {
		wants := []string{
			renderByStructure(krefTree(0, 4+round%3)),
			renderByStructure(krefTestBox{V: krefTestItem{Name: krefMk(round)}, Kids: []*krefTestNode{{Name: krefMk(round + 1)}}}),
			renderByStructure(krefCycle(round + 2)),
			renderByStructure(krefItems(3 + round%2)),
		}
		for k, want := range wants {
			observations.compareString(fmt.Sprintf("kept %d/%d", round, k), want, renderByStructure(host.kept[4*round+k]))
		}
	}
	after := engine.LicenceDiagnostics().KRefChunks
	observations.note("KRef chunks %d, slots %d, detached %d, reused %d, refused %d",
		after.Chunks-before.Chunks, after.Slots-before.Slots, after.Detached-before.Detached, after.Reused-before.Reused, after.Refused-before.Refused)
	window.settle(observations)
	inconclusive := ""
	if after.Slots == before.Slots {
		inconclusive = "no KRef slot was handed out"
	}
	assertNoHazard(t, observations, inconclusive)
}

func TestKRefChunkGlobalsSurviveReuse(t *testing.T) {
	host := &krefHost{}
	service, compiled := krefService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	for round := range krefRounds {
		hazardCall(t, service, compiled, "SetGlobals", round)
		reuseKRefArenas(t, service, compiled)
		if round%2 == 1 {

			hazardCall(t, service, compiled, "SetRing", round)
			reuseKRefArenas(t, service, compiled)
		}
		text := krefMk(round + 6)
		ring := round - 1 + round%2
		want := renderByStructure([]any{
			krefTestItem{ID: round, Name: krefMk(round), Tags: []string{krefMk(round + 1), krefMk(round + 2)}},
			krefTestBox{V: &krefTestRing{S: krefMk(round + 3)}, Kids: []*krefTestNode{{Name: krefMk(round + 4)}}},
			krefRingOrNil(ring), text,
			krefTestItem{ID: round, Name: krefMk(round + 7)},
		})
		observations.compareString(fmt.Sprintf("globals after reuse n=%d", round), want, renderByStructure(hazardCall(t, service, compiled, "Globals")))
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestKRefCoveredMemberStoresSurviveReuse(t *testing.T) {
	host := &krefHost{}
	service, compiled := krefService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	for n := range krefRounds {
		hazardCall(t, service, compiled, "Covered", n)
	}
	reuseKRefArenas(t, service, compiled)
	require.Len(t, host.kept, 3*krefRounds)
	for round := range krefRounds {
		wants := []string{
			renderByStructure(&krefTestNode{Name: krefMk(round), Left: &krefTestNode{Name: krefMk(round + 1)}}),
			renderByStructure(&krefTestItem{Name: krefMk(round + 2), Tags: []string{krefMk(round + 3)}}),
			renderByStructure(&krefTestBox{V: krefMk(round + 4), Kids: []*krefTestNode{{Name: krefMk(round + 5)}}}),
		}
		for k, want := range wants {
			observations.compareString(fmt.Sprintf("kept then stored %d/%d", round, k), want, renderByStructure(host.kept[3*round+k]))
		}
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestKRefPointeeCellsSurviveReuse(t *testing.T) {
	host := &krefHost{}
	service, compiled := krefService(t, host)
	window := openHazardWindows(t)
	before := engine.LicenceDiagnostics().KRefPlacement
	observations := &hazardObservations{}
	type cell struct {
		label string
		value any
		want  string
	}
	cells := make([]cell, 0, 3*krefRounds)
	for round := range krefRounds {
		var boxed any = krefTestItem{Name: krefMk(round + 1)}
		node := &krefTestNode{Name: krefMk(round + 2)}
		cells = append(cells,
			cell{fmt.Sprintf("Cell(%d)", round), hazardCall(t, service, compiled, "Cell", round), renderByStructure(new(krefMk(round)))},
			cell{fmt.Sprintf("NewAny(%d)", round), hazardCall(t, service, compiled, "NewAny", round+1), renderByStructure(&boxed)},
			cell{fmt.Sprintf("NewPtr(%d)", round), hazardCall(t, service, compiled, "NewPtr", round+2), renderByStructure(&node)},
		)
		got := hazardCall(t, service, compiled, "CellLocal", round)
		observations.compareString(fmt.Sprintf("CellLocal(%d)", round), strconv.Itoa(len(krefMk(round)+"!")), fmt.Sprint(got))
	}
	reuseKRefArenas(t, service, compiled)
	for _, c := range cells {
		observations.compareString(c.label+" after reuse", c.want, renderByStructure(c.value))
	}
	after := engine.LicenceDiagnostics().KRefPlacement
	observations.note("pointees placed %d, clean %d, revoked %d, vetoed sites %d",
		after.PointeePlaced-before.PointeePlaced, after.PointeeClean-before.PointeeClean, after.PointeeRevoked-before.PointeeRevoked, after.PointeeVetoes-before.PointeeVetoes)
	window.settle(observations)
	inconclusive := ""
	if after.PointeePlaced == before.PointeePlaced {
		inconclusive = "no pointee was placed"
	}
	assertNoHazard(t, observations, inconclusive)
}

func TestKRefConfinedAndGoroutineBuildersMatchNativeGo(t *testing.T) {
	host := &krefHost{}
	service, compiled := krefService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	trees := make([]any, 0, krefRounds)
	for round := range krefRounds {
		observations.compareString(fmt.Sprintf("Confined(%d)", round), strconv.Itoa(krefConfined(round)), fmt.Sprint(hazardCall(t, service, compiled, "Confined", round)))
		sum := 0
		for g := range 4 {
			sum += krefConfined(round + g)
		}
		observations.compareString(fmt.Sprintf("Goroutines(%d)", round), renderByStructure([]int{sum, 4}), renderByStructure(hazardCall(t, service, compiled, "Goroutines", round)))
		trees = append(trees, hazardCall(t, service, compiled, "GoroutineTrees", round))
		hazardCall(t, service, compiled, "Callback", round)
		observations.compareString(fmt.Sprintf("Finalised(%d)", round), strconv.Itoa(len(krefMk(round))), fmt.Sprint(hazardCall(t, service, compiled, "Finalised", round)))
	}
	reuseKRefArenas(t, service, compiled)
	for round, value := range trees {
		want := make([]string, 3)
		for g := range 3 {
			want[g] = renderByStructure(krefTree(round+g, round+g+5))
		}
		got := reflect.ValueOf(value)
		var renders []string
		for index := range got.Len() {
			renders = append(renders, renderByStructure(got.Index(index).Interface()))
		}
		slices.Sort(renders)
		slices.Sort(want)
		observations.compareString(fmt.Sprintf("GoroutineTrees(%d)", round), strings.Join(want, " "), strings.Join(renders, " "))
	}
	require.Len(t, host.kept, 3*krefRounds)
	for n := range krefRounds {
		for i := range 3 {
			want := renderByStructure(&krefTestNode{Name: krefMk(n + i), Left: &krefTestNode{Name: krefMk(n + i + 10)}})
			observations.compareString(fmt.Sprintf("callback kept %d/%d", n, i), want, renderByStructure(host.kept[3*n+i]))
		}
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestKRefMemberPublishedByACallbackSurvivesReuse(t *testing.T) {
	host := &krefHost{}
	service, compiled := krefService(t, host)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	for round := range krefRounds {
		hazardCall(t, service, compiled, "Publish", round)
		reuseKRefArenas(t, service, compiled)
		held := hazardCall(t, service, compiled, "Held")
		want := renderByStructure(&krefTestNode{Name: krefMk(round + 20), Left: &krefTestNode{Name: krefMk(round + 10)}})
		observations.compareString(fmt.Sprintf("published member n=%d after reuse", round), want, renderByStructure(held))
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestKRefKeptInterfaceAdaptersHoldACopy(t *testing.T) {
	host := &krefHost{}
	adapters := &krefAdapters{}
	service, compiled := krefServiceWith(t, host, adapters)
	window := openHazardWindows(t)
	observations := &hazardObservations{}
	for n := range krefRounds {
		hazardCall(t, service, compiled, "Adapt", n)
	}
	reuseKRefArenas(t, service, compiled)
	require.Len(t, adapters.stringers, krefRounds)
	require.Len(t, adapters.errs, krefRounds)
	for n := range krefRounds {
		observations.compareString(fmt.Sprintf("kept Stringer n=%d", n), krefMk(n)+"/"+krefMk(n+1), adapters.stringers[n].String())
		observations.compareString(fmt.Sprintf("kept error n=%d", n), "fail:"+krefMk(n+2), adapters.errs[n].Error())
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func guardService(t *testing.T) (*app.Service, func() int) {
	t.Helper()
	kept := 0
	exports := maps.Clone(stdlib.Exports())
	exports["guardhost"] = map[string]reflect.Value{
		"Node": reflect.ValueOf((*guardHostNode)(nil)),
		"Keep": reflect.ValueOf(func(v []string) { kept += len(v) }),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	return service, func() int { return kept }
}

func TestSnapshotPlacementIsVetoedAfterAWalkBudgetEpoch(t *testing.T) {
	service, _ := guardService(t)
	compiled := hazardCompile(t, service, guardSource)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	const nodeCount = 4096
	check := func(call int) {
		got, ok := hazardCall(t, service, compiled, "AppendHostNodes", nodeCount).(*guardHostNode)
		require.True(t, ok, "call %d returns the host node", call)
		require.Equal(t, "root", got.Name)
		require.Len(t, got.Children, nodeCount)
		for i, child := range got.Children {
			require.Equal(t, "n"+strconv.Itoa(i), child.Name, "call %d child %d", call, i)
		}
	}

	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	before := engine.LicenceDiagnostics().KRefPlacement
	placed := make([]int64, 0, 8)
	for call := range 8 {
		previous := engine.LicenceDiagnostics().KRefPlacement
		check(call)
		placed = append(placed, engine.LicenceDiagnostics().KRefPlacement.SnapshotSlots-previous.SnapshotSlots)
	}
	after := engine.LicenceDiagnostics().KRefPlacement
	vetoes := after.SnapshotVetoes - before.SnapshotVetoes
	t.Logf("snapshot slots per call %v; vetoes %d (process %d)", placed, vetoes, after.SnapshotVetoes)

	placing, quiet := 0, 0
	for _, count := range placed {
		switch {
		case count >= nodeCount:
			placing++
		case count < nodeCount/16:
			quiet++
		}
	}
	if placing == 0 && after.SnapshotVetoes == 0 {
		t.Skip("no snapshot was placed (an inexact-probe or non-placing build)")
	}
	require.Equal(t, len(placed), placing+quiet, "a call either places the nodes (an arena new to the case) or none")
	require.Equal(t, int64(placing), vetoes, "every placing call's epoch ran out of its walk budget and vetoed the node type")
	require.Positive(t, after.SnapshotVetoes, "the node type is vetoed in the arenas that ran the case")
	require.GreaterOrEqual(t, quiet, len(placed)/2, "once vetoed, the nodes take the VM's boundary chunks")
}

func TestPointeePlacementIsVetoedInARevokedEpoch(t *testing.T) {
	service, kept := guardService(t)
	compiled := hazardCompile(t, service, guardSource)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	const placements = 70000
	before := engine.LicenceDiagnostics().KRefPlacement
	got := hazardCall(t, service, compiled, "PlaceThenRevoke", placements)
	require.Equal(t, len("q"+strconv.Itoa(placements)+"!"), got)
	first := engine.LicenceDiagnostics().KRefPlacement
	if first.PointeePlaced == before.PointeePlaced {
		t.Skip("no pointee was placed (an inexact-probe or non-placing build)")
	}
	require.Greater(t, first.PointeeEpochVetoes, before.PointeeEpochVetoes,
		"the placement's epoch was revoked whole, so the site is vetoed")
	for call := range 3 {
		got := hazardCall(t, service, compiled, "PlaceThenRevoke", placements+call)
		require.Equal(t, len("q"+strconv.Itoa(placements+call)+"!"), got)
	}
	after := engine.LicenceDiagnostics().KRefPlacement
	require.Equal(t, first.PointeePlaced, after.PointeePlaced, "a vetoed site allocates its pointee on the heap")
	require.Equal(t, 4*placements+3, kept())
	t.Logf("pointees placed %d, epoch vetoes %d", first.PointeePlaced-before.PointeePlaced, after.PointeeEpochVetoes-before.PointeeEpochVetoes)
}
