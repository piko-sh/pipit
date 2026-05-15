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

package main

import (
	"fmt"

	"github.com/stretchr/testify/assert"
)

type localT struct {
	failures []string
}

func (t *localT) Errorf(format string, args ...any) {
	t.failures = append(t.failures, fmt.Sprintf(format, args...))
}

func (t *localT) Helper() {}

func (t *localT) Name() string { return "pipit-testify-example" }

func main() {
	t := &localT{}

	assert.Equal(t, 42, 42, "answer matches")
	assert.NotEqual(t, 1, 2, "one is not two")

	assert.Contains(t, "the quick brown fox", "quick", "substring present")
	assert.NotContains(t, "abc", "z", "substring absent")
	assert.True(t, len("hello") == 5, "len works on strings")
	assert.False(t, 1 > 2, "ordering")

	assert.Greater(t, 10, 5, "10 > 5")
	assert.LessOrEqual(t, 7, 7, "7 <= 7")
	assert.InDelta(t, 3.14159, 3.14, 0.01, "pi approx")

	xs := []int{2, 4, 6, 8}
	assert.Len(t, xs, 4, "four elements")
	assert.ElementsMatch(t, xs, []int{8, 6, 4, 2}, "same multiset, any order")
	assert.Contains(t, xs, 6, "6 is in the slice")

	m := map[string]int{"go": 1, "rust": 2, "zig": 3}
	assert.Len(t, m, 3, "three keys")
	assert.Contains(t, m, "rust", "rust key present")

	var emptySlice []int
	assert.Empty(t, emptySlice, "nil slice is empty")
	assert.NotEmpty(t, xs, "populated slice is not empty")
	assert.NoError(t, error(nil), "nil error is no error")
	assert.Error(t, fmt.Errorf("boom"), "non-nil error qualifies")

	assert.Equal(t, "expected", "actual", "this should fail on purpose")

	fmt.Printf("ran assertions; %d failure(s) captured\n", len(t.failures))
	for i, f := range t.failures {
		fmt.Printf("--- failure [%d] ---%s\n", i+1, f)
	}
}
