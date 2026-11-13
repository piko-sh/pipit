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
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/sdk/stdlib"
)

const mutableByteSlabsSource = `package main

import (
	"bytes"
	"unsafe"
)

var (
	keptBytes  []byte
	keptText   string
	keptByte   *byte
	keptBuffer *bytes.Buffer
)

// fill writes seed+i to each byte of b.
func fill(b []byte, seed int) {
	for i := range b {
		b[i] = byte(seed + i)
	}
}

// wrong counts the bytes of b that do not read as fill(b, seed) wrote them.
func wrong(b []byte, seed int) int {
	bad := 0
	for i := range b {
		if b[i] != byte(seed+i) {
			bad++
		}
	}
	return bad
}

// growInto appends n bytes from seed to an empty *sink, each append past the capacity
// doubling the backing, so every backing but the last is garbage.
func growInto(sink *[]byte, seed, n int) {
	*sink = nil
	for i := 0; i < n; i++ {
		*sink = append(*sink, byte(seed+i))
	}
}

// makeInto stores a new []byte of n bytes from seed through sink, as issue78081 does.
func makeInto(sink *[]byte, seed, n int) {
	*sink = make([]byte, n)
	fill(*sink, seed)
}

// churn grows rounds []byte values of n bytes, keeping only the last.
func churn(seed, rounds, n int) int {
	return churnShape("append", seed, rounds, n)
}

func churnShape(shape string, seed, rounds, n int) int {
	var b []byte
	total := 0
	for r := 0; r < rounds; r++ {
		if shape == "make" {
			makeInto(&b, seed+r, n)
		} else {
			growInto(&b, seed+r, n)
		}
		total += len(b) - wrong(b, seed+r)
	}
	return total
}

func worker(shape string, seed, rounds, n int, done chan int) {
	done <- churnShape(shape, seed, rounds, n)
}

// Grow churns in the calling goroutine (workers 0) or in workers goroutines, and returns
// the bytes that read as written.
func Grow(shape string, workers, rounds, n int) int {
	if workers == 0 {
		return churnShape(shape, 0, rounds, n)
	}
	done := make(chan int)
	for w := 0; w < workers; w++ {
		go worker(shape, w, rounds, n, done)
	}
	total := 0
	for w := 0; w < workers; w++ {
		total += <-done
	}
	return total
}

const seed = 11

func storeBytes(b []byte)              { keptBytes = b }
func storeText(b []byte)               { keptText = unsafe.String(&b[0], len(b)) }
func storeByte(b []byte)               { keptByte = &b[3] }
func storeBuffer(b []byte)             { keptBuffer = bytes.NewBuffer(b) }
func send(out chan []byte, b []byte)   { out <- b }
func put(m map[int][]byte, b []byte)   { m[1] = b }

func keepLocal(rounds, n int) int {
	held := make([]byte, n)
	fill(held, seed)
	churn(2, rounds, n)
	return wrong(held, seed)
}

func keepSubslice(rounds, n int) int {
	held := make([]byte, n)
	fill(held, seed)
	part := held[n/2 : n/2+16]
	held = nil
	churn(2, rounds, n)
	return wrong(part, seed+n/2)
}

func keepGlobal(rounds, n int) int {
	held := make([]byte, n)
	fill(held, seed)
	storeBytes(held)
	held = nil
	churn(2, rounds, n)
	return wrong(keptBytes, seed)
}

func keepString(rounds, n int) int {
	held := make([]byte, n)
	fill(held, seed)
	storeText(held)
	held = nil
	churn(2, rounds, n)
	return wrong([]byte(keptText), seed)
}

func keepPointer(rounds, n int) int {
	held := make([]byte, n)
	fill(held, seed)
	storeByte(held)
	held = nil
	churn(2, rounds, n)
	if *keptByte != byte(seed+3) {
		return 1
	}
	return 0
}

func keepBuffer(rounds, n int) int {
	held := make([]byte, n)
	fill(held, seed)
	storeBuffer(held)
	held = nil
	churn(2, rounds, n)
	return wrong(keptBuffer.Bytes(), seed)
}

func keepChannel(rounds, n int) int {
	out := make(chan []byte, 1)
	held := make([]byte, n)
	fill(held, seed)
	send(out, held)
	held = nil
	churn(2, rounds, n)
	return wrong(<-out, seed)
}

func keepMap(rounds, n int) int {
	m := map[int][]byte{}
	held := make([]byte, n)
	fill(held, seed)
	put(m, held)
	held = nil
	churn(2, rounds, n)
	return wrong(m[1], seed)
}

func keepCaptured(rounds, n int) int {
	held := make([]byte, n)
	fill(held, seed)
	read := func() int { return wrong(held, seed) }
	churn(2, rounds, n)
	return read()
}

func keep(route string, rounds, n int) int {
	churn(1, rounds, n)
	bad := 0
	switch route {
	case "local":
		bad = keepLocal(rounds, n)
	case "subslice":
		bad = keepSubslice(rounds, n)
	case "global":
		bad = keepGlobal(rounds, n)
	case "unsafe.String in a global":
		bad = keepString(rounds, n)
	case "pointer to a byte in a global":
		bad = keepPointer(rounds, n)
	case "bytes.Buffer":
		bad = keepBuffer(rounds, n)
	case "channel":
		bad = keepChannel(rounds, n)
	case "map":
		bad = keepMap(rounds, n)
	case "captured variable":
		bad = keepCaptured(rounds, n)
	}
	churn(3, rounds, n)
	return bad
}

func keepWorker(route string, rounds, n int, done chan int) {
	done <- keep(route, rounds, n)
}

// Keep runs keep in the calling goroutine or in a goroutine of its own.
func Keep(route string, inGoroutine bool, rounds, n int) int {
	if !inGoroutine {
		return keep(route, rounds, n)
	}
	done := make(chan int)
	go keepWorker(route, rounds, n, done)
	return <-done
}
`

