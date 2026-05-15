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

package language_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

func TestEvalStringByteConversions(t *testing.T) {
	t.Parallel()

	t.Run("bytes to string", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		result, err := service.Eval(context.Background(), `string([]byte{72, 105})`)
		require.NoError(t, err)
		require.Equal(t, "Hi", result)
	})

	t.Run("string to bytes len", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		result, err := service.Eval(context.Background(), `len([]byte("hello"))`)
		require.NoError(t, err)
		require.Equal(t, 5, result)
	})

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		result, err := service.Eval(context.Background(), `string([]byte("test"))`)
		require.NoError(t, err)
		require.Equal(t, "test", result)
	})
}

func TestEvalSliceToArrayConversion(t *testing.T) {
	t.Parallel()

	t.Run("byte slice to array", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		result, err := service.Eval(context.Background(),
			`s := []byte{1, 2, 3, 4}; a := [4]byte(s); int(a[2])`)
		require.NoError(t, err)
		require.Equal(t, 3, result)
	})

	t.Run("int slice to array", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		result, err := service.Eval(context.Background(),
			`s := []int{10, 20, 30}; a := [3]int(s); a[1]`)
		require.NoError(t, err)
		require.Equal(t, 20, result)
	})

	t.Run("partial slice to shorter array", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		result, err := service.Eval(context.Background(),
			`s := []int{10, 20, 30, 40}; a := [2]int(s); a[0] + a[1]`)
		require.NoError(t, err)
		require.Equal(t, 30, result)
	})

	t.Run("string slice to array", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		result, err := service.Eval(context.Background(),
			`s := []string{"a", "b", "c"}; a := [3]string(s); a[0] + a[1] + a[2]`)
		require.NoError(t, err)
		require.Equal(t, "abc", result)
	})
}
