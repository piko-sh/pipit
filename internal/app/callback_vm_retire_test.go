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

//go:build !safe

package app

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

func TestClosureWrapperRetiresArenaPastThreshold(t *testing.T) {
	t.Parallel()
	const calls = 5
	var results []string
	var stats []engine.CallbackVMStats
	var harness *callbackHarness
	harness = newCallbackHarness(t, map[string]reflect.Value{
		"Walk": reflect.ValueOf(func(fn func(int) string) int {
			for index := range calls {
				results = append(results, fn(index))
				stats = append(stats, harness.vm.CallbackVMStats())
			}
			return len(results)
		}),
	})
	harness.vm.Limits.CallbackArenaRetireBytes = 1
	result := harness.run(t, `import "host"
host.Walk(func(index int) string { return "abc" + string(rune('0'+index)) })`)
	require.EqualValues(t, calls, result)
	for index, stat := range stats {
		require.Equal(t, engine.CallbackVMStats{Built: int64(index + 1), Discarded: int64(index + 1), Idle: false}, stat, "callback %d built a VM and retired it", index)
	}
	for index, got := range results {
		require.Equal(t, "abc"+string(rune('0'+index)), got, "result %d survives the retire of the VM that built it", index)
	}
}

func TestClosureWrapperStringsSurviveRetireMidWalk(t *testing.T) {
	t.Parallel()
	const walkLength = 12
	var results []string
	var harness *callbackHarness
	harness = newCallbackHarness(t, map[string]reflect.Value{
		"Walk": reflect.ValueOf(func(fn func(int) string) int {
			for index := range walkLength {
				results = append(results, fn(index))
				if index == walkLength/2 {
					harness.vm.Limits.CallbackArenaRetireBytes = 1
				}
			}
			return len(results)
		}),
	})

	result := harness.run(t, `import "host"
prefix := "value-"
host.Walk(func(index int) string { return prefix + string(rune('a'+index)) })
host.Walk(func(index int) string { return prefix + string(rune('A'+index)) })`)
	require.EqualValues(t, 2*walkLength, result, "the second walk sees every result so far")
	require.Len(t, results, 2*walkLength)
	for index := range walkLength {
		require.Equal(t, "value-"+string(rune('a'+index)), results[index], "first walk result %d intact after the retire", index)
		require.Equal(t, "value-"+string(rune('A'+index)), results[walkLength+index], "second walk result %d", index)
	}
	stats := harness.vm.CallbackVMStats()
	require.Equal(t, stats.Built, stats.Discarded, "every callback VM was discarded exactly once")
	require.Greater(t, stats.Built, int64(1), "the lowered threshold retired VMs during the second walk")
}
