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
)

func TestCleanupErrorContract(t *testing.T) {
	t.Parallel()
	launch := ErrInvalidIsolatedConfig
	cleanup := errors.New("cleanup failed once")
	attempts := 0
	wrapped, _ := errors.AsType[*IsolatedCleanupError](cleanupOrFail(launch, func() error {
		attempts++
		if attempts < 3 {
			return cleanup
		}
		return nil
	}))
	err := error(wrapped)

	if !errors.Is(err, launch) || !errors.Is(err, cleanup) {
		t.Fatalf("cleanup error does not wrap both causes: %v", err)
	}
	var target *IsolatedCleanupError
	if !errors.As(err, &target) {
		t.Fatal("not an *IsolatedCleanupError")
	}

	if retryErr := target.Retry(context.Background()); retryErr == nil {
		t.Fatal("first retry unexpectedly succeeded")
	}
	if retryErr := target.Retry(context.Background()); retryErr != nil {
		t.Fatal("second retry did not release:", retryErr)
	}
}
