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

package limits_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/sdk/stdlib"
)

func newService(opts ...app.Option) *app.Service {
	s := app.NewService(opts...)
	s.UseSymbolProviders(stdlib.Providers()...)
	return s
}

func allFeaturesExcept(excluded policy.InterpFeature) policy.InterpFeature {
	return policy.InterpFeaturesAll &^ excluded
}

func TestFeature_ForLoop_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureForLoops)))
	_, err := s.Eval(context.Background(), `for i := 0; i < 10; i++ {}; 0`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "for loops")
}

func TestFeature_ForLoop_Allowed(t *testing.T) {
	t.Parallel()
	s := newService()
	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 5; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 10, result)
}

func TestFeature_RangeLoop_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureRangeLoops)))
	_, err := s.Eval(context.Background(), `sum := 0; for _, v := range []int{1, 2, 3} { sum += v }; sum`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "range loops")
}

func TestFeature_Goroutine_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureGoroutines)))
	_, err := s.Eval(context.Background(), `go func() {}()`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "goroutines")
}

func TestFeature_Channel_Make_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureChannels)))
	_, err := s.Eval(context.Background(), `ch := make(chan int); _ = ch; 0`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "channels")
}

func TestFeature_Channel_Send_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureChannels)))
	_, err := s.Eval(context.Background(), `ch := make(chan int, 1); ch <- 1`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "channels")
}

func TestFeature_Channel_Receive_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureChannels)))
	_, err := s.Eval(context.Background(), `ch := make(chan int, 1); ch <- 1; <-ch`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "channels")
}

func TestFeature_Defer_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureDefer)))
	_, err := s.Eval(context.Background(), `defer func() {}()`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "defer")
}

func TestFeature_Goto_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureGoto)))
	code := `
package main
func entrypoint() int {
	goto end
end:
	return 1
}
`
	cfs, err := s.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "goto")
	_ = cfs
}

func TestFeature_Closure_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureClosures)))
	_, err := s.Eval(context.Background(), `f := func() int { return 42 }; f()`)
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "closures")
}

func TestFeature_Panic_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeaturePanicRecover)))
	code := `
package main
func entrypoint() int {
	panic("boom")
	return 0
}
`
	_, err := s.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "panic/recover")
}

func TestFeature_Recover_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeaturePanicRecover)))
	code := `
package main
func entrypoint() interface{} {
	return recover()
}
`
	_, err := s.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "panic/recover")
}

func TestFeature_Unsafe_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureUnsafeOps)))
	code := `
package main

import "unsafe"

func entrypoint() unsafe.Pointer {
	x := 42
	p := unsafe.Pointer(&x)
	return unsafe.Add(p, 1)
}
`
	_, err := s.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "unsafe")
}

func TestFeature_Recursion_Direct_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureRecursion)))
	code := `
package main

func factorial(n int) int {
	if n <= 1 { return 1 }
	return n * factorial(n-1)
}

func entrypoint() int {
	return factorial(5)
}
`
	_, err := s.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "recursion")
}

func TestFeature_Recursion_Mutual_Disallowed(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(allFeaturesExcept(policy.InterpFeatureRecursion)))
	code := `
package main

func isEven(n int) bool {
	if n == 0 { return true }
	return isOdd(n - 1)
}

func isOdd(n int) bool {
	if n == 0 { return false }
	return isEven(n - 1)
}

func entrypoint() bool {
	return isEven(4)
}
`
	_, err := s.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.Error(t, err)
	require.ErrorContains(t, err, "language feature not allowed")
	require.ErrorContains(t, err, "recursion")
}

func TestFeature_Recursion_Allowed(t *testing.T) {
	t.Parallel()
	s := newService()
	code := `
package main

func factorial(n int) int {
	if n <= 1 { return 1 }
	return n * factorial(n-1)
}

func entrypoint() int {
	return factorial(5)
}
`
	cfs, err := s.CompileFileSet(context.Background(), map[string]string{"main.go": code})
	require.NoError(t, err)
	result, err := s.ExecuteEntrypoint(context.Background(), cfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t, 120, result)
}

func TestFeatures_All_AllowsEverything(t *testing.T) {
	t.Parallel()
	s := newService()
	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 3; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 3, result)
}

func TestFeatures_Restricted_Composite(t *testing.T) {
	t.Parallel()
	s := newService(app.WithFeatures(policy.InterpFeaturesRestricted))

	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 3; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 3, result)

	_, err = s.Eval(context.Background(), `go func() {}()`)
	require.ErrorContains(t, err, "language feature not allowed")
}

func TestFeatures_Default_AllowsAll(t *testing.T) {
	t.Parallel()
	s := newService()
	result, err := s.Eval(context.Background(), `1 + 2`)
	require.NoError(t, err)
	require.Equal(t, 3, result)
}

