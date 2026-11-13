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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type declarationHost struct {
	keptErrors  []error
	keptStrings []string
	keptBytes   [][]byte
	counter     int
}

const borrowDeclarationSource = `package main

import (
	"errors"
	"declhost"
	"strconv"
)

func mk(n int) string { return "decl-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) + "-tail" }

func ScoreOnly(n int) int {
	s := mk(n)
	xs := []string{mk(n + 1), mk(n + 2)}
	return declhost.Score(s, xs)
}

func Run(n int) int {
	total := ScoreOnly(n)
	declhost.KeepErr(errors.New(mk(n + 3)))
	declhost.KeepStr(mk(n + 4))
	total += declhost.Apply([]string{mk(n + 5)}, func(s string) int { return len(s) })
	return total
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7))
	}
	return t
}
`

const declarationRounds = 12

const readsOnlySource = `package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"declhost"
	"strconv"
	"unicode/utf8"
)

func mk(n int) string { return "decl-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) + "-tail" }

func Views(n int) string {
	s := mk(n)
	sep := "-"
	a := bytes.Contains([]byte(s), []byte(sep))
	b := utf8.Valid([]byte(s))
	h := sha256.Sum256([]byte(s))
	x := hex.EncodeToString([]byte(s))
	c := bytes.Count([]byte(s), []byte(sep))
	return strconv.FormatBool(a) + strconv.FormatBool(b) + hex.EncodeToString(h[:4]) + x + strconv.Itoa(c) + s
}

func Writers(n int) string {
	s := mk(n)
	k := declhost.Scribble([]byte(s))
	m := declhost.Peek([]byte(s))
	kept := declhost.KeepBytes([]byte(s))
	return s + "/" + strconv.Itoa(k) + "/" + strconv.Itoa(m) + "/" + strconv.Itoa(kept)
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7))
	}
	return t
}
`

type globalReuseCase struct {
	id       string
	source   string
	want     func(round int) string
	eligible bool
	skips    bool
	between  func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet)
}

const globalReuseChurn = `

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7))
	}
	return t
}
`

var globalReuseCases = []globalReuseCase{
	{
		id: "tables", eligible: true, skips: true,
		source: `package main

import (
	"strconv"
	"strings"
)

var table = map[string]int{"a": 1, "bb": 2, "ccc": 3}
var names = []string{"zeta", "alpha", "mid"}
var limits = struct{ Lo, Hi int }{Lo: 2, Hi: 9}
var banner = strings.Repeat("=", 4) + "x"
var label = func() string { return strings.ToUpper(names[1]) }()
var grid = [3]int{4, 5, 6}

func run() string {
	total := 0
	for k, v := range table {
		total += v + len(k)
	}
	out := banner + label + strconv.Itoa(total) + names[0] + strconv.Itoa(len(names)) + strconv.Itoa(limits.Hi-limits.Lo) + strconv.Itoa(grid[1])
	for _, n := range names {
		out += n[:1]
	}
	return out
}
` + globalReuseChurn,
		want: func(int) string { return "====xALPHA12zeta375zam" },
	},
	{
		id: "scalar-written", eligible: true,
		source: `package main

import "strconv"

var hits = 0
var greeting = "hi"

func run() string {
	hits++
	greeting += "!"
	return greeting + strconv.Itoa(hits)
}
` + globalReuseChurn,
		want: func(int) string { return "hi!1" },
	},
	{
		id: "struct-field-written", eligible: true,
		source: `package main

import "strconv"

type config struct {
	N int
	S string
}

var cfg = config{N: 1, S: "x"}

func run() string {
	cfg.N++
	cfg.S += "y"
	return strconv.Itoa(cfg.N) + cfg.S
}
` + globalReuseChurn,
		want: func(int) string { return "2xy" },
	},
	{
		id: "sorted-by-native", eligible: true,
		source: `package main

import (
	"sort"
	"strconv"
	"strings"
)

var names = []string{"c", "a", "b"}

func run() string {
	first := names[0]
	sort.Strings(names)
	return first + strings.Join(names, "")
}
` + globalReuseChurn,
		want: func(int) string { return "cabc" },
	},
	{
		id: "reads-only-native", eligible: true, skips: true,
		source: `package main

import (
	"bytes"
	"strconv"
)

var raw = []byte("abc,def")

func run() string {
	return strconv.FormatBool(bytes.Contains(raw, []byte(","))) + strconv.Itoa(bytes.Count(raw, []byte("d")))
}
` + globalReuseChurn,
		want: func(int) string { return "true1" },
	},
	{
		id: "written-by-call-function", eligible: true,
		source: `package main

import "strconv"

var hits = 1

func Bump() int {
	hits = 41
	return hits
}

func run() string { return strconv.Itoa(hits + 1) }
` + globalReuseChurn,
		want: func(int) string { return "2" },
		between: func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
			hazardCall(t, service, compiled, "Bump")
		},
	},
	{
		id: "map-written-in-place",
		source: `package main

import "strconv"

var counts = map[string]int{}

func run() string {
	counts["k"]++
	return strconv.Itoa(counts["k"])
}
` + globalReuseChurn,
		want: func(int) string { return "1" },
	},
	{
		id: "impure-initialiser",
		source: `package main

import (
	"declhost"
	"strconv"
)

var stamp = declhost.Next()

func run() string { return strconv.Itoa(stamp) }
` + globalReuseChurn,
		want: func(round int) string { return strconv.Itoa(round + 1) },
	},
	{
		id: "global-escapes",
		source: `package main

import (
	"declhost"
	"strconv"
	"strings"
)

var names = []string{"a", "b"}

func keep(xs []string) int { return len(xs) }

func run() string {
	declhost.KeepStr(strings.Join(names, ""))
	return strconv.Itoa(keep(names))
}
` + globalReuseChurn,
		want: func(int) string { return "2" },
	},
}

