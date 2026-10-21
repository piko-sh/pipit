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

package bytecode_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/sdk/stdlib"
)

const finalisedNewsSource = `package main

import (
	"runtime"
	"time"
)

func FinalisedNews(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		x := new([4]int64)
		x[0] = int64(i)
		runtime.SetFinalizer(x, func(p *[4]int64) { finalised <- p[0] })
	}
	return await(finalised, n)
}
` + finaliserAwaitSource

const finaliserAwaitSource = `
// await collects finaliser reports until more than nine tenths of n values have reported.
// It returns how many distinct values reported, or a negative number when a report was
// wrong or repeated. It gives up once five seconds pass with no report, or after two
// minutes. A loaded machine (a whole suite running, the census build) runs finalisers
// slowly but steadily, and a fixed window would fail it while they are still arriving.
// It runs a collection on each tick that brought no report, so a pending finaliser is
// queued without collections crowding out the ones already running.
func await(finalised chan int64, n int) int {
	seen := make(map[int64]bool, n)
	count := 0
	stall := time.NewTimer(5 * time.Second)
	defer stall.Stop()
	limit := time.After(2 * time.Minute)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	progressed := false
	runtime.GC()
	for count <= n/10*9 {
		select {
		case <-stall.C:
			return count
		case <-limit:
			return count
		case <-tick.C:
			if !progressed {
				runtime.GC()
			}
			progressed = false
		case id := <-finalised:
			if id < 0 || seen[id] {
				return -1 - count
			}
			seen[id] = true
			count++
			progressed = true
			stall.Reset(5 * time.Second)
		}
	}
	return count
}
`

var finaliserDispatches = []struct {
	name string

	options []app.Option
}{
	{name: "default", options: nil},
	{name: "go", options: []app.Option{app.WithForceGoDispatch()}},
}

