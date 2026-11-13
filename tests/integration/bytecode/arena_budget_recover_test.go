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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

const (
	arenaBudgetProbeSource = `package main

func accumulate() int {
	total := 0
	for i := 0; i < 1000000; i++ {
		scratch := make([]int, 4096)
		total += len(scratch)
	}
	return total
}

var overBudget = accumulate()

func run() int {
	return overBudget
}
`
)

const goroutineChurnSource = `package main

func fill(depth int) int {
	var block [4096]byte
	block[depth] = byte(depth)
	if depth == 0 {
		return int(block[0])
	}
	return fill(depth-1) + int(block[depth])
}

func worker(rounds int, done chan int) {
	total := 0
	for i := 0; i < rounds; i++ {
		total += fill(16)
	}
	done <- total
}

func workerWithCell(rounds int, done chan int) {
	total := 0
	for i := 0; i < rounds; i++ {
		add := func(n int) { total += n }
		add(fill(16))
	}
	done <- total
}

func refill(sink *[]byte) {
	*sink = make([]byte, 1024)
}

func workerHoldingASlice(rounds int, done chan int) {
	var kept []byte
	total := 0
	for i := 0; i < rounds; i++ {
		total += fill(16)
		refill(&kept)
	}
	if len(kept) != 1024 {
		total = -1
	}
	done <- total
}

type holder struct {
	items []int
	label string
}

func workerHoldingAStruct(rounds int, done chan int) {
	held := &holder{items: []int{1, 2, 3}, label: "held"}
	total := 0
	for i := 0; i < rounds; i++ {
		total += fill(16)
	}
	if len(held.items) != 3 || held.label != "held" {
		total = -1
	}
	done <- total
}

func Run(workers, rounds int, shape string) int {
	done := make(chan int)
	for w := 0; w < workers; w++ {
		switch shape {
		case "cell":
			go workerWithCell(rounds, done)
		case "slice":
			go workerHoldingASlice(rounds, done)
		case "struct":
			go workerHoldingAStruct(rounds, done)
		default:
			go worker(rounds, done)
		}
	}
	total := 0
	for w := 0; w < workers; w++ {
		total += <-done
	}
	return total
}
`

func runGoroutineChurn(t *testing.T, shape string) int64 {
	t.Helper()
	const (
		budget  = 256 << 20
		workers = 4
		rounds  = 2000
	)
	service := app.NewService(app.WithMaxArenaSizeBytes(budget))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": goroutineChurnSource})
	require.NoError(t, err)
	before := engine.LicenceDiagnostics().Reuse

	result, err := service.CallFunction(t.Context(), compiled, "Run", workers, rounds, shape)

	require.NoError(t, err, "each goroutine allocates %d MiB of frames over the run, past the %d MiB budget, but holds little at once",
		rounds*17*4096>>20, budget>>20)
	require.EqualValues(t, workers*rounds*136, result)
	after := engine.LicenceDiagnostics().Reuse
	failedClosed := func(counts engine.LicenceCounts) (total int64) {
		for _, count := range counts.CompactionsFailedClosed {
			total += count
		}
		return total
	}
	return after.Compactions - before.Compactions - (failedClosed(after) - failedClosed(before))
}

func TestGoroutinesThatAllocateInALoopStayWithinTheArenaBudget(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	for _, testCase := range []struct {
		name  string
		shape string
	}{
		{"goroutines that hold only their frames", "frames"},
		{"goroutines that made a captured variable", "cell"},
		{"goroutines that hold the address of a slice variable", "slice"},
		{"goroutines that hold a struct with pointers", "struct"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reclaiming := runGoroutineChurn(t, testCase.shape)
			if testCase.shape == "frames" {
				require.Positive(t, reclaiming, "test harness: the goroutines' collections must reclaim")
			}
		})
	}
}

func TestArenaBudgetDuringVarInitSurfacesAsError(t *testing.T) {
	t.Parallel()
	const tinyBudget = 1 << 20
	ctx := t.Context()

	compile := func(t *testing.T) (*app.Service, *program.CompiledFileSet) {
		t.Helper()
		service := app.NewService(app.WithMaxArenaSizeBytes(tinyBudget))
		compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": arenaBudgetProbeSource})
		require.NoError(t, err)
		return service, compiled
	}

	t.Run("ExecuteEntrypoint", func(t *testing.T) {
		t.Parallel()
		service, compiled := compile(t)
		_, err := service.ExecuteEntrypoint(ctx, compiled, "run")
		require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	})

	t.Run("ExecuteInits", func(t *testing.T) {
		t.Parallel()
		service, compiled := compile(t)
		err := service.ExecuteInits(ctx, compiled)
		require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	})
}
