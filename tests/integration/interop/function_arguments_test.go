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

//go:build integration

package interop_test

import (
	"iter"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/sdk/stdlib"
)

type hostShape interface{ Area() int }

type hostSquare int

func (s hostSquare) Area() int { return int(s * s) }

type hostShapePredicate func(hostShape) bool

type hostSizer struct{ limit int }

func (s hostSizer) Above(shape hostShape) bool { return shape.Area() > s.limit }

func newFunctionArgumentService(t *testing.T, options ...app.Option) *app.Service {
	t.Helper()
	service := app.NewService(options...)
	service.UseSymbolProviders(stdlib.Providers()...)
	isBig := func(shape hostShape) bool { return shape.Area() > 10 }
	threshold := 3
	signalled := make(chan int, 1)
	service.RegisterPackage("host", map[string]reflect.Value{
		"Shape":     reflect.ValueOf((*hostShape)(nil)),
		"Predicate": reflect.ValueOf((*hostShapePredicate)(nil)),
		"Sizer":     reflect.ValueOf((*hostSizer)(nil)),
		"Shapes": reflect.ValueOf(func() []hostShape {
			return []hostShape{hostSquare(2), hostSquare(5), hostSquare(1)}
		}),
		"IsBig":      reflect.ValueOf(isBig),
		"BigByType":  reflect.ValueOf(hostShapePredicate(isBig)),
		"AboveThree": reflect.ValueOf(func(v any) bool { n, ok := v.(int); return ok && n > threshold }),
		"IsEven":     reflect.ValueOf(func(n int) bool { return n%2 == 0 }),
		"ByArea":     reflect.ValueOf(func(a, b hostShape) int { return a.Area() - b.Area() }),
		"Matches":    reflect.ValueOf(func(key string, value int) bool { return len(key) == value }),
		"Descending": reflect.ValueOf(func(values []int) func(i, j int) bool {
			return func(i, j int) bool { return values[i] > values[j] }
		}),
		"Pairs": reflect.ValueOf(func() iter.Seq2[string, int] {
			return func(yield func(string, int) bool) {
				for _, key := range []string{"x", "yy"} {
					if !yield(key, len(key)) {
						return
					}
				}
			}
		}),
		"Areas": reflect.ValueOf(func() iter.Seq[hostShape] {
			return func(yield func(hostShape) bool) {
				_ = yield(hostSquare(3)) && yield(hostSquare(4))
			}
		}),
		"Biggest": reflect.ValueOf(func() hostShape { return hostSquare(9) }),
		"Signal": reflect.ValueOf(func(shape hostShape) bool {
			signalled <- shape.Area()
			return true
		}),
		"Signalled": reflect.ValueOf(func() <-chan int { return signalled }),
	})
	return service
}