const finalisedLiteralsSource = `package main

import (
	"runtime"
	"time"
)

type small struct {
	id    int64
	check int64
}

type large struct {
	id    int64
	data  [512]int64
	check int64
}

type pointers struct {
	id    int64
	name  string
	next  *pointers
	vals  []int
	check int64
}

type inner struct {
	a, b int64
}

type nested struct {
	id    int64
	in    inner
	pair  [2]inner
	child *inner
	check int64
}

func check(id int64) int64 { return id*7919 + 13 }

func Small(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		x := &small{id: int64(i), check: check(int64(i))}
		runtime.SetFinalizer(x, func(p *small) {
			if p.check != check(p.id) {
				finalised <- -1
				return
			}
			finalised <- p.id
		})
	}
	return await(finalised, n)
}

func Large(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		x := &large{id: int64(i), check: check(int64(i))}
		x.data[511] = int64(i)
		runtime.SetFinalizer(x, func(p *large) {
			if p.check != check(p.id) || p.data[511] != p.id {
				finalised <- -1
				return
			}
			finalised <- p.id
		})
	}
	return await(finalised, n)
}

func Pointers(n int) int {
	finalised := make(chan int64, n)
	var prev *pointers
	for i := 0; i < n; i++ {
		x := &pointers{id: int64(i), name: "value", next: prev, vals: []int{i, i + 1}, check: check(int64(i))}
		runtime.SetFinalizer(x, func(p *pointers) {
			if p.check != check(p.id) || p.name != "value" || len(p.vals) != 2 || p.vals[0] != int(p.id) {
				finalised <- -1
				return
			}
			finalised <- p.id
		})
		if i%2 == 0 {
			prev = x
		} else {
			prev = nil
		}
	}
	prev = nil
	return await(finalised, n)
}

func Nested(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		id := int64(i)
		x := &nested{id: id, in: inner{a: id, b: 1}, pair: [2]inner{{a: 2}, {b: id}}, child: &inner{a: id}, check: check(id)}
		runtime.SetFinalizer(x, func(p *nested) {
			if p.check != check(p.id) || p.in.a != p.id || p.pair[1].b != p.id || p.child.a != p.id {
				finalised <- -1
				return
			}
			finalised <- p.id
		})
	}
	return await(finalised, n)
}

func makeSmall(i int, finalised chan int64) *small {
	x := &small{id: int64(i), check: check(int64(i))}
	runtime.SetFinalizer(x, func(p *small) { finalised <- p.id })
	return x
}

func Returned(n int) int {
	finalised := make(chan int64, n)
	total := int64(0)
	for i := 0; i < n; i++ {
		total += makeSmall(i, finalised).check
	}
	if total == 0 {
		return -1
	}
	return await(finalised, n)
}

func (s *small) register(finalised chan int64) {
	runtime.SetFinalizer(s, func(p *small) {
		if p.check != check(p.id) {
			finalised <- -1
			return
		}
		finalised <- p.id
	})
}

// Receiver registers each finaliser in a pointer-receiver method of a local struct value.
func Receiver(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		v := small{id: int64(i), check: check(int64(i))}
		v.register(finalised)
	}
	return await(finalised, n)
}

// Local takes the address of a declared local variable.
func Local(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		var v small
		v.id, v.check = int64(i), check(int64(i))
		p := &v
		p.register(finalised)
	}
	return await(finalised, n)
}

// Captured takes the address of a local variable a closure captures.
func Captured(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		v := small{id: int64(i)}
		set := func() { v.check = check(v.id) }
		set()
		(&v).register(finalised)
	}
	return await(finalised, n)
}

// Goroutine makes the values in another goroutine.
func Goroutine(n int) int {
	finalised := make(chan int64, n)
	done := make(chan bool)
	go func() {
		for i := 0; i < n; i++ {
			x := &small{id: int64(i), check: check(int64(i))}
			x.register(finalised)
		}
		done <- true
	}()
	<-done
	return await(finalised, n)
}

type containers struct {
	id    int64
	vals  []int
	names map[string]int
	check int64
}

// Containers makes values whose fields hold a slice and a map, and appends to the slice.
func Containers(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		x := &containers{id: int64(i), vals: make([]int, 2), names: map[string]int{"i": i}, check: check(int64(i))}
		x.vals = append(x.vals, i)
		runtime.SetFinalizer(x, func(p *containers) {
			if p.check != check(p.id) || len(p.vals) != 3 || p.vals[2] != int(p.id) || p.names["i"] != int(p.id) {
				finalised <- -1
				return
			}
			finalised <- p.id
		})
	}
	return await(finalised, n)
}

// Array makes &[3]int64{...} array literals.
func Array(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		x := &[3]int64{int64(i), check(int64(i)), 0}
		runtime.SetFinalizer(x, func(p *[3]int64) {
			if p[1] != check(p[0]) {
				finalised <- -1
				return
			}
			finalised <- p[0]
		})
	}
	return await(finalised, n)
}

var held []*small

// hold appends n values with finalisers to the package variable held. It runs in its own
// frame, because a register of a running frame keeps what it last held reachable until it
// is overwritten or the frame returns, where Go's liveness analysis would let it go.
func hold(n int, finalised chan int64) {
	for i := 0; i < n; i++ {
		x := &small{id: int64(i), check: check(int64(i))}
		runtime.SetFinalizer(x, func(p *small) { finalised <- p.id })
		held = append(held, x)
	}
}

// Global keeps every value reachable from a package variable, checks that no finaliser
// has run, then drops them. The pause before the check is best effort, as in the Go
// toolchain's own finaliser tests, so a finaliser slower than the pause passes that check
// without being seen.
func Global(n int) int {
	finalised := make(chan int64, n)
	hold(n, finalised)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-finalised:
		return -1000
	default:
	}
	held = nil
	return await(finalised, n)
}

// Cleared sets a finaliser on every value, then clears it on the even ones. Only the odd
// values report, and a report from an even one, even after await returns, fails it. The
// pause before the last check is best effort, as in the Go toolchain's own finaliser
// tests, so a finaliser slower than the pause passes that check without being seen.
func Cleared(n int) int {
	finalised := make(chan int64, n)
	for i := 0; i < n; i++ {
		x := &small{id: int64(i), check: check(int64(i))}
		runtime.SetFinalizer(x, func(p *small) {
			if p.id%2 == 0 {
				finalised <- -1
				return
			}
			finalised <- p.id
		})
		if i%2 == 0 {
			runtime.SetFinalizer(x, nil)
		}
	}
	got := await(finalised, n/2)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	for {
		select {
		case id := <-finalised:
			if id < 0 {
				return -1000
			}
		default:
			return got
		}
	}
}

// keep registers a finaliser and checks, after three collections, that it has not run
// while the value is still kept alive. The pauses are best effort, as in Global.
func keep(ran chan int) {
	x := &small{id: 1, check: check(1)}
	runtime.SetFinalizer(x, func(p *small) { ran <- int(p.id) })
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-ran:
		ran <- -1
	default:
	}
	runtime.KeepAlive(x)
}

// KeptAlive returns 1 when the finaliser ran only after runtime.KeepAlive, -1 when it
// ran before, and 0 when it never ran.
func KeptAlive() int {
	ran := make(chan int, 2)
	keep(ran)
	select {
	case got := <-ran:
		return got
	default:
	}
	timeout := time.After(5 * time.Second)
	for {
		runtime.GC()
		select {
		case got := <-ran:
			return got
		case <-timeout:
			return 0
		case <-time.After(20 * time.Millisecond):
		}
	}
}
` + finaliserAwaitSource

