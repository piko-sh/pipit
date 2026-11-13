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
	"strconv"
	"strings"
	"sync"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type CopyRegistry struct {
	mu   sync.Mutex
	kept []string
}

type nativeCopyHost struct {
	mu   sync.Mutex
	kept []string
	reg  *CopyRegistry
}

const nativeCopySource = `package main

import (
	"copyhost"
	"strconv"
)

func mk(n, size int) string {
	s := "copy-" + strconv.Itoa(n) + "-"
	for len(s) < size {
		s = s + "abcdefghij"
	}
	return s[:size]
}

func KeepFunc(n, size int) int { return copyhost.Keep(mk(n, size)) }

func KeepVar(n, size int) int {
	f := copyhost.Keep
	return f(mk(n, size))
}

func KeepMethod(n, size int) int { return copyhost.Reg().Add(mk(n, size)) }

type adder interface{ Add(s string) int }

func KeepDynamic(n, size int) int {
	var a adder = copyhost.Reg()
	return a.Add(mk(n, size))
}

func KeepAny(n, size int) int { return copyhost.KeepAny(mk(n, size)) }

func KeepMany(n, size int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += copyhost.Keep(mk(n+i, size))
	}
	return t
}

func KeepInCallback(n, size int) int {
	t := 0
	copyhost.Each(4, func(i int) { t += copyhost.Keep(mk(n+i, size)) })
	return t
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7) + "-ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ")
	}
	return t
}
`

func (r *CopyRegistry) Add(s string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kept = append(r.kept, s)
	return len(s)
}

func (h *nativeCopyHost) keep(s string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kept = append(h.kept, s)
	return len(s)
}

func nativeCopyMk(seed, size int) string {
	text := "copy-" + strconv.Itoa(seed) + "-"
	for len(text) < size {
		text += "abcdefghij"
	}
	return text[:size]
}

func nativeCopyService(t *testing.T) (*app.Service, *program.CompiledFileSet, *nativeCopyHost) {
	t.Helper()
	host := &nativeCopyHost{reg: &CopyRegistry{}}
	exports := maps.Clone(stdlib.Exports())
	exports["copyhost"] = map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(s string) int { return host.keep(s) }),
		"KeepAny": reflect.ValueOf(func(value any) int {
			text, ok := value.(string)
			if !ok {
				return -1
			}
			return host.keep(text)
		}),
		"Registry": reflect.ValueOf((*CopyRegistry)(nil)),
		"Reg":      reflect.ValueOf(func() *CopyRegistry { return host.reg }),
		"Each": reflect.ValueOf(func(n int, fn func(int)) {
			for index := range n {
				fn(index)
			}
		}),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	return service, hazardCompile(t, service, nativeCopySource), host
}

func reuseNativeCopyArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 16)
	for round := range 8 {
		hazardCall(t, service, compiled, "Churn", 1000*round)
	}
}

func TestStringsKeptByRetainingNativesSurviveReuse(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	cases := []struct {
		id, entry string
		perCall   int
		method    bool
	}{
		{id: "func", entry: "KeepFunc", perCall: 1},
		{id: "func-value", entry: "KeepVar", perCall: 1},
		{id: "method", entry: "KeepMethod", perCall: 1, method: true},
		{id: "method-dynamic", entry: "KeepDynamic", perCall: 1, method: true},
		{id: "interface", entry: "KeepAny", perCall: 1},
		{id: "many", entry: "KeepMany", perCall: 64},
		{id: "callback", entry: "KeepInCallback", perCall: 4},
	}
	for _, tc := range cases {
		for _, size := range []int{8, 64, 256, engine.NativeCopyMaxBytes(), engine.NativeCopyMaxBytes() + 1} {
			t.Run(fmt.Sprintf("%s/size=%d", tc.id, size), func(t *testing.T) {
				window := openAuditWindow(t, 0)
				census := openCensusWindow()
				service, compiled, host := nativeCopyService(t)
				before := engine.LicenceDiagnostics().NativeCopies
				var want []string
				for round := range hazardRounds {
					seed := 100*round + 5
					hazardCall(t, service, compiled, tc.entry, seed, size)
					for i := range tc.perCall {
						want = append(want, nativeCopyMk(seed+i, size))
					}
				}
				stats := engine.LicenceDiagnostics().NativeCopies
				kept := func() []string {
					if tc.method {
						host.reg.mu.Lock()
						defer host.reg.mu.Unlock()
						return append([]string(nil), host.reg.kept...)
					}
					host.mu.Lock()
					defer host.mu.Unlock()
					return append([]string(nil), host.kept...)
				}
				observations := &hazardObservations{}
				observations.compareString("kept strings straight after the runs", strings.Join(want, ";"), strings.Join(kept(), ";"))
				reuseNativeCopyArenas(t, service, compiled)
				observations.compareString("kept strings after reuse", strings.Join(want, ";"), strings.Join(kept(), ";"))
				copied := stats.Copied - before.Copied
				observations.note("native copies: copied=%d copiedBytes=%d declined=%d", copied, stats.CopiedBytes-before.CopiedBytes, stats.Declined-before.Declined)
				switch {
				case size > engine.NativeCopyMaxBytes() || tc.id == "interface":
				case copied == 0:
					observations.fire("native-copy-not-applied", "no string of %d bytes was copied", size)
				}
				window.settle(observations)
				census.settle(observations)
				runtime.KeepAlive(host)
				assertNoHazard(t, observations, "")
			})
		}
	}
}

func TestValuesKeptFromStandardLibraryResultsSurviveReuse(t *testing.T) {
	route := newHandOffRoute(t, `var keepErr error
var keepFields []string
var keepWrapped error
var keepBuf *bytes.Buffer

func Fill(n int) int {
	keepErr = errors.New(mk(n))
	keepWrapped = fmt.Errorf("wrapped %s", mk(n+1))
	keepFields = strings.Fields(mk(n+2) + " " + mk(n+3))
	keepBuf = bytes.NewBuffer([]byte(mk(n + 4)))
	return n
}

func Read() string {
	return keepErr.Error() + "|" + keepWrapped.Error() + "|" + strings.Join(keepFields, ",") + "|" + keepBuf.String()
}
`, "bytes", "errors", "fmt", "strings")
	route.call("Fill", 4242)
	route.keeps(func() map[string]string {
		return map[string]string{"native results": fmt.Sprint(route.call("Read"))}
	})
	route.finish("native-argument")
}
