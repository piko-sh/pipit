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

package sandboxhost

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanupErrorContract(t *testing.T) {
	t.Parallel()
	launch := ErrInvalidIsolatedConfig
	cleanup := errors.New("cleanup failed once")
	attempts := 0
	wrapped, _ := errors.AsType[*IsolatedCleanupError](cleanupOrFail(t.Context(), launch, func(ctx context.Context) error {
		_, bounded := ctx.Deadline()
		require.True(t, bounded || attempts > 0, "the first cleanup is bounded")
		attempts++
		if attempts < 3 {
			return cleanup
		}
		return nil
	}))
	err := error(wrapped)

	require.ErrorIs(t, err, launch, "cleanup error does not wrap both causes")
	require.ErrorIs(t, err, cleanup)
	var target *IsolatedCleanupError
	require.ErrorAs(t, err, &target, "not an *IsolatedCleanupError")

	require.Error(t, target.Retry(t.Context()), "the first retry fails")
	require.NoError(t, target.Retry(t.Context()), "the second retry releases")
}
