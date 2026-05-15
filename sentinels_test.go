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
	"errors"
	"testing"

	"pipit.sh/pipit"
)

func TestTierSentinelsWrapTheBases(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		tier error
		base error
	}{
		{"isolated unavailable", pipit.ErrIsolatedUnavailable, pipit.ErrUnavailable},
		{"invalid isolated config", pipit.ErrInvalidIsolatedConfig, pipit.ErrInvalidConfig},
		{"isolated closed", pipit.ErrIsolatedClosed, pipit.ErrClosed},
		{"isolated used alias", pipit.ErrIsolatedUsed, pipit.ErrClosed},
		{"invalid restricted config", pipit.ErrInvalidRestrictedConfig, pipit.ErrInvalidConfig},
		{"restricted busy", pipit.ErrRestrictedBusy, pipit.ErrBusy},
		{"restricted limit", pipit.ErrRestrictedLimit, pipit.ErrLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(test.tier, test.base) {
				t.Fatalf("%v does not wrap the base sentinel", test.tier)
			}
		})
	}
}

func TestEvaluationErrorAsMatches(t *testing.T) {
	t.Parallel()
	err := error(&pipit.EvaluationError{Tier: "isolated", Message: "boom"})
	var target *pipit.EvaluationError
	if !errors.As(err, &target) || target.Tier != "isolated" || target.Message != "boom" {
		t.Fatalf("EvaluationError not matched by errors.As: %+v", target)
	}
}
