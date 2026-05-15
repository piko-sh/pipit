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

package clock_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/clock"
)

type fakeClock struct {
	now   time.Time
	slept time.Duration
	timer *time.Timer
}

func (f *fakeClock) Now() time.Time                         { return f.now }
func (f *fakeClock) Since(t time.Time) time.Duration        { return f.now.Sub(t) }
func (f *fakeClock) Until(t time.Time) time.Duration        { return t.Sub(f.now) }
func (f *fakeClock) Sleep(d time.Duration)                  { f.slept += d }
func (f *fakeClock) NewTimer(time.Duration) *time.Timer     { return f.timer }
func (f *fakeClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func newFakeClock(t *testing.T) *fakeClock {
	t.Helper()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	return &fakeClock{now: time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC), slept: 0, timer: timer}
}

func TestWallClockNowIsMonotonicAndSinceNonNegative(t *testing.T) {
	t.Parallel()
	first := clock.WallClock.Now()
	second := clock.WallClock.Now()
	require.False(t, second.Before(first), "Now must not go backwards")
	require.GreaterOrEqual(t, clock.WallClock.Since(first), time.Duration(0))
	require.LessOrEqual(t, clock.WallClock.Since(first), time.Minute, "Since must be measured against the real clock")
}

func TestWallClockUntil(t *testing.T) {
	t.Parallel()
	now := clock.WallClock.Now()
	require.Positive(t, clock.WallClock.Until(now.Add(time.Hour)))
	require.Negative(t, clock.WallClock.Until(now.Add(-time.Hour)))
}

func TestWallClockSleepBlocksForAtLeastDuration(t *testing.T) {
	t.Parallel()
	const d = 2 * time.Millisecond
	start := time.Now()
	clock.WallClock.Sleep(d)
	require.GreaterOrEqual(t, time.Since(start), d)
}

func TestWallClockTimerAndTicker(t *testing.T) {
	t.Parallel()
	timer := clock.WallClock.NewTimer(time.Hour)
	require.NotNil(t, timer)
	require.True(t, timer.Stop(), "a fresh hour-long timer must still be pending")

	ticker := clock.WallClock.NewTicker(time.Hour)
	require.NotNil(t, ticker)
	ticker.Stop()

	require.Panics(t, func() { clock.WallClock.NewTicker(0) }, "matching time.NewTicker, a non-positive interval panics")
}

func TestEffectiveClock(t *testing.T) {
	t.Parallel()
	fake := newFakeClock(t)
	cases := []struct {
		name  string
		input clock.Clock
		want  clock.Clock
	}{
		{name: "nil falls back to wall clock", input: nil, want: clock.WallClock},
		{name: "wall clock passes through", input: clock.WallClock, want: clock.WallClock},
		{name: "custom clock passes through", input: fake, want: fake},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clock.EffectiveClock(tc.input)
			require.NotNil(t, got)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestClockOverrideSymbolsShape(t *testing.T) {
	t.Parallel()
	symbols := clock.ClockOverrideSymbols(newFakeClock(t))

	timeType := reflect.TypeFor[time.Time]()
	durationType := reflect.TypeFor[time.Duration]()
	cases := []struct {
		name    string
		wantIn  []reflect.Type
		wantOut []reflect.Type
	}{
		{name: "Now", wantIn: nil, wantOut: []reflect.Type{timeType}},
		{name: "Since", wantIn: []reflect.Type{timeType}, wantOut: []reflect.Type{durationType}},
		{name: "Until", wantIn: []reflect.Type{timeType}, wantOut: []reflect.Type{durationType}},
		{name: "Sleep", wantIn: []reflect.Type{durationType}, wantOut: nil},
		{name: "NewTimer", wantIn: []reflect.Type{durationType}, wantOut: []reflect.Type{reflect.TypeFor[*time.Timer]()}},
		{name: "NewTicker", wantIn: []reflect.Type{durationType}, wantOut: []reflect.Type{reflect.TypeFor[*time.Ticker]()}},
	}
	require.Len(t, symbols, len(cases), "exactly the six time bindings are overridden")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			value, ok := symbols[tc.name]
			require.True(t, ok, "symbol %s must be present", tc.name)
			require.Equal(t, reflect.Func, value.Kind())
			fnType := value.Type()
			require.Equal(t, len(tc.wantIn), fnType.NumIn())
			for i, in := range tc.wantIn {
				require.Equal(t, in, fnType.In(i))
			}
			require.Equal(t, len(tc.wantOut), fnType.NumOut())
			for i, out := range tc.wantOut {
				require.Equal(t, out, fnType.Out(i))
			}
		})
	}
}

func TestClockOverrideSymbolsDispatchToSuppliedClock(t *testing.T) {
	t.Parallel()
	fake := newFakeClock(t)
	symbols := clock.ClockOverrideSymbols(fake)

	now := symbols["Now"].Call(nil)
	require.Len(t, now, 1)
	gotNow, ok := reflect.TypeAssert[time.Time](now[0])
	require.True(t, ok)
	require.True(t, gotNow.Equal(fake.now))

	since := symbols["Since"].Call([]reflect.Value{reflect.ValueOf(fake.now.Add(-time.Hour))})
	require.Equal(t, time.Hour, since[0].Interface())

	until := symbols["Until"].Call([]reflect.Value{reflect.ValueOf(fake.now.Add(2 * time.Hour))})
	require.Equal(t, 2*time.Hour, until[0].Interface())

	symbols["Sleep"].Call([]reflect.Value{reflect.ValueOf(3 * time.Second)})
	require.Equal(t, 3*time.Second, fake.slept, "Sleep must reach the fake rather than the wall clock")

	timer := symbols["NewTimer"].Call([]reflect.Value{reflect.ValueOf(time.Minute)})
	require.Same(t, fake.timer, timer[0].Interface())

	ticker := symbols["NewTicker"].Call([]reflect.Value{reflect.ValueOf(time.Hour)})
	gotTicker, ok := reflect.TypeAssert[*time.Ticker](ticker[0])
	require.True(t, ok)
	require.NotNil(t, gotTicker)
	gotTicker.Stop()
}

func TestClockOverrideSymbolsNilClockUsesWallClock(t *testing.T) {
	t.Parallel()
	symbols := clock.ClockOverrideSymbols(nil)
	before := time.Now()
	got, ok := reflect.TypeAssert[time.Time](symbols["Now"].Call(nil)[0])
	require.True(t, ok)
	require.False(t, got.Before(before))
	require.LessOrEqual(t, time.Since(got), time.Minute)
}
