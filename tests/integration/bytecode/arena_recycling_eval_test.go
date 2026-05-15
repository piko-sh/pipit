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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"
)

const (
	arenaRecyclingChurn = `
type P struct {
	A int
	B int
}
func churn() int {
	keep := make([]P, 256)
	for i := 0; i < 256; i++ {
		keep[i] = P{A: i, B: i * 2}
	}
	narrow := make([]int32, 64)
	for i := 0; i < 64; i++ {
		narrow[i] = int32(i)
	}
	p := &P{A: 7, B: 8}
	sink := 0
	for j := 0; j < 400000; j++ {
		tmp := make([]P, 64)
		tmp[0].A = j
		sink += tmp[0].A - j
	}
	sum := 0
	for i := 0; i < 256; i++ {
		sum += keep[i].A + keep[i].B
	}
	for i := 0; i < 64; i++ {
		sum += int(narrow[i])
	}
	return sum + p.A + p.B + sink
}
churn()`
)

func TestArenaRecyclingKeepsReachableValuesAcrossMinorGC(t *testing.T) {
	t.Parallel()
	require.True(t, engine.ArenaChunkRecyclingEnabled)
	service := newTestService(t)
	for cycle := range 3 {
		result, err := service.Eval(context.Background(), arenaRecyclingChurn)
		require.NoError(t, err)
		require.Equalf(t, int(97920+2016+15), result, "cycle %d", cycle)
	}
}
