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

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

func TestNewVMWithoutCancelHasNoCancellationWatcher(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(fault.ErrMainReturned)

	tests := []struct {
		name        string
		ctx         context.Context
		wantWatcher bool
	}{
		{name: "cancelled context installs a watcher", ctx: ctx, wantWatcher: true},
		{name: "cancellation removed installs none", ctx: context.WithoutCancel(ctx), wantWatcher: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vm := NewVM(tc.ctx, NewGlobalStore(), symtab.NewSymbolRegistry(nil))
			require.Equal(t, tc.wantWatcher, vm.stopWatcher != nil)
			if tc.wantWatcher {
				require.Eventually(t, func() bool { return vm.Cancelled.Load() != 0 }, 2e9, 1e6, "the watcher flags a cancelled context")
				vm.FinishWatcher()
				return
			}
			require.Zero(t, vm.Cancelled.Load())
		})
	}
}
