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

package pipit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func TestTierSentinelsWrapTheBases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tier error
		base error
	}{
		{name: "isolated unavailable", tier: pipit.ErrIsolatedUnavailable, base: pipit.ErrUnavailable},
		{name: "invalid isolated config", tier: pipit.ErrInvalidIsolatedConfig, base: pipit.ErrInvalidConfig},
		{name: "isolated closed", tier: pipit.ErrIsolatedClosed, base: pipit.ErrClosed},
		{name: "isolated used alias", tier: pipit.ErrIsolatedUsed, base: pipit.ErrClosed},
		{name: "invalid restricted config", tier: pipit.ErrInvalidRestrictedConfig, base: pipit.ErrInvalidConfig},
		{name: "invalid option", tier: pipit.ErrInvalidOption, base: pipit.ErrInvalidConfig},
		{name: "restricted busy", tier: pipit.ErrRestrictedBusy, base: pipit.ErrBusy},
		{name: "restricted limit", tier: pipit.ErrRestrictedLimit, base: pipit.ErrLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, tt.tier, tt.base, "the tier sentinel wraps its base sentinel")
		})
	}
}

func TestEvaluationErrorAsMatches(t *testing.T) {
	t.Parallel()
	err := error(&pipit.EvaluationError{Tier: "isolated", Message: "boom"})
	var target *pipit.EvaluationError
	require.ErrorAs(t, err, &target)
	require.Equal(t, "isolated", target.Tier)
	require.Equal(t, "boom", target.Message)
}