func hostScore(text string, xs []string) int {
	total := len(text)
	for _, x := range xs {
		total += len(x) + strings.Count(x, "-")
	}
	return total
}

func hostApply(xs []string, score func(string) int) int {
	total := 0
	for _, x := range xs {
		total += score(x)
	}
	return total
}

func hostScribble(b []byte) int {
	if len(b) > 0 {
		b[0] = 'X'
	}
	return len(b)
}

func hostPeek(b []byte) int {
	total := 0
	for _, c := range b {
		total += int(c)
	}
	return total
}

func declarationExports(host *declarationHost) symtab.SymbolExports {
	exports := maps.Clone(stdlib.Exports())
	exports["declhost"] = map[string]reflect.Value{
		"Score":    reflect.ValueOf(hostScore),
		"Apply":    reflect.ValueOf(hostApply),
		"Scribble": reflect.ValueOf(hostScribble),
		"Peek":     reflect.ValueOf(hostPeek),
		"KeepErr":  reflect.ValueOf(func(err error) { host.keptErrors = append(host.keptErrors, err) }),
		"KeepStr":  reflect.ValueOf(func(s string) { host.keptStrings = append(host.keptStrings, s) }),
		"KeepBytes": reflect.ValueOf(func(b []byte) int {
			host.keptBytes = append(host.keptBytes, b)
			return len(b)
		}),
		"Next": reflect.ValueOf(func() int { host.counter++; return host.counter }),
	}
	return exports
}

func declarationService(t *testing.T, h *declarationHost) *app.Service {
	t.Helper()
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(declarationExports(h)))
	return service
}

func declarationMk(n int) string {
	return "decl-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) + "-tail"
}

func reuseDeclarationArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 16)
	if compiled != nil {
		for round := range 8 {
			hazardCall(t, service, compiled, "Churn", 1000*round)
		}
	}
}

func finishDeclarationCase(t *testing.T, observations *hazardObservations, window *auditWindow) {
	t.Helper()
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestBorrowDeclarationsAreCheckedAndKeptValuesSurviveReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	host := &declarationHost{}
	service := declarationService(t, host)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().HostDeclarations
	require.NoError(t, service.DeclareNativeBorrowClass(hostScore, engine.BorrowBorrows), "an honest borrowing native is accepted")
	refused := map[string]error{
		"errors.New borrows (gc tags retain)": service.DeclareNativeBorrowClass(errors.New, engine.BorrowBorrows),
		"Apply borrows (callback parameter)":  service.DeclareNativeBorrowClass(hostApply, engine.BorrowBorrows),
		"strings.Builder.Len (method value)":  service.DeclareNativeBorrowClass((&strings.Builder{}).Len, engine.BorrowBorrows),
		"Peek readsOnly ([]byte -> int)":      service.DeclareNativeBorrowClass(hostPeek, engine.BorrowReadsOnly),
	}
	for label, err := range refused {
		wantRefused := !strings.HasPrefix(label, "Peek")
		switch {
		case wantRefused && !errors.Is(err, engine.ErrBorrowDeclarationRefused):
			observations.fire("declaration-not-refused", "%s: err=%v", label, err)
		case !wantRefused && err != nil:
			observations.fire("declaration-refused", "%s: err=%v", label, err)
		default:
			observations.note("%s: err=%v", label, err)
		}
	}
	compiled := hazardCompile(t, service, borrowDeclarationSource)
	got := make([]int, 0, declarationRounds)
	for round := range declarationRounds {
		got = append(got, hazardCall(t, service, compiled, "Run", 10*round+1).(int))
	}
	if err := service.DeclareNativeBorrowClass(hostPeek, engine.BorrowBorrows); !errors.Is(err, engine.ErrBorrowDeclarationsFrozen) {
		observations.fire("declaration-not-frozen", "a declaration after the first evaluation returned %v", err)
	}
	reuseDeclarationArenas(t, service, compiled)
	for round, value := range got {
		seed := 10*round + 1
		want := hostScore(declarationMk(seed), []string{declarationMk(seed + 1), declarationMk(seed + 2)}) + len(declarationMk(seed+5))
		observations.compareString(fmt.Sprintf("Run(%d)", seed), strconv.Itoa(want), strconv.Itoa(value))
		observations.compareString(fmt.Sprintf("kept error %d after reuse", seed), declarationMk(seed+3), host.keptErrors[round].Error())
		observations.compareString(fmt.Sprintf("kept string %d after reuse", seed), declarationMk(seed+4), host.keptStrings[round])
	}
	stats := engine.LicenceDiagnostics().HostDeclarations
	observations.note("borrow declarations: declared=%d refused=%d refusals=%v", stats.Declared-before.Declared, stats.Refused-before.Refused, service.BorrowDeclarationRefusals())
	finishDeclarationCase(t, observations, window)
}

