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

func TestCallPassesComparisonResultToBoolParameter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	result, err := app.NewService().Eval(ctx, `type C struct{ b bool }
func (c *C) SetB(v bool) { c.b = v }
type I interface{ SetB(v bool) }
func keep(v bool) bool { return v }
func run() int {
	c := &C{}
	var i I = c
	hits := 0
	for k := 0; k < 8; k++ {
		i.SetB(k%2 == 0)
		if c.b { hits++ }
		c.SetB(k%2 == 1)
		if c.b { hits += 10 }
		if keep(k > 5) { hits += 100 }
	}
	return hits
}
run()`)
	require.NoError(t, err)
	require.EqualValues(t, 4+40+200, result)
}