const freshArenaPoolEnvVar = "PIPIT_TEST_FRESH_ARENA_POOL"

func runInFreshArenaPool(t *testing.T) bool {
	t.Helper()
	if os.Getenv(freshArenaPoolEnvVar) == "1" {
		return false
	}
	levels := strings.Split(t.Name(), "/")
	for index, level := range levels {
		levels[index] = "^" + regexp.QuoteMeta(level) + "$"
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run="+strings.Join(levels, "/"), "-test.count=1", "-test.v")
	command.Env = append(os.Environ(), freshArenaPoolEnvVar+"=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "the test in a fresh process:\n%s", output)
	require.Contains(t, string(output), "--- PASS: "+t.Name(), "the child ran the test:\n%s", output)
	return true
}

func mutableByteDrops(before, after engine.LicenceSnapshot) (dropped, installed int64) {
	return after.MutableByteSlabsDropped - before.MutableByteSlabsDropped, after.MutableByteSlabs - before.MutableByteSlabs
}

func TestCollectionsDropTheRetiredMutableByteSlabsOfAGrowingByteSlice(t *testing.T) {
	const (
		budget = 192 << 20
		rounds = 2048
		n      = 4096
	)
	for _, testCase := range []struct {
		name    string
		shape   string
		workers int
	}{
		{"append in one goroutine", "append", 0},
		{"append in several goroutines", "append", 4},
		{"make in several goroutines", "make", 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if runInFreshArenaPool(t) {
				return
			}
			previous := engine.SetLicenceCountingForTest(true)
			t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
			service := app.NewService(app.WithMaxArenaSizeBytes(budget), app.WithArenaFactory(engine.NewRegisterArena))
			service.UseSymbolProviders(stdlib.Providers()...)
			compiled := hazardCompile(t, service, mutableByteSlabsSource)
			before := engine.LicenceDiagnostics()

			result, err := service.CallFunction(t.Context(), compiled, "Grow", testCase.shape, testCase.workers, rounds, n)

			require.NoError(t, err, "each goroutine's current mutable slab carries the charge of its whole chain, well within %d MiB", budget>>20)
			require.EqualValues(t, max(testCase.workers, 1)*rounds*n, result, "every grown []byte reads as written")
			dropped, installed := mutableByteDrops(before, engine.LicenceDiagnostics())
			arenas := int64(testCase.workers + 1)
			require.GreaterOrEqual(t, installed, 4*int64(max(testCase.workers, 1)), "test harness: each churning arena's mutable slab grows through a chain")
			require.LessOrEqual(t, installed-dropped, 3*arenas,
				"collections drop each retired slab once it is dead, leaving each arena its current slab and those retired since its last collection (installed %d, dropped %d)",
				installed, dropped)
		})
	}
}

func TestBytesOfARetiredMutableByteSlabSurviveCollectionsThatDropTheRest(t *testing.T) {
	const (
		rounds = 512
		n      = 4096
	)
	routes := []string{"local", "subslice", "unsafe.String in a global", "pointer to a byte in a global", "bytes.Buffer",
		"global", "channel", "map", "captured variable"}
	for _, inGoroutine := range []bool{false, true} {
		for _, route := range routes {
			name := route
			if inGoroutine {
				name = "in a goroutine/" + route
			}
			t.Run(name, func(t *testing.T) {
				if runInFreshArenaPool(t) {
					return
				}
				previous := engine.SetLicenceCountingForTest(true)
				t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
				window := openAuditWindow(t, 0)
				service := newHazardService(t, app.WithArenaFactory(engine.NewRegisterArena))
				compiled := hazardCompile(t, service, mutableByteSlabsSource)
				before := engine.LicenceDiagnostics()

				bad := hazardCall(t, service, compiled, "Keep", route, inGoroutine, rounds, n)

				after := engine.LicenceDiagnostics()
				require.EqualValues(t, 0, bad, "the kept bytes read as they were written")
				observations := &hazardObservations{}
				settleReplayAudit(window, observations)
				assertNoHazard(t, observations, "")
				dropped, _ := mutableByteDrops(before, after)
				require.Positive(t, dropped, "test harness: collections must drop retired mutable byte slabs while the bytes are kept (compactions %d, failed closed %v -> %v)",
					after.Reuse.Compactions-before.Reuse.Compactions, before.Reuse.CompactionsFailedClosed, after.Reuse.CompactionsFailedClosed)
			})
		}
	}
}
