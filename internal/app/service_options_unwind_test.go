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
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/policy"
)

func unwindLimitsOf(limits engine.VMLimits) engine.VMLimits {
	return engine.VMLimits{
		AbortUnwindCost:         limits.AbortUnwindCost,
		AbortUnwindWall:         limits.AbortUnwindWall,
		AbortUnwindDeferWallMax: limits.AbortUnwindDeferWallMax,
		AbortUnwindNativeGrace:  limits.AbortUnwindNativeGrace,
		GoroutineJoinUnwindCap:  limits.GoroutineJoinUnwindCap,
		LeakedUnwinderCap:       limits.LeakedUnwinderCap,
		LockWalkMaxValues:       limits.LockWalkMaxValues,
	}
}

func TestUnwindLimitsDefaultToTheEngineDefaults(t *testing.T) {
	t.Parallel()
	service := NewService()
	require.Equal(t, engine.VMLimits{}, unwindLimitsOf(service.limits), "an unset limit leaves the engine default in force")
	require.NoError(t, service.checkOpen())
}

func TestUnwindLimitOptions_PlumbedToLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		option Option
		want   engine.VMLimits
	}{
		{name: "cost", option: WithAbortUnwindCost(500), want: engine.VMLimits{AbortUnwindCost: 500}},
		{name: "wall", option: WithAbortUnwindWall(3 * time.Second), want: engine.VMLimits{AbortUnwindWall: 3 * time.Second}},
		{name: "defer wall", option: WithAbortUnwindDeferWallMax(time.Second), want: engine.VMLimits{AbortUnwindDeferWallMax: time.Second}},
		{name: "native grace", option: WithAbortUnwindNativeGrace(time.Minute), want: engine.VMLimits{AbortUnwindNativeGrace: time.Minute}},
		{name: "join cap", option: WithGoroutineJoinUnwindCap(time.Second), want: engine.VMLimits{GoroutineJoinUnwindCap: time.Second}},
		{name: "leak cap", option: WithLeakedUnwinderCap(3), want: engine.VMLimits{LeakedUnwinderCap: 3}},
		{name: "lock walk", option: WithLockWalkMaxValues(1000), want: engine.VMLimits{LockWalkMaxValues: 1000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(tt.option)
			require.NoError(t, service.checkOpen())
			require.Equal(t, tt.want, unwindLimitsOf(service.limits))
			require.Equal(t, tt.want, unwindLimitsOf(service.Clone().limits), "a clone keeps the limits")
		})
	}
}

func TestUnwindLimitOptions_RejectValuesThatAreNotPositive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		option Option
	}{
		{name: "WithAbortUnwindCost", option: WithAbortUnwindCost(0)},
		{name: "WithAbortUnwindWall", option: WithAbortUnwindWall(-time.Second)},
		{name: "WithAbortUnwindDeferWallMax", option: WithAbortUnwindDeferWallMax(0)},
		{name: "WithAbortUnwindNativeGrace", option: WithAbortUnwindNativeGrace(-1)},
		{name: "WithGoroutineJoinUnwindCap", option: WithGoroutineJoinUnwindCap(0)},
		{name: "WithLeakedUnwinderCap", option: WithLeakedUnwinderCap(-3)},
		{name: "WithLockWalkMaxValues", option: WithLockWalkMaxValues(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(tt.option)
			require.Equal(t, engine.VMLimits{}, unwindLimitsOf(service.limits), "the rejected value is not applied")

			_, err := service.Eval(t.Context(), "1 + 1")
			require.ErrorIs(t, err, ErrInvalidOption)
			require.ErrorIs(t, err, policy.ErrInvalidConfig)
			require.ErrorContains(t, err, tt.name)

			_, err = service.CompileFileSet(t.Context(), map[string]string{"main.go": "package main\n\nfunc Main() int { return 1 }\n"})
			require.ErrorIs(t, err, ErrInvalidOption)
			require.ErrorIs(t, service.Clone().checkOpen(), ErrInvalidOption, "a clone shares the rejection")
		})
	}
}

func TestUnwindLimitOptions_ReportEveryRejection(t *testing.T) {
	t.Parallel()
	service := NewService(WithAbortUnwindCost(0), WithLeakedUnwinderCap(2), WithLockWalkMaxValues(-1))
	err := service.checkOpen()
	require.ErrorIs(t, err, ErrInvalidOption)
	require.ErrorContains(t, err, "WithAbortUnwindCost(0)")
	require.ErrorContains(t, err, "WithLockWalkMaxValues(-1)")
	require.NotContains(t, err.Error(), "WithLeakedUnwinderCap")
	require.Equal(t, int64(2), service.limits.LeakedUnwinderCap, "a valid option still applies")
}

func TestClosedServiceReportsClosedBeforeAnInvalidOption(t *testing.T) {
	t.Parallel()
	service := NewService(WithAbortUnwindWall(0))
	require.NoError(t, service.Close())
	require.ErrorIs(t, service.checkOpen(), policy.ErrClosed)
}
