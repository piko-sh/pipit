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
	"image"
	"runtime"
	"sync/atomic"
	"testing"
	"weak"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
)

type expectedCall struct {
	name     string
	argument any
	want     any
}

const callSiteCacheSource = `package main

import (
	"fmt"
	"image"
	"strconv"
	"sync/atomic"
)

func ValueLocal(k int) string {
	p := image.Pt(k, k+1)
	return p.String()
}

func SliceElement(k int) string {
	ps := []image.Point{image.Pt(k, 1), image.Pt(2, k)}
	out := ""
	for i := range ps {
		out += ps[i].String()
	}
	return out
}

func PointerLocal(k int) int64 {
	var c atomic.Int64
	c.Add(int64(k))
	c.Add(7)
	return c.Load()
}

func PointerElement(k int) int64 {
	cs := make([]atomic.Int64, 3)
	for i := range cs {
		cs[i].Add(int64(k + i))
	}
	return cs[0].Load() + cs[1].Load()*10 + cs[2].Load()*100
}

func Iface(k int) string {
	var s fmt.Stringer
	if k%2 == 0 {
		s = image.Pt(k, 2)
	} else {
		s = image.Rect(k, 6, 7, 8)
	}
	return s.String()
}

func IfaceSlice(k int) string {
	ss := []fmt.Stringer{image.Pt(k, 1)}
	if k%2 == 1 {
		ss[0] = image.Rect(k, 2, 3, 4)
	}
	return ss[0].String()
}

func str[T fmt.Stringer](xs []T) string {
	return xs[0].String()
}

func Generic(k int) string {
	if k%2 == 0 {
		return str([]image.Point{image.Pt(k, 2)})
	}
	return str([]image.Rectangle{image.Rect(k, 6, 7, 8)})
}

type EmbedAt0 struct {
	image.Point
}

type EmbedAt8 struct {
	Tag int
	image.Point
}

func Promoted(k int) string {
	a := []EmbedAt0{{image.Pt(k, 3)}}
	b := []EmbedAt8{{Tag: 9, Point: image.Pt(4, k)}}
	return a[0].String() + b[0].String()
}

func strW[T fmt.Stringer](xs []T) string {
	return xs[0].String()
}

func PromotedGeneric(k int) string {
	if k%2 == 0 {
		return strW([]EmbedAt0{{image.Pt(k, 3)}})
	}
	return strW([]EmbedAt8{{Tag: k * 1000, Point: image.Pt(4, k)}})
}

type Holder struct {
	P image.Point
	R image.Rectangle
}

func Field(k int) string {
	h := &Holder{P: image.Pt(k, 5), R: image.Rect(1, 2, k, 4)}
	return h.P.String() + h.R.String()
}

func Closure(k int) int {
	acc := k
	add := func(n int) { acc += n }
	for i := 0; i < 3; i++ {
		add(i + 1)
	}
	return acc
}

func ValueCopy(k int) string {
	hs := []Holder{{P: image.Pt(k, 5)}}
	h := hs[0]
	return h.P.String() + strconv.Itoa(k)
}
`

func runCallRounds(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, observations *hazardObservations, rounds int, calls []expectedCall) {
	t.Helper()
	for round := range rounds {
		for _, call := range calls {
			var got any
			if call.argument == nil {
				got = hazardCall(t, service, compiled, call.name)
			} else {
				got = hazardCall(t, service, compiled, call.name, call.argument)
			}
			compareRendering(observations, fmt.Sprintf("round %d %s(%v)", round, call.name, call.argument), call.want, got)
		}
		clobberArenas(t, service, 2)
	}
	observations.note("%d rounds of %d calls compared with native Go", rounds, len(calls))
}