func TestNativeFunctionValuesReachGenericHelpersWithTheirOwnSignatures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		imports string
		code    string
		want    any
	}{
		{name: "ContainsFunc, named interface", imports: `"host"; "slices"`, want: true,
			code: `slices.ContainsFunc(host.Shapes(), host.IsBig)`},
		{name: "IndexFunc, named interface", imports: `"host"; "slices"`, want: 1,
			code: `slices.IndexFunc(host.Shapes(), host.IsBig)`},
		{name: "DeleteFunc, named interface", imports: `"host"; "slices"`, want: 2,
			code: `len(slices.DeleteFunc(host.Shapes(), host.IsBig))`},
		{name: "SortFunc, named interface", imports: `"host"; "slices"`, want: 25,
			code: `s := host.Shapes(); slices.SortFunc(s, host.ByArea); s[2].Area()`},
		{name: "ContainsFunc, unnamed interface", imports: `"host"; "slices"`, want: true,
			code: `slices.ContainsFunc([]any{"a", 2, 7}, host.AboveThree)`},
		{name: "ContainsFunc, scalar element", imports: `"host"; "slices"`, want: true,
			code: `slices.ContainsFunc([]int{1, 3, 4}, host.IsEven)`},
		{name: "named function type", imports: `"host"; "slices"`, want: true,
			code: `slices.ContainsFunc(host.Shapes(), host.BigByType)`},
		{name: "converted to a named function type", imports: `"host"; "slices"`, want: 1,
			code: `slices.IndexFunc(host.Shapes(), host.Predicate(host.IsBig))`},
		{name: "method value", imports: `"host"; "slices"`, want: 0,
			code: `slices.IndexFunc(host.Shapes(), host.Sizer{}.Above)`},
		{name: "method value held in a variable", imports: `"host"; "slices"`, want: true,
			code: `above := host.Sizer{}.Above; slices.ContainsFunc(host.Shapes()[:1], above)`},
		{name: "function held in a variable", imports: `"host"; "slices"`, want: true,
			code: `f := host.IsBig; slices.ContainsFunc(host.Shapes(), f)`},
		{name: "script closure", imports: `"host"; "slices"`, want: true,
			code: `slices.ContainsFunc(host.Shapes(), func(s host.Shape) bool { return host.IsBig(s) })`},
		{name: "script function", imports: `"host"; "slices"`, want: 1, code: `
func big(s host.Shape) bool { return s.Area() > 10 }
slices.IndexFunc(host.Shapes(), big)`},
		{name: "script generic function", imports: `"host"`, want: true, code: `
func anyMatch[E any](items []E, match func(E) bool) bool {
	for _, item := range items {
		if match(item) {
			return true
		}
	}
	return false
}
anyMatch(host.Shapes(), host.IsBig)`},
		{name: "maps.DeleteFunc", imports: `"host"; "maps"`, want: 1,
			code: `m := map[string]int{"ab": 2, "c": 5}; maps.DeleteFunc(m, host.Matches); len(m)`},
		{name: "sort.Slice", imports: `"host"; "sort"`, want: 9,
			code: `s := []int{1, 9, 4}; sort.Slice(s, host.Descending(s)); s[0]`},
		{name: "script iterator into slices.AppendSeq", imports: `"host"; "slices"`, want: 25, code: `
s := slices.AppendSeq([]host.Shape(nil), func(yield func(host.Shape) bool) {
	for _, shape := range host.Shapes() {
		if !yield(shape) {
			return
		}
	}
})
s[1].Area()`},
		{name: "native iterator into maps.Insert", imports: `"host"; "maps"`, want: 2,
			code: `m := map[string]int{}; maps.Insert(m, host.Pairs()); m["yy"]`},
		{name: "native iterator into slices.AppendSeq", imports: `"host"; "slices"`, want: 16,
			code: `s := slices.AppendSeq([]host.Shape(nil), host.Areas()); s[1].Area()`},
		{name: "native producer into sync.OnceValue", imports: `"host"; "sync"`, want: 81,
			code: `once := sync.OnceValue(host.Biggest); once().Area()`},
		{name: "deferred call", imports: `"host"; "slices"`, want: true, code: `
func check() (found bool) {
	defer func() { found = slices.ContainsFunc(host.Shapes(), host.IsBig) }()
	return false
}
check()`},
		{name: "deferred native call", imports: `"host"; "slices"`, want: 2, code: `
func trim() (n int) {
	s := host.Shapes()
	defer slices.SortFunc(s, host.ByArea)
	return len(slices.DeleteFunc(s, host.IsBig))
}
trim()`},
		{name: "goroutine", imports: `"host"; "slices"`, want: true, code: `
done := make(chan bool)
go func() { done <- slices.ContainsFunc(host.Shapes(), host.IsBig) }()
<-done`},
		{name: "native goroutine", imports: `"host"; "slices"`, want: 4, code: `
go slices.ContainsFunc(host.Shapes()[:1], host.Signal)
<-host.Signalled()`},
	}

	modes := []struct {
		name    string
		options []app.Option
	}{
		{name: "default dispatch", options: nil},
		{name: "go dispatch", options: []app.Option{app.WithForceGoDispatch()}},
	}
	for _, tt := range tests {
		for _, mode := range modes {
			t.Run(tt.name+"/"+mode.name, func(t *testing.T) {
				t.Parallel()
				service := newFunctionArgumentService(t, mode.options...)
				source := "import (" + tt.imports + ")\n" + tt.code
				result, err := service.Eval(t.Context(), source)
				require.NoError(t, err)
				require.Equal(t, tt.want, result)
			})
		}
	}
}
