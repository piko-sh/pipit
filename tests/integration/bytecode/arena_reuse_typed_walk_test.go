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
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type TypedWalkHostNode struct {
	Name     string
	Children []*TypedWalkHostNode
}

var typedWalkKept any

const typedWalkSource = `package main

import (
	"strconv"
	"walkhost"
)

type Item struct {
	ID   int
	Name string
}

type Node struct {
	Name  string
	Left  *Node
	Right *Node
}

func label(i int) string { return "walk-" + strconv.Itoa(i) + "-" + strconv.Itoa(i*7) }

func buildItems(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: i, Name: label(i)}
	}
	return out
}

func buildTree(lo, hi int) *Node {
	if lo >= hi {
		return nil
	}
	mid := (lo + hi) / 2
	return &Node{Name: label(mid), Left: buildTree(lo, mid), Right: buildTree(mid+1, hi)}
}

func render(n *Node) string {
	if n == nil {
		return ""
	}
	return render(n.Left) + n.Name + ";" + render(n.Right)
}

func ReturnItems(n int) []Item         { return buildItems(n) }
func ReturnTree(n int) *Node           { return buildTree(0, n) }
func ReturnItemPointer(n int) *Item    { it := &Item{ID: n, Name: label(n)}; return it }
func PassItems(n int) int              { walkhost.Keep(buildItems(n)); return n }
func PassTree(n int) int               { walkhost.Keep(buildTree(0, n)); return n }
func StoreItems(m map[string]any, n int) int { m["v"] = buildItems(n); return n }
func StoreTree(m map[string]any, n int) int  { m["v"] = buildTree(0, n); return n }
func AppendHostNodes(n int) *walkhost.Node {
	root := &walkhost.Node{Name: "root"}
	for i := 0; i < n; i++ {
		root.Children = append(root.Children, &walkhost.Node{Name: label(i)})
	}
	return root
}

type Pair struct {
	A Item
	B Item
}

func ReturnItemValue(n int) Item   { var it Item; it.ID = n; it.Name = label(n); return it }
func PassItemValue(n int) int      { var it Item; it.ID = n; it.Name = label(n); walkhost.Keep(it); return n }
func PassPairValue(n int) int      { var p Pair; p.A.Name = label(n); p.B.Name = label(n + 1); walkhost.Keep(p); return n }
func PassItemArray(n int) int      { var a [2]Item; a[0].Name = label(n); a[1].Name = label(n + 1); walkhost.Keep(a); return n }
func StoreItemValue(m map[string]any, n int) int { var it Item; it.Name = label(n); m["v"] = it; return n }

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7))
	}
	return t
}
`

func typedWalkLabel(i int) string { return fmt.Sprintf("walk-%d-%d", i, i*7) }

func renderTypedWalk(value reflect.Value) string {
	switch value.Kind() {
	case reflect.String:
		return value.String() + ";"
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return ""
		}
		return renderTypedWalk(value.Elem())
	case reflect.Slice, reflect.Array:
		var b strings.Builder
		for index := range value.Len() {
			b.WriteString(renderTypedWalk(value.Index(index)))
		}
		return b.String()
	case reflect.Struct:
		var b strings.Builder
		for _, field := range value.Fields() {
			b.WriteString(renderTypedWalk(field))
		}
		return b.String()
	case reflect.Map:
		return renderTypedWalk(value.MapIndex(reflect.ValueOf("v")))
	default:
		return ""
	}
}

func typedWalkWantItems(count int) string {
	var b strings.Builder
	for i := range count {
		b.WriteString(typedWalkLabel(i) + ";")
	}
	return b.String()
}

func typedWalkWantTree(low, high int) string {
	if low >= high {
		return ""
	}
	mid := (low + high) / 2
	return typedWalkLabel(mid) + ";" + typedWalkWantTree(low, mid) + typedWalkWantTree(mid+1, high)
}

func TestTypedWalkShapesKeptByTheHostSurviveReuse(t *testing.T) {
	const itemCount = 32
	hostNodes := "root;" + typedWalkWantItems(itemCount)
	cases := []struct {
		id, entry string
		host      bool
		want      string
	}{
		{"return-items", "ReturnItems", false, typedWalkWantItems(itemCount)},
		{"return-tree", "ReturnTree", false, typedWalkWantTree(0, itemCount)},
		{"return-item-pointer", "ReturnItemPointer", false, typedWalkLabel(itemCount) + ";"},
		{"native-items", "PassItems", false, typedWalkWantItems(itemCount)},
		{"native-tree", "PassTree", false, typedWalkWantTree(0, itemCount)},
		{"host-map-items", "StoreItems", true, typedWalkWantItems(itemCount)},
		{"host-map-tree", "StoreTree", true, typedWalkWantTree(0, itemCount)},
		{"field-append", "AppendHostNodes", false, hostNodes},
		{"return-item-value", "ReturnItemValue", false, typedWalkLabel(itemCount) + ";"},
		{"native-item-value", "PassItemValue", false, typedWalkLabel(itemCount) + ";"},
		{"native-pair-value", "PassPairValue", false, typedWalkLabel(itemCount) + ";" + typedWalkLabel(itemCount+1) + ";"},
		{"native-item-array", "PassItemArray", false, typedWalkLabel(itemCount) + ";" + typedWalkLabel(itemCount+1) + ";"},
		{"host-map-item-value", "StoreItemValue", true, typedWalkLabel(itemCount) + ";"},
	}
	exports := maps.Clone(stdlib.Exports())
	exports["walkhost"] = map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(v any) { typedWalkKept = v }),
		"Node": reflect.ValueOf((*TypedWalkHostNode)(nil)),
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			service := app.NewService(hazardServiceOptions()...)
			service.UseSymbols(symtab.NewSymbolRegistry(exports))
			compiled := hazardCompile(t, service, typedWalkSource)
			hostMap := map[string]any{}
			typedWalkKept = nil
			var result any
			if tc.host {
				hazardCall(t, service, compiled, tc.entry, hostMap, itemCount)
				result = hostMap
			} else {
				result = hazardCall(t, service, compiled, tc.entry, itemCount)
				if typedWalkKept != nil {
					result = typedWalkKept
				}
			}
			observations := &hazardObservations{}
			observations.compareString("host-kept value straight after the run", tc.want, renderTypedWalk(reflect.ValueOf(result)))
			clobberArenas(t, service, 24)
			for round := range 8 {
				hazardCall(t, service, compiled, "Churn", 1000*round)
			}
			observations.compareString("host-kept value after reuse", tc.want, renderTypedWalk(reflect.ValueOf(result)))
			window.settle(observations)
			assertNoHazard(t, observations, "")
		})
	}
}
