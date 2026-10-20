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

package fault_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/fault"
)

func TestJoinProblems(t *testing.T) {
	t.Parallel()

	first := fmt.Errorf("%w: first", fault.ErrTypeCheck)
	second := fmt.Errorf("%w: second", fault.ErrCompileEmbedUnsupported)
	third := errors.New("third")

	require.NoError(t, fault.JoinProblems(nil))
	require.NoError(t, fault.JoinProblems([]error{nil, nil}))
	require.Same(t, first, fault.JoinProblems([]error{nil, first}), "a single problem keeps its shape")

	joined := fault.JoinProblems([]error{first, nil, second})
	var list *fault.ProblemList
	require.ErrorAs(t, joined, &list)
	require.Equal(t, []error{first, second}, list.Problems)
	require.ErrorIs(t, joined, fault.ErrTypeCheck)
	require.ErrorIs(t, joined, fault.ErrCompileEmbedUnsupported)
	require.Equal(t, first.Error()+"\n\n"+second.Error(), joined.Error(), "the first line is the first problem")

	flattened := fault.JoinProblems([]error{joined, third})
	require.ErrorAs(t, flattened, &list)
	require.Equal(t, []error{first, second, third}, list.Problems, "nested lists are flattened")
}

func TestProblems(t *testing.T) {
	t.Parallel()

	require.Nil(t, fault.Problems(nil))

	single := errors.New("single")
	require.Equal(t, []error{single}, fault.Problems(single))

	first, second := errors.New("first"), errors.New("second")
	wrapped := fmt.Errorf("compiling program: %w", fault.JoinProblems([]error{first, second}))
	require.Equal(t, []error{first, second}, fault.Problems(wrapped))
}
