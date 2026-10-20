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
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
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

func TestWithCallbackTimeout_PlumbedToConfigAndLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "a positive timeout bounds each callback", timeout: 250 * time.Millisecond, want: 250 * time.Millisecond},
		{name: "zero disables the bound", timeout: 0, want: 0},
		{name: "a negative timeout disables the bound", timeout: -time.Second, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(WithCallbackTimeout(tt.timeout))

			require.Equal(t, tt.want, service.config.callbackTimeout)
			require.Equal(t, tt.want, service.limits.CallbackTimeout, "every execution VM takes the bound from the service limits")
		})
	}
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

	tests := []struct {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fast, fastErr := NewService().Eval(t.Context(), tt.code)
			require.NoError(t, fastErr)

			safe, safeErr := NewService(WithSafeMode()).Eval(t.Context(), tt.code)
			require.NoError(t, safeErr)

			require.Equal(t, tt.want, fast)
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
	require.False(t, got.Before(before), "the wall clock reads the current time")
	require.False(t, got.After(after))

	clock.WallClock.Sleep(1 * time.Millisecond)
	require.Greater(t, clock.WallClock.Since(before), time.Duration(0), "the wall clock moves forward")
}

func TestEffectiveClockNilSubstitutesWallClock(t *testing.T) {
	t.Parallel()
	require.Equal(t, clock.WallClock, clock.EffectiveClock(nil), "a nil clock falls back to the wall clock")
	custom := &fixedClock{}
	require.Same(t, custom, clock.EffectiveClock(custom))
}

func TestWithClockOption(t *testing.T) {
	t.Parallel()
	fixedSource := &fixedClock{now: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)}
	service := NewService(WithClock(fixedSource))
	require.Same(t, fixedSource, service.config.clock)
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

	result, err := service.Eval(t.Context(), `
		import "time"
		time.Now()
	`)
	require.NoError(t, err)
	got, ok := result.(time.Time)
	require.True(t, ok, "the result is a time.Time")
	require.True(t, got.Equal(fixed), "interpreted time.Now reads the fixed clock")
	require.NotEqual(t, int64(0), fixedSource.nowCalls.Load(), "the fixed clock is consulted")
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
	require.True(t, ok, "the clock overlay keeps the time package")
	_, ok = pkg["Hour"]
	require.True(t, ok, "the clock overlay keeps time.Hour")
	_, ok = pkg["Millisecond"]
	require.True(t, ok, "the clock overlay keeps time.Millisecond")
	_, ok = pkg["Now"]
	require.True(t, ok, "the clock overlay provides time.Now")
}

func TestOverlayPackageOnAbsentPackageCreatesIt(t *testing.T) {
	t.Parallel()
	registry := symtab.NewSymbolRegistry(nil)
	registry.OverlayPackage("custom/pkg", map[string]reflect.Value{
		"X": reflect.ValueOf(42),
	})
	got, ok := registry.PackageSymbols("custom/pkg")
	require.True(t, ok, "the overlay creates the package")
	value, ok := got["X"]
	require.True(t, ok, "the overlay stores its entry")
	require.Equal(t, 42, value.Interface())
}
