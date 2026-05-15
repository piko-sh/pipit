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
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"
)

func TestGlobalStoreInt(t *testing.T) {
	t.Parallel()
	g := engine.NewGlobalStore()
	idx0 := g.AllocInt(42)
	idx1 := g.AllocInt(99)
	require.Equal(t, 0, idx0)
	require.Equal(t, 1, idx1)
	require.Equal(t, int64(42), g.GetInt(0))
	require.Equal(t, int64(99), g.GetInt(1))
	g.SetInt(0, 100)
	require.Equal(t, int64(100), g.GetInt(0))
}

func TestGlobalStoreFloat(t *testing.T) {
	t.Parallel()
	g := engine.NewGlobalStore()
	index := g.AllocFloat(3.14)
	require.Equal(t, 0, index)
	require.InDelta(t, 3.14, g.GetFloat(0), 0.0001)
	g.SetFloat(0, 2.72)
	require.InDelta(t, 2.72, g.GetFloat(0), 0.0001)
}

func TestGlobalStoreString(t *testing.T) {
	t.Parallel()
	g := engine.NewGlobalStore()
	index := g.AllocString("hello")
	require.Equal(t, 0, index)
	require.Equal(t, "hello", g.GetString(0))
	g.SetString(0, "world")
	require.Equal(t, "world", g.GetString(0))
}

func TestGlobalStoreGeneral(t *testing.T) {
	t.Parallel()
	g := engine.NewGlobalStore()
	v := reflect.ValueOf([]int{1, 2, 3})
	index := g.AllocGeneral(v)
	require.Equal(t, 0, index)
	got := g.GetGeneral(0)
	require.Equal(t, 3, got.Len())
	g.SetGeneral(0, reflect.ValueOf("replaced"))
	require.Equal(t, "replaced", g.GetGeneral(0).Interface())
}

func TestGlobalStoreReset(t *testing.T) {
	t.Parallel()
	g := engine.NewGlobalStore()
	g.AllocInt(1)
	g.AllocFloat(1.0)
	g.AllocString("x")
	g.AllocGeneral(reflect.ValueOf(true))
	g.Reset()
	index := g.AllocInt(2)
	require.Equal(t, 0, index)
	require.Equal(t, int64(2), g.GetInt(0))
}

func TestGlobalStoreMultipleAllocs(t *testing.T) {
	t.Parallel()
	g := engine.NewGlobalStore()
	for i := range 10 {
		index := g.AllocInt(int64(i))
		require.Equal(t, i, index)
	}
	for i := range 10 {
		require.Equal(t, int64(i), g.GetInt(i))
	}
}

func TestGlobalStoreIsolationBetweenServices(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc1 := app.NewService()
	svc2 := app.NewService()

	_, err := svc1.Eval(ctx, `var x int = 42; x`)
	require.NoError(t, err)

	_, err = svc2.Eval(ctx, `x`)
	require.Error(t, err, "svc2 should not have access to svc1's global x")
}

func TestGlobalsPersistInStore(t *testing.T) {
	t.Parallel()

	g := engine.NewGlobalStore()

	index := g.AllocInt(10)
	require.Equal(t, int64(10), g.GetInt(index))

	g.SetInt(index, 15)
	require.Equal(t, int64(15), g.GetInt(index))

	idx2 := g.AllocString("hello")
	g.SetInt(index, 42)
	require.Equal(t, int64(42), g.GetInt(index))
	require.Equal(t, "hello", g.GetString(idx2))
}

func TestServiceResetClearsGlobals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := app.NewService()

	_, err := service.Eval(ctx, `var x int = 42; x`)
	require.NoError(t, err)

	service.Reset()

	_, err = service.Eval(ctx, `x`)
	require.Error(t, err, "global x should not exist after Reset()")
}

func TestServiceCloneIndependentGlobals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := app.NewService()

	result, err := service.Eval(ctx, "1 + 1")
	require.NoError(t, err)
	require.Equal(t, 2, result)

	cloned := service.Clone()

	r1, err := service.Eval(ctx, "10 + 20")
	require.NoError(t, err)
	require.Equal(t, 30, r1)

	r2, err := cloned.Eval(ctx, "30 + 40")
	require.NoError(t, err)
	require.Equal(t, 70, r2)
}

func TestGlobalStoreResetAndReuse(t *testing.T) {
	t.Parallel()

	g := engine.NewGlobalStore()

	g.AllocInt(1)
	g.AllocInt(2)
	g.AllocFloat(3.14)
	g.AllocString("hello")

	g.Reset()

	index := g.AllocInt(99)
	require.Equal(t, 0, index)
	require.Equal(t, int64(99), g.GetInt(0))

	fidx := g.AllocFloat(2.72)
	require.Equal(t, 0, fidx)
	require.InDelta(t, 2.72, g.GetFloat(0), 0.0001)
}

func TestGlobalStoreBoolAlloc(t *testing.T) {
	t.Parallel()

	g := engine.NewGlobalStore()
	idx0 := g.AllocBool(true)
	idx1 := g.AllocBool(false)
	require.Equal(t, 0, idx0)
	require.Equal(t, 1, idx1)
	require.True(t, g.GetBool(0))
	require.False(t, g.GetBool(1))
	g.SetBool(0, false)
	require.False(t, g.GetBool(0))
}

func TestGlobalStoreUintAlloc(t *testing.T) {
	t.Parallel()

	g := engine.NewGlobalStore()
	index := g.AllocUint(42)
	require.Equal(t, 0, index)
	require.Equal(t, uint64(42), g.GetUint(0))
	g.SetUint(0, 99)
	require.Equal(t, uint64(99), g.GetUint(0))
}

func TestMultipleServicesParallelEval(t *testing.T) {
	t.Parallel()

	const numServices = 10
	ctx := context.Background()

	services := make([]*app.Service, numServices)
	for i := range numServices {
		services[i] = app.NewService()
	}

	t.Run("parallel", func(t *testing.T) {
		for i := range numServices {
			t.Run("", func(t *testing.T) {
				t.Parallel()
				service := services[i]
				result, err := service.Eval(ctx, "1 + 2")
				require.NoError(t, err)
				require.Equal(t, 3, result)
			})
		}
	})
}
