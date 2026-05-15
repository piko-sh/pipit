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

func TestGoroutineClosureCacheNotTorn(t *testing.T) {
	t.Parallel()

	code := `results := make(chan int, 6)
for w := 0; w < 6; w++ {
	go func() {
		local := 0
		add := func() { local++ }
		for i := 0; i < 10000; i++ {
			add()
		}
		results <- local
	}()
}
total := 0
for i := 0; i < 6; i++ {
	total += <-results
}
total`
	result, err := app.NewService().Eval(context.Background(), code)
	require.NoError(t, err)
	require.EqualValues(t, 60000, result)
}