func TestBorrowDeclarationRemovesTheRevocation(t *testing.T) {
	window := openAuditWindow(t, 0)
	observations := &hazardObservations{}
	unknown := isa.HandOffNativeArgUnknown.String()
	counts := map[bool]int64{}
	for _, declare := range []bool{false, true} {
		host := &declarationHost{}
		service := declarationService(t, host)
		if declare {
			require.NoError(t, service.DeclareNativeBorrowClass(hostScore, engine.BorrowBorrows))
		}
		compiled := hazardCompile(t, service, borrowDeclarationSource)
		before := engine.LicenceDiagnostics().Reuse.HandOffs[unknown]
		for round := range declarationRounds {
			n := 10*round + 1
			value := requireAs[int](t, hazardCall(t, service, compiled, "ScoreOnly", n))
			observations.compareString(fmt.Sprintf("declared=%v ScoreOnly(%d)", declare, n),
				strconv.Itoa(hostScore(declarationMk(n), []string{declarationMk(n + 1), declarationMk(n + 2)})), strconv.Itoa(value))
		}
		counts[declare] = engine.LicenceDiagnostics().Reuse.HandOffs[unknown] - before
		reuseDeclarationArenas(t, service, compiled)
	}
	observations.note("unknown-class native hand-offs: undeclared=%d declared=%d", counts[false], counts[true])
	if engine.LicenceDiagnostics().Reuse.HandOffs != nil && counts[true] != 0 {
		observations.fire("borrow-declaration-not-applied", "a declared borrowing native still revoked (%d unknown-class hand-offs)", counts[true])
	}
	finishDeclarationCase(t, observations, window)
}

func nativeViews(seed int) string {
	text := declarationMk(seed)
	sep := "-"
	digest := sha256.Sum256([]byte(text))
	return strconv.FormatBool(strings.Contains(text, sep)) + strconv.FormatBool(utf8.ValidString(text)) +
		hex.EncodeToString(digest[:4]) + hex.EncodeToString([]byte(text)) + strconv.Itoa(strings.Count(text, sep)) + text
}

func nativeWriters(n int) string {
	s := declarationMk(n)
	return s + "/" + strconv.Itoa(len(s)) + "/" + strconv.Itoa(hostPeek([]byte(s))) + "/" + strconv.Itoa(len(s))
}

func TestReadsOnlyNativesGetViewsAndWritersGetCopies(t *testing.T) {
	window := openAuditWindow(t, 0)
	host := &declarationHost{}
	service := declarationService(t, host)
	require.NoError(t, service.DeclareNativeBorrowClass(hostPeek, engine.BorrowReadsOnly))
	compiled := hazardCompile(t, service, readsOnlySource)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().HostDeclarations
	views, writers := make([]string, 0, declarationRounds), make([]string, 0, declarationRounds)
	for round := range declarationRounds {
		n := 10*round + 1
		views = append(views, hazardCall(t, service, compiled, "Views", n).(string))
		writers = append(writers, hazardCall(t, service, compiled, "Writers", n).(string))
	}
	stats := engine.LicenceDiagnostics().HostDeclarations
	for round := range declarationRounds {
		n := 10*round + 1
		observations.compareString(fmt.Sprintf("Views(%d)", n), nativeViews(n), views[round])
		observations.compareString(fmt.Sprintf("Writers(%d)", n), nativeWriters(n), writers[round])
	}
	reuseDeclarationArenas(t, service, compiled)
	for round, kept := range host.keptBytes {
		n := 10*round + 1
		observations.compareString(fmt.Sprintf("kept bytes %d after reuse", n), declarationMk(n), string(kept))
	}
	views64, fallbacks := stats.Views-before.Views, stats.ViewFallbacks-before.ViewFallbacks
	observations.note("reads-only: views=%d fallbacks=%d attestations=%d checksums=%d violations=%d", views64, fallbacks,
		stats.Attestations-before.Attestations, stats.Checksums-before.Checksums, stats.Violations-before.Violations)
	if fallbacks < 2*declarationRounds {
		observations.fire("reads-only-fallback-missing", "%d copies for the %d writing and retaining calls", fallbacks, 2*declarationRounds)
	}
	if views64+stats.Attestations-before.Attestations == 0 {
		observations.fire("reads-only-not-applied", "no []byte(s) argument was passed as the string's bytes")
	}
	if stats.Violations != before.Violations {
		observations.fire("reads-only-violation", "an honest reads-only native was reported writing: %v", engine.ReadsOnlyViolations())
	}
	finishDeclarationCase(t, observations, window)
}

