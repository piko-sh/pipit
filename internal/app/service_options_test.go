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

package app

import (
	"testing"

	"context"
	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
	"reflect"
	"sync/atomic"
	"time"
)

const (
	optionSetterTestBudget = 12345
)

func TestWithMaxArenaSizeBytes_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxArenaSizeBytes(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, uint64(optionSetterTestBudget), service.config.maxArenaBytes,
		"WithMaxArenaSizeBytes should set the canonical maxArenaBytes field")
	require.Equal(t, uint64(optionSetterTestBudget), service.config.maxArenaSizeBytes,
		"WithMaxArenaSizeBytes should also set the mirror maxArenaSizeBytes alias")
}

func TestWithMaxConstantPoolSize_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxConstantPoolSize(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.maxConstantPoolSize)
}

func TestWithMaxSpecialisations_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxSpecialisations(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.maxSpecialisations)
}

func TestWithMaxMethods_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxMethods(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.maxMethods)
}

func TestWithMaxExpressionDepth_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxExpressionDepth(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.maxExpressionDepth)
}

func TestWithVerifierIterationLimit_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithVerifierIterationLimit(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.verifierIterationLimit)
}

func TestWithMaxSourceSize_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxSourceSize(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.maxSourceSize)
}

func TestWithMaxStringSize_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxStringSize(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.maxStringSize)
}

func TestWithMaxLiteralElements_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxLiteralElements(optionSetterTestBudget))

	require.NotNil(t, service.config)
	require.Equal(t, optionSetterTestBudget, service.config.maxLiteralElements)
}

func TestWithYieldInterval_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithYieldInterval(uint32(optionSetterTestBudget)))

	require.NotNil(t, service.config)
	require.Equal(t, uint32(optionSetterTestBudget), service.config.yieldInterval)
}

func TestWithCostBudget_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithCostBudget(int64(optionSetterTestBudget)))

	require.NotNil(t, service.config)
	require.Equal(t, int64(optionSetterTestBudget), service.config.costBudget)
}

func TestWithCostTable_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	table := &isa.CostTable{}
	service := NewService(WithCostTable(table))

	require.NotNil(t, service.config)
	require.Same(t, table, service.config.costTable,
		"WithCostTable must record the exact pointer for identity-checks")
}

func TestWithFeatures_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	const restrictedFeatures policy.InterpFeature = 1
	service := NewService(WithFeatures(restrictedFeatures))

	require.NotNil(t, service.config)
	require.Equal(t, restrictedFeatures, service.config.features)
}

func TestWithBytecodeVerification_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithBytecodeVerification(false))

	require.NotNil(t, service.config)
	require.True(t, service.config.bytecodeVerificationDisabled,
		"WithBytecodeVerification(false) should record disable flag")

	service = NewService(WithBytecodeVerification(true))
	require.False(t, service.config.bytecodeVerificationDisabled,
		"WithBytecodeVerification(true) should clear disable flag")
}

func TestWithDeniedImports_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithDeniedImports("unsafe", "runtime"))

	require.NotNil(t, service.config)
	require.Contains(t, service.config.deniedImports, "unsafe")
	require.Contains(t, service.config.deniedImports, "runtime")
}

func TestWithImportAllowlist_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	service := NewService(WithImportAllowlist("host/widgets"))

	require.NotNil(t, service.config)
	require.Contains(t, service.config.allowedImports, "host/widgets")
	require.Contains(t, service.config.allowedImports, importAllowlistSentinel,
		"a configured allowlist must stay non-empty so it is distinguishable from no allowlist")

	empty := NewService(WithImportAllowlist())
	require.NotEmpty(t, empty.config.allowedImports,
		"an empty allowlist must still install the sentinel so it denies all external imports")
}

func TestWithCompilationSnapshot_PlumbedToConfig(t *testing.T) {
	t.Parallel()
	var captured *program.CompiledFileSet
	callback := func(cfs *program.CompiledFileSet) { captured = cfs }

	service := NewService(withCompilationSnapshot(callback))

	require.NotNil(t, service.config)
	require.NotNil(t, service.config.compilationSnapshotCallback,
		"WithCompilationSnapshot must record the callback")
	_ = captured
}

func TestWithSafeMode_PlumbedToConfigAndLimits(t *testing.T) {
	t.Parallel()
	service := NewService(WithSafeMode())

	require.NotNil(t, service.config)
	require.True(t, service.config.safeMode, "WithSafeMode must set config.safeMode")
	require.True(t, service.limits.SafeMode, "safe mode must propagate to vmLimits")
	require.True(t, service.limits.ForceGoDispatch,
		"safe mode must force Go dispatch so guards run off the ASM fast path")
}

func TestSafeModeParityWithDefault(t *testing.T) {
	t.Parallel()

	programs := []struct {
		want any
		name string
		code string
	}{
		{name: "arithmetic", code: `x := 0; for i := 0; i < 10; i++ { x += i }; x`, want: 45},
		{name: "string build", code: `s := ""; for i := 0; i < 3; i++ { s += "a" }; s`, want: "aaa"},
		{name: "slice sum", code: `xs := []int{1, 2, 3, 4}; t := 0; for _, v := range xs { t += v }; t`, want: 10},
		{name: "map lookup", code: `m := map[string]int{"a": 1, "b": 2}; m["a"] + m["b"]`, want: 3},
		{name: "closure", code: `add := func(a, b int) int { return a + b }; add(3, 4)`, want: 7},
	}

	for _, candidate := range programs {
		t.Run(candidate.name, func(t *testing.T) {
			t.Parallel()
			fast, fastErr := NewService().Eval(context.Background(), candidate.code)
			require.NoError(t, fastErr)

			safe, safeErr := NewService(WithSafeMode()).Eval(context.Background(), candidate.code)
			require.NoError(t, safeErr)

			require.Equal(t, candidate.want, fast)
			require.Equal(t, fast, safe, "safe mode must produce identical results to fast mode")
		})
	}
}