func forEachFinaliserDispatch(t *testing.T, body func(t *testing.T, service *app.Service)) {
	t.Helper()
	for _, dispatch := range finaliserDispatches {
		t.Run(dispatch.name, func(t *testing.T) {
			service := app.NewService(dispatch.options...)
			service.UseSymbolProviders(stdlib.Providers()...)
			body(t, service)
		})
	}
}

func TestFinaliserOfANewValueRuns(t *testing.T) {
	forEachFinaliserDispatch(t, func(t *testing.T, service *app.Service) {
		compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": finalisedNewsSource})
		require.NoError(t, err)
		const values = 100
		result, err := service.CallFunction(t.Context(), compiled, "FinalisedNews", values)
		require.NoError(t, err)
		finalised := fmt.Sprint(result)
		require.Equal(t, fmt.Sprint(values/10*9+1), finalised, "most finalisers run, as in the Go toolchain's tinyfin.go")
	})
}

func TestFinaliserOfACompositeLiteralRuns(t *testing.T) {
	forEachFinaliserDispatch(t, func(t *testing.T, service *app.Service) {
		compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": finalisedLiteralsSource})
		require.NoError(t, err)
		const values = 100
		for _, function := range []string{"Small", "Large", "Pointers", "Nested", "Returned", "Receiver", "Local", "Captured",
			"Goroutine", "Containers", "Array", "Global"} {
			t.Run(function, func(t *testing.T) {
				result, err := service.CallFunction(t.Context(), compiled, function, values)
				require.NoError(t, err)
				require.Equal(t, fmt.Sprint(values/10*9+1), fmt.Sprint(result),
					"most finalisers run, each once and on its own value, as in Go")
			})
		}
		t.Run("Cleared", func(t *testing.T) {
			result, err := service.CallFunction(t.Context(), compiled, "Cleared", values)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprint(values/2/10*9+1), fmt.Sprint(result),
				"SetFinalizer(x, nil) clears a finaliser, and the others run")
		})
		t.Run("KeptAlive", func(t *testing.T) {
			result, err := service.CallFunction(t.Context(), compiled, "KeptAlive")
			require.NoError(t, err)
			require.Equal(t, "1", fmt.Sprint(result), "a finaliser runs after runtime.KeepAlive, never before")
		})
	})
}