func TestGlobalReuseSkipsInitialisersOnlyWhileTheGlobalsAreUnchanged(t *testing.T) {
	for _, testCase := range globalReuseCases {
		t.Run(testCase.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			host := &declarationHost{}
			service := declarationService(t, host)
			compiled := hazardCompile(t, service, testCase.source)
			observations := &hazardObservations{}
			plan := service.VarInitReusePlanFor(compiled)
			observations.note("plan: eligible=%v reason=%q", plan.Eligible, plan.Reason)
			if plan.Eligible != testCase.eligible {
				observations.fire("global-reuse-analysis", "eligible=%v, want %v (%s)", plan.Eligible, testCase.eligible, plan.Reason)
			}
			before := service.VarInitReuseStatistics()
			watchesBefore := engine.LicenceDiagnostics().HostDeclarations
			for round := range declarationRounds {
				result, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
				require.NoError(t, err)
				observations.compareString(fmt.Sprintf("run %d", round), testCase.want(round), fmt.Sprint(result))
				if testCase.between != nil {
					testCase.between(t, service, compiled)
				}
				if round%4 == 3 {
					reuseDeclarationArenas(t, service, compiled)
				}
			}
			stats := service.VarInitReuseStatistics()
			skips := stats.Skips - before.Skips
			observations.note("global reuse: skips=%d reruns=%d ineligible=%d last=%q dirtyByNative=%d", skips, stats.Reruns-before.Reruns,
				stats.Ineligible-before.Ineligible, stats.LastFallback, engine.LicenceDiagnostics().HostDeclarations.DirtyByNative-watchesBefore.DirtyByNative)
			wantSkips := int64(0)
			if testCase.skips {
				wantSkips = declarationRounds - 1
			}
			if skips != wantSkips {
				observations.fire("global-reuse-skips", "%d skipped initialiser runs, want %d", skips, wantSkips)
			}
			finishDeclarationCase(t, observations, window)
		})
	}
}

func TestGlobalReuseWithTwoFileSetsSharingTheGlobals(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := declarationService(t, &declarationHost{})
	first := hazardCompile(t, service, globalReuseCases[0].source)
	second := hazardCompile(t, service, strings.Replace(globalReuseCases[0].source, `"zeta"`, `"omega"`, 1))
	observations := &hazardObservations{}
	for round := range declarationRounds {
		for index, compiled := range []*program.CompiledFileSet{first, second} {
			result, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
			require.NoError(t, err)
			want := globalReuseCases[0].want(round)
			if index == 1 {
				want = strings.Replace(want, "zeta", "omega", 1)
				want = strings.Replace(want, "zam", "oam", 1)
			}
			observations.compareString(fmt.Sprintf("file set %d run %d", index, round), want, fmt.Sprint(result))
		}
	}
	stats := service.VarInitReuseStatistics()
	observations.note("global reuse: skips=%d reruns=%d last=%q", stats.Skips, stats.Reruns, stats.LastFallback)
	finishDeclarationCase(t, observations, window)
}

func TestReadsOnlyStdlibFunctionsAreChecked(t *testing.T) {
	listed := map[string]bool{}
	for _, name := range engine.ReadsOnlyStdlibFunctions() {
		listed[name] = false
	}
	for _, symbols := range stdlib.Exports() {
		for _, value := range symbols {
			if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
				continue
			}
			info := runtime.FuncForPC(value.Pointer())
			if info == nil {
				continue
			}
			if _, isListed := listed[info.Name()]; !isListed {
				continue
			}
			listed[info.Name()] = true
			require.Empty(t, engine.ReadsOnlyRefusal(value.Interface()), "%s fails the reads-only declaration checks", info.Name())
		}
	}
	for name, found := range listed {
		require.True(t, found, "%s is not exported by the standard library symbols", name)
	}
	require.Empty(t, engine.ReadsOnlyDisagreements())
}
