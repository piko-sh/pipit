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
//
// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package pipit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func TestIsolatedConstructorsRefuseAnEmptyConfiguration(t *testing.T) {
	t.Parallel()
	constructors := map[string]func(context.Context) error{
		"worker": func(ctx context.Context) error {
			_, err := pipit.NewIsolatedWorker(ctx, pipit.IsolatedConfig{})
			return err
		},
		"filesystem worker": func(ctx context.Context) error {
			_, err := pipit.NewIsolatedFilesystemWorker(ctx, pipit.IsolatedFilesystemConfig{})
			return err
		},
		"session": func(ctx context.Context) error {
			_, err := pipit.NewIsolatedSession(ctx, pipit.IsolatedConfig{})
			return err
		},
		"recovery": func(ctx context.Context) error {
			_, err := pipit.NewIsolatedRecovery(ctx, pipit.IsolatedConfig{})
			return err
		},
		"filesystem recovery": func(ctx context.Context) error {
			_, err := pipit.NewIsolatedFilesystemRecovery(ctx, pipit.IsolatedFilesystemConfig{})
			return err
		},
		"images": func(ctx context.Context) error {
			_, err := pipit.PrepareIsolatedImages(ctx, pipit.IsolatedConfig{})
			return err
		},
	}
	for name, construct := range constructors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := construct(t.Context())
			require.Error(t, err)
			if !errors.Is(err, pipit.ErrIsolatedUnavailable) {
				require.ErrorIs(t, err, pipit.ErrInvalidIsolatedConfig, "an empty configuration is invalid where isolation is available")
			}
		})
	}
}
