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
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/isa"
)

func TestArenaIsolation(t *testing.T) {
	t.Parallel()

	arenaCount := 0
	var mu sync.Mutex
	factory := func() *engine.RegisterArena {
		mu.Lock()
		arenaCount++
		mu.Unlock()
		return engine.NewRegisterArena()
	}

	service := app.NewService(app.WithArenaFactory(factory))
	ctx := context.Background()

	for i := range 5 {
		result, err := service.Eval(ctx, "1 + 2")
		require.NoError(t, err, "iteration %d", i)
		require.Equal(t, 3, result, "iteration %d", i)
	}

	mu.Lock()
	count := arenaCount
	mu.Unlock()
	require.GreaterOrEqual(t, count, 5,
		"expected at least 5 arena allocations from factory, got %d", count)
}

func TestArenaIsolationParallel(t *testing.T) {
	t.Parallel()

	var arenaCount int64
	var mu sync.Mutex
	factory := func() *engine.RegisterArena {
		mu.Lock()
		arenaCount++
		mu.Unlock()
		return engine.NewRegisterArena()
	}

	service := app.NewService(app.WithArenaFactory(factory))
	ctx := context.Background()

	var wg sync.WaitGroup
	const concurrency = 10
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			result, err := service.Eval(ctx, "2 + 3")
			require.NoError(t, err)
			require.Equal(t, 5, result)
		}()
	}
	wg.Wait()

	mu.Lock()
	count := arenaCount
	mu.Unlock()
	require.GreaterOrEqual(t, count, int64(concurrency),
		"expected at least %d arena allocations, got %d", concurrency, count)
}

func TestArenaDefaultPoolUsed(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	ctx := context.Background()

	result, err := service.Eval(ctx, "10 * 4")
	require.NoError(t, err)
	require.Equal(t, 40, result)
}

func TestArenaFactoryWithExecute(t *testing.T) {
	t.Parallel()

	factoryCalled := false
	factory := func() *engine.RegisterArena {
		factoryCalled = true
		return engine.NewRegisterArena()
	}

	service := app.NewService(app.WithArenaFactory(factory))

	b := newBytecodeBuilder()
	b.addIntConst(21)
	b.addIntConst(21)
	b.intRegisters(3).returnInt()
	b.Emit(isa.OpLoadIntConst, 1, 0, 0)
	b.Emit(isa.OpLoadIntConst, 2, 1, 0)
	b.Emit(isa.OpAddInt, 0, 1, 2)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	result, err := service.Execute(context.Background(), b.build())
	require.NoError(t, err)
	require.Equal(t, 42, result)
	require.True(t, factoryCalled, "arena factory should have been called")
}

func TestArenaDeepRecursion(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	source := `package main

func sum(n int) int {
	if n <= 0 {
		return 0
	}
	return n + sum(n-1)
}

func run() int {
	return sum(50)
}

func main() {}
`
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 1275, result)
}

func TestArenaStringSlabGrowth(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	source := `package main

func run() int {
	result := ""
	for i := 0; i < 100; i++ {
		result += "abcdefghij"
	}
	return len(result)
}

func main() {}
`
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 1000, result)
}

func TestArenaUintSlabGrowth(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	source := `package main

func run() uint {
	var sum uint
	for i := uint(0); i < 100; i++ {
		sum += i
	}
	return sum
}

func main() {}
`
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, uint(4950), result)
}

func TestArenaDeepRecursionGrowFrameStack(t *testing.T) {
	t.Parallel()

	service := app.NewService()

	source := `package main

func countdown(n int) int {
	if n <= 0 {
		return 0
	}
	return 1 + countdown(n-1)
}

func run() int {
	return countdown(80)
}

func main() {}
`
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 80, result)
}

func TestArenaManyFunctionCalls(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	source := `package main

func add(a, b int) int { return a + b }

func run() int {
	sum := 0
	for i := 0; i < 200; i++ {
		sum = add(sum, i)
	}
	return sum
}

func main() {}
`
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 19900, result)
}

func TestSizeArenaFromFunctionsPresizesByteSlab(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	source := `package main

func run() int {
	out := ""
	for i := 0; i < 100; i++ {
		out += "abcdefghij"
	}
	return len(out)
}

func main() {}
`
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 1000, result)
}

func TestArenaMakeSliceEndToEnd(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	source := `package main

func run() int {
	ints := make([]int, 0, 10)
	for i := 0; i < 10; i++ {
		ints = append(ints, i*i)
	}
	sum := 0
	for _, v := range ints {
		sum += v
	}
	return sum
}

func main() {}
`
	result, err := service.EvalFile(context.Background(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 285, result, "sum of i*i for i in 0..10")
}