func callSiteCacheCalls() []expectedCall {
	calls := make([]expectedCall, 0, 44)
	for round := range 4 {
		var iface, ifaceSlice, generic string
		if round%2 == 0 {
			iface = image.Pt(round, 2).String()
			ifaceSlice = image.Pt(round, 1).String()
			generic = image.Pt(round, 2).String()
		} else {
			iface = image.Rect(round, 6, 7, 8).String()
			ifaceSlice = image.Rect(round, 2, 3, 4).String()
			generic = image.Rect(round, 6, 7, 8).String()
		}
		calls = append(calls,
			expectedCall{"ValueLocal", round, image.Pt(round, round+1).String()},
			expectedCall{"SliceElement", round, image.Pt(round, 1).String() + image.Pt(2, round).String()},
			expectedCall{"PointerLocal", round, int64(round + 7)},
			expectedCall{"PointerElement", round, int64(round) + int64(round+1)*10 + int64(round+2)*100},
			expectedCall{"Iface", round, iface},
			expectedCall{"IfaceSlice", round, ifaceSlice},
			expectedCall{"Generic", round, generic},
			expectedCall{"Promoted", round, image.Pt(round, 3).String() + image.Pt(4, round).String()},
			expectedCall{"Field", round, image.Pt(round, 5).String() + image.Rect(1, 2, round, 4).String()},
			expectedCall{"ValueCopy", round, image.Pt(round, 5).String() + itoa(round)},
			expectedCall{"Closure", round, int64(round + 6)},
		)
	}
	return calls
}

func TestNativeMethodCallSiteCacheKeepsNoReceiver(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled, counts := compileCountingMnemonics(t, service, callSiteCacheSource, "CALL_NATIVE", "BIND_METHOD_BY_NAME")
	observations := &hazardObservations{}
	observations.note("listing: CALL_NATIVE=%d BIND_METHOD_BY_NAME=%d", counts["CALL_NATIVE"], counts["BIND_METHOD_BY_NAME"])
	runCallRounds(t, service, compiled, observations, 6, callSiteCacheCalls())
	window.settle(observations)
	runtime.KeepAlive(compiled)
	assertNoHazard(t, observations, missingMnemonic(counts, "CALL_NATIVE"))
}

func TestNativeMethodCallSiteCacheKeepsNoReceiverPerShape(t *testing.T) {
	shapes := []string{"ValueLocal", "SliceElement", "PointerLocal", "PointerElement", "Iface", "IfaceSlice",
		"Generic", "Promoted", "Field", "ValueCopy", "Closure"}
	all := callSiteCacheCalls()
	for _, variant := range []string{"", "-cleared"} {
		for _, shape := range shapes {
			runCallSiteShape(t, shape+variant, shape, variant != "", all)
		}
	}
}

func clearCallSiteCaches(compiled *program.CompiledFileSet) int {
	cleared := 0
	for _, name := range compiled.FunctionNames() {
		function, err := compiled.FindFunction(name)
		if err != nil || function == nil {
			continue
		}
		for index := range function.CallSites {
			site := &function.CallSites[index]
			if atomic.SwapPointer(&site.NativeFastPath, nil) != nil {
				cleared++
			}
			if site.CachedClosureCallee != nil {
				site.CachedClosure, site.CachedClosureCallee, site.CachedClosureRoot = weak.Pointer[byte]{}, nil, nil
				cleared++
			}
		}
	}
	return cleared
}

func runCallSiteShape(t *testing.T, id, shape string, clearCaches bool, all []expectedCall) {
	t.Helper()
	{
		t.Run(id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			service := newHazardService(t)
			compiled, counts := compileCountingMnemonics(t, service, callSiteCacheSource, "CALL_NATIVE")
			var calls []expectedCall
			for _, call := range all {
				if call.name == shape {
					calls = append(calls, call)
				}
			}
			observations := &hazardObservations{}
			for round := range 8 {
				for _, call := range calls {
					got := hazardCall(t, service, compiled, call.name, call.argument)
					compareRendering(observations, fmt.Sprintf("round %d %s(%v)", round, call.name, call.argument), call.want, got)
				}
			}
			observations.note("8 rounds of %d calls without clobbering", len(calls))
			if clearCaches {
				observations.note("control: cleared %d native fast-path entries before settling", clearCallSiteCaches(compiled))
			}
			window.settle(observations)
			runtime.KeepAlive(compiled)
			assertNoHazard(t, observations, missingMnemonic(counts, "CALL_NATIVE"))
		})
	}
}