func TestCost_BudgetExceeded(t *testing.T) {
	t.Parallel()
	s := newService(app.WithCostBudget(50))
	_, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 1000; i++ { sum += i }; sum`)
	require.Error(t, err)
	require.ErrorContains(t, err, "cost budget exceeded")
}

func TestCost_BudgetSufficient(t *testing.T) {
	t.Parallel()
	s := newService(app.WithCostBudget(1000000))
	result, err := s.Eval(context.Background(), `1 + 2 + 3`)
	require.NoError(t, err)
	require.Equal(t, 6, result)
}

func TestCost_DisabledByDefault(t *testing.T) {
	t.Parallel()
	s := newService()
	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 10000; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 49995000, result)
}

func TestCost_Reporting(t *testing.T) {
	t.Parallel()
	s := newService(app.WithCostBudget(1000000))
	_, err := s.Eval(context.Background(), `x := 1; y := 2; x + y`)
	require.NoError(t, err)
	require.Greater(t, s.LastCostUsed(), int64(0))
}

func TestCost_ZeroBudgetMeansDisabled(t *testing.T) {
	t.Parallel()
	s := newService(app.WithCostBudget(0))
	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 10000; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 49995000, result)
	require.Equal(t, int64(0), s.LastCostUsed())
}

func TestCost_InfiniteLoop_Halts(t *testing.T) {
	t.Parallel()
	s := newService(app.WithCostBudget(1000))
	_, err := s.Eval(context.Background(), `x := 0; for { x++ }; x`)
	require.Error(t, err)
	require.ErrorContains(t, err, "cost budget exceeded")
}

func TestCost_CustomTable(t *testing.T) {
	t.Parallel()
	table := isa.DefaultCostTable()

	for i := range table {
		table[i] = 100
	}
	s := newService(
		app.WithCostBudget(50),
		app.WithCostTable(&table),
	)
	_, err := s.Eval(context.Background(), `x := 1; y := 2; x + y`)
	require.Error(t, err)
	require.ErrorContains(t, err, "cost budget exceeded")
}

func TestFeaturesAndCost_Combined(t *testing.T) {
	t.Parallel()
	s := newService(
		app.WithFeatures(policy.InterpFeaturesRestricted),
		app.WithCostBudget(1000000),
	)

	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 10; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 45, result)
	require.Greater(t, s.LastCostUsed(), int64(0))

	_, err = s.Eval(context.Background(), `go func() {}()`)
	require.ErrorContains(t, err, "language feature not allowed")
}

func TestDefault_Regression(t *testing.T) {
	t.Parallel()
	s := newService()
	result, err := s.Eval(context.Background(), `42`)
	require.NoError(t, err)
	require.Equal(t, 42, result)
	require.Equal(t, int64(0), s.LastCostUsed())
}

func TestSourceSize_ExceedsLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxSourceSize(10))
	_, err := s.Eval(context.Background(), `1 + 2 + 3 + 4 + 5`)
	require.Error(t, err)
	require.ErrorContains(t, err, "source size limit exceeded")
}

func TestSourceSize_WithinLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxSourceSize(1000))
	result, err := s.Eval(context.Background(), `1 + 2`)
	require.NoError(t, err)
	require.Equal(t, 3, result)
}

func TestSourceSize_Disabled(t *testing.T) {
	t.Parallel()
	s := newService()
	result, err := s.Eval(context.Background(), `1 + 2`)
	require.NoError(t, err)
	require.Equal(t, 3, result)
}

func TestSourceSize_FileSet_TotalSize(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxSourceSize(50))

	_, err := s.CompileFileSet(context.Background(), map[string]string{
		"a.go": `package main; func a() int { return 1 }`,
		"b.go": `package main; func b() int { return 2 }`,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "source size limit exceeded")
}

func TestStringSize_ConcatExceedsLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxStringSize(100))
	_, err := s.Eval(context.Background(), `s := "a"; for i := 0; i < 10; i++ { s = s + s }; s`)
	require.Error(t, err)
	require.ErrorContains(t, err, "string size limit exceeded")
}

func TestStringSize_ConcatWithinLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxStringSize(1000))
	result, err := s.Eval(context.Background(), `"hello" + " " + "world"`)
	require.NoError(t, err)
	require.Equal(t, "hello world", result)
}

func TestStringSize_Disabled(t *testing.T) {
	t.Parallel()
	s := newService()

	_, err := s.Eval(context.Background(), `s := "a"; for i := 0; i < 15; i++ { s = s + s }; len(s)`)
	require.NoError(t, err)
}

func TestAppendLimit_ExceedsAllocSize(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxAllocSize(50))
	_, err := s.Eval(context.Background(), `s := make([]int, 0); for i := 0; i < 100; i++ { s = append(s, i) }; len(s)`)
	require.Error(t, err)
	require.ErrorContains(t, err, "allocation size limit exceeded")
}

func TestAppendLimit_WithinLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxAllocSize(100))
	result, err := s.Eval(context.Background(), `s := make([]int, 0); for i := 0; i < 10; i++ { s = append(s, i) }; len(s)`)
	require.NoError(t, err)
	require.Equal(t, 10, result)
}

func TestLiteralElements_SliceExceedsLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxLiteralElements(3))
	_, err := s.Eval(context.Background(), `[]int{1, 2, 3, 4, 5}`)
	require.Error(t, err)
	require.ErrorContains(t, err, "literal element count limit exceeded")
}

func TestLiteralElements_MapExceedsLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxLiteralElements(2))
	_, err := s.Eval(context.Background(), `map[string]int{"a": 1, "b": 2, "c": 3}`)
	require.Error(t, err)
	require.ErrorContains(t, err, "literal element count limit exceeded")
}

func TestLiteralElements_WithinLimit(t *testing.T) {
	t.Parallel()
	s := newService(app.WithMaxLiteralElements(10))
	result, err := s.Eval(context.Background(), `[]int{1, 2, 3}`)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestLiteralElements_Disabled(t *testing.T) {
	t.Parallel()
	s := newService()
	result, err := s.Eval(context.Background(), `[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}`)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestYieldInterval_ExecutesCorrectly(t *testing.T) {
	t.Parallel()

	s := newService(app.WithYieldInterval(1024))
	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 100; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 4950, result)
}

func TestYieldInterval_Disabled(t *testing.T) {
	t.Parallel()

	s := newService()
	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 100; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 4950, result)
}

func TestYieldInterval_WithCostBudget(t *testing.T) {
	t.Parallel()

	s := newService(
		app.WithYieldInterval(512),
		app.WithCostBudget(1000000),
	)
	result, err := s.Eval(context.Background(), `sum := 0; for i := 0; i < 50; i++ { sum += i }; sum`)
	require.NoError(t, err)
	require.Equal(t, 1225, result)
	require.Greater(t, s.LastCostUsed(), int64(0))
}

func TestCostBudgetAppliesInsideGoroutine(t *testing.T) {
	t.Parallel()
	s := newService(app.WithCostBudget(50_000))
	_, err := s.Eval(context.Background(), `x := 0; for i := 0; i < 2000000; i++ { x += i }; x`)
	require.Error(t, err, "main goroutine must exhaust the cost budget")
	require.ErrorContains(t, err, "cost budget exceeded")
	_, err = s.Eval(context.Background(), `done := make(chan int); go func() { x := 0; for i := 0; i < 2000000; i++ { x += i }; done <- x }(); <-done`)
	require.Error(t, err, "spawned goroutine must also exhaust the cost budget")
	require.ErrorContains(t, err, "cost budget exceeded")
}

func TestCostBudgetSharedAcrossGoroutines(t *testing.T) {
	t.Parallel()
	const spin = `done := make(chan int, 4)
for g := 0; g < 4; g++ {
	go func() { x := 0; for i := 0; i < 200000; i++ { x += i }; done <- x }()
}
total := 0
for g := 0; g < 4; g++ { total += <-done }
total`
	unlimited := newService(app.WithCostBudget(1 << 40))
	_, err := unlimited.Eval(context.Background(), spin)
	require.NoError(t, err)
	fourWorkers := unlimited.LastCostUsed()
	require.Greater(t, fourWorkers, int64(0))

	s := newService(app.WithCostBudget(fourWorkers / 2))
	_, err = s.Eval(context.Background(), spin)
	require.Error(t, err)
	require.ErrorContains(t, err, "cost budget exceeded")
}

func TestLastCostUsedIncludesGoroutineWork(t *testing.T) {
	t.Parallel()
	s := newService(app.WithCostBudget(1 << 40))
	_, err := s.Eval(context.Background(), `done := make(chan int); go func() { done <- 0 }(); <-done`)
	require.NoError(t, err)
	idle := s.LastCostUsed()
	_, err = s.Eval(context.Background(), `done := make(chan int); go func() { x := 0; for i := 0; i < 100000; i++ { x += i }; done <- x }(); <-done`)
	require.NoError(t, err)
	require.Greater(t, s.LastCostUsed(), idle+100000)
}

func TestArenaBudgetSharedAcrossGoroutines(t *testing.T) {
	t.Parallel()
	const program = `package main

func hold(ready chan<- int, release <-chan int) {
	total := 0
	for i := 0; i < 64; i++ {
		scratch := make([]int, 4096)
		total += len(scratch)
	}
	ready <- total
	<-release
}

func run(workers int) int {
	ready := make(chan int, workers)
	release := make(chan int)
	for g := 0; g < workers; g++ {
		go hold(ready, release)
	}
	total := 0
	for g := 0; g < workers; g++ {
		total += <-ready
	}
	close(release)
	return total
}

func one() int { return run(1) }

func eight() int { return run(8) }

func main() {}
`
	ctx := context.Background()
	s := newService(app.WithMaxArenaSizeBytes(8 << 20))
	compiled, err := s.CompileFileSet(ctx, map[string]string{"main.go": program})
	require.NoError(t, err)

	result, err := s.ExecuteEntrypoint(ctx, compiled, "one")
	require.NoError(t, err)
	require.Equal(t, int(64*4096), result)

	_, err = s.ExecuteEntrypoint(ctx, compiled, "eight")
	require.Error(t, err)
	require.ErrorContains(t, err, "register arena byte budget exceeded")
}
