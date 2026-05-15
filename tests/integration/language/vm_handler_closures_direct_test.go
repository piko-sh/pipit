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

func TestClosureWriteIntSliceCoercionReadsBackAsInt(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `
xs := []int{}
fill := func(n int) {
	ys := make([]int, n)
	for i := 0; i < n; i++ {
		ys[i] = i * i
	}
	xs = ys
}
fill(4)
var boxed any = xs
_, isInts := boxed.([]int)
isInts && len(xs) == 4 && xs[3] == 9 && cap(xs) >= 4`)
	require.NoError(t, err)
	require.Equal(t, true, result)
}

func TestClosureWriteScalarsAndStringsReadBackInParent(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `
count := 0
ratio := 0.0
flag := false
label := ""
update := func() {
	count = 41 + 1
	ratio = 1.5 * 2
	flag = !flag
	label = "a" + "b"
}
update()
count == 42 && ratio == 3.0 && flag && label == "ab"`)
	require.NoError(t, err)
	require.Equal(t, true, result)
}