type fixedClock struct {
	now           time.Time
	nowCalls      atomic.Int64
	sinceCalls    atomic.Int64
	untilCalls    atomic.Int64
	sleepCalls    atomic.Int64
	timerCalls    atomic.Int64
	tickerCalls   atomic.Int64
	lastSleepFor  atomic.Int64
	lastTimerFor  atomic.Int64
	lastTickerFor atomic.Int64
}

func (c *fixedClock) Now() time.Time {
	c.nowCalls.Add(1)
	return c.now
}

func (c *fixedClock) Since(t time.Time) time.Duration {
	c.sinceCalls.Add(1)
	return c.now.Sub(t)
}

func (c *fixedClock) Until(t time.Time) time.Duration {
	c.untilCalls.Add(1)
	return t.Sub(c.now)
}

func (c *fixedClock) Sleep(d time.Duration) {
	c.sleepCalls.Add(1)
	c.lastSleepFor.Store(int64(d))
}

func (c *fixedClock) NewTimer(d time.Duration) *time.Timer {
	c.timerCalls.Add(1)
	c.lastTimerFor.Store(int64(d))
	return time.NewTimer(d)
}

func (c *fixedClock) NewTicker(d time.Duration) *time.Ticker {
	c.tickerCalls.Add(1)
	c.lastTickerFor.Store(int64(d))
	return time.NewTicker(d)
}

func TestWallClockDefaults(t *testing.T) {
	t.Parallel()
	before := time.Now()
	got := clock.WallClock.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("WallClock.Now() outside expected range: got %v, before=%v after=%v", got, before, after)
	}

	clock.WallClock.Sleep(1 * time.Millisecond)
	if d := clock.WallClock.Since(before); d <= 0 {
		t.Fatalf("WallClock.Since returned non-positive duration: %v", d)
	}
}

func TestEffectiveClockNilSubstitutesWallClock(t *testing.T) {
	t.Parallel()
	if got := clock.EffectiveClock(nil); got != clock.WallClock {
		t.Fatalf("effectiveClock(nil) should return WallClock")
	}
	custom := &fixedClock{}
	if got := clock.EffectiveClock(custom); got != custom {
		t.Fatalf("EffectiveClock(custom) should return the custom clock")
	}
}

func TestWithClockOption(t *testing.T) {
	t.Parallel()
	fixedSource := &fixedClock{now: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)}
	service := NewService(WithClock(fixedSource))
	if service.config.clock != fixedSource {
		t.Fatalf("service.config.clock did not receive WithClock value")
	}
}

func TestClockOverridesTimeNowInInterpretedCode(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	fixedSource := &fixedClock{now: fixed}
	service := NewService(WithClock(fixedSource), WithForceGoDispatch())
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{
		"time": {
			"Now":   reflect.ValueOf(time.Now),
			"Since": reflect.ValueOf(time.Since),
		},
	}))

	result, err := service.Eval(context.Background(), `
		import "time"
		time.Now()
	`)
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	got, ok := result.(time.Time)
	if !ok {
		t.Fatalf("Eval result is not time.Time: %T %v", result, result)
	}
	if !got.Equal(fixed) {
		t.Fatalf("interpreted time.Now() = %v, want fixed %v", got, fixed)
	}
	if fixedSource.nowCalls.Load() == 0 {
		t.Fatalf("fixed clock Now() was never called")
	}
}

func TestClockOverlaysPreserveOtherTimeSymbols(t *testing.T) {
	t.Parallel()
	fixedSource := &fixedClock{now: time.Now()}
	service := NewService(WithClock(fixedSource))
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{
		"time": {
			"Now":         reflect.ValueOf(time.Now),
			"Hour":        reflect.ValueOf(time.Hour),
			"Millisecond": reflect.ValueOf(time.Millisecond),
		},
	}))

	pkg, ok := service.symbols.PackageSymbols("time")
	if !ok {
		t.Fatalf("time package missing from registry after clock overlay")
	}
	if _, ok := pkg["Hour"]; !ok {
		t.Fatalf("time.Hour symbol lost after clock overlay")
	}
	if _, ok := pkg["Millisecond"]; !ok {
		t.Fatalf("time.Millisecond symbol lost after clock overlay")
	}
	if _, ok := pkg["Now"]; !ok {
		t.Fatalf("time.Now symbol missing after clock overlay")
	}
}

func TestOverlayPackageOnAbsentPackageCreatesIt(t *testing.T) {
	t.Parallel()
	registry := symtab.NewSymbolRegistry(nil)
	registry.OverlayPackage("custom/pkg", map[string]reflect.Value{
		"X": reflect.ValueOf(42),
	})
	got, ok := registry.PackageSymbols("custom/pkg")
	if !ok {
		t.Fatalf("OverlayPackage did not create the package")
	}
	if value, ok := got["X"]; !ok || value.Interface() != 42 {
		t.Fatalf("OverlayPackage did not store the entry")
	}
}
